package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"whatsapptool/internal/automation"
	"whatsapptool/internal/db"
	mw "whatsapptool/internal/web/middleware"
	"whatsapptool/internal/web/templates"
	"whatsapptool/internal/web/ws"
	"whatsapptool/internal/whatsapp"
)

// InboxHandler wires the shared inbox routes.
type InboxHandler struct {
	pool     *pgxpool.Pool
	waClient *whatsapp.Client
	hub      *ws.Hub
}

func NewInboxHandler(pool *pgxpool.Pool, waClient *whatsapp.Client, hub *ws.Hub) *InboxHandler {
	return &InboxHandler{pool: pool, waClient: waClient, hub: hub}
}

// Mount registers all inbox routes on the given router.
func (h *InboxHandler) Mount(r chi.Router) {
	r.Get("/", h.Page)
	r.Get("/convs", h.ListConversations)
	r.Get("/convs/{id}", h.GetConversation)
	r.Get("/convs/{id}/messages", h.GetMessages)
	r.Post("/convs/{id}/messages", h.PostMessage)
}

// ── Page ──────────────────────────────────────────────────────────────────

func (h *InboxHandler) Page(w http.ResponseWriter, r *http.Request) {
	agent := mw.AgentFromCtx(r.Context())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.InboxPage(agent).Render(r.Context(), w); err != nil {
		log.Printf("inbox page render: %v", err)
	}
}

// ── Conversations list ────────────────────────────────────────────────────

func (h *InboxHandler) ListConversations(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := db.ListFilter{Status: q.Get("status")}

	switch q.Get("assigned") {
	case "unassigned":
		empty := ""
		filter.AssignedTo = &empty
	}

	convs, err := db.ListConversations(r.Context(), h.pool, filter)
	if err != nil {
		log.Printf("list conversations: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ConversationList(convs).Render(r.Context(), w); err != nil {
		log.Printf("conv list render: %v", err)
	}
}

// ── Conversation detail ───────────────────────────────────────────────────

func (h *InboxHandler) GetConversation(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	conv, err := db.GetConversation(r.Context(), h.pool, id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(conv)
}

// ── Message thread ────────────────────────────────────────────────────────

func (h *InboxHandler) GetMessages(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	conv, err := db.GetConversation(r.Context(), h.pool, id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	msgs, err := db.ListMessages(r.Context(), h.pool, id, 100, offset)
	if err != nil {
		log.Printf("list messages: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	contactName := ""
	contactSub := ""
	var cName, cPhone, cRole, cCompany string
	err = h.pool.QueryRow(r.Context(),
		`SELECT COALESCE(name,''), COALESCE(wa_phone,''),
		        COALESCE(custom_fields->>'role',''),
		        COALESCE(custom_fields->>'company','')
		 FROM contacts WHERE id = $1::uuid`,
		conv.ContactID,
	).Scan(&cName, &cPhone, &cRole, &cCompany)
	if err != nil {
		log.Printf("contact lookup for %s: %v", conv.ContactID, err)
	}
	contactName = cName
	if contactName == "" {
		contactName = cPhone
	}
	var parts []string
	if cRole != "" {
		parts = append(parts, cRole)
	}
	if cCompany != "" {
		parts = append(parts, "at "+cCompany)
	}
	contactSub = strings.Join(parts, " ")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.MessageThread(conv, msgs, contactName, contactSub).Render(r.Context(), w); err != nil {
		log.Printf("message thread render: %v", err)
	}
}

// ── Post reply ────────────────────────────────────────────────────────────

type postMessageRequest struct {
	Type       string `json:"type"`        // "text" | "template"
	Body       string `json:"body"`        // for type=text
	TemplateID string `json:"template_id"` // for type=template (Phase 3)
}

func (h *InboxHandler) PostMessage(w http.ResponseWriter, r *http.Request) {
	convID := chi.URLParam(r, "id")

	var req postMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	conv, err := db.GetConversation(r.Context(), h.pool, convID)
	if err != nil {
		http.Error(w, "conversation not found", http.StatusNotFound)
		return
	}

	// Look up the contact's E.164 phone number for the Meta API call.
	contact, err := db.GetContact(r.Context(), h.pool, conv.ContactID)
	if err != nil {
		http.Error(w, "contact not found", http.StatusNotFound)
		return
	}

	// Enforce 24h window: only template sends are allowed outside the window.
	if req.Type == "text" && !whatsapp.IsWindowOpen(conv.LastInboundAt) {
		http.Error(w,
			`{"error":"24h window closed — use a template message"}`,
			http.StatusUnprocessableEntity,
		)
		return
	}

	category := "service"
	msg := &db.Message{
		ConversationID: convID,
		Direction:      "outbound",
		MessageType:    "text",
		Content:        map[string]any{"body": req.Body},
		Status:         "pending",
		Category:       &category,
		// TODO: set SentBy from JWT session (Phase 6)
	}
	if err := db.InsertMessage(r.Context(), h.pool, msg); err != nil {
		log.Printf("insert outbound message: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	_ = db.TouchLastMessage(r.Context(), h.pool, convID, msg.CreatedAt)

	// Attempt Meta send
	var waID string
	var sendErr error
	switch req.Type {
	case "text":
		waID, sendErr = h.waClient.SendText(r.Context(), contact.WAPhone, req.Body)
	default:
		sendErr = fmt.Errorf("unsupported message type %q (template sends coming in Phase 3)", req.Type)
	}

	if sendErr != nil {
		errMsg := sendErr.Error()
		_ = db.UpdateMessageStatus(r.Context(), h.pool, msg.ID, "failed", nil, &errMsg)
		log.Printf("send to %s: %v", conv.ContactID, sendErr)
		http.Error(w,
			fmt.Sprintf(`{"error":"send failed: %s"}`, errMsg),
			http.StatusBadGateway,
		)
		return
	}

	if err := db.SetMessageWAID(r.Context(), h.pool, msg.ID, waID); err != nil {
		log.Printf("set wa_message_id: %v", err)
	}
	msg.WAMessageID = &waID

	h.hub.BroadcastConversation(convID, ws.Event{
		Type:           ws.EventNewMessage,
		ConversationID: convID,
		Data:           msg,
	})

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.MessageBubble(msg).Render(r.Context(), w); err != nil {
		log.Printf("message bubble render: %v", err)
	}
}

// ── Inbound event handler (called from webhook handler) ──────────────────

// HandleInbound processes a genuinely new inbound WhatsApp message.
func (h *InboxHandler) HandleInbound(ctx context.Context, msg whatsapp.InboundMessage) {
	contactID, err := h.ensureContact(ctx, msg.From)
	if err != nil {
		log.Printf("ensure contact %s: %v", msg.From, err)
		return
	}

	conv, err := db.GetOrCreateByContact(ctx, h.pool, contactID)
	if err != nil {
		log.Printf("get/create conversation for %s: %v", msg.From, err)
		return
	}
	isNewContact := conv.LastMessageAt == nil // true when no prior messages

	now := time.Now().UTC()
	if err := db.SetLastInbound(ctx, h.pool, conv.ID, now); err != nil {
		log.Printf("set last inbound: %v", err)
	}

	content := map[string]any{}
	if msg.Text != nil {
		content["body"] = msg.Text.Body
	}

	dbMsg := &db.Message{
		ConversationID: conv.ID,
		Direction:      "inbound",
		MessageType:    msg.Type,
		Content:        content,
		Status:         "delivered",
		Category:       strPtr("service"),
	}
	if msg.ID != "" {
		dbMsg.WAMessageID = &msg.ID
	}

	if err := db.InsertMessage(ctx, h.pool, dbMsg); err != nil {
		log.Printf("insert inbound message: %v", err)
		return
	}

	// STOP/UNSUBSCRIBE: opt out instantly and send confirmation.
	if msg.Text != nil && automation.IsStopKeyword(msg.Text.Body) {
		phone := msg.From
		if !strings.HasPrefix(phone, "+") {
			phone = "+" + phone
		}
		go func() {
			if err := automation.HandleStop(context.Background(), h.pool, h.waClient, phone); err != nil {
				log.Printf("handle stop %s: %v", phone, err)
			}
		}()
	} else {
		// Run other automation rules (keyword, welcome, away).
		go func() {
			if _, err := automation.RunRules(context.Background(), h.pool, h.waClient, msg, contactID, conv.ID, isNewContact); err != nil {
				log.Printf("automation rules for %s: %v", msg.From, err)
			}
		}()
	}

	// Download media immediately — Meta URLs expire in ~5 minutes.
	if mediaID := msg.MediaID(); mediaID != "" {
		go h.downloadMedia(dbMsg.ID, mediaID)
	}

	// Round-robin assign if still unassigned
	if conv.AssignedTo == nil {
		if _, err := db.AssignRoundRobin(ctx, h.pool, conv.ID); err != nil {
			log.Printf("round-robin assign: %v", err)
		}
	}

	h.hub.BroadcastConversation(conv.ID, ws.Event{
		Type:           ws.EventNewMessage,
		ConversationID: conv.ID,
		Data:           dbMsg,
	})
}

// HandleStatusUpdate applies a delivery/read/failed status.
func (h *InboxHandler) HandleStatusUpdate(ctx context.Context, status whatsapp.MessageStatus) {
	var errCode, errMsg *string
	if len(status.Errors) > 0 {
		code := strconv.Itoa(status.Errors[0].Code)
		title := status.Errors[0].Title
		errCode, errMsg = &code, &title
	}
	if err := db.UpdateMessageStatus(ctx, h.pool, status.ID, status.Status, errCode, errMsg); err != nil {
		log.Printf("update message status %s→%s: %v", status.ID, status.Status, err)
	}
	h.hub.BroadcastAll(ws.Event{
		Type: ws.EventMessageStatus,
		Data: map[string]string{"wa_message_id": status.ID, "status": status.Status},
	})
}

// ── helpers ───────────────────────────────────────────────────────────────

func (h *InboxHandler) ensureContact(ctx context.Context, phone string) (string, error) {
	if !strings.HasPrefix(phone, "+") {
		phone = "+" + phone
	}
	var id string
	err := h.pool.QueryRow(ctx, `
		INSERT INTO contacts (wa_phone, opted_in, opt_in_source, opt_in_at)
		VALUES ($1, FALSE, 'inbound_message', NOW())
		ON CONFLICT (wa_phone) DO UPDATE SET updated_at = NOW()
		RETURNING id::text
	`, phone).Scan(&id)
	return id, err
}

func (h *InboxHandler) downloadMedia(messageID, mediaID string) {
	ctx := context.Background()
	url, _, err := h.waClient.GetMediaURL(ctx, mediaID)
	if err != nil {
		log.Printf("get media url %s: %v", mediaID, err)
		return
	}
	data, err := h.waClient.DownloadMedia(ctx, url)
	if err != nil {
		log.Printf("download media %s: %v", mediaID, err)
		return
	}
	dir := filepath.Join("media", mediaID[:2])
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("mkdir %s: %v", dir, err)
		return
	}
	path := filepath.Join(dir, mediaID)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Printf("write media %s: %v", path, err)
		return
	}
	if err := db.SetMediaPath(ctx, h.pool, messageID, path); err != nil {
		log.Printf("set media path: %v", err)
	}
}

func strPtr(s string) *string { return &s }
