package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"whatsapptool/internal/whatsapp"
)

// Compile-time check that WebhookStore satisfies whatsapp.Store.
var _ whatsapp.Store = (*WebhookStore)(nil)

// WebhookStore implements whatsapp.Store backed by PostgreSQL.
type WebhookStore struct {
	pool *pgxpool.Pool
}

func NewWebhookStore(pool *pgxpool.Pool) *WebhookStore {
	return &WebhookStore{pool: pool}
}

// LogWebhook inserts the raw JSON payload into webhook_log and returns the row id.
func (s *WebhookStore) LogWebhook(ctx context.Context, payload []byte) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO webhook_log (payload) VALUES ($1) RETURNING id`,
		json.RawMessage(payload),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert webhook_log: %w", err)
	}
	return id, nil
}

// RecordMessageIDs batch-inserts wa_message_ids with ON CONFLICT DO NOTHING,
// counting rows affected to determine which IDs are genuinely new.
func (s *WebhookStore) RecordMessageIDs(ctx context.Context, logID int64, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	fresh := make([]string, 0, len(ids))
	for _, id := range ids {
		tag, err := tx.Exec(ctx,
			`INSERT INTO webhook_message_ids (wa_message_id, webhook_log_id)
			 VALUES ($1, $2)
			 ON CONFLICT (wa_message_id) DO NOTHING`,
			id, logID,
		)
		if err != nil {
			return nil, fmt.Errorf("insert webhook_message_ids: %w", err)
		}
		if tag.RowsAffected() == 1 {
			fresh = append(fresh, id)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return fresh, nil
}
