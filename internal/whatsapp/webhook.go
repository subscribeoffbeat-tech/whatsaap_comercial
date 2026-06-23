package whatsapp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// ── Meta webhook payload types ────────────────────────────────────────────

type WebhookPayload struct {
	Object string  `json:"object"`
	Entry  []Entry `json:"entry"`
}

type Entry struct {
	ID      string   `json:"id"`
	Changes []Change `json:"changes"`
}

type Change struct {
	Value ChangeValue `json:"value"`
	Field string      `json:"field"`
}

type ChangeValue struct {
	// messaging_product change fields
	MessagingProduct string           `json:"messaging_product"`
	Metadata         WAMetadata       `json:"metadata"`
	Messages         []InboundMessage `json:"messages"`
	Statuses         []MessageStatus  `json:"statuses"`
	// message_template_status_update fields
	Event                   string `json:"event"`
	MessageTemplateID       int64  `json:"message_template_id"`
	MessageTemplateName     string `json:"message_template_name"`
	MessageTemplateLanguage string `json:"message_template_language"`
	Reason                  string `json:"reason"`
	// business_capability_update fields (legacy + v24+ rename)
	MaxDailyConversationPerPhone    int64 `json:"max_daily_conversation_per_phone"`
	MaxDailyConversationsPerBusiness int64 `json:"max_daily_conversations_per_business"`
}

// TemplateStatusUpdate carries a Meta template approval/rejection event.
type TemplateStatusUpdate struct {
	Event    string // APPROVED|REJECTED|PENDING_DELETION|FLAGGED
	Name     string
	Language string
	Reason   string
}

type WAMetadata struct {
	DisplayPhoneNumber string `json:"display_phone_number"`
	PhoneNumberID      string `json:"phone_number_id"`
}

// InboundMessage represents a message received from a WhatsApp user.
type InboundMessage struct {
	From      string           `json:"from"`      // E.164 phone number
	ID        string           `json:"id"`        // wa_message_id
	Timestamp string           `json:"timestamp"` // Unix epoch string
	Type      string           `json:"type"`      // text|image|video|audio|document|sticker
	Text      *TextContent     `json:"text,omitempty"`
	Image     *MediaContent    `json:"image,omitempty"`
	Video     *MediaContent    `json:"video,omitempty"`
	Audio     *MediaContent    `json:"audio,omitempty"`
	Sticker   *MediaContent    `json:"sticker,omitempty"`
	Document  *DocumentContent `json:"document,omitempty"`
}

// MediaID returns the Meta media ID for download, or "" for text messages.
func (m *InboundMessage) MediaID() string {
	switch {
	case m.Image != nil:
		return m.Image.ID
	case m.Video != nil:
		return m.Video.ID
	case m.Audio != nil:
		return m.Audio.ID
	case m.Sticker != nil:
		return m.Sticker.ID
	case m.Document != nil:
		return m.Document.ID
	}
	return ""
}

type TextContent struct {
	Body string `json:"body"`
}

type MediaContent struct {
	ID       string `json:"id"`
	MimeType string `json:"mime_type"`
	SHA256   string `json:"sha256"`
	Caption  string `json:"caption,omitempty"`
}

type DocumentContent struct {
	MediaContent
	Filename string `json:"filename,omitempty"`
}

type MessageStatus struct {
	ID          string         `json:"id"`     // wa_message_id of the sent message
	Status      string         `json:"status"` // sent|delivered|read|failed
	Timestamp   string         `json:"timestamp"`
	RecipientID string         `json:"recipient_id"`
	Errors      []MetaErrorObj `json:"errors,omitempty"`
}

type MetaErrorObj struct {
	Code    int    `json:"code"`
	Title   string `json:"title"`
	Message string `json:"message,omitempty"`
}

// ── Signature verification ────────────────────────────────────────────────

// VerifySignature validates the X-Hub-Signature-256 header sent by Meta.
func VerifySignature(appSecret, body []byte, signatureHeader string) bool {
	if !strings.HasPrefix(signatureHeader, "sha256=") {
		return false
	}
	sig, err := hex.DecodeString(strings.TrimPrefix(signatureHeader, "sha256="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, appSecret)
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), sig)
}

// ── ID extraction ─────────────────────────────────────────────────────────

// ExtractInboundIDs returns wa_message_ids from inbound messages only.
// Status-update IDs are intentionally excluded: status processing is idempotent
// (setting delivered/read twice causes no harm) so we must not block retries.
func ExtractInboundIDs(payload []byte) ([]string, error) {
	var wp WebhookPayload
	if err := json.Unmarshal(payload, &wp); err != nil {
		return nil, fmt.Errorf("unmarshal webhook payload: %w", err)
	}
	var ids []string
	for _, entry := range wp.Entry {
		for _, change := range entry.Changes {
			for _, msg := range change.Value.Messages {
				if msg.ID != "" {
					ids = append(ids, msg.ID)
				}
			}
		}
	}
	return ids, nil
}

// parsePayload unmarshals a webhook payload (used internally after logging).
func parsePayload(payload []byte) (*WebhookPayload, error) {
	var wp WebhookPayload
	if err := json.Unmarshal(payload, &wp); err != nil {
		return nil, fmt.Errorf("unmarshal webhook payload: %w", err)
	}
	return &wp, nil
}

// ── Store interface ───────────────────────────────────────────────────────

// Store handles raw webhook persistence and inbound-message deduplication.
type Store interface {
	LogWebhook(ctx context.Context, payload []byte) (int64, error)
	// RecordMessageIDs inserts wa_message_ids; returns only the previously-unseen ones.
	RecordMessageIDs(ctx context.Context, logID int64, ids []string) ([]string, error)
}

// ── ProcessResult ─────────────────────────────────────────────────────────

// ProcessResult carries the decoded, deduplicated events from one webhook call.
type ProcessResult struct {
	// NewInboundMessages are genuinely new inbound messages (not seen before).
	NewInboundMessages []InboundMessage
	// StatusUpdates are delivery/read/failed status events for outbound messages.
	// All statuses are returned — no deduplication (updates are idempotent).
	StatusUpdates []MessageStatus
	// TemplateStatusUpdates are Meta template approval/rejection events.
	TemplateStatusUpdates []TemplateStatusUpdate
	// NewDailyCap is set when Meta sends a business_capability_update.
	// Non-nil means the caller should persist this as the new daily tier cap
	// (already pre-multiplied to 90% of the tier value).
	NewDailyCap *int64
}

// ── Processor ─────────────────────────────────────────────────────────────

type Processor struct {
	store Store
}

func NewProcessor(store Store) *Processor {
	return &Processor{store: store}
}

// ProcessRaw logs the payload, deduplicates inbound message IDs, and returns
// the decoded events for further handling by the inbox layer.
func (p *Processor) ProcessRaw(ctx context.Context, payload []byte) (*ProcessResult, error) {
	logID, err := p.store.LogWebhook(ctx, payload)
	if err != nil {
		return nil, fmt.Errorf("log webhook: %w", err)
	}

	wp, err := parsePayload(payload)
	if err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}

	// Collect inbound message IDs for deduplication.
	var inboundIDs []string
	for _, entry := range wp.Entry {
		for _, change := range entry.Changes {
			for _, msg := range change.Value.Messages {
				if msg.ID != "" {
					inboundIDs = append(inboundIDs, msg.ID)
				}
			}
		}
	}

	var newIDs []string
	if len(inboundIDs) > 0 {
		newIDs, err = p.store.RecordMessageIDs(ctx, logID, inboundIDs)
		if err != nil {
			return nil, fmt.Errorf("record message ids: %w", err)
		}
	}

	// Build result from the full parsed payload.
	newIDSet := make(map[string]bool, len(newIDs))
	for _, id := range newIDs {
		newIDSet[id] = true
	}

	result := &ProcessResult{}
	for _, entry := range wp.Entry {
		for _, change := range entry.Changes {
			switch change.Field {
			case "message_template_status_update":
				if change.Value.MessageTemplateName != "" {
					result.TemplateStatusUpdates = append(result.TemplateStatusUpdates, TemplateStatusUpdate{
						Event:    change.Value.Event,
						Name:     change.Value.MessageTemplateName,
						Language: change.Value.MessageTemplateLanguage,
						Reason:   change.Value.Reason,
					})
				}
			case "business_capability_update", "account_update":
				// Meta sends the new daily messaging tier here. Store 90% of the
				// tier as the daily send cap. Handle both the legacy field name
				// and the v24+ rename.
				tier := change.Value.MaxDailyConversationPerPhone
				if tier == 0 {
					tier = change.Value.MaxDailyConversationsPerBusiness
				}
				if tier > 0 {
					cap := int64(float64(tier) * 0.9)
					result.NewDailyCap = &cap
				}
			default:
				for _, msg := range change.Value.Messages {
					if newIDSet[msg.ID] {
						result.NewInboundMessages = append(result.NewInboundMessages, msg)
					}
				}
				result.StatusUpdates = append(result.StatusUpdates, change.Value.Statuses...)
			}
		}
	}

	return result, nil
}
