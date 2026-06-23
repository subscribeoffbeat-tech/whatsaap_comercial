package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ── Types ─────────────────────────────────────────────────────────────────

// Message mirrors the messages table row.
type Message struct {
	ID             string
	ConversationID string
	Direction      string            // inbound|outbound
	MessageType    string            // text|image|video|audio|document|template|…
	Content        map[string]any    // JSONB: {"body":"…"} | {"url":"…","caption":"…"} | …
	WAMessageID    *string           // nil until confirmed by Meta
	Status         string            // pending|sent|delivered|read|failed
	Category       *string           // marketing|utility|authentication|service
	CostINR        *float64
	ErrorCode      *string
	ErrorMessage   *string
	MediaPath      *string           // local path after inbound media download
	CampaignID     *string
	TemplateID     *string
	SentBy         *string           // agent UUID for manual outbound
	CreatedAt      time.Time
}

// InsertMessage saves a new message and populates msg.ID and msg.CreatedAt.
func InsertMessage(ctx context.Context, pool *pgxpool.Pool, msg *Message) error {
	content, err := json.Marshal(msg.Content)
	if err != nil {
		return fmt.Errorf("marshal message content: %w", err)
	}

	err = pool.QueryRow(ctx, `
		INSERT INTO messages (
		  conversation_id, direction, message_type, content,
		  wa_message_id, status, category, cost_inr,
		  error_code, error_message, media_path,
		  campaign_id, template_id, sent_by
		) VALUES (
		  $1::uuid, $2, $3, $4::jsonb,
		  $5, $6, $7, $8,
		  $9, $10, $11,
		  $12::uuid, $13::uuid, $14::uuid
		)
		RETURNING id::text, created_at
	`,
		msg.ConversationID,
		msg.Direction,
		msg.MessageType,
		json.RawMessage(content),
		msg.WAMessageID,
		msg.Status,
		msg.Category,
		msg.CostINR,
		msg.ErrorCode,
		msg.ErrorMessage,
		msg.MediaPath,
		msg.CampaignID,
		msg.TemplateID,
		msg.SentBy,
	).Scan(&msg.ID, &msg.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}
	return nil
}

// SetMessageWAID updates the wa_message_id and sets status to "sent" once
// the Meta send API confirms acceptance.
func SetMessageWAID(ctx context.Context, pool *pgxpool.Pool, localID, waMessageID string) error {
	_, err := pool.Exec(ctx, `
		UPDATE messages
		SET wa_message_id = $2, status = 'sent'
		WHERE id = $1::uuid
	`, localID, waMessageID)
	return err
}

// UpdateMessageStatus applies a delivery/read/failed status from a webhook
// status-update event. errCode and errMsg are set only on failure.
// A failed message has its cost zeroed — Meta does not bill for messages that
// fail to deliver, so the optimistic cost stamped at send time is reversed.
//
// Status writes are monotonic: WhatsApp does not guarantee webhook ordering, so
// a late 'delivered' can arrive after 'read'. We only advance the status forward
// (sent < delivered < read) and never downgrade; 'failed' is terminal and always
// applies (it carries the error reason).
func UpdateMessageStatus(ctx context.Context, pool *pgxpool.Pool, waMessageID, status string, errCode, errMsg *string) error {
	_, err := pool.Exec(ctx, `
		UPDATE messages
		SET status        = $2,
		    error_code    = $3,
		    error_message = $4,
		    cost_inr      = CASE WHEN $2 = 'failed' THEN 0 ELSE cost_inr END
		WHERE wa_message_id = $1
		  AND (
		    $2 = 'failed'
		    OR (CASE $2     WHEN 'sent' THEN 1 WHEN 'delivered' THEN 2 WHEN 'read' THEN 3 ELSE 0 END)
		     > (CASE status WHEN 'sent' THEN 1 WHEN 'delivered' THEN 2 WHEN 'read' THEN 3 WHEN 'failed' THEN 4 ELSE 0 END)
		  )
	`, waMessageID, status, errCode, errMsg)
	return err
}

// SetMediaPath records the local file path after downloading inbound media.
func SetMediaPath(ctx context.Context, pool *pgxpool.Pool, messageID, path string) error {
	_, err := pool.Exec(ctx, `
		UPDATE messages SET media_path = $2 WHERE id = $1::uuid
	`, messageID, path)
	return err
}

// ListMessages returns messages for a conversation, newest-last (ascending by created_at).
func ListMessages(ctx context.Context, pool *pgxpool.Pool, convID string, limit, offset int) ([]Message, error) {
	if limit == 0 {
		limit = 100
	}
	rows, err := pool.Query(ctx, `
		SELECT
		  id::text, conversation_id::text, direction, message_type,
		  content::text, wa_message_id, status,
		  category, cost_inr, error_code, error_message,
		  media_path, campaign_id::text, template_id::text, sent_by::text,
		  created_at
		FROM messages
		WHERE conversation_id = $1::uuid
		ORDER BY created_at ASC
		LIMIT $2 OFFSET $3
	`, convID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, *m)
	}
	return msgs, rows.Err()
}

// ── helpers ───────────────────────────────────────────────────────────────

type rowScanner interface {
	Scan(dest ...any) error
}

func scanMessage(row rowScanner) (*Message, error) {
	var m Message
	var contentRaw string
	var (
		waID, category, errCode, errMsg, mediaPath pgtype.Text
		campaignID, templateID, sentBy             pgtype.Text
		costINR                                     pgtype.Float8
	)
	if err := row.Scan(
		&m.ID, &m.ConversationID, &m.Direction, &m.MessageType,
		&contentRaw, &waID, &m.Status,
		&category, &costINR, &errCode, &errMsg,
		&mediaPath, &campaignID, &templateID, &sentBy,
		&m.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan message: %w", err)
	}
	_ = json.Unmarshal([]byte(contentRaw), &m.Content)
	if waID.Valid {
		m.WAMessageID = &waID.String
	}
	if category.Valid {
		m.Category = &category.String
	}
	if costINR.Valid {
		m.CostINR = &costINR.Float64
	}
	if errCode.Valid {
		m.ErrorCode = &errCode.String
	}
	if errMsg.Valid {
		m.ErrorMessage = &errMsg.String
	}
	if mediaPath.Valid {
		m.MediaPath = &mediaPath.String
	}
	if campaignID.Valid {
		m.CampaignID = &campaignID.String
	}
	if templateID.Valid {
		m.TemplateID = &templateID.String
	}
	if sentBy.Valid {
		m.SentBy = &sentBy.String
	}
	return &m, nil
}
