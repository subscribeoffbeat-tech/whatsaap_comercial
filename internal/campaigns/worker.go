package campaigns

import (
	"context"
	"fmt"
	"log"
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

	// Daily limit guard — check before consuming the rate-limiter token.
	sent, err := db.DailyMessagesSent(ctx, w.pool)
	if err != nil {
		log.Printf("campaign worker: daily check: %v", err)
	}
	cap := db.DailyCap(ctx, w.pool)
	if !LimitGuardCheck(sent, cap) {
		// Snooze until next day begins (10-minute polling interval).
		return river.JobSnooze(10 * time.Minute)
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

	// Get or create conversation for this contact.
	conv, convErr := db.GetOrCreateByContact(ctx, w.pool, args.ContactID)
	if convErr != nil {
		log.Printf("campaign worker: conversation for %s: %v", args.ContactID, convErr)
		return convErr
	}

	// Load rates for cost stamping.
	rates := db.LoadRates(ctx, w.pool)
	cost := CalcCost(args.Category, rates)

	// Record the message row.
	msg := &db.Message{
		ConversationID: conv.ID,
		Direction:      "outbound",
		MessageType:    "template",
		Content:        map[string]any{"template": args.TemplateName},
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

	// Update recipient and campaign counters.
	_ = db.UpdateRecipientSent(ctx, w.pool, args.RecipientRowID, msg.ID)
	done, _ := db.IncrCampaignSent(ctx, w.pool, args.CampaignID, cost)
	w.broadcastProgress(args.CampaignID)
	if done {
		_ = db.UpdateCampaignStatus(ctx, w.pool, args.CampaignID, "completed")
	}

	return nil
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
		}

		if _, err := rc.InsertTx(ctx, tx, args, &river.InsertOpts{
			MaxAttempts: maxSendAttempts,
		}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
