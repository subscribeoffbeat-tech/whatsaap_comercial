package db

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ── Overview stats ────────────────────────────────────────────────────────────

// OverviewStats holds aggregate message delivery counts for a date range.
type OverviewStats struct {
	Sent      int64
	Delivered int64
	Read      int64
	Failed    int64
	CostINR   float64
}

// GetOverviewStats returns outbound message stats between from and to.
func GetOverviewStats(ctx context.Context, pool *pgxpool.Pool, from, to time.Time) (OverviewStats, error) {
	var s OverviewStats
	err := pool.QueryRow(ctx, `
		SELECT
		  COUNT(*) FILTER (WHERE status IN ('sent','delivered','read'))        AS sent,
		  COUNT(*) FILTER (WHERE status = 'delivered')                         AS delivered,
		  COUNT(*) FILTER (WHERE status = 'read')                              AS read,
		  COUNT(*) FILTER (WHERE status = 'failed')                            AS failed,
		  COALESCE(SUM(cost_inr), 0)                                           AS cost_inr
		FROM messages
		WHERE direction = 'outbound'
		  AND created_at >= $1 AND created_at < $2
	`, from, to).Scan(&s.Sent, &s.Delivered, &s.Read, &s.Failed, &s.CostINR)
	return s, err
}

// GetAgentOverviewStats returns the same shape as GetOverviewStats but scoped
// to messages sent by a specific agent (sent_by = agentID). Cost is excluded
// because agents should not see financial data.
func GetAgentOverviewStats(ctx context.Context, pool *pgxpool.Pool, from, to time.Time, agentID string) (OverviewStats, error) {
	var s OverviewStats
	err := pool.QueryRow(ctx, `
		SELECT
		  COUNT(*) FILTER (WHERE status IN ('sent','delivered','read'))  AS sent,
		  COUNT(*) FILTER (WHERE status = 'delivered')                   AS delivered,
		  COUNT(*) FILTER (WHERE status = 'read')                        AS read,
		  COUNT(*) FILTER (WHERE status = 'failed')                      AS failed,
		  0::numeric                                                      AS cost_inr
		FROM messages
		WHERE direction = 'outbound'
		  AND sent_by = $3::uuid
		  AND created_at >= $1 AND created_at < $2
	`, from, to, agentID).Scan(&s.Sent, &s.Delivered, &s.Read, &s.Failed, &s.CostINR)
	return s, err
}

// ── Messages per day ──────────────────────────────────────────────────────────

// DayStat holds outbound message counts for one calendar day.
type DayStat struct {
	Day       time.Time
	Sent      int64
	Failed    int64
}

// MessagesPerDay returns daily outbound counts between from and to.
func MessagesPerDay(ctx context.Context, pool *pgxpool.Pool, from, to time.Time) ([]DayStat, error) {
	rows, err := pool.Query(ctx, `
		SELECT
		  DATE_TRUNC('day', created_at AT TIME ZONE 'Asia/Kolkata') AS day,
		  COUNT(*) FILTER (WHERE status IN ('sent','delivered','read')) AS sent,
		  COUNT(*) FILTER (WHERE status = 'failed')                    AS failed
		FROM messages
		WHERE direction = 'outbound'
		  AND created_at >= $1 AND created_at < $2
		GROUP BY 1
		ORDER BY 1
	`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var stats []DayStat
	for rows.Next() {
		var s DayStat
		if err := rows.Scan(&s.Day, &s.Sent, &s.Failed); err != nil {
			return nil, err
		}
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

// ── Cost breakdown ─────────────────────────────────────────────────────────────

// CategoryCost holds total spend per message category.
type CategoryCost struct {
	Category string
	Count    int64
	CostINR  float64
}

// CostByCategory returns cost breakdown by category for the date range.
func CostByCategory(ctx context.Context, pool *pgxpool.Pool, from, to time.Time) ([]CategoryCost, error) {
	rows, err := pool.Query(ctx, `
		SELECT
		  COALESCE(category, 'unknown') AS category,
		  COUNT(*)                       AS cnt,
		  COALESCE(SUM(cost_inr), 0)    AS cost_inr
		FROM messages
		WHERE direction = 'outbound'
		  AND created_at >= $1 AND created_at < $2
		GROUP BY 1
		ORDER BY cost_inr DESC
	`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CategoryCost
	for rows.Next() {
		var c CategoryCost
		if err := rows.Scan(&c.Category, &c.Count, &c.CostINR); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CampaignCost holds cost per campaign.
type CampaignCost struct {
	CampaignID   string
	CampaignName string
	Count        int64
	CostINR      float64
}

// CostByCampaign returns cost breakdown by campaign for the date range.
func CostByCampaign(ctx context.Context, pool *pgxpool.Pool, from, to time.Time) ([]CampaignCost, error) {
	rows, err := pool.Query(ctx, `
		SELECT
		  m.campaign_id::text,
		  COALESCE(c.name, 'Unknown')    AS campaign_name,
		  COUNT(*)                        AS cnt,
		  COALESCE(SUM(m.cost_inr), 0)   AS cost_inr
		FROM messages m
		LEFT JOIN campaigns c ON c.id = m.campaign_id
		WHERE m.direction = 'outbound'
		  AND m.campaign_id IS NOT NULL
		  AND m.created_at >= $1 AND m.created_at < $2
		GROUP BY 1, 2
		ORDER BY cost_inr DESC
		LIMIT 20
	`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CampaignCost
	for rows.Next() {
		var c CampaignCost
		if err := rows.Scan(&c.CampaignID, &c.CampaignName, &c.Count, &c.CostINR); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ── Quality / tier ────────────────────────────────────────────────────────────

// QualityInfo holds the current tier, daily usage, and quality rating.
type QualityInfo struct {
	Tier        int64   // meta_tier from app_config
	DailyCap    int64   // daily_message_cap from app_config
	SentToday   int64   // outbound messages since midnight UTC
	QualityRating string // green|yellow|red (from app_config or computed)
}

// GetQualityInfo loads tier + usage + quality rating.
func GetQualityInfo(ctx context.Context, pool *pgxpool.Pool) (QualityInfo, error) {
	var q QualityInfo
	if v, err := GetConfigInt(ctx, pool, "meta_tier"); err == nil {
		q.Tier = v
	}
	q.DailyCap = DailyCap(ctx, pool)
	sent, _ := DailyMessagesSent(ctx, pool)
	q.SentToday = sent

	// Read quality_rating as a string from app_config.
	// Meta sets this via the phone_number_quality_update webhook ("green"/"yellow"/"red").
	// Never infer from usage — that is not Meta's signal.
	var rawQ json.RawMessage
	if err2 := pool.QueryRow(ctx,
		`SELECT value FROM app_config WHERE key = 'quality_rating'`,
	).Scan(&rawQ); err2 == nil {
		var qs string
		if json.Unmarshal(rawQ, &qs) == nil {
			switch qs {
			case "green", "yellow", "red":
				q.QualityRating = qs
			default:
				log.Printf("dashboard: quality_rating unexpected value %q — showing Unknown", qs)
				q.QualityRating = "unknown"
			}
		} else {
			log.Printf("dashboard: quality_rating value is not a string — showing Unknown")
			q.QualityRating = "unknown"
		}
	} else {
		log.Printf("dashboard: quality_rating key missing from app_config — showing Unknown; update via Meta phone_number_quality_update webhook")
		q.QualityRating = "unknown"
	}
	return q, nil
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// ── Agent performance ─────────────────────────────────────────────────────────

// AgentStat holds per-agent performance numbers.
type AgentStat struct {
	AgentID        string
	AgentName      string
	MessagesSent   int64
	ConvsResolved  int64
}

// AgentPerformance returns per-agent stats for the date range.
func AgentPerformance(ctx context.Context, pool *pgxpool.Pool, from, to time.Time) ([]AgentStat, error) {
	rows, err := pool.Query(ctx, `
		SELECT
		  a.id::text,
		  a.name,
		  COUNT(DISTINCT m.id) FILTER (WHERE m.direction='outbound' AND m.created_at >= $1 AND m.created_at < $2) AS messages_sent,
		  COUNT(DISTINCT conv.id) FILTER (WHERE conv.status='closed' AND conv.updated_at >= $1 AND conv.updated_at < $2) AS convs_resolved
		FROM agents a
		LEFT JOIN conversations conv ON conv.assigned_to = a.id
		LEFT JOIN messages m ON m.sent_by = a.id
		GROUP BY a.id, a.name
		ORDER BY messages_sent DESC
	`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var stats []AgentStat
	for rows.Next() {
		var s AgentStat
		if err := rows.Scan(&s.AgentID, &s.AgentName, &s.MessagesSent, &s.ConvsResolved); err != nil {
			return nil, err
		}
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

// ── Dashboard quick stats ─────────────────────────────────────────────────────

// DashboardStats holds the numbers shown on the home dashboard.
type DashboardStats struct {
	SentToday      int64
	DeliveredToday int64
	ChatsWaiting   int64   // open conversations not assigned
	CostThisMonth  float64
	QualityRating  string
	DailyCap       int64   // 90%-of-tier cap — same source as WizardAudience and LimitGuardCheck
	CapUsedToday   int64   // ALL outbound today regardless of status — matches enforcement counter
}

// GetDashboardStats returns the quick-stat numbers for the dashboard.
func GetDashboardStats(ctx context.Context, pool *pgxpool.Pool) (DashboardStats, error) {
	var s DashboardStats

	// Sent + delivered today (midnight UTC)
	err := pool.QueryRow(ctx, `
		SELECT
		  COUNT(*) FILTER (WHERE status IN ('sent','delivered','read')),
		  COUNT(*) FILTER (WHERE status = 'delivered' OR status = 'read')
		FROM messages
		WHERE direction = 'outbound'
		  AND created_at >= CURRENT_DATE
	`).Scan(&s.SentToday, &s.DeliveredToday)
	if err != nil {
		return s, err
	}

	// Unassigned open conversations
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM conversations
		WHERE status = 'open' AND assigned_to IS NULL
	`).Scan(&s.ChatsWaiting)
	if err != nil {
		return s, err
	}

	// Cost this calendar month
	err = pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(cost_inr), 0)
		FROM messages
		WHERE direction = 'outbound'
		  AND created_at >= DATE_TRUNC('month', NOW())
	`).Scan(&s.CostThisMonth)
	if err != nil {
		return s, err
	}

	// Quality rating + daily cap (qi.DailyCap already calls DailyCap(ctx,pool) — same source as WizardAudience)
	qi, _ := GetQualityInfo(ctx, pool)
	s.QualityRating = qi.QualityRating
	s.DailyCap = qi.DailyCap
	s.CapUsedToday = qi.SentToday // reuse count already fetched by GetQualityInfo

	return s, nil
}
