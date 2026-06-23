package campaigns

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"whatsapptool/internal/db"
	"whatsapptool/internal/web/ws"
	"whatsapptool/internal/whatsapp"
)

const maxSendAttempts = 3

// ── Job args ──────────────────────────────────────────────────────────────────

// SendMessageArgs is the payload stored in the River job for each recipient.
// Template variables are pre-resolved at enqueue time.
type SendMessageArgs struct {
	RecipientRowID int64    // campaign_recipients.id
	CampaignID     string
	ContactID      string
	WAPhone        string
	TemplateName   string
	LangCode       string
	Category       string   // marketing|utility|authentication
	Params         []string // resolved body variable values, index 0 = {{1}}
	BodyText       string   // rendered message body (variables substituted) for the chat
}

func (SendMessageArgs) Kind() string { return "send_message" }

// ── Worker ────────────────────────────────────────────────────────────────────

// SendMessageWorker processes one River send-message job.
type SendMessageWorker struct {
	river.WorkerDefaults[SendMessageArgs]
	pool     *pgxpool.Pool
	waClient *whatsapp.Client
	hub      *ws.Hub
	tb       *tokenBucket
}

// NewSendMessageWorker creates a worker with a shared 80 msg/sec token bucket.
func NewSendMessageWorker(pool *pgxpool.Pool, waClient *whatsapp.Client, hub *ws.Hub) *SendMessageWorker {
	return &SendMessageWorker{
		pool:     pool,
		waClient: waClient,
		hub:      hub,
		tb:       newTokenBucket(80),
	}
}

// Work implements river.Worker. Each call handles one recipient.
func (w *SendMessageWorker) Work(ctx context.Context, job *river.Job[SendMessageArgs]) error {
	args := job.Args

	// Respect campaign control state: a cancelled campaign drops its remaining
	// queued sends; a paused one defers them until resumed.
	if camp, cerr := db.GetCampaign(ctx, w.pool, args.CampaignID); cerr == nil {
		switch camp.Status {
		case "cancelled":
			return river.JobCancel(fmt.Errorf("campaign cancelled"))
		case "paused":
			return river.JobSnooze(2 * time.Minute)
		}
	}

	// Honour quiet hours for marketing (business-initiated) sends — defer the
	// job until the window passes. This also covers campaigns enqueued just
	// before 9pm and "send now" campaigns started during quiet hours.
	if args.Category == "marketing" && IsQuietHoursCfg(ctx, w.pool, time.Now()) {
		return river.JobSnooze(15 * time.Minute)
	}

	// Daily limit guard — check before consuming the rate-limiter token.
	sent, err := db.DailyMessagesSent(ctx, w.pool)
	if err != nil {
		// Fail closed: don't risk over-sending past the cap on a DB blip.
		log.Printf("campaign worker: daily check: %v — deferring", err)
		return river.JobSnooze(5 * time.Minute)
	}
	cap := db.DailyCap(ctx, w.pool)
	if !LimitGuardCheck(sent, cap) {
		// Snooze until next day begins (10-minute polling interval).
		return river.JobSnooze(10 * time.Minute)
	}

	// Send-time compliance re-check. Consent and the frequency cap are filtered
	// when the audience is built, but jobs can sit queued for a long time across
	// quiet-hours and cap snoozes. A contact may reply STOP (opt out) or hit the
	// 24h marketing cap in that window, so re-validate per contact just before
	// sending — otherwise we'd send to someone who opted out after enqueue.
	contact, cerr := db.GetContact(ctx, w.pool, args.ContactID)
	if cerr != nil {
		// Fail closed: don't send blind if we can't confirm consent.
		log.Printf("campaign worker: consent re-check for %s: %v — deferring", args.ContactID, cerr)
		return river.JobSnooze(5 * time.Minute)
	}
	if !contact.OptedIn || contact.IsBlocked {
		w.failRecipient(ctx, args, "contact opted out / blocked before send")
		return river.JobCancel(fmt.Errorf("contact %s opted out or blocked before send", args.ContactID))
	}
	if args.Category == "marketing" {
		hours := db.FreqCapHours(ctx, w.pool)
		if capped, err := db.MarketingSentWithin(ctx, w.pool, args.ContactID, hours); err == nil && capped {
			w.failRecipient(ctx, args, fmt.Sprintf("frequency cap: already received a marketing message in the last %dh", hours))
			return river.JobCancel(fmt.Errorf("frequency cap hit for %s before send", args.ContactID))
		}
	}

	// Acquire a send token (rate limiting — blocks until a slot is available).
	if err := w.tb.Wait(ctx); err != nil {
		return err // context cancelled
	}

	// Build template body components from pre-resolved params.
	components, compErr := buildComponents(args.Params)
	if compErr != nil {
		_ = db.UpdateRecipientFailed(ctx, w.pool, args.RecipientRowID, compErr.Error())
		done, _ := db.IncrCampaignFailed(ctx, w.pool, args.CampaignID)
		w.broadcastProgress(args.CampaignID)
		if done {
			_ = db.UpdateCampaignStatus(ctx, w.pool, args.CampaignID, "completed")
		}
		return river.JobCancel(compErr)
	}

	// Call Meta API.
	waID, sendErr := w.waClient.SendTemplate(ctx, args.WAPhone, args.TemplateName, args.LangCode, components)

	if sendErr != nil {
		isPermanent := whatsapp.IsPermanent(sendErr) || job.Attempt >= maxSendAttempts
		if isPermanent {
			_ = db.UpdateRecipientFailed(ctx, w.pool, args.RecipientRowID, sendErr.Error())
			done, _ := db.IncrCampaignFailed(ctx, w.pool, args.CampaignID)
			w.broadcastProgress(args.CampaignID)
			if done {
				_ = db.UpdateCampaignStatus(ctx, w.pool, args.CampaignID, "completed")
			}
			return river.JobCancel(sendErr)
		}
		return sendErr // transient: River retries with exponential backoff
	}

	// Get or create conversation for this contact. The Meta send has ALREADY
	// succeeded by this point — returning an error here would make River retry
	// and re-send a paid message. So on failure we log, count the send, and
	// return nil (no retry) rather than risk a duplicate.
	conv, convErr := db.GetOrCreateByContact(ctx, w.pool, args.ContactID)
	if convErr != nil {
		log.Printf("campaign worker: conversation for %s (post-send, not retrying): %v", args.ContactID, convErr)
		done, _ := db.IncrCampaignSent(ctx, w.pool, args.CampaignID, 0)
		w.broadcastProgress(args.CampaignID)
		if done {
			_ = db.UpdateCampaignStatus(ctx, w.pool, args.CampaignID, "completed")
		}
		return nil
	}

	// Load rates for cost stamping.
	rates := db.LoadRates(ctx, w.pool)
	cost := CalcCost(args.Category, rates)

	// Record the message row. Store the rendered body so the chat shows the
	// actual message text (not just the template name).
	content := map[string]any{"template": args.TemplateName}
	if args.BodyText != "" {
		content["body"] = args.BodyText
	}
	msg := &db.Message{
		ConversationID: conv.ID,
		Direction:      "outbound",
		MessageType:    "template",
		Content:        content,
		WAMessageID:    &waID,
		Status:         "sent",
		Category:       &args.Category,
		CostINR:        &cost,
		CampaignID:     &args.CampaignID,
	}
	if err := db.InsertMessage(ctx, w.pool, msg); err != nil {
		log.Printf("campaign worker: insert message: %v", err)
		// Non-fatal: the send already happened; continue with status update.
	}
	_ = db.TouchLastMessage(ctx, w.pool, conv.ID, msg.CreatedAt)

	// Update recipient and campaign counters.
	_ = db.UpdateRecipientSent(ctx, w.pool, args.RecipientRowID, msg.ID)
	done, _ := db.IncrCampaignSent(ctx, w.pool, args.CampaignID, cost)
	w.broadcastProgress(args.CampaignID)
	if done {
		_ = db.UpdateCampaignStatus(ctx, w.pool, args.CampaignID, "completed")
	}

	return nil
}

// failRecipient marks a recipient failed with a reason, bumps the campaign
// failed counter, broadcasts progress, and completes the campaign if this was
// the last outstanding send.
func (w *SendMessageWorker) failRecipient(ctx context.Context, args SendMessageArgs, reason string) {
	_ = db.UpdateRecipientFailed(ctx, w.pool, args.RecipientRowID, reason)
	done, _ := db.IncrCampaignFailed(ctx, w.pool, args.CampaignID)
	w.broadcastProgress(args.CampaignID)
	if done {
		_ = db.UpdateCampaignStatus(ctx, w.pool, args.CampaignID, "completed")
	}
}

func (w *SendMessageWorker) broadcastProgress(campaignID string) {
	if w.hub == nil {
		return
	}
	w.hub.BroadcastAll(ws.Event{
		Type: ws.EventCampaignProgress,
		Data: map[string]any{"campaign_id": campaignID},
	})
}

// templateBodyText extracts the BODY text (with {{N}} placeholders) from a template.
func templateBodyText(t *db.Template) string {
	for _, c := range t.Components {
		if s, _ := c["type"].(string); strings.EqualFold(s, "BODY") {
			if txt, ok := c["text"].(string); ok {
				return txt
			}
		}
	}
	return ""
}

// renderTemplateBody substitutes resolved params into the raw body, producing the
// exact text the recipient sees — stored on the message row for the chat thread.
func renderTemplateBody(raw string, params []string) string {
	if raw == "" {
		return ""
	}
	for i, p := range params {
		raw = strings.ReplaceAll(raw, fmt.Sprintf("{{%d}}", i+1), p)
	}
	return raw
}

func buildComponents(params []string) ([]whatsapp.TemplateComponent, error) {
	if len(params) == 0 {
		return nil, nil
	}
	ps := make([]whatsapp.TemplateParameter, len(params))
	for i, p := range params {
		if p == "" {
			return nil, fmt.Errorf("variable {{%d}} resolved to empty string — set a fallback when building the campaign", i+1)
		}
		ps[i] = whatsapp.TemplateParameter{Type: "text", Text: p}
	}
	return []whatsapp.TemplateComponent{
		{Type: "body", Parameters: ps},
	}, nil
}

// ── Token bucket ──────────────────────────────────────────────────────────────

type tokenBucket struct {
	tokens chan struct{}
	done   chan struct{}
}

func newTokenBucket(rps int) *tokenBucket {
	tb := &tokenBucket{
		tokens: make(chan struct{}, rps),
		done:   make(chan struct{}),
	}
	go func() {
		ticker := time.NewTicker(time.Second / time.Duration(rps))
		defer ticker.Stop()
		for {
			select {
			case <-tb.done:
				return
			case <-ticker.C:
				select {
				case tb.tokens <- struct{}{}:
				default: // bucket full — skip this tick
				}
			}
		}
	}()
	return tb
}

func (tb *tokenBucket) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-tb.tokens:
		return nil
	}
}

// ── River setup helpers ───────────────────────────────────────────────────────

// RunRiverMigrations creates or upgrades River's internal tables in Postgres.
func RunRiverMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), &rivermigrate.Config{})
	if err != nil {
		return fmt.Errorf("river migrator: %w", err)
	}
	_, err = migrator.Migrate(ctx, rivermigrate.DirectionUp, &rivermigrate.MigrateOpts{})
	return err
}

// NewRiverClient creates a River client that can run the SendMessageWorker.
func NewRiverClient(pool *pgxpool.Pool, waClient *whatsapp.Client, hub *ws.Hub) (*river.Client[pgx.Tx], error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, NewSendMessageWorker(pool, waClient, hub))

	return river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 10},
		},
		Workers: workers,
	})
}

// EnqueueCampaignJobs inserts one River job per pending campaign recipient.
// All inserts happen in a single transaction alongside the recipient rows.
func EnqueueCampaignJobs(ctx context.Context, pool *pgxpool.Pool, rc *river.Client[pgx.Tx],
	campaign db.Campaign, recipients []db.CampaignRecipient,
) error {
	// Build contact map for variable resolution.
	ids := make([]string, len(recipients))
	for i, r := range recipients {
		ids[i] = r.ContactID
	}
	contactMap, err := db.GetContactsByIDs(ctx, pool, ids)
	if err != nil {
		return fmt.Errorf("load contacts for variable resolution: %w", err)
	}

	// Fetch the template's BODY text once so we can render the per-recipient
	// message and store it on each message row (for the chat thread).
	rawBody := ""
	if tmpl, terr := db.GetTemplate(ctx, pool, campaign.TemplateID); terr == nil {
		rawBody = templateBodyText(tmpl)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	for _, r := range recipients {
		contact := contactMap[r.ContactID]
		params := ResolveVars(contact, campaign.TemplateVariables, campaign.Fallbacks)

		args := SendMessageArgs{
			RecipientRowID: r.ID,
			CampaignID:     campaign.ID,
			ContactID:      r.ContactID,
			WAPhone:        r.WAPhone,
			TemplateName:   campaign.TemplateName,
			LangCode:       campaign.TemplateLanguage,
			Category:       campaign.Category,
			Params:         params,
			BodyText:       renderTemplateBody(rawBody, params),
		}

		if _, err := rc.InsertTx(ctx, tx, args, &river.InsertOpts{
			MaxAttempts: maxSendAttempts,
		}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
