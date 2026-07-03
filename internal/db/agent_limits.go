package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AgentLimit holds the per-agent monthly limits.
type AgentLimit struct {
	AgentID              string
	MonthlyMsgCap        int
	MonthlySpendCapPaise int64
	UpdatedAt            time.Time
}

// GetAgentLimits returns the limit row for one agent (nil if none set).
func GetAgentLimits(ctx context.Context, pool *pgxpool.Pool, agentID string) (*AgentLimit, error) {
	row := pool.QueryRow(ctx,
		`SELECT agent_id::text, monthly_msg_cap, monthly_spend_cap_paise, updated_at
		 FROM agent_limits WHERE agent_id = $1::uuid`, agentID)
	l := &AgentLimit{}
	if err := row.Scan(&l.AgentID, &l.MonthlyMsgCap, &l.MonthlySpendCapPaise, &l.UpdatedAt); err != nil {
		return nil, err
	}
	return l, nil
}

// ListAgentLimits returns all rows keyed by agent_id string.
func ListAgentLimits(ctx context.Context, pool *pgxpool.Pool) (map[string]*AgentLimit, error) {
	rows, err := pool.Query(ctx,
		`SELECT agent_id::text, monthly_msg_cap, monthly_spend_cap_paise, updated_at FROM agent_limits`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]*AgentLimit)
	for rows.Next() {
		l := &AgentLimit{}
		if err := rows.Scan(&l.AgentID, &l.MonthlyMsgCap, &l.MonthlySpendCapPaise, &l.UpdatedAt); err != nil {
			return nil, err
		}
		out[l.AgentID] = l
	}
	return out, rows.Err()
}

// AgentMessagesThisMonth counts an agent's outbound messages since the start of
// the current month — used to enforce the per-agent monthly cap.
func AgentMessagesThisMonth(ctx context.Context, pool *pgxpool.Pool, agentID string) (int64, error) {
	var n int64
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM messages
		WHERE sent_by = $1::uuid AND direction = 'outbound'
		  AND created_at >= DATE_TRUNC('month', NOW())
	`, agentID).Scan(&n)
	return n, err
}

// SetAgentMsgCap upserts the monthly message cap for an agent (0 = unlimited).
func SetAgentMsgCap(ctx context.Context, pool *pgxpool.Pool, agentID string, cap int) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO agent_limits (agent_id, monthly_msg_cap, updated_at)
		 VALUES ($1::uuid, $2, NOW())
		 ON CONFLICT (agent_id) DO UPDATE
		   SET monthly_msg_cap = EXCLUDED.monthly_msg_cap,
		       updated_at      = NOW()`,
		agentID, cap)
	return err
}
