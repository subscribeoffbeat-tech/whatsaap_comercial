package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AuditEntry mirrors the audit_log table.
type AuditEntry struct {
	ID         int64
	ActorID    *string
	ActorName  string // joined from agents
	Action     string
	EntityType *string
	EntityID   *string
	Details    map[string]any
	IPAddress  *string
	CreatedAt  time.Time
}

// Log inserts a row into the audit_log table. It is safe to call in a goroutine.
// details may be nil.
func Log(ctx context.Context, pool *pgxpool.Pool,
	actorID, action, entityType, entityID string,
	details map[string]any,
) {
	raw, _ := json.Marshal(details)
	var aID, eType, eID *string
	if actorID != "" {
		aID = &actorID
	}
	if entityType != "" {
		eType = &entityType
	}
	if entityID != "" {
		eID = &entityID
	}
	pool.Exec(ctx, `
		INSERT INTO audit_log (actor_id, action, entity_type, entity_id, details)
		VALUES ($1::uuid, $2, $3, $4, $5::jsonb)
	`, aID, action, eType, eID, json.RawMessage(raw)) //nolint:errcheck
}

// ListAuditLog returns audit entries newest-first, with optional action filter and pagination.
func ListAuditLog(ctx context.Context, pool *pgxpool.Pool, action string, limit, offset int) ([]*AuditEntry, error) {
	if limit == 0 {
		limit = 50
	}
	var rows interface{ Next() bool; Scan(...any) error; Close(); Err() error }
	var err error
	if action != "" {
		rows2, e := pool.Query(ctx, `
			SELECT al.id, al.actor_id::text, COALESCE(ag.name,'(system)'),
			       al.action, al.entity_type, al.entity_id,
			       al.details::text, al.ip_address, al.created_at
			FROM audit_log al
			LEFT JOIN agents ag ON ag.id = al.actor_id
			WHERE al.action ILIKE $1
			ORDER BY al.created_at DESC
			LIMIT $2 OFFSET $3
		`, "%"+action+"%", limit, offset)
		rows, err = rows2, e
	} else {
		rows2, e := pool.Query(ctx, `
			SELECT al.id, al.actor_id::text, COALESCE(ag.name,'(system)'),
			       al.action, al.entity_type, al.entity_id,
			       al.details::text, al.ip_address, al.created_at
			FROM audit_log al
			LEFT JOIN agents ag ON ag.id = al.actor_id
			ORDER BY al.created_at DESC
			LIMIT $1 OFFSET $2
		`, limit, offset)
		rows, err = rows2, e
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*AuditEntry
	for rows.Next() {
		e := &AuditEntry{}
		var actorID, entityType, entityID, ip *string
		var detailsRaw string
		if err := rows.Scan(
			&e.ID, &actorID, &e.ActorName,
			&e.Action, &entityType, &entityID,
			&detailsRaw, &ip, &e.CreatedAt,
		); err != nil {
			return nil, err
		}
		e.ActorID = actorID
		e.EntityType = entityType
		e.EntityID = entityID
		e.IPAddress = ip
		_ = json.Unmarshal([]byte(detailsRaw), &e.Details)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
