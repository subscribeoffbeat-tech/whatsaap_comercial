package db

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ── Types ─────────────────────────────────────────────────────────────────────

// Campaign mirrors the campaigns table (+ joined template name).
type Campaign struct {
	ID                string
	Name              string
	TemplateID        string
	TemplateName      string
	TemplateLanguage  string // joined from templates
	Category          string // joined from templates
	TemplateVariables map[string]string // JSON: {"1": "name", "2": "custom_fields.order"}
	Fallbacks         map[string]string // JSON: {"1": "there", "2": "N/A"}
	SegmentTags       []int64
	ExcludeTags       []int64
	Status            string
	ScheduledAt       *time.Time
	StartedAt         *time.Time
	CompletedAt       *time.Time
	TotalRecipients   int
	SentCount         int
	DeliveredCount    int
	ReadCount         int
	FailedCount       int
	SkippedCount      int
	CostTotalINR      float64
	CreatedAt         time.Time
}

// CampaignRecipient mirrors the campaign_recipients table.
type CampaignRecipient struct {
	ID         int64
	CampaignID string
	ContactID  string
	MessageID  *string
	Status     string
	SkipReason *string
	SentAt     *time.Time
	CreatedAt  time.Time
	// Joined from contacts
	WAPhone string
	Name    string
}

// CampaignReport adds click stats to a Campaign.
type CampaignReport struct {
	Campaign
	ClickCount int
}

// ── Campaign CRUD ─────────────────────────────────────────────────────────────

// CreateCampaign inserts a campaign row and sets c.ID from the RETURNING clause.
// Fallbacks is stored in template_variables["_fallbacks"] as a sub-key to keep
// everything in the same JSONB column.
func CreateCampaign(ctx context.Context, pool *pgxpool.Pool, c *Campaign) error {
	// Pack template_variables + fallbacks into one JSONB blob.
	tvars := map[string]any{}
	for k, v := range c.TemplateVariables {
		tvars[k] = v
	}
	if len(c.Fallbacks) > 0 {
		tvars["_fallbacks"] = c.Fallbacks
	}
	varJSON, _ := json.Marshal(tvars)
	tagJSON, _ := json.Marshal(c.SegmentTags)
	exclJSON, _ := json.Marshal(c.ExcludeTags)

	return pool.QueryRow(ctx, `
		INSERT INTO campaigns
		    (name, template_id, template_variables, segment_tags, exclude_tags,
		     status, scheduled_at, total_recipients)
		VALUES
		    ($1, $2::uuid, $3, $4, $5, $6, $7, $8)
		RETURNING id::text
	`,
		c.Name, c.TemplateID,
		varJSON, tagJSON, exclJSON,
		c.Status, c.ScheduledAt, c.TotalRecipients,
	).Scan(&c.ID)
}

// GetCampaign fetches a campaign by UUID string.
func GetCampaign(ctx context.Context, pool *pgxpool.Pool, id string) (*Campaign, error) {
	return scanCampaign(pool.QueryRow(ctx, `
		SELECT c.id::text, c.name, c.template_id::text, t.name, t.language, t.category,
		       c.template_variables, c.segment_tags, c.exclude_tags,
		       c.status,
		       c.scheduled_at, c.started_at, c.completed_at,
		       c.total_recipients, c.sent_count, c.delivered_count,
		       c.read_count, c.failed_count, c.skipped_count,
		       c.cost_total_inr, c.created_at
		FROM campaigns c
		JOIN templates t ON t.id = c.template_id
		WHERE c.id = $1::uuid
	`, id))
}

// ListCampaigns returns all campaigns ordered by most recent first.
func ListCampaigns(ctx context.Context, pool *pgxpool.Pool) ([]Campaign, error) {
	rows, err := pool.Query(ctx, `
		SELECT c.id::text, c.name, c.template_id::text, t.name, t.language, t.category,
		       c.template_variables, c.segment_tags, c.exclude_tags,
		       c.status,
		       c.scheduled_at, c.started_at, c.completed_at,
		       c.total_recipients, c.sent_count, c.delivered_count,
		       c.read_count, c.failed_count, c.skipped_count,
		       c.cost_total_inr, c.created_at
		FROM campaigns c
		JOIN templates t ON t.id = c.template_id
		ORDER BY c.created_at DESC
		LIMIT 200
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cs []Campaign
	for rows.Next() {
		c, err := scanCampaign(rows)
		if err != nil {
			return nil, err
		}
		cs = append(cs, *c)
	}
	return cs, rows.Err()
}

// UpdateCampaignStatus updates status and timestamps accordingly.
func UpdateCampaignStatus(ctx context.Context, pool *pgxpool.Pool, id, status string) error {
	_, err := pool.Exec(ctx, `
		UPDATE campaigns SET
		    status       = $2,
		    started_at   = CASE WHEN $2 = 'running'                  THEN NOW() ELSE started_at   END,
		    completed_at = CASE WHEN $2 IN ('completed','cancelled')  THEN NOW() ELSE completed_at END,
		    updated_at   = NOW()
		WHERE id = $1::uuid
	`, id, status)
	return err
}

// IncrCampaignSent atomically increments sent_count + cost and returns
// whether the campaign is now fully processed.
func IncrCampaignSent(ctx context.Context, pool *pgxpool.Pool, id string, costINR float64) (bool, error) {
	var done bool
	err := pool.QueryRow(ctx, `
		UPDATE campaigns SET
		    sent_count     = sent_count + 1,
		    cost_total_inr = cost_total_inr + $2,
		    updated_at     = NOW()
		WHERE id = $1::uuid
		RETURNING total_recipients > 0
		      AND (sent_count + failed_count + skipped_count) >= total_recipients
	`, id, costINR).Scan(&done)
	return done, err
}

// IncrCampaignFailed atomically increments failed_count and returns done flag.
func IncrCampaignFailed(ctx context.Context, pool *pgxpool.Pool, id string) (bool, error) {
	var done bool
	err := pool.QueryRow(ctx, `
		UPDATE campaigns SET
		    failed_count = failed_count + 1,
		    updated_at   = NOW()
		WHERE id = $1::uuid
		RETURNING total_recipients > 0
		      AND (sent_count + failed_count + skipped_count) >= total_recipients
	`, id).Scan(&done)
	return done, err
}

// ── Recipients ────────────────────────────────────────────────────────────────

// CreateRecipients bulk-inserts pending campaign_recipients for eligible contacts
// and skipped rows (with reason) for excluded ones.
func CreateRecipients(ctx context.Context, pool *pgxpool.Pool,
	campaignID string, eligibleIDs []string, skipMap map[string]string,
) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, cid := range eligibleIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO campaign_recipients (campaign_id, contact_id)
			VALUES ($1::uuid, $2::uuid)
			ON CONFLICT (campaign_id, contact_id) DO NOTHING
		`, campaignID, cid); err != nil {
			return err
		}
	}
	for cid, reason := range skipMap {
		r := reason
		if _, err := tx.Exec(ctx, `
			INSERT INTO campaign_recipients (campaign_id, contact_id, status, skip_reason)
			VALUES ($1::uuid, $2::uuid, 'skipped', $3)
			ON CONFLICT (campaign_id, contact_id) DO NOTHING
		`, campaignID, cid, r); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// GetRecipient fetches a campaign_recipient row by primary key.
func GetRecipient(ctx context.Context, pool *pgxpool.Pool, id int64) (*CampaignRecipient, error) {
	return scanRecipient(pool.QueryRow(ctx, `
		SELECT cr.id, cr.campaign_id::text, cr.contact_id::text,
		       cr.message_id::text, cr.status, cr.skip_reason, cr.sent_at, cr.created_at,
		       c.wa_phone, c.name
		FROM campaign_recipients cr
		JOIN contacts c ON c.id = cr.contact_id
		WHERE cr.id = $1
	`, id))
}

// UpdateRecipientSent marks a recipient as sent and links its message row.
func UpdateRecipientSent(ctx context.Context, pool *pgxpool.Pool, id int64, messageID string) error {
	_, err := pool.Exec(ctx, `
		UPDATE campaign_recipients
		SET status = 'sent', message_id = $2::uuid, sent_at = NOW()
		WHERE id = $1
	`, id, messageID)
	return err
}

// UpdateRecipientFailed marks a recipient as failed with a reason string.
func UpdateRecipientFailed(ctx context.Context, pool *pgxpool.Pool, id int64, reason string) error {
	_, err := pool.Exec(ctx, `
		UPDATE campaign_recipients
		SET status = 'failed', skip_reason = $2
		WHERE id = $1
	`, id, reason)
	return err
}

// ── Audience queries ──────────────────────────────────────────────────────────

// GetAudienceContacts returns opted-in, unblocked contacts matching ALL
// segmentTags and not matched by any excludeTag.
func GetAudienceContacts(ctx context.Context, pool *pgxpool.Pool, segmentTags, excludeTags []int64) ([]Contact, error) {
	var args []any
	conds := []string{"c.opted_in = true", "c.is_blocked = false"}

	if len(segmentTags) > 0 {
		args = append(args, segmentTags)
		conds = append(conds, fmt.Sprintf(
			`(SELECT COUNT(*) FROM contact_tags ct WHERE ct.contact_id = c.id AND ct.tag_id = ANY($%d::bigint[])) = %d`,
			len(args), len(segmentTags),
		))
	}
	if len(excludeTags) > 0 {
		args = append(args, excludeTags)
		conds = append(conds, fmt.Sprintf(
			`NOT EXISTS (SELECT 1 FROM contact_tags ct WHERE ct.contact_id = c.id AND ct.tag_id = ANY($%d::bigint[]))`,
			len(args),
		))
	}

	q := `SELECT c.id::text, c.wa_phone, c.name, c.email, c.industry, c.custom_fields::text,
		         c.opted_in, c.opt_in_source, c.opt_in_at, c.opt_out_at, c.is_blocked,
		         c.created_at, c.updated_at
		  FROM contacts c
		  WHERE ` + strings.Join(conds, " AND ") + `
		  ORDER BY c.created_at`

	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cs []Contact
	for rows.Next() {
		c, err := scanContact(rows)
		if err != nil {
			return nil, err
		}
		cs = append(cs, *c)
	}
	return cs, rows.Err()
}

// FreqCappedContactIDs returns the subset of contactIDs that received a
// marketing outbound message in the last freqCapHours hours.
func FreqCappedContactIDs(ctx context.Context, pool *pgxpool.Pool, contactIDs []string, freqCapHours int) (map[string]bool, error) {
	if len(contactIDs) == 0 {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT cv.contact_id::text
		FROM messages m
		JOIN conversations cv ON cv.id = m.conversation_id
		WHERE m.direction = 'outbound'
		  AND m.category  = 'marketing'
		  AND m.created_at > NOW() - ($2 * INTERVAL '1 hour')
		  AND cv.contact_id = ANY($1::uuid[])
	`, contactIDs, freqCapHours)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result[id] = true
	}
	return result, rows.Err()
}

// ── Campaign report ───────────────────────────────────────────────────────────

// GetCampaignReport fetches a campaign + click stats.
func GetCampaignReport(ctx context.Context, pool *pgxpool.Pool, id string) (*CampaignReport, error) {
	c, err := GetCampaign(ctx, pool, id)
	if err != nil {
		return nil, err
	}
	var clicks int
	_ = pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(click_count), 0)
		FROM click_tracking WHERE campaign_id = $1::uuid
	`, id).Scan(&clicks)
	return &CampaignReport{Campaign: *c, ClickCount: clicks}, nil
}

// GetCampaignRecipients returns all recipients for a campaign report.
func GetCampaignRecipients(ctx context.Context, pool *pgxpool.Pool, campaignID string, limit int) ([]CampaignRecipient, error) {
	if limit == 0 {
		limit = 500
	}
	rows, err := pool.Query(ctx, `
		SELECT cr.id, cr.campaign_id::text, cr.contact_id::text,
		       cr.message_id::text, cr.status, cr.skip_reason, cr.sent_at, cr.created_at,
		       c.wa_phone, c.name
		FROM campaign_recipients cr
		JOIN contacts c ON c.id = cr.contact_id
		WHERE cr.campaign_id = $1::uuid
		ORDER BY cr.id
		LIMIT $2
	`, campaignID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rs []CampaignRecipient
	for rows.Next() {
		r, err := scanRecipient(rows)
		if err != nil {
			return nil, err
		}
		rs = append(rs, *r)
	}
	return rs, rows.Err()
}

// ListPendingRecipients returns pending recipients for a campaign (used at job-enqueue time).
func ListPendingRecipients(ctx context.Context, pool *pgxpool.Pool, campaignID string) ([]CampaignRecipient, error) {
	rows, err := pool.Query(ctx, `
		SELECT cr.id, cr.campaign_id::text, cr.contact_id::text,
		       cr.message_id::text, cr.status, cr.skip_reason, cr.sent_at, cr.created_at,
		       c.wa_phone, c.name
		FROM campaign_recipients cr
		JOIN contacts c ON c.id = cr.contact_id
		WHERE cr.campaign_id = $1::uuid AND cr.status = 'pending'
		ORDER BY cr.id
	`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rs []CampaignRecipient
	for rows.Next() {
		r, err := scanRecipient(rows)
		if err != nil {
			return nil, err
		}
		rs = append(rs, *r)
	}
	return rs, rows.Err()
}

// ── Private helpers ───────────────────────────────────────────────────────────

func scanCampaign(row rowScanner) (*Campaign, error) {
	c := &Campaign{}
	var varRaw, tagRaw, exclRaw []byte
	var scheduledAt, startedAt, completedAt pgtype.Timestamptz

	err := row.Scan(
		&c.ID, &c.Name, &c.TemplateID, &c.TemplateName, &c.TemplateLanguage, &c.Category,
		&varRaw, &tagRaw, &exclRaw,
		&c.Status,
		&scheduledAt, &startedAt, &completedAt,
		&c.TotalRecipients, &c.SentCount, &c.DeliveredCount,
		&c.ReadCount, &c.FailedCount, &c.SkippedCount,
		&c.CostTotalINR, &c.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	if scheduledAt.Valid {
		t := scheduledAt.Time
		c.ScheduledAt = &t
	}
	if startedAt.Valid {
		t := startedAt.Time
		c.StartedAt = &t
	}
	if completedAt.Valid {
		t := completedAt.Time
		c.CompletedAt = &t
	}

	// Unpack template_variables (contains both variable mapping and fallbacks).
	var raw map[string]any
	if err := json.Unmarshal(varRaw, &raw); err == nil {
		c.TemplateVariables = map[string]string{}
		c.Fallbacks = map[string]string{}
		for k, v := range raw {
			if k == "_fallbacks" {
				if fb, ok := v.(map[string]any); ok {
					for fk, fv := range fb {
						c.Fallbacks[fk] = fmt.Sprintf("%v", fv)
					}
				}
				continue
			}
			c.TemplateVariables[k] = fmt.Sprintf("%v", v)
		}
	}

	json.Unmarshal(tagRaw, &c.SegmentTags)
	json.Unmarshal(exclRaw, &c.ExcludeTags)

	if c.TemplateVariables == nil {
		c.TemplateVariables = map[string]string{}
	}
	if c.Fallbacks == nil {
		c.Fallbacks = map[string]string{}
	}
	if c.SegmentTags == nil {
		c.SegmentTags = []int64{}
	}
	if c.ExcludeTags == nil {
		c.ExcludeTags = []int64{}
	}
	return c, nil
}

func scanRecipient(row rowScanner) (*CampaignRecipient, error) {
	r := &CampaignRecipient{}
	var messageID, skipReason pgtype.Text
	var sentAt pgtype.Timestamptz

	err := row.Scan(
		&r.ID, &r.CampaignID, &r.ContactID,
		&messageID, &r.Status, &skipReason, &sentAt, &r.CreatedAt,
		&r.WAPhone, &r.Name,
	)
	if err != nil {
		return nil, err
	}
	if messageID.Valid {
		r.MessageID = &messageID.String
	}
	if skipReason.Valid {
		r.SkipReason = &skipReason.String
	}
	if sentAt.Valid {
		t := sentAt.Time
		r.SentAt = &t
	}
	return r, nil
}
