package campaigns

import (
	"context"
	"log"
	"time"

	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"whatsapptool/internal/db"
	"whatsapptool/internal/web/ws"
)

// StartScheduledDispatcher launches a background loop that starts scheduled
// campaigns when their time arrives: it flips them to running and enqueues the
// River send jobs for their pending recipients. Runs until ctx is cancelled.
func StartScheduledDispatcher(ctx context.Context, pool *pgxpool.Pool, rc *river.Client[pgx.Tx], hub *ws.Hub) {
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		// Run once shortly after boot so a due campaign isn't delayed a full minute.
		time.Sleep(5 * time.Second)
		dispatchDue(ctx, pool, rc)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				dispatchDue(ctx, pool, rc)
			}
		}
	}()
	log.Printf("scheduled-campaign dispatcher started (60s interval)")
}

func dispatchDue(ctx context.Context, pool *pgxpool.Pool, rc *river.Client[pgx.Tx]) {
	ids, err := db.ListDueScheduledCampaigns(ctx, pool, time.Now())
	if err != nil {
		log.Printf("dispatcher: list due campaigns: %v", err)
		return
	}
	for _, id := range ids {
		c, err := db.GetCampaign(ctx, pool, id)
		if err != nil {
			log.Printf("dispatcher: load campaign %s: %v", id, err)
			continue
		}
		// Honour the configured quiet hours for marketing — defer to a later tick.
		if c.Category == "marketing" && IsQuietHoursCfg(ctx, pool, time.Now()) {
			continue
		}
		pending, err := db.ListPendingRecipients(ctx, pool, id)
		if err != nil {
			log.Printf("dispatcher: pending recipients %s: %v", id, err)
			continue
		}
		if len(pending) == 0 {
			_ = db.UpdateCampaignStatus(ctx, pool, id, "completed")
			continue
		}
		if err := db.UpdateCampaignStatus(ctx, pool, id, "running"); err != nil {
			log.Printf("dispatcher: mark running %s: %v", id, err)
			continue
		}
		if err := EnqueueCampaignJobs(ctx, pool, rc, *c, pending); err != nil {
			log.Printf("dispatcher: enqueue %s: %v", id, err)
			continue
		}
		log.Printf("dispatcher: started scheduled campaign %s (%d recipients)", id, len(pending))
	}
}
