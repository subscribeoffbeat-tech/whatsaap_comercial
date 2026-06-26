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
	TemplateLanguage  string            // joined from templates
	Category          string            // joined from templates
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
	// Header media (only for templates with an IMAGE/VIDEO/DOCUMENT header).
	HeaderMediaPath string // local file path of the uploaded media
	HeaderMediaID   string // reusable Meta media ID for sending
	HeaderMediaType string // image | video | document
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

// SetCampaignHeaderMedia stores the uploaded header media's local path, Meta
// media ID, and type on the campaign. Called after the media is stored on disk
// and uploaded to Meta.
func SetCampaignHeaderMedia(ctx context.Context, pool *pgxpool.Pool, id, path, mediaID, mediaType string) error {
	_, err := pool.Exec(ctx, `
		UPDATE campaigns
		SET header_media_path = $2, header_media_id = $3, header_media_type = $4, updated_at = NOW()
		WHERE id = $1::uuid
	`, id, path, mediaID, mediaType)
	return err
}

// SetCampaignHeaderMediaID updates only the Meta media ID (used when the worker
// re-uploads from the stored file after the previous ID went stale).
func SetCampaignHeaderMediaID(ctx context.Context, pool *pgxpool.Pool, id, mediaID string) error {
	_, err := pool.Exec(ctx, `
		UPDATE campaigns SET header_media_id = $2, updated_at = NOW() WHERE id = $1::uuid
	`, id, mediaID)
	return err
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
		       c.cost_total_inr, c.created_at,
		       COALESCE(c.header_media_path,''), COALESCE(c.header_media_id,''), COALESCE(c.header_media_type,'')
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
		       c.cost_total_inr, c.created_at,
		       COALESCE(c.header_media_path,''), COALESCE(c.header_media_id,''), COALESCE(c.header_media_type,'')
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

// ListRecentCampaigns returns the n most recent campaigns. Use this for
// dashboard snippets where fetching all 200 rows would waste DB bandwidth.
// ListCampaigns (LIMIT 200) is unchanged and used by the campaigns list page.
func ListRecentCampaigns(ctx context.Context, pool *pgxpool.Pool, n int) ([]Campaign, error) {
	rows, err := pool.Query(ctx, `
		SELECT c.id::text, c.name, c.template_id::text, t.name, t.language, t.category,
		       c.template_variables, c.segment_tags, c.exclude_tags,
		       c.status,
		       c.scheduled_at, c.started_at, c.completed_at,
		       c.total_recipients, c.sent_count, c.delivered_count,
		       c.read_count, c.failed_count, c.skipped_count,
		       c.cost_total_inr, c.created_at,
		       COALESCE(c.header_media_path,''), COALESCE(c.header_media_id,''), COALESCE(c.header_media_type,'')
		FROM campaigns c
		JOIN templates t ON t.id = c.template_id
		ORDER BY c.created_at DESC
		LIMIT $1
	`, n)
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

// DeleteCampaign removes a campaign. Its recipients cascade-delete; sent message
// rows keep their history (the FK sets their campaign_id to NULL).
func DeleteCampaign(ctx context.Context, pool *pgxpool.Pool, id string) error {
	_, err := pool.Exec(ctx, `DELETE FROM campaigns WHERE id = $1::uuid`, id)
	return err
}

// DeleteRecipients removes all recipients of a campaign (used before recreating
// them when an edited draft's audience changes).
func DeleteRecipients(ctx context.Context, pool *pgxpool.Pool, campaignID string) error {
	_, err := pool.Exec(ctx, `DELETE FROM campaign_recipients WHERE campaign_id = $1::uuid`, campaignID)
	return err
}

// UpdateCampaignConfig overwrites an existing campaign's editable configuration
// (used when saving an edited draft).
func UpdateCampaignConfig(ctx context.Context, pool *pgxpool.Pool, c *Campaign) error {
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
	_, err := pool.Exec(ctx, `
		UPDATE campaigns SET
		    name = $2, template_id = $3::uuid, template_variables = $4,
		    segment_tags = $5, exclude_tags = $6, status = $7,
		    scheduled_at = $8, total_recipients = $9, updated_at = NOW()
		WHERE id = $1::uuid
	`, c.ID, c.Name, c.TemplateID, varJSON, tagJSON, exclJSON, c.Status, c.ScheduledAt, c.TotalRecipients)
	return err
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

// IncrCampaignSkipped atomically increments skipped_count and returns done flag.
// Used for send-time compliance skips (contact opted out / hit the frequency cap
// after the campaign was queued) so they don't inflate the failed count.
func IncrCampaignSkipped(ctx context.Context, pool *pgxpool.Pool, id string) (bool, error) {
	var done bool
	err := pool.QueryRow(ctx, `
		UPDATE campaigns SET
		    skipped_count = skipped_count + 1,
		    updated_at    = NOW()
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

// UpdateRecipientSkipped marks a recipient as skipped with a reason string.
// Used for send-time compliance skips (opt-out / frequency cap after enqueue),
// distinct from a genuine send failure.
func UpdateRecipientSkipped(ctx context.Context, pool *pgxpool.Pool, id int64, reason string) error {
	_, err := pool.Exec(ctx, `
		UPDATE campaign_recipients
		SET status = 'skipped', skip_reason = $2
		WHERE id = $1
	`, id, reason)
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

// GetAudienceContactsByIDs returns the opted-in, non-blocked contacts among the
// given IDs (used when the owner refines the audience down to specific contacts).
func GetAudienceContactsByIDs(ctx context.Context, pool *pgxpool.Pool, ids []string) ([]Contact, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	q := `SELECT c.id::text, c.wa_phone, c.name, c.email, c.industry, c.custom_fields::text,
	             c.opted_in, c.opt_in_source, c.opt_in_at, c.opt_out_at, c.is_blocked,
	             c.created_at, c.updated_at
	      FROM contacts c
	      WHERE c.opted_in = true AND c.is_blocked = false AND c.id = ANY($1::uuid[])
	      ORDER BY c.created_at`
	rows, err := pool.Query(ctx, q, ids)
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

// GetAudienceContactsAny returns opted-in, non-blocked contacts with ANY of the
// given tag IDs. If tagIDs is nil or empty, all opted-in non-blocked contacts
// are returned (equivalent to "All contacts").
func GetAudienceContactsAny(ctx context.Context, pool *pgxpool.Pool, tagIDs []int64) ([]Contact, error) {
	conds := []string{"c.opted_in = true", "c.is_blocked = false"}
	var args []any
	if len(tagIDs) > 0 {
		args = append(args, tagIDs)
		conds = append(conds, `EXISTS (SELECT 1 FROM contact_tags ct WHERE ct.contact_id = c.id AND ct.tag_id = ANY($1::bigint[]))`)
	}
	q := `SELECT c.id::text, c.wa_phone, c.name, c.email, c.industry, c.custom_fields::text,
	             c.opted_in, c.opt_in_source, c.opt_in_at, c.opt_out_at, c.is_blocked,
	             c.created_at, c.updated_at
	      FROM contacts c
	      WHERE ` + strings.Join(conds, " AND ") + ` ORDER BY c.created_at`
	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("get audience contacts any: %w", err)
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

// MarketingSentWithin reports whether a single contact received a marketing
// outbound message in the last freqCapHours hours. Used as a send-time re-check
// in the worker: the audience-build frequency filter can go stale while jobs
// sit queued across quiet-hours/cap snoozes.
func MarketingSentWithin(ctx context.Context, pool *pgxpool.Pool, contactID string, freqCapHours int) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM messages m
			JOIN conversations cv ON cv.id = m.conversation_id
			WHERE m.direction = 'outbound'
			  AND m.category  = 'marketing'
			  AND m.created_at > NOW() - ($2 * INTERVAL '1 hour')
			  AND cv.contact_id = $1::uuid
		)
	`, contactID, freqCapHours).Scan(&exists)
	return exists, err
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

// ListDueScheduledCampaigns returns the IDs of campaigns whose scheduled time
// has arrived and are still in 'scheduled' status — for the dispatcher to start.
func ListDueScheduledCampaigns(ctx context.Context, pool *pgxpool.Pool, now time.Time) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT id::text FROM campaigns
		WHERE status = 'scheduled'
		  AND scheduled_at IS NOT NULL
		  AND scheduled_at <= $1
		ORDER BY scheduled_at ASC
		LIMIT 50
	`, now)
	if err != nil {
		return nil, fmt.Errorf("list due scheduled campaigns: %w", err)
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

// SyncCampaignStats recomputes a campaign's delivery counters from the messages
// table (the source of truth, updated by delivery-status webhooks) and writes
// each recipient's delivery status + failure reason back to campaign_recipients,
// so the report shows accurate numbers and per-contact failure reasons.
func SyncCampaignStats(ctx context.Context, pool *pgxpool.Pool, campaignID string) error {
	if _, err := pool.Exec(ctx, `
		UPDATE campaigns SET
		  sent_count      = sub.sent,
		  delivered_count = sub.delivered,
		  read_count      = sub.rd,
		  failed_count    = sub.failed,
		  cost_total_inr  = sub.cost
		FROM (
		  SELECT
		    COUNT(*) FILTER (WHERE status IN ('sent','delivered','read')) AS sent,
		    COUNT(*) FILTER (WHERE status IN ('delivered','read'))        AS delivered,
		    COUNT(*) FILTER (WHERE status = 'read')                       AS rd,
		    COUNT(*) FILTER (WHERE status = 'failed')                     AS failed,
		    COALESCE(SUM(cost_inr) FILTER (WHERE status <> 'failed'), 0)  AS cost
		  FROM messages
		  WHERE campaign_id = $1::uuid AND direction = 'outbound'
		) sub
		WHERE id = $1::uuid
	`, campaignID); err != nil {
		return fmt.Errorf("sync campaign counters: %w", err)
	}

	// Propagate each outbound message's delivery status + failure reason to its
	// recipient row (matched via conversation → contact). For failures, store
	// "code message" so the report can categorise and explain it.
	if _, err := pool.Exec(ctx, `
		UPDATE campaign_recipients cr SET
		  status      = m.status,
		  skip_reason = CASE WHEN m.status = 'failed'
		                  THEN NULLIF(TRIM(COALESCE(m.error_code,'') || ' ' || COALESCE(m.error_message,'')), '')
		                  ELSE cr.skip_reason END
		FROM messages m
		JOIN conversations conv ON conv.id = m.conversation_id
		WHERE m.campaign_id = $1::uuid
		  AND m.direction = 'outbound'
		  AND m.status IN ('delivered','read','failed')
		  AND cr.campaign_id = $1::uuid
		  AND cr.contact_id = conv.contact_id
	`, campaignID); err != nil {
		return fmt.Errorf("sync recipient statuses: %w", err)
	}
	return nil
}

// GetMessageCampaignID returns the campaign_id of an outbound message by its
// wa_message_id, and false if the message has no campaign (e.g. a normal chat).
func GetMessageCampaignID(ctx context.Context, pool *pgxpool.Pool, waMessageID string) (string, bool) {
	var cid pgtype.Text
	if err := pool.QueryRow(ctx,
		`SELECT campaign_id::text FROM messages WHERE wa_message_id = $1`, waMessageID,
	).Scan(&cid); err != nil {
		return "", false
	}
	if !cid.Valid || cid.String == "" {
		return "", false
	}
	return cid.String, true
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

// FailedRecipient is a failed or skipped campaign recipient with categorised reason.
type FailedRecipient struct {
	ContactID    string
	Name         string
	WAPhone      string
	FailReason   string
	FailCategory string // opted_out | invalid_number | limit_reached | blocked | rejected
	FailedAt     *time.Time
}

// CategorizeFail maps a raw skip_reason string to a display category.
func CategorizeFail(reason string) string {
	r := strings.ToLower(reason)
	switch {
	case strings.Contains(r, "opted") || strings.Contains(r, "stop") || strings.Contains(r, "opt_out"):
		return "opted_out"
	case strings.Contains(r, "131026") || strings.Contains(r, "invalid") ||
		strings.Contains(r, "not registered") || strings.Contains(r, "undeliverable") ||
		strings.Contains(r, "deactivat") || strings.Contains(r, "ported"):
		return "invalid_number"
	case strings.Contains(r, "131049") || strings.Contains(r, "131048") ||
		strings.Contains(r, "limit") || strings.Contains(r, "cap") || strings.Contains(r, "ecosystem"):
		return "limit_reached"
	case strings.Contains(r, "blocked") || strings.Contains(r, "131031"):
		return "blocked"
	default:
		return "rejected"
	}
}

// GetFailedRecipients returns failed and skipped recipients for a campaign.
func GetFailedRecipients(ctx context.Context, pool *pgxpool.Pool, campaignID string) ([]FailedRecipient, error) {
	rows, err := pool.Query(ctx, `
		SELECT c.id::text, c.name, c.wa_phone,
		       COALESCE(cr.skip_reason, ''), cr.sent_at
		FROM campaign_recipients cr
		JOIN contacts c ON c.id = cr.contact_id
		WHERE cr.campaign_id = $1::uuid
		  AND cr.status IN ('failed', 'skipped')
		  AND cr.skip_reason IS NOT NULL AND cr.skip_reason <> ''
		ORDER BY cr.id
		LIMIT 500
	`, campaignID)
	if err != nil {
		return nil, fmt.Errorf("get failed recipients: %w", err)
	}
	defer rows.Close()
	var out []FailedRecipient
	for rows.Next() {
		var fr FailedRecipient
		var sentAt pgtype.Timestamptz
		if err := rows.Scan(&fr.ContactID, &fr.Name, &fr.WAPhone, &fr.FailReason, &sentAt); err != nil {
			return nil, err
		}
		if sentAt.Valid {
			t := sentAt.Time
			fr.FailedAt = &t
		}
		fr.FailCategory = CategorizeFail(fr.FailReason)
		out = append(out, fr)
	}
	return out, rows.Err()
}

// HourlyCount is one bar in the send-distribution chart.
type HourlyCount struct {
	Hour  int
	Count int
}

// GetHourlySendDistribution returns send counts grouped by hour for a campaign.
func GetHourlySendDistribution(ctx context.Context, pool *pgxpool.Pool, campaignID string) ([]HourlyCount, error) {
	rows, err := pool.Query(ctx, `
		SELECT EXTRACT(HOUR FROM sent_at AT TIME ZONE 'Asia/Kolkata')::int, COUNT(*)
		FROM campaign_recipients
		WHERE campaign_id = $1::uuid AND sent_at IS NOT NULL
		GROUP BY 1 ORDER BY 1
	`, campaignID)
	if err != nil {
		return nil, fmt.Errorf("hourly distribution: %w", err)
	}
	defer rows.Close()
	var out []HourlyCount
	for rows.Next() {
		var hc HourlyCount
		if err := rows.Scan(&hc.Hour, &hc.Count); err != nil {
			return nil, err
		}
		out = append(out, hc)
	}
	return out, rows.Err()
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
		&c.HeaderMediaPath, &c.HeaderMediaID, &c.HeaderMediaType,
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
