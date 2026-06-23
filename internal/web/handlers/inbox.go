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
	r.Post("/convs/{id}/resolve", h.ResolveConversation)
	r.Get("/convs/{id}/template-picker", h.TemplatePicker)
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
	case "me":
		if agent := mw.AgentFromCtx(r.Context()); agent != nil {
			id := agent.ID
			filter.AssignedTo = &id
		}
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

	var cName, cPhone string
	var cOptedIn bool
	var cCreatedAt time.Time
	err = h.pool.QueryRow(r.Context(),
		`SELECT COALESCE(name,''), COALESCE(wa_phone,''), opted_in, created_at
		 FROM contacts WHERE id = $1::uuid`,
		conv.ContactID,
	).Scan(&cName, &cPhone, &cOptedIn, &cCreatedAt)
	if err != nil {
		log.Printf("contact lookup for %s: %v", conv.ContactID, err)
	}

	contactName := cName
	if contactName == "" {
		contactName = cPhone
	}
	if contactName == "" {
		contactName = "Unknown contact"
	}

	assignedName := ""
	if conv.AssignedTo != nil && *conv.AssignedTo != "" {
		_ = h.pool.QueryRow(r.Context(),
			`SELECT name FROM agents WHERE id = $1::uuid`, *conv.AssignedTo,
		).Scan(&assignedName)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.MessageThread(conv, msgs, contactName, cPhone).Render(r.Context(), w); err != nil {
		log.Printf("message thread render: %v", err)
	}
	if err := templates.ContactPanel(conv.ContactID, contactName, cPhone, cOptedIn, cCreatedAt, assignedName).Render(r.Context(), w); err != nil {
		log.Printf("contact panel render: %v", err)
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

	// Attribute the send to the current agent, and enforce their monthly cap.
	var sentBy *string
	if agent := mw.AgentFromCtx(r.Context()); agent != nil {
		id := agent.ID
		sentBy = &id
		if lim, _ := db.GetAgentLimits(r.Context(), h.pool, id); lim != nil && lim.MonthlyMsgCap > 0 {
			used, _ := db.AgentMessagesThisMonth(r.Context(), h.pool, id)
			if used >= int64(lim.MonthlyMsgCap) {
				http.Error(w, `{"error":"Monthly message limit reached — ask your admin to raise it."}`, http.StatusUnprocessableEntity)
				return
			}
		}
	}

	// Build the outbound message + Meta send for the chosen type.
	var (
		msg        *db.Message
		waID       string
		sendErr    error
		templateNm string
	)

	if req.Type == "template" {
		tmpl, terr := db.GetTemplate(r.Context(), h.pool, req.TemplateID)
		if terr != nil {
			http.Error(w, `{"error":"template not found"}`, http.StatusNotFound)
			return
		}
		templateNm = tmpl.Name
		params := templateBodyParams(tmpl)
		preview := substituteVars(templateBodyText(tmpl), params)
		msg = &db.Message{
			ConversationID: convID,
			Direction:      "outbound",
			MessageType:    "template",
			Content:        map[string]any{"body": preview, "template": tmpl.Name},
			Status:         "pending",
			Category:       &tmpl.Category,
			TemplateID:     &tmpl.ID,
			SentBy:         sentBy,
		}
		if err := db.InsertMessage(r.Context(), h.pool, msg); err != nil {
			log.Printf("insert outbound template: %v", err)
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		_ = db.TouchLastMessage(r.Context(), h.pool, convID, msg.CreatedAt)
		waID, sendErr = h.waClient.SendTemplate(r.Context(), contact.WAPhone, tmpl.Name, tmpl.Language, inboxTemplateComponents(params))
	} else {
		// Free-form text: only allowed inside the 24h window.
		if !whatsapp.IsWindowOpen(conv.LastInboundAt) {
			http.Error(w, `{"error":"24h window closed — send an approved template instead"}`, http.StatusUnprocessableEntity)
			return
		}
		if strings.TrimSpace(req.Body) == "" {
			http.Error(w, `{"error":"empty message"}`, http.StatusUnprocessableEntity)
			return
		}
		category := "service"
		msg = &db.Message{
			ConversationID: convID,
			Direction:      "outbound",
			MessageType:    "text",
			Content:        map[string]any{"body": req.Body},
			Status:         "pending",
			Category:       &category,
			SentBy:         sentBy,
		}
		if err := db.InsertMessage(r.Context(), h.pool, msg); err != nil {
			log.Printf("insert outbound message: %v", err)
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		_ = db.TouchLastMessage(r.Context(), h.pool, convID, msg.CreatedAt)
		waID, sendErr = h.waClient.SendText(r.Context(), contact.WAPhone, req.Body)
	}
	_ = templateNm

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
	if err := templates.MessageBubble(msg, "", "").Render(r.Context(), w); err != nil {
		log.Printf("message bubble render: %v", err)
	}
}

// ── Resolve / close conversation ─────────────────────────────────────────

func (h *InboxHandler) ResolveConversation(w http.ResponseWriter, r *http.Request) {
	convID := chi.URLParam(r, "id")
	if err := db.SetStatus(r.Context(), h.pool, convID, "closed"); err != nil {
		log.Printf("resolve conversation %s: %v", convID, err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Replace the thread with an empty "resolved" state; the button also
	// reloads the conversation list (client-side) so it drops off the Open tab.
	fmt.Fprint(w, `<div class="th-wrap" id="thread"><div class="th-empty"><p>&#10003; Conversation resolved</p></div></div>`)
}

// ── Template picker (for sending outside the 24h window) ──────────────────

func (h *InboxHandler) TemplatePicker(w http.ResponseWriter, r *http.Request) {
	convID := chi.URLParam(r, "id")
	all, err := db.ListTemplates(r.Context(), h.pool)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<div class="tpick-overlay" onclick="if(event.target===this)this.remove()">`+
		`<div class="tpick-modal"><div class="tpick-hd"><strong>Send a template</strong>`+
		`<button class="tpick-close" onclick="this.closest('.tpick-overlay').remove()">&times;</button></div>`+
		`<div class="tpick-sub">Outside the 24-hour window, only approved templates can be sent.</div>`+
		`<div class="tpick-list">`)
	count := 0
	for _, t := range all {
		if t.Status != "approved" || templateHasMediaHeader(&t) {
			continue // only approved, text-only templates are sendable from here
		}
		count++
		preview := substituteVars(templateBodyText(&t), templateBodyParams(&t))
		if len(preview) > 120 {
			preview = preview[:120] + "…"
		}
		fmt.Fprintf(w,
			`<button class="tpick-item" type="button" onclick="sendTemplate('%s','%s');this.closest('.tpick-overlay').remove()">`+
				`<div class="tpick-name">%s</div><div class="tpick-body">%s</div></button>`,
			convID, t.ID, htmlEscape(t.Name), htmlEscape(preview))
	}
	if count == 0 {
		fmt.Fprint(w, `<div class="tpick-empty">No approved text templates available. Create &amp; get one approved under Templates.</div>`)
	}
	fmt.Fprint(w, `</div></div></div>`)
}

// ── Template helpers ──────────────────────────────────────────────────────

// templateBodyText returns the raw BODY text (with {{N}} placeholders).
func templateBodyText(t *db.Template) string {
	for _, c := range t.Components {
		if strings.EqualFold(toStr(c["type"]), "BODY") {
			return toStr(c["text"])
		}
	}
	return ""
}

// templateBodyParams returns the stored example values for the BODY variables,
// in order — used as the parameters when sending the template.
func templateBodyParams(t *db.Template) []string {
	for _, c := range t.Components {
		if !strings.EqualFold(toStr(c["type"]), "BODY") {
			continue
		}
		ex, ok := c["example"].(map[string]any)
		if !ok {
			return nil
		}
		bt, ok := ex["body_text"].([]any)
		if !ok || len(bt) == 0 {
			return nil
		}
		row, ok := bt[0].([]any)
		if !ok {
			return nil
		}
		var out []string
		for _, v := range row {
			out = append(out, toStr(v))
		}
		return out
	}
	return nil
}

// templateHasMediaHeader reports whether the template uses an image/video/doc
// header (those need a media parameter we don't collect in the inbox yet).
func templateHasMediaHeader(t *db.Template) bool {
	for _, c := range t.Components {
		if strings.EqualFold(toStr(c["type"]), "HEADER") {
			switch strings.ToUpper(toStr(c["format"])) {
			case "IMAGE", "VIDEO", "DOCUMENT":
				return true
			}
		}
	}
	return false
}

// inboxTemplateComponents builds the body-parameter components for SendTemplate.
func inboxTemplateComponents(params []string) []whatsapp.TemplateComponent {
	if len(params) == 0 {
		return nil
	}
	ps := make([]whatsapp.TemplateParameter, len(params))
	for i, p := range params {
		ps[i] = whatsapp.TemplateParameter{Type: "text", Text: p}
	}
	return []whatsapp.TemplateComponent{{Type: "body", Parameters: ps}}
}

// substituteVars replaces {{1}},{{2}}… in text with the given params (for display).
func substituteVars(text string, params []string) string {
	for i, p := range params {
		text = strings.ReplaceAll(text, fmt.Sprintf("{{%d}}", i+1), p)
	}
	return text
}

func toStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
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
	// Preserve inbound media metadata so the inbox shows the caption and the
	// downloaded file keeps its original name / content-type. Without this the
	// caption text and document filename were silently dropped.
	var mediaMeta *whatsapp.MediaContent
	switch {
	case msg.Image != nil:
		mediaMeta = msg.Image
	case msg.Video != nil:
		mediaMeta = msg.Video
	case msg.Audio != nil:
		mediaMeta = msg.Audio
	case msg.Sticker != nil:
		mediaMeta = msg.Sticker
	case msg.Document != nil:
		mediaMeta = &msg.Document.MediaContent
		if msg.Document.Filename != "" {
			content["filename"] = msg.Document.Filename
		}
	}
	if mediaMeta != nil {
		if mediaMeta.Caption != "" {
			content["caption"] = mediaMeta.Caption
		}
		if mediaMeta.MimeType != "" {
			content["mime_type"] = mediaMeta.MimeType
		}
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

	// STOP/UNSUBSCRIBE: opt-out write is SYNCHRONOUS. context.WithoutCancel ensures
	// a Meta disconnect cannot abort the write; the 5s timeout prevents a stalled
	// DB from hanging the webhook worker.
	if msg.Text != nil && automation.MatchesStopKeyword(msg.Text.Body, db.GetStopKeywords(ctx, h.pool)) {
		phone := msg.From
		if !strings.HasPrefix(phone, "+") {
			phone = "+" + phone
		}
		optCtx, optCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		if err := db.OptOut(optCtx, h.pool, phone); err != nil {
			log.Printf("OPT-OUT WRITE FAILED phone=%s: %v", phone, err)
		}
		optCancel()
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("STOP CONFIRMATION PANIC phone=%s: %v", phone, r)
				}
			}()
			// Retry the confirmation on transient send failures so the mandated
			// opt-out acknowledgement isn't silently dropped on a single blip.
			const attempts = 3
			for attempt := 1; ; attempt++ {
				_, err := h.waClient.SendText(context.Background(), phone, automation.StopConfirmMessage)
				if err == nil {
					return
				}
				if whatsapp.IsPermanent(err) || attempt >= attempts {
					log.Printf("stop confirm send %s failed after %d attempt(s): %v", phone, attempt, err)
					return
				}
				log.Printf("stop confirm send %s attempt %d/%d: %v — retrying", phone, attempt, attempts, err)
				time.Sleep(time.Duration(attempt) * 2 * time.Second)
			}
		}()
	} else {
		// Run other automation rules (keyword, welcome, away).
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("RunRules panic: %v", r)
				}
			}()
			if _, err := automation.RunRules(context.Background(), h.pool, h.waClient, msg, contactID, conv.ID, isNewContact); err != nil {
				log.Printf("automation rules for %s: %v", msg.From, err)
			}
		}()
	}

	// Download media immediately — Meta URLs expire in ~5 minutes.
	if mediaID := msg.MediaID(); mediaID != "" {
		origName, mime := "", ""
		if msg.Document != nil {
			origName = msg.Document.Filename
		}
		if mediaMeta != nil {
			mime = mediaMeta.MimeType
		}
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("downloadMedia panic: %v", r)
				}
			}()
			h.downloadMedia(dbMsg.ID, mediaID, origName, mime)
		}()
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
	// If this message belongs to a campaign, roll the new delivery status (and
	// any failure reason) up into the campaign's counters and recipient rows.
	if cid, ok := db.GetMessageCampaignID(ctx, h.pool, status.ID); ok {
		if err := db.SyncCampaignStats(ctx, h.pool, cid); err != nil {
			log.Printf("sync campaign stats %s: %v", cid, err)
		}
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

// downloadMedia fetches an inbound media file from Meta and stores it locally.
// Meta media URLs expire in ~5 minutes and the resolution URL is single-shot, so
// a transient failure means permanent data loss — we retry with backoff and
// re-resolve the URL each attempt (the previous URL may have expired).
func (h *InboxHandler) downloadMedia(messageID, mediaID, origName, mime string) {
	ctx := context.Background()
	const attempts = 3
	var data []byte
	for attempt := 1; ; attempt++ {
		url, _, err := h.waClient.GetMediaURL(ctx, mediaID)
		if err == nil {
			data, err = h.waClient.DownloadMedia(ctx, url)
		}
		if err == nil {
			break
		}
		if attempt >= attempts {
			log.Printf("MEDIA DOWNLOAD FAILED permanently mediaID=%s msg=%s after %d attempts: %v",
				mediaID, messageID, attempts, err)
			return
		}
		log.Printf("download media %s attempt %d/%d: %v — retrying", mediaID, attempt, attempts, err)
		time.Sleep(time.Duration(attempt) * 2 * time.Second)
	}
	dir := filepath.Join("media", mediaID[:2])
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("mkdir %s: %v", dir, err)
		return
	}
	path := filepath.Join(dir, mediaID+mediaExt(origName, mime))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Printf("write media %s: %v", path, err)
		return
	}
	if err := db.SetMediaPath(ctx, h.pool, messageID, path); err != nil {
		log.Printf("set media path: %v", err)
	}
}

// mediaExt picks a file extension from the original document name, falling back
// to the MIME type, so stored media keeps a sensible extension for serving.
func mediaExt(origName, mime string) string {
	if origName != "" {
		if e := filepath.Ext(origName); e != "" {
			return e
		}
	}
	switch {
	case strings.Contains(mime, "jpeg"), strings.Contains(mime, "jpg"):
		return ".jpg"
	case strings.Contains(mime, "png"):
		return ".png"
	case strings.Contains(mime, "gif"):
		return ".gif"
	case strings.Contains(mime, "webp"):
		return ".webp"
	case strings.Contains(mime, "mp4"):
		return ".mp4"
	case strings.Contains(mime, "3gpp"):
		return ".3gp"
	case strings.Contains(mime, "ogg"):
		return ".ogg"
	case strings.Contains(mime, "mpeg"):
		return ".mp3"
	case strings.Contains(mime, "amr"):
		return ".amr"
	case strings.Contains(mime, "pdf"):
		return ".pdf"
	}
	return ""
}

func strPtr(s string) *string { return &s }
