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
	Industry     string
	CustomFields map[string]any
	OptedIn      bool
	OptInSource  *string
	OptInAt      *time.Time
	OptOutAt     *time.Time
	IsBlocked    bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
	// Populated by ListContacts (joined from tags + conversations).
	Tags          []string
	LastMessageAt *time.Time
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
	Search   string  // ILIKE match on wa_phone or name; "" = all
	TagID    *int64  // filter by single tag presence (legacy)
	TagIDs   []int64 // filter by ANY of these tags (multi-select); takes precedence
	OptedIn  *bool   // nil = all
	Industry string  // "" = all
	Limit    int     // 0 → 50
	Offset   int
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
	var industry *string
	if f.Industry != "" {
		industry = &f.Industry
	}
	// Effective tag set: prefer the multi-select list; fall back to single TagID.
	// nil → no tag filter (encoded as SQL NULL).
	var tagIDs []int64
	if len(f.TagIDs) > 0 {
		tagIDs = f.TagIDs
	} else if f.TagID != nil {
		tagIDs = []int64{*f.TagID}
	}

	// Count query
	var total int
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM contacts c
		WHERE ($1::text IS NULL OR c.wa_phone ILIKE $1 OR c.name ILIKE $1)
		  AND ($2::bigint[] IS NULL OR EXISTS (
		        SELECT 1 FROM contact_tags ct WHERE ct.contact_id = c.id AND ct.tag_id = ANY($2::bigint[])
		      ))
		  AND ($3::boolean IS NULL OR c.opted_in = $3)
		  AND ($4::text IS NULL OR c.industry = $4)
	`, search, tagIDs, f.OptedIn, industry).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count contacts: %w", err)
	}

	rows, err := pool.Query(ctx, `
		SELECT
		  c.id::text, c.wa_phone, c.name, c.email, c.industry,
		  c.custom_fields::text, c.opted_in, c.opt_in_source,
		  c.opt_in_at, c.opt_out_at, c.is_blocked,
		  c.created_at, c.updated_at,
		  COALESCE(array_agg(DISTINCT t.name ORDER BY t.name) FILTER (WHERE t.name IS NOT NULL), ARRAY[]::text[]) AS tags,
		  conv.last_message_at
		FROM contacts c
		LEFT JOIN contact_tags ct ON ct.contact_id = c.id
		LEFT JOIN tags t ON t.id = ct.tag_id
		LEFT JOIN conversations conv ON conv.contact_id = c.id
		WHERE ($1::text IS NULL OR c.wa_phone ILIKE $1 OR c.name ILIKE $1)
		  AND ($2::bigint[] IS NULL OR EXISTS (
		        SELECT 1 FROM contact_tags ct2 WHERE ct2.contact_id = c.id AND ct2.tag_id = ANY($2::bigint[])
		      ))
		  AND ($3::boolean IS NULL OR c.opted_in = $3)
		  AND ($4::text IS NULL OR c.industry = $4)
		GROUP BY c.id, conv.last_message_at
		ORDER BY c.created_at DESC
		LIMIT $5 OFFSET $6
	`, search, tagIDs, f.OptedIn, industry, limit, f.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list contacts: %w", err)
	}
	defer rows.Close()

	var contacts []Contact
	for rows.Next() {
		var c Contact
		var (
			email, optInSource, industry2 pgtype.Text
			optInAt, optOutAt, lastMsgAt  pgtype.Timestamptz
			customRaw                     string
		)
		if err := rows.Scan(
			&c.ID, &c.WAPhone, &c.Name, &email, &industry2,
			&customRaw, &c.OptedIn, &optInSource,
			&optInAt, &optOutAt, &c.IsBlocked,
			&c.CreatedAt, &c.UpdatedAt,
			&c.Tags, &lastMsgAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan contact row: %w", err)
		}
		if industry2.Valid {
			c.Industry = industry2.String
		}
		c.CustomFields = map[string]any{}
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
		if lastMsgAt.Valid {
			t := lastMsgAt.Time
			c.LastMessageAt = &t
		}
		contacts = append(contacts, c)
	}
	return contacts, total, rows.Err()
}

// GetContact returns a single contact by ID.
func GetContact(ctx context.Context, pool *pgxpool.Pool, id string) (*Contact, error) {
	row := pool.QueryRow(ctx, `
		SELECT
		  id::text, wa_phone, name, email, industry,
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
		INSERT INTO contacts (wa_phone, name, email, industry, custom_fields, opted_in, opt_in_source, opt_in_at)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8)
		RETURNING id::text, created_at
	`, c.WAPhone, c.Name, c.Email, c.Industry, string(custom), c.OptedIn, c.OptInSource, c.OptInAt).
		Scan(&c.ID, &c.CreatedAt)
}

// UpdateContact writes name, email, industry, custom_fields, and opted_in back to the DB.
func UpdateContact(ctx context.Context, pool *pgxpool.Pool, c *Contact) error {
	custom, _ := json.Marshal(c.CustomFields)
	_, err := pool.Exec(ctx, `
		UPDATE contacts
		SET name = $2, email = $3, industry = $4, custom_fields = $5::jsonb, opted_in = $6,
		    wa_phone = $7, updated_at = NOW()
		WHERE id = $1::uuid
	`, c.ID, c.Name, c.Email, c.Industry, string(custom), c.OptedIn, c.WAPhone)
	return err
}

// ContactMessageRow is a lightweight message row for the contact activity feed.
type ContactMessageRow struct {
	Direction   string
	Body        string
	Status      string
	MessageType string
	IsCampaign  bool
	CreatedAt   time.Time
}

// ListContactMessages returns this contact's messages (both directions) newest
// first, for the activity timeline.
func ListContactMessages(ctx context.Context, pool *pgxpool.Pool, contactID string, limit int) ([]ContactMessageRow, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := pool.Query(ctx, `
		SELECT m.direction, COALESCE(m.content->>'body', m.content->>'caption', ''),
		       m.status, m.message_type, (m.campaign_id IS NOT NULL), m.created_at
		FROM messages m
		JOIN conversations conv ON conv.id = m.conversation_id
		WHERE conv.contact_id = $1::uuid
		ORDER BY m.created_at DESC
		LIMIT $2
	`, contactID, limit)
	if err != nil {
		return nil, fmt.Errorf("list contact messages: %w", err)
	}
	defer rows.Close()
	var out []ContactMessageRow
	for rows.Next() {
		var m ContactMessageRow
		if err := rows.Scan(&m.Direction, &m.Body, &m.Status, &m.MessageType, &m.IsCampaign, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ContactCampaign is one row of a contact's campaign history.
type ContactCampaign struct {
	CampaignName string
	TemplateName string
	Status       string // message delivery status, or recipient status (e.g. skipped)
	SkipReason   string
	SentAt       time.Time
}

// ListContactCampaigns returns every campaign this contact was enrolled in, with
// the resulting message status (or skip reason), newest first.
func ListContactCampaigns(ctx context.Context, pool *pgxpool.Pool, contactID string) ([]ContactCampaign, error) {
	rows, err := pool.Query(ctx, `
		SELECT camp.name,
		       COALESCE(t.name, ''),
		       COALESCE(m.status, cr.status),
		       COALESCE(cr.skip_reason, ''),
		       COALESCE(cr.sent_at, camp.created_at)
		FROM campaign_recipients cr
		JOIN campaigns camp ON camp.id = cr.campaign_id
		LEFT JOIN templates t ON t.id = camp.template_id
		LEFT JOIN messages m ON m.id = cr.message_id
		WHERE cr.contact_id = $1::uuid
		ORDER BY camp.created_at DESC
	`, contactID)
	if err != nil {
		return nil, fmt.Errorf("list contact campaigns: %w", err)
	}
	defer rows.Close()
	var out []ContactCampaign
	for rows.Next() {
		var c ContactCampaign
		if err := rows.Scan(&c.CampaignName, &c.TemplateName, &c.Status, &c.SkipReason, &c.SentAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetContactConsent flips a contact's marketing opt-in, stamping the
// source/timestamp so the consent record stays auditable.
func SetContactConsent(ctx context.Context, pool *pgxpool.Pool, id string, optedIn bool) error {
	_, err := pool.Exec(ctx, `
		UPDATE contacts
		SET opted_in      = $2,
		    opt_in_source = CASE WHEN $2 THEN 'manual_admin' ELSE opt_in_source END,
		    opt_in_at     = CASE WHEN $2 AND opt_in_at IS NULL THEN NOW() ELSE opt_in_at END,
		    opt_out_at    = CASE WHEN $2 THEN NULL ELSE NOW() END,
		    updated_at    = NOW()
		WHERE id = $1::uuid
	`, id, optedIn)
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

	batch := &pgx.Batch{}
	for _, c := range contacts {
		custom, _ := json.Marshal(c.CustomFields)
		batch.Queue(`
			INSERT INTO contacts (wa_phone, name, email, industry, custom_fields, opted_in, opt_in_source, opt_in_at)
			VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, NOW())
			ON CONFLICT (wa_phone) DO NOTHING
			RETURNING id::text
		`, c.WAPhone, c.Name, c.Email, c.Industry, string(custom), c.OptedIn, c.OptInSource)
	}

	br := tx.SendBatch(ctx, batch)
	defer br.Close()

	for range contacts {
		var id string
		scanErr := br.QueryRow().Scan(&id)
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

// GetContactIDMapByPhones returns a phone→contact-id map for the given phones
// (used by CSV import to apply per-row tags to the right contact).
func GetContactIDMapByPhones(ctx context.Context, pool *pgxpool.Pool, phones []string) (map[string]string, error) {
	if len(phones) == 0 {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `
		SELECT wa_phone, id::text FROM contacts WHERE wa_phone = ANY($1::text[])
	`, phones)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var phone, id string
		if err := rows.Scan(&phone, &id); err != nil {
			return nil, err
		}
		m[phone] = id
	}
	return m, rows.Err()
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
		SELECT c.id::text, c.wa_phone, c.name, c.email, c.industry, c.custom_fields::text,
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
	if len(contactIDs) == 0 {
		return nil
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO contact_tags (contact_id, tag_id)
		SELECT unnest($1::uuid[]), $2
		ON CONFLICT DO NOTHING
	`, contactIDs, tagID)
	return err
}

func BulkRemoveTag(ctx context.Context, pool *pgxpool.Pool, contactIDs []string, tagID int64) error {
	if len(contactIDs) == 0 {
		return nil
	}
	_, err := pool.Exec(ctx, `
		DELETE FROM contact_tags
		WHERE tag_id = $2 AND contact_id = ANY($1::uuid[])
	`, contactIDs, tagID)
	return err
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
		email, optInSource, industry pgtype.Text
		optInAt, optOutAt            pgtype.Timestamptz
		customRaw                    string
	)
	if err := row.Scan(
		&c.ID, &c.WAPhone, &c.Name, &email, &industry,
		&customRaw, &c.OptedIn, &optInSource,
		&optInAt, &optOutAt, &c.IsBlocked,
		&c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan contact: %w", err)
	}
	if industry.Valid {
		c.Industry = industry.String
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
