package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ── Types ─────────────────────────────────────────────────────────────────

// Conversation is a row from the conversations table.
type Conversation struct {
	ID            string
	ContactID     string
	AssignedTo    *string // nil = unassigned
	Status        string
	LastInboundAt *time.Time
	LastMessageAt *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// ConvListRow joins conversations with contact data for the inbox list view.
type ConvListRow struct {
	Conversation
	ContactName  string
	ContactPhone string
	LastBody     string
	LastDir      string // inbound|outbound
}

// ListFilter controls which conversations the List function returns.
type ListFilter struct {
	AssignedTo *string // nil = all; empty string = unassigned only
	Status     string  // "" = all; "open"|"closed"|"pending"
	Limit      int     // 0 → default 50
	Offset     int
}

// AgentLoad is used by the round-robin router to pick the least-busy agent.
type AgentLoad struct {
	AgentID   string
	OpenConvs int
}

// ── Routing (pure, testable without DB) ──────────────────────────────────

// ChooseAgent returns the ID of the available agent with the fewest open
// conversations. Returns nil when no agents are available.
func ChooseAgent(agents []AgentLoad) *string {
	if len(agents) == 0 {
		return nil
	}
	best := 0
	for i, a := range agents {
		if a.OpenConvs < agents[best].OpenConvs {
			best = i
		}
	}
	id := agents[best].AgentID
	return &id
}

// ── DB operations ─────────────────────────────────────────────────────────

// GetOrCreateByContact returns the existing conversation for a contact or
// creates a new one with status "open".
func GetOrCreateByContact(ctx context.Context, pool *pgxpool.Pool, contactID string) (*Conversation, error) {
	tid := effectiveTenantID(ctx)
	row := pool.QueryRow(ctx, `
		INSERT INTO conversations (contact_id, tenant_id)
		VALUES ($1::uuid, $2::uuid)
		ON CONFLICT (contact_id) DO UPDATE
		  SET updated_at = NOW()   -- no-op update to satisfy RETURNING
		RETURNING
		  id::text, contact_id::text, assigned_to::text,
		  status, last_inbound_at, last_message_at, created_at, updated_at
	`, contactID, tid)
	return scanConversation(row)
}

// GetConversation returns a single conversation by ID.
func GetConversation(ctx context.Context, pool *pgxpool.Pool, id string) (*Conversation, error) {
	tid := effectiveTenantID(ctx)
	row := pool.QueryRow(ctx, `
		SELECT
		  id::text, contact_id::text, assigned_to::text,
		  status, last_inbound_at, last_message_at, created_at, updated_at
		FROM conversations
		WHERE tenant_id = $2::uuid AND id = $1::uuid
	`, id, tid)
	return scanConversation(row)
}

// SetLastInbound updates last_inbound_at (opening/resetting the 24h window)
// and also sets last_message_at and status to "open".
func SetLastInbound(ctx context.Context, pool *pgxpool.Pool, convID string, t time.Time) error {
	tid := effectiveTenantID(ctx)
	_, err := pool.Exec(ctx, `
		UPDATE conversations
		SET last_inbound_at = $2,
		    last_message_at = $2,
		    status          = 'open',
		    updated_at      = NOW()
		WHERE tenant_id = $3::uuid AND id = $1::uuid
	`, convID, t, tid)
	return err
}

// TouchLastMessage bumps last_message_at without changing the inbound window.
func TouchLastMessage(ctx context.Context, pool *pgxpool.Pool, convID string, t time.Time) error {
	tid := effectiveTenantID(ctx)
	_, err := pool.Exec(ctx, `
		UPDATE conversations
		SET last_message_at = $2, updated_at = NOW()
		WHERE tenant_id = $3::uuid AND id = $1::uuid
	`, convID, t, tid)
	return err
}

// AssignRoundRobin assigns the conversation to the available agent with the
// fewest open conversations. Returns the chosen agent ID (nil = left unassigned).
func AssignRoundRobin(ctx context.Context, pool *pgxpool.Pool, convID string) (*string, error) {
	tid := effectiveTenantID(ctx)
	rows, err := pool.Query(ctx, `
		SELECT a.id::text,
		       COUNT(c.id) AS open_convs
		FROM agents a
		LEFT JOIN conversations c
		       ON c.assigned_to = a.id AND c.status = 'open'
		WHERE a.tenant_id = $1::uuid
		  AND a.available = TRUE
		  AND a.role      = 'agent'
		GROUP BY a.id
		ORDER BY open_convs ASC, a.created_at ASC
	`, tid)
	if err != nil {
		return nil, fmt.Errorf("query agent loads: %w", err)
	}
	defer rows.Close()

	var agents []AgentLoad
	for rows.Next() {
		var al AgentLoad
		if err := rows.Scan(&al.AgentID, &al.OpenConvs); err != nil {
			return nil, err
		}
		agents = append(agents, al)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	chosen := ChooseAgent(agents)
	if chosen == nil {
		return nil, nil // no available agents → leave unassigned
	}

	_, err = pool.Exec(ctx, `
		UPDATE conversations
		SET assigned_to = $2::uuid, updated_at = NOW()
		WHERE tenant_id = $3::uuid AND id = $1::uuid AND assigned_to IS NULL
	`, convID, *chosen, tid)
	return chosen, err
}

// SetStatus updates the conversation status (open|closed|pending).
func SetStatus(ctx context.Context, pool *pgxpool.Pool, convID, status string) error {
	tid := effectiveTenantID(ctx)
	_, err := pool.Exec(ctx, `
		UPDATE conversations
		SET status = $2, updated_at = NOW()
		WHERE tenant_id = $3::uuid AND id = $1::uuid
	`, convID, status, tid)
	return err
}

// ListConversations returns conversations matching the filter for the inbox list pane.
func ListConversations(ctx context.Context, pool *pgxpool.Pool, f ListFilter) ([]ConvListRow, error) {
	tid := effectiveTenantID(ctx)
	limit := f.Limit
	if limit == 0 {
		limit = 50
	}

	rows, err := pool.Query(ctx, `
		SELECT
		  c.id::text,
		  c.contact_id::text,
		  c.assigned_to::text,
		  c.status,
		  c.last_inbound_at,
		  c.last_message_at,
		  c.created_at,
		  c.updated_at,
		  ct.name        AS contact_name,
		  ct.wa_phone    AS contact_phone,
		  COALESCE(m.content->>'body', '')    AS last_body,
		  COALESCE(m.direction, '')           AS last_dir
		FROM conversations c
		JOIN contacts ct ON ct.id = c.contact_id
		LEFT JOIN LATERAL (
		  SELECT content, direction
		  FROM messages
		  WHERE conversation_id = c.id
		  ORDER BY created_at DESC
		  LIMIT 1
		) m ON TRUE
		WHERE c.tenant_id = $5::uuid
		  AND ($1::text IS NULL OR c.status = $1)
		  AND ($2::text IS NULL OR (
		         $2 = ''  AND c.assigned_to IS NULL
		      OR $2 != '' AND c.assigned_to = $2::uuid
		  ))
		ORDER BY c.last_message_at DESC NULLS LAST
		LIMIT $3 OFFSET $4
	`, nilIfEmpty(f.Status), f.AssignedTo, limit, f.Offset, tid)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()

	var result []ConvListRow
	for rows.Next() {
		var r ConvListRow
		var assignedTo pgtype.Text
		if err := rows.Scan(
			&r.ID, &r.ContactID, &assignedTo,
			&r.Status, &r.LastInboundAt, &r.LastMessageAt,
			&r.CreatedAt, &r.UpdatedAt,
			&r.ContactName, &r.ContactPhone,
			&r.LastBody, &r.LastDir,
		); err != nil {
			return nil, err
		}
		if assignedTo.Valid {
			r.AssignedTo = &assignedTo.String
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// ── helpers ───────────────────────────────────────────────────────────────

type scannableRow interface {
	Scan(dest ...any) error
}

func scanConversation(row scannableRow) (*Conversation, error) {
	var c Conversation
	var assignedTo pgtype.Text
	if err := row.Scan(
		&c.ID, &c.ContactID, &assignedTo,
		&c.Status, &c.LastInboundAt, &c.LastMessageAt,
		&c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan conversation: %w", err)
	}
	if assignedTo.Valid {
		c.AssignedTo = &assignedTo.String
	}
	return &c, nil
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
