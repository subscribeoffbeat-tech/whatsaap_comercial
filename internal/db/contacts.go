package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Contact mirrors the contacts table.
type Contact struct {
	ID           string
	WAPhone      string
	Name         string
	Email        *string
	CustomFields map[string]any
	OptedIn      bool
	OptInSource  *string
	OptInAt      *time.Time
	OptOutAt     *time.Time
	IsBlocked    bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ContactNote mirrors the contact_notes table.
type ContactNote struct {
	ID        int64
	ContactID string
	AgentID   *string
	Body      string
	CreatedAt time.Time
}

// ListContactsFilter controls which contacts ListContacts returns.
type ListContactsFilter struct {
	Search  string // ILIKE match on wa_phone or name; "" = all
	TagID   *int64 // filter by tag presence
	OptedIn *bool  // nil = all
	Limit   int    // 0 → 50
	Offset  int
}

// phoneRE validates E.164: + followed by 8–15 digits, first digit non-zero.
var phoneRE = regexp.MustCompile(`^\+[1-9]\d{7,14}$`)

// NormalizePhone strips common formatting characters, prepends + if missing,
// and validates the result as E.164.
func NormalizePhone(phone string) (string, error) {
	r := strings.NewReplacer(" ", "", "-", "", "(", "", ")", "", ".", "")
	phone = r.Replace(strings.TrimSpace(phone))
	if !strings.HasPrefix(phone, "+") {
		phone = "+" + phone
	}
	if !phoneRE.MatchString(phone) {
		return "", fmt.Errorf("invalid E.164 phone: %q", phone)
	}
	return phone, nil
}

// ValidateImportRow normalises the phone and checks required fields.
// Returns the normalised phone on success.
func ValidateImportRow(phone, _ string) (string, error) {
	if strings.TrimSpace(phone) == "" {
		return "", errors.New("phone is required")
	}
	return NormalizePhone(phone)
}

// ── CRUD ─────────────────────────────────────────────────────────────────────

// ListContacts returns a page of contacts matching the filter and the total count.
func ListContacts(ctx context.Context, pool *pgxpool.Pool, f ListContactsFilter) ([]Contact, int, error) {
	limit := f.Limit
	if limit == 0 {
		limit = 50
	}
	var search *string
	if f.Search != "" {
		s := "%" + f.Search + "%"
		search = &s
	}

	// Count query
	var total int
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM contacts c
		WHERE ($1::text IS NULL OR c.wa_phone ILIKE $1 OR c.name ILIKE $1)
		  AND ($2::bigint IS NULL OR EXISTS (
		        SELECT 1 FROM contact_tags ct WHERE ct.contact_id = c.id AND ct.tag_id = $2
		      ))
		  AND ($3::boolean IS NULL OR c.opted_in = $3)
	`, search, f.TagID, f.OptedIn).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count contacts: %w", err)
	}

	rows, err := pool.Query(ctx, `
		SELECT
		  id::text, wa_phone, name, email,
		  custom_fields::text, opted_in, opt_in_source,
		  opt_in_at, opt_out_at, is_blocked,
		  created_at, updated_at
		FROM contacts c
		WHERE ($1::text IS NULL OR c.wa_phone ILIKE $1 OR c.name ILIKE $1)
		  AND ($2::bigint IS NULL OR EXISTS (
		        SELECT 1 FROM contact_tags ct WHERE ct.contact_id = c.id AND ct.tag_id = $2
		      ))
		  AND ($3::boolean IS NULL OR c.opted_in = $3)
		ORDER BY c.created_at DESC
		LIMIT $4 OFFSET $5
	`, search, f.TagID, f.OptedIn, limit, f.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list contacts: %w", err)
	}
	defer rows.Close()

	var contacts []Contact
	for rows.Next() {
		c, err := scanContact(rows)
		if err != nil {
			return nil, 0, err
		}
		contacts = append(contacts, *c)
	}
	return contacts, total, rows.Err()
}

// GetContact returns a single contact by ID.
func GetContact(ctx context.Context, pool *pgxpool.Pool, id string) (*Contact, error) {
	row := pool.QueryRow(ctx, `
		SELECT
		  id::text, wa_phone, name, email,
		  custom_fields::text, opted_in, opt_in_source,
		  opt_in_at, opt_out_at, is_blocked,
		  created_at, updated_at
		FROM contacts
		WHERE id = $1::uuid
	`, id)
	c, err := scanContact(row)
	if err != nil {
		return nil, fmt.Errorf("get contact: %w", err)
	}
	return c, nil
}

// CreateContact inserts a new contact and populates c.ID and c.CreatedAt.
func CreateContact(ctx context.Context, pool *pgxpool.Pool, c *Contact) error {
	custom, _ := json.Marshal(c.CustomFields)
	return pool.QueryRow(ctx, `
		INSERT INTO contacts (wa_phone, name, email, custom_fields, opted_in, opt_in_source, opt_in_at)
		VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7)
		RETURNING id::text, created_at
	`, c.WAPhone, c.Name, c.Email, string(custom), c.OptedIn, c.OptInSource, c.OptInAt).
		Scan(&c.ID, &c.CreatedAt)
}

// UpdateContact writes name, email, custom_fields, and opted_in back to the DB.
func UpdateContact(ctx context.Context, pool *pgxpool.Pool, c *Contact) error {
	custom, _ := json.Marshal(c.CustomFields)
	_, err := pool.Exec(ctx, `
		UPDATE contacts
		SET name = $2, email = $3, custom_fields = $4::jsonb, opted_in = $5, updated_at = NOW()
		WHERE id = $1::uuid
	`, c.ID, c.Name, c.Email, string(custom), c.OptedIn)
	return err
}

// DeleteContact removes a contact (cascades to tags, notes, conversations).
func DeleteContact(ctx context.Context, pool *pgxpool.Pool, id string) error {
	_, err := pool.Exec(ctx, `DELETE FROM contacts WHERE id = $1::uuid`, id)
	return err
}

// OptOut marks a contact opted-out by phone number. Called by the STOP handler.
func OptOut(ctx context.Context, pool *pgxpool.Pool, waPhone string) error {
	_, err := pool.Exec(ctx, `
		UPDATE contacts
		SET opted_in = FALSE, opt_out_at = NOW(), updated_at = NOW()
		WHERE wa_phone = $1
	`, waPhone)
	return err
}

// BulkInsertContacts upserts contacts (skips on duplicate wa_phone).
// Returns (inserted, skipped, error).
func BulkInsertContacts(ctx context.Context, pool *pgxpool.Pool, contacts []Contact) (inserted, skipped int, err error) {
	tx, txErr := pool.Begin(ctx)
	if txErr != nil {
		return 0, 0, txErr
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	for _, c := range contacts {
		custom, _ := json.Marshal(c.CustomFields)
		var id string
		scanErr := tx.QueryRow(ctx, `
			INSERT INTO contacts (wa_phone, name, email, custom_fields, opted_in, opt_in_source, opt_in_at)
			VALUES ($1, $2, $3, $4::jsonb, $5, $6, NOW())
			ON CONFLICT (wa_phone) DO NOTHING
			RETURNING id::text
		`, c.WAPhone, c.Name, c.Email, string(custom), c.OptedIn, c.OptInSource).Scan(&id)

		if scanErr == nil {
			inserted++
		} else if errors.Is(scanErr, pgx.ErrNoRows) {
			skipped++
		} else {
			return inserted, skipped, scanErr
		}
	}
	return inserted, skipped, tx.Commit(ctx)
}

// GetContactIDsByPhones returns contact IDs for the given E.164 phone numbers.
func GetContactIDsByPhones(ctx context.Context, pool *pgxpool.Pool, phones []string) ([]string, error) {
	if len(phones) == 0 {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `
		SELECT id::text FROM contacts WHERE wa_phone = ANY($1::text[])
	`, phones)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetContactsByIDs returns a map of contactID → Contact for the given IDs.
// Missing IDs are silently omitted.
func GetContactsByIDs(ctx context.Context, pool *pgxpool.Pool, ids []string) (map[string]Contact, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `
		SELECT c.id::text, c.wa_phone, c.name, c.email, c.custom_fields::text,
		       c.opted_in, c.opt_in_source, c.opt_in_at, c.opt_out_at, c.is_blocked,
		       c.created_at, c.updated_at
		FROM contacts c
		WHERE c.id = ANY($1::uuid[])
	`, ids)
	if err != nil {
		return nil, fmt.Errorf("get contacts by ids: %w", err)
	}
	defer rows.Close()
	result := make(map[string]Contact, len(ids))
	for rows.Next() {
		c, err := scanContact(rows)
		if err != nil {
			return nil, err
		}
		result[c.ID] = *c
	}
	return result, rows.Err()
}

// ── Tag operations ────────────────────────────────────────────────────────────

func AddContactTag(ctx context.Context, pool *pgxpool.Pool, contactID string, tagID int64) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO contact_tags (contact_id, tag_id) VALUES ($1::uuid, $2) ON CONFLICT DO NOTHING
	`, contactID, tagID)
	return err
}

func RemoveContactTag(ctx context.Context, pool *pgxpool.Pool, contactID string, tagID int64) error {
	_, err := pool.Exec(ctx, `
		DELETE FROM contact_tags WHERE contact_id = $1::uuid AND tag_id = $2
	`, contactID, tagID)
	return err
}

func BulkAddTag(ctx context.Context, pool *pgxpool.Pool, contactIDs []string, tagID int64) error {
	for _, id := range contactIDs {
		if _, err := pool.Exec(ctx, `
			INSERT INTO contact_tags (contact_id, tag_id) VALUES ($1::uuid, $2) ON CONFLICT DO NOTHING
		`, id, tagID); err != nil {
			return err
		}
	}
	return nil
}

func BulkRemoveTag(ctx context.Context, pool *pgxpool.Pool, contactIDs []string, tagID int64) error {
	for _, id := range contactIDs {
		if _, err := pool.Exec(ctx, `
			DELETE FROM contact_tags WHERE contact_id = $1::uuid AND tag_id = $2
		`, id, tagID); err != nil {
			return err
		}
	}
	return nil
}

func GetContactTags(ctx context.Context, pool *pgxpool.Pool, contactID string) ([]Tag, error) {
	rows, err := pool.Query(ctx, `
		SELECT t.id, t.name, t.color, t.created_at
		FROM tags t
		JOIN contact_tags ct ON ct.tag_id = t.id
		WHERE ct.contact_id = $1::uuid
		ORDER BY t.name
	`, contactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Color, &t.CreatedAt); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// ── Notes ─────────────────────────────────────────────────────────────────────

func CreateNote(ctx context.Context, pool *pgxpool.Pool, note *ContactNote) error {
	return pool.QueryRow(ctx, `
		INSERT INTO contact_notes (contact_id, agent_id, body)
		VALUES ($1::uuid, $2::uuid, $3)
		RETURNING id, created_at
	`, note.ContactID, note.AgentID, note.Body).Scan(&note.ID, &note.CreatedAt)
}

func ListNotes(ctx context.Context, pool *pgxpool.Pool, contactID string) ([]ContactNote, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, contact_id::text, agent_id::text, body, created_at
		FROM contact_notes
		WHERE contact_id = $1::uuid
		ORDER BY created_at DESC
	`, contactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notes []ContactNote
	for rows.Next() {
		var n ContactNote
		var agentID pgtype.Text
		if err := rows.Scan(&n.ID, &n.ContactID, &agentID, &n.Body, &n.CreatedAt); err != nil {
			return nil, err
		}
		if agentID.Valid {
			n.AgentID = &agentID.String
		}
		notes = append(notes, n)
	}
	return notes, rows.Err()
}

// ── scan helper ───────────────────────────────────────────────────────────────

func scanContact(row interface{ Scan(dest ...any) error }) (*Contact, error) {
	var c Contact
	var (
		email, optInSource     pgtype.Text
		optInAt, optOutAt      pgtype.Timestamptz
		customRaw              string
	)
	if err := row.Scan(
		&c.ID, &c.WAPhone, &c.Name, &email,
		&customRaw, &c.OptedIn, &optInSource,
		&optInAt, &optOutAt, &c.IsBlocked,
		&c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan contact: %w", err)
	}
	if c.CustomFields == nil {
		c.CustomFields = map[string]any{}
	}
	_ = json.Unmarshal([]byte(customRaw), &c.CustomFields)
	if email.Valid {
		c.Email = &email.String
	}
	if optInSource.Valid {
		c.OptInSource = &optInSource.String
	}
	if optInAt.Valid {
		t := optInAt.Time
		c.OptInAt = &t
	}
	if optOutAt.Valid {
		t := optOutAt.Time
		c.OptOutAt = &t
	}
	return &c, nil
}
