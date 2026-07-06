package db

import (
	"context"
	"encoding/json"
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
	tp := TenantParam(ctx)
	err := pool.QueryRow(ctx, `
		SELECT
		  COUNT(*) FILTER (WHERE m.status IN ('sent','delivered','read'))        AS sent,
		  COUNT(*) FILTER (WHERE m.status = 'delivered')                         AS delivered,
		  COUNT(*) FILTER (WHERE m.status = 'read')                              AS read,
		  COUNT(*) FILTER (WHERE m.status = 'failed')                            AS failed,
		  COALESCE(SUM(m.cost_inr), 0)                                           AS cost_inr
		FROM messages m
		JOIN conversations conv ON conv.id = m.conversation_id
		WHERE m.direction = 'outbound'
		  AND ($3::text IS NULL OR conv.tenant_id = $3::uuid)
		  AND m.created_at >= $1 AND m.created_at < $2
	`, from, to, tp).Scan(&s.Sent, &s.Delivered, &s.Read, &s.Failed, &s.CostINR)
	return s, err
}

// GetAgentOverviewStats returns the same shape as GetOverviewStats but scoped
// to messages sent by a specific agent.
func GetAgentOverviewStats(ctx context.Context, pool *pgxpool.Pool, from, to time.Time, agentID string) (OverviewStats, error) {
	var s OverviewStats
	tp := TenantParam(ctx)
	err := pool.QueryRow(ctx, `
		SELECT
		  COUNT(*) FILTER (WHERE m.status IN ('sent','delivered','read'))  AS sent,
		  COUNT(*) FILTER (WHERE m.status = 'delivered')                   AS delivered,
		  COUNT(*) FILTER (WHERE m.status = 'read')                        AS read,
		  COUNT(*) FILTER (WHERE m.status = 'failed')                      AS failed,
		  0::numeric                                                      AS cost_inr
		FROM messages m
		JOIN conversations conv ON conv.id = m.conversation_id
		WHERE m.direction = 'outbound'
		  AND m.sent_by = $3::uuid
		  AND ($4::text IS NULL OR conv.tenant_id = $4::uuid)
		  AND m.created_at >= $1 AND m.created_at < $2
	`, from, to, agentID, tp).Scan(&s.Sent, &s.Delivered, &s.Read, &s.Failed, &s.CostINR)
	return s, err
}

// ── Messages per day ──────────────────────────────────────────────────────────

// DayStat holds outbound message counts for one calendar day.
type DayStat struct {
	Day       time.Time
	Sent      int64
	Delivered int64
	Failed    int64
}

// MessagesPerDay returns daily outbound counts between from and to.
func MessagesPerDay(ctx context.Context, pool *pgxpool.Pool, from, to time.Time) ([]DayStat, error) {
	tp := TenantParam(ctx)
	rows, err := pool.Query(ctx, `
		SELECT
		  DATE_TRUNC('day', m.created_at AT TIME ZONE 'Asia/Kolkata') AS day,
		  COUNT(*) FILTER (WHERE m.status IN ('sent','delivered','read')) AS sent,
		  COUNT(*) FILTER (WHERE m.status IN ('delivered','read'))        AS delivered,
		  COUNT(*) FILTER (WHERE m.status = 'failed')                    AS failed
		FROM messages m
		JOIN conversations conv ON conv.id = m.conversation_id
		WHERE m.direction = 'outbound'
		  AND ($3::text IS NULL OR conv.tenant_id = $3::uuid)
		  AND m.created_at >= $1 AND m.created_at < $2
		GROUP BY 1
		ORDER BY 1
	`, from, to, tp)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var stats []DayStat
	for rows.Next() {
		var s DayStat
		if err := rows.Scan(&s.Day, &s.Sent, &s.Delivered, &s.Failed); err != nil {
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
	tp := TenantParam(ctx)
	rows, err := pool.Query(ctx, `
		SELECT
		  COALESCE(m.category, 'unknown') AS category,
		  COUNT(*)                       AS cnt,
		  COALESCE(SUM(m.cost_inr), 0)    AS cost_inr
		FROM messages m
		JOIN conversations conv ON conv.id = m.conversation_id
		WHERE m.direction = 'outbound'
		  AND ($3::text IS NULL OR conv.tenant_id = $3::uuid)
		  AND m.created_at >= $1 AND m.created_at < $2
		GROUP BY 1
		ORDER BY cost_inr DESC
	`, from, to, tp)
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
	tp := TenantParam(ctx)
	rows, err := pool.Query(ctx, `
		SELECT
		  m.campaign_id::text,
		  COALESCE(c.name, 'Unknown')    AS campaign_name,
		  COUNT(*)                        AS cnt,
		  COALESCE(SUM(m.cost_inr), 0)   AS cost_inr
		FROM messages m
		LEFT JOIN campaigns c ON c.id = m.campaign_id
		JOIN conversations conv ON conv.id = m.conversation_id
		WHERE m.direction = 'outbound'
		  AND m.campaign_id IS NOT NULL
		  AND ($3::text IS NULL OR conv.tenant_id = $3::uuid)
		  AND m.created_at >= $1 AND m.created_at < $2
		GROUP BY 1, 2
		ORDER BY cost_inr DESC
		LIMIT 20
	`, from, to, tp)
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

// MonthlyCost holds spend for one calendar month (IST), split by category.
type MonthlyCost struct {
	Month     string
	Label     string
	Sent      int64
	Marketing float64
	Utility   float64
	Auth      float64
	Total     float64
}

// CostByMonth returns per-month spend across ALL history.
func CostByMonth(ctx context.Context, pool *pgxpool.Pool) ([]MonthlyCost, error) {
	tp := TenantParam(ctx)
	rows, err := pool.Query(ctx, `
		SELECT
		  to_char((m.created_at AT TIME ZONE 'Asia/Kolkata'), 'YYYY-MM')      AS ym,
		  to_char((m.created_at AT TIME ZONE 'Asia/Kolkata'), 'Mon YYYY')     AS label,
		  COUNT(*) FILTER (WHERE m.status IN ('sent','delivered','read'))     AS sent,
		  COALESCE(SUM(m.cost_inr) FILTER (WHERE m.category = 'marketing'), 0)  AS marketing,
		  COALESCE(SUM(m.cost_inr) FILTER (WHERE m.category = 'utility'), 0)    AS utility,
		  COALESCE(SUM(m.cost_inr) FILTER (WHERE m.category IN ('auth','authentication')), 0) AS auth,
		  COALESCE(SUM(m.cost_inr), 0)                                        AS total
		FROM messages m
		JOIN conversations conv ON conv.id = m.conversation_id
		WHERE m.direction = 'outbound'
		  AND ($1::text IS NULL OR conv.tenant_id = $1::uuid)
		GROUP BY 1, 2
		ORDER BY 1 DESC
		LIMIT 24
	`, tp)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MonthlyCost
	for rows.Next() {
		var m MonthlyCost
		if err := rows.Scan(&m.Month, &m.Label, &m.Sent, &m.Marketing, &m.Utility, &m.Auth, &m.Total); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ── Quality / tier ────────────────────────────────────────────────────────────

// QualityInfo holds the current tier, daily usage, and quality rating.
type QualityInfo struct {
	Tier          int64
	DailyCap      int64
	SentToday     int64
	QualityRating string
}

// GetQualityInfo loads tier + usage + quality rating.
func GetQualityInfo(ctx context.Context, pool *pgxpool.Pool) (QualityInfo, error) {
	var q QualityInfo
	tid := effectiveTenantID(ctx)

	cfg := make(map[string]json.RawMessage)
	cfgRows, cfgErr := pool.Query(ctx,
		`SELECT key, value FROM app_config
		  WHERE tenant_id = $1::uuid AND key IN ('meta_tier', 'daily_message_cap', 'quality_rating')`,
		tid,
	)
	if cfgErr == nil {
		for cfgRows.Next() {
			var k string
			var v json.RawMessage
			if cfgRows.Scan(&k, &v) == nil {
				cfg[k] = v
			}
		}
		cfgRows.Close()
	}

	if raw, ok := cfg["meta_tier"]; ok {
		var n int64
		if json.Unmarshal(raw, &n) == nil {
			q.Tier = n
		}
	}

	q.DailyCap = 900
	if raw, ok := cfg["daily_message_cap"]; ok {
		var n int64
		if json.Unmarshal(raw, &n) == nil {
			q.DailyCap = n
		}
	}

	if raw, ok := cfg["quality_rating"]; ok {
		var qs string
		if json.Unmarshal(raw, &qs) == nil {
			switch qs {
			case "green", "yellow", "red":
				q.QualityRating = qs
			default:
				q.QualityRating = "unknown"
			}
		} else {
			q.QualityRating = "unknown"
		}
	} else {
		q.QualityRating = "unknown"
	}

	sent, _ := DailyMessagesSent(ctx, pool, tid)
	q.SentToday = sent

	return q, nil
}

// ── Agent performance ─────────────────────────────────────────────────────────

// AgentStat holds per-agent performance numbers.
type AgentStat struct {
	AgentID       string
	AgentName     string
	MessagesSent  int64
	ConvsResolved int64
}

// AgentPerformance returns per-agent stats for the date range.
func AgentPerformance(ctx context.Context, pool *pgxpool.Pool, from, to time.Time) ([]AgentStat, error) {
	tp := TenantParam(ctx)
	rows, err := pool.Query(ctx, `
		SELECT
		  a.id::text,
		  a.name,
		  (SELECT COUNT(*) FROM messages m
		     JOIN conversations conv ON conv.id = m.conversation_id
		     WHERE m.sent_by = a.id AND m.direction = 'outbound'
		       AND m.created_at >= $1 AND m.created_at < $2) AS messages_sent,
		  (SELECT COUNT(*) FROM conversations conv
		     WHERE conv.assigned_to = a.id AND conv.status = 'closed'
		       AND conv.updated_at >= $1 AND conv.updated_at < $2) AS convs_resolved
		FROM agents a
		WHERE ($3::text IS NULL OR a.tenant_id = $3::uuid)
		ORDER BY messages_sent DESC
	`, from, to, tp)
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
	ChatsWaiting   int64
	CostThisMonth  float64
	QualityRating  string
	DailyCap       int64
	CapUsedToday   int64
}

// GetDashboardStats returns the quick-stat numbers for the dashboard.
func GetDashboardStats(ctx context.Context, pool *pgxpool.Pool) (DashboardStats, error) {
	var s DashboardStats
	tid := effectiveTenantID(ctx)

	// Sent + delivered today
	err := pool.QueryRow(ctx, `
		SELECT
		  COUNT(*) FILTER (WHERE m.status IN ('sent','delivered','read')),
		  COUNT(*) FILTER (WHERE m.status = 'delivered' OR m.status = 'read')
		FROM messages m
		JOIN conversations conv ON conv.id = m.conversation_id
		WHERE m.direction = 'outbound'
		  AND conv.tenant_id = $1::uuid
		  AND m.created_at >= CURRENT_DATE
	`, tid).Scan(&s.SentToday, &s.DeliveredToday)
	if err != nil {
		return s, err
	}

	// Unassigned open conversations
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM conversations
		WHERE tenant_id = $1::uuid AND status = 'open' AND assigned_to IS NULL
	`, tid).Scan(&s.ChatsWaiting)
	if err != nil {
		return s, err
	}

	// Cost this calendar month
	err = pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(m.cost_inr), 0)
		FROM messages m
		JOIN conversations conv ON conv.id = m.conversation_id
		WHERE m.direction = 'outbound'
		  AND conv.tenant_id = $1::uuid
		  AND m.created_at >= DATE_TRUNC('month', NOW())
	`, tid).Scan(&s.CostThisMonth)
	if err != nil {
		return s, err
	}

	// Quality rating + daily cap
	qi, _ := GetQualityInfo(ctx, pool)
	s.QualityRating = qi.QualityRating
	s.DailyCap = qi.DailyCap
	s.CapUsedToday = qi.SentToday

	return s, nil
}
