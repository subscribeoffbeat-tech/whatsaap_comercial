package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Template mirrors the templates table.
type Template struct {
	ID              string
	Name            string
	Language        string
	Category        string           // marketing|utility|authentication
	Status          string           // pending|approved|rejected|paused
	Components      []map[string]any // Meta components array
	WATemplateID    *string          // Meta's internal ID
	RejectionReason *string
	SubmittedAt     *time.Time
	ApprovedAt      *time.Time
	CreatedBy       *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// ListTemplates returns all templates ordered by created_at DESC.
func ListTemplates(ctx context.Context, pool *pgxpool.Pool) ([]Template, error) {
	rows, err := pool.Query(ctx, `
		SELECT
		  id::text, name, language, category, status,
		  components::text, wa_template_id, rejection_reason,
		  submitted_at, approved_at, created_by::text,
		  created_at, updated_at
		FROM templates
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}
	defer rows.Close()

	var templates []Template
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		templates = append(templates, *t)
	}
	return templates, rows.Err()
}

// GetTemplate returns a single template by ID.
func GetTemplate(ctx context.Context, pool *pgxpool.Pool, id string) (*Template, error) {
	row := pool.QueryRow(ctx, `
		SELECT
		  id::text, name, language, category, status,
		  components::text, wa_template_id, rejection_reason,
		  submitted_at, approved_at, created_by::text,
		  created_at, updated_at
		FROM templates
		WHERE id = $1::uuid
	`, id)
	t, err := scanTemplate(row)
	if err != nil {
		return nil, fmt.Errorf("get template: %w", err)
	}
	return t, nil
}

// CreateTemplate inserts a new template and populates t.ID and t.CreatedAt.
func CreateTemplate(ctx context.Context, pool *pgxpool.Pool, t *Template) error {
	comps, _ := json.Marshal(t.Components)
	return pool.QueryRow(ctx, `
		INSERT INTO templates (name, language, category, components, created_by)
		VALUES ($1, $2, $3, $4::jsonb, $5::uuid)
		RETURNING id::text, created_at
	`, t.Name, t.Language, t.Category, string(comps), t.CreatedBy).
		Scan(&t.ID, &t.CreatedAt)
}

// UpdateTemplate writes name, language, category, and components back to the DB.
// Only draft (pending) templates should be editable.
func UpdateTemplate(ctx context.Context, pool *pgxpool.Pool, t *Template) error {
	comps, _ := json.Marshal(t.Components)
	_, err := pool.Exec(ctx, `
		UPDATE templates
		SET name = $2, language = $3, category = $4, components = $5::jsonb,
		    status = 'pending', updated_at = NOW()
		WHERE id = $1::uuid
	`, t.ID, t.Name, t.Language, t.Category, string(comps))
	return err
}

// DeleteTemplate removes a template by ID.
func DeleteTemplate(ctx context.Context, pool *pgxpool.Pool, id string) error {
	_, err := pool.Exec(ctx, `DELETE FROM templates WHERE id = $1::uuid`, id)
	return err
}

// SetTemplateWAID records the Meta template ID after a successful API submission.
func SetTemplateWAID(ctx context.Context, pool *pgxpool.Pool, localID, waTemplateID string) error {
	_, err := pool.Exec(ctx, `
		UPDATE templates
		SET wa_template_id = $2, submitted_at = NOW(), updated_at = NOW()
		WHERE id = $1::uuid
	`, localID, waTemplateID)
	return err
}

// SetTemplateStatus updates status from a Meta template_status_update webhook.
// Meta identifies templates by name+language, not our internal ID.
func SetTemplateStatus(ctx context.Context, pool *pgxpool.Pool, name, language, status, reason string) error {
	_, err := pool.Exec(ctx, `
		UPDATE templates
		SET status           = $3,
		    rejection_reason = CASE WHEN $3 = 'rejected' THEN $4 ELSE rejection_reason END,
		    approved_at      = CASE WHEN $3 = 'approved' THEN NOW() ELSE approved_at END,
		    updated_at       = NOW()
		WHERE name = $1 AND language = $2
	`, name, language, status, reason)
	return err
}

// ── scan helper ───────────────────────────────────────────────────────────────

func scanTemplate(row interface{ Scan(dest ...any) error }) (*Template, error) {
	var t Template
	var (
		waTemplateID, rejectionReason, createdBy pgtype.Text
		submittedAt, approvedAt                  pgtype.Timestamptz
		compsRaw                                 string
	)
	if err := row.Scan(
		&t.ID, &t.Name, &t.Language, &t.Category, &t.Status,
		&compsRaw, &waTemplateID, &rejectionReason,
		&submittedAt, &approvedAt, &createdBy,
		&t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan template: %w", err)
	}
	_ = json.Unmarshal([]byte(compsRaw), &t.Components)
	if waTemplateID.Valid {
		t.WATemplateID = &waTemplateID.String
	}
	if rejectionReason.Valid {
		t.RejectionReason = &rejectionReason.String
	}
	if submittedAt.Valid {
		ts := submittedAt.Time
		t.SubmittedAt = &ts
	}
	if approvedAt.Valid {
		ta := approvedAt.Time
		t.ApprovedAt = &ta
	}
	if createdBy.Valid {
		t.CreatedBy = &createdBy.String
	}
	return &t, nil
}
