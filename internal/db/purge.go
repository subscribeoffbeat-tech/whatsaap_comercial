package db

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// StartPurgeWorker launches a background goroutine that runs runPurge once
// per day at startup and then every 24 hours.
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
	rows, err := pool.Query(ctx, "SELECT id::text FROM tenants")
	if err != nil {
		log.Printf("purge: list tenants: %v", err)
		return
	}
	defer rows.Close()

	var tenantIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			tenantIDs = append(tenantIDs, id)
		}
	}

	for _, tid := range tenantIDs {
		months, err := GetConfigInt(ctx, pool, tid, "data_retention_months")
		if err != nil || months <= 0 {
			months = 24
		}
		cutoff := time.Now().UTC().AddDate(0, -int(months), 0)
		tag, err := pool.Exec(ctx, `
			DELETE FROM conversations
			WHERE tenant_id = $2::uuid AND updated_at < $1
		`, cutoff, tid)
		if err != nil {
			log.Printf("purge tenant %s: %v", tid, err)
			continue
		}
		n := tag.RowsAffected()
		if n > 0 {
			log.Printf("purge: deleted %d conversations older than %d months for tenant %s", n, months, tid)
		}
	}
}
