package db

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PurgeOldData deletes conversations (and their messages via CASCADE) that are
// older than retentionMonths. Returns the number of conversations deleted.
func PurgeOldData(ctx context.Context, pool *pgxpool.Pool, retentionMonths int64) (int64, error) {
	cutoff := time.Now().UTC().AddDate(0, -int(retentionMonths), 0)
	tag, err := pool.Exec(ctx, `
		DELETE FROM conversations
		WHERE updated_at < $1
	`, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// StartPurgeWorker launches a background goroutine that runs PurgeOldData once
// per day at startup and then every 24 hours. retentionMonths is re-read from
// app_config on each run so changes in Settings take effect without restart.
func StartPurgeWorker(ctx context.Context, pool *pgxpool.Pool) {
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		runPurge(ctx, pool)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runPurge(ctx, pool)
			}
		}
	}()
}

func runPurge(ctx context.Context, pool *pgxpool.Pool) {
	months, err := GetConfigInt(ctx, pool, "data_retention_months")
	if err != nil || months <= 0 {
		months = 24
	}
	n, err := PurgeOldData(ctx, pool, months)
	if err != nil {
		log.Printf("purge: %v", err)
		return
	}
	if n > 0 {
		log.Printf("purge: deleted %d conversations older than %d months", n, months)
	}
}
