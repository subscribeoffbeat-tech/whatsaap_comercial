package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AutomationRule mirrors the automation_rules table.
type AutomationRule struct {
	ID           int64
	Name         string
	TriggerType  string  // keyword|welcome|away|stop|no_reply|new_conversation|opt_out
	Keyword      *string // keyword(s) for trigger=keyword; hours for trigger=no_reply
	KeywordMatch string  // exact|contains
	TemplateID   *string // UUID as text
	ResponseText *string
	ActionType   string  // send_template|assign_agent|add_tag|remove_consent|webhook
	Active       bool
	Priority     int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// IsSystem returns true for the system-locked STOP rule.
func (r *AutomationRule) IsSystem() bool { return r.TriggerType == "stop" }

const ruleColumns = `id, name, trigger_type, keyword, keyword_match,
	template_id::text, response_text, COALESCE(action_type,''), active, priority, created_at, updated_at`

func scanRule(row rowScanner) (*AutomationRule, error) {
	r := &AutomationRule{}
	var keyword, templateID, responseText pgtype.Text
	err := row.Scan(
		&r.ID, &r.Name, &r.TriggerType, &keyword, &r.KeywordMatch,
		&templateID, &responseText, &r.ActionType, &r.Active, &r.Priority,
		&r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if keyword.Valid {
		r.Keyword = &keyword.String
	}
	if templateID.Valid {
		r.TemplateID = &templateID.String
	}
	if responseText.Valid {
		r.ResponseText = &responseText.String
	}
	return r, nil
}

// ListRules returns all rules for the current tenant ordered by priority ASC.
func ListRules(ctx context.Context, pool *pgxpool.Pool) ([]*AutomationRule, error) {
	tid := effectiveTenantID(ctx)
	rows, err := pool.Query(ctx, `
		SELECT `+ruleColumns+`
		FROM automation_rules
		WHERE tenant_id = $1::uuid
		ORDER BY
		  CASE trigger_type WHEN 'stop' THEN 0 ELSE 1 END,
		  priority ASC, id ASC
	`, tid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rules []*AutomationRule
	for rows.Next() {
		rule, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

// ListActiveRules returns only active rules for the current tenant, ordered for engine evaluation.
func ListActiveRules(ctx context.Context, pool *pgxpool.Pool) ([]*AutomationRule, error) {
	tid := effectiveTenantID(ctx)
	rows, err := pool.Query(ctx, `
		SELECT `+ruleColumns+`
		FROM automation_rules
		WHERE tenant_id = $1::uuid AND active = TRUE
		ORDER BY
		  CASE trigger_type WHEN 'stop' THEN 0 ELSE 1 END,
		  priority ASC, id ASC
	`, tid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rules []*AutomationRule
	for rows.Next() {
		rule, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

// GetRule fetches a single rule by ID.
func GetRule(ctx context.Context, pool *pgxpool.Pool, id int64) (*AutomationRule, error) {
	tid := effectiveTenantID(ctx)
	return scanRule(pool.QueryRow(ctx, `
		SELECT `+ruleColumns+` FROM automation_rules WHERE tenant_id = $2::uuid AND id = $1
	`, id, tid))
}

// CreateRule inserts a new rule and populates r.ID / timestamps.
func CreateRule(ctx context.Context, pool *pgxpool.Pool, r *AutomationRule) error {
	tid := effectiveTenantID(ctx)
	var actionType *string
	if r.ActionType != "" {
		actionType = &r.ActionType
	}
	return pool.QueryRow(ctx, `
		INSERT INTO automation_rules
		  (tenant_id, name, trigger_type, keyword, keyword_match, template_id, response_text, action_type, active, priority)
		VALUES ($1::uuid, $2, $3, $4, $5, $6::uuid, $7, $8, $9, $10)
		RETURNING id, created_at, updated_at
	`, tid, r.Name, r.TriggerType, r.Keyword, r.KeywordMatch,
		r.TemplateID, r.ResponseText, actionType, r.Active, r.Priority,
	).Scan(&r.ID, &r.CreatedAt, &r.UpdatedAt)
}

// UpdateRule updates a non-system rule.
func UpdateRule(ctx context.Context, pool *pgxpool.Pool, r *AutomationRule) error {
	tid := effectiveTenantID(ctx)
	var actionType *string
	if r.ActionType != "" {
		actionType = &r.ActionType
	}
	_, err := pool.Exec(ctx, `
		UPDATE automation_rules
		SET name=$3, trigger_type=$4, keyword=$5, keyword_match=$6,
		    template_id=$7::uuid, response_text=$8, action_type=$9, active=$10, priority=$11, updated_at=NOW()
		WHERE tenant_id=$1::uuid AND id=$2 AND trigger_type != 'stop'
	`, tid, r.ID, r.Name, r.TriggerType, r.Keyword, r.KeywordMatch,
		r.TemplateID, r.ResponseText, actionType, r.Active, r.Priority)
	return err
}

// ToggleRule enables/disables a non-system rule.
func ToggleRule(ctx context.Context, pool *pgxpool.Pool, id int64, active bool) error {
	tid := effectiveTenantID(ctx)
	_, err := pool.Exec(ctx, `
		UPDATE automation_rules SET active=$3, updated_at=NOW()
		WHERE tenant_id=$1::uuid AND id=$2 AND trigger_type != 'stop'
	`, tid, id, active)
	return err
}

// DeleteRule removes a non-system rule.
func DeleteRule(ctx context.Context, pool *pgxpool.Pool, id int64) error {
	tid := effectiveTenantID(ctx)
	_, err := pool.Exec(ctx, `
		DELETE FROM automation_rules WHERE tenant_id=$1::uuid AND id=$2 AND trigger_type != 'stop'
	`, tid, id)
	return err
}

// EnsureStopRule inserts the system STOP rule if it does not yet exist for the current tenant.
func EnsureStopRule(ctx context.Context, pool *pgxpool.Pool) error {
	tid := effectiveTenantID(ctx)
	_, err := pool.Exec(ctx, `
		INSERT INTO automation_rules (tenant_id, name, trigger_type, keyword_match, response_text, active, priority)
		SELECT $1::uuid, 'STOP / Opt-out', 'stop', 'exact',
		  'You have been unsubscribed and will no longer receive marketing messages. Reply START to opt back in.',
		  TRUE, 0
		WHERE NOT EXISTS (SELECT 1 FROM automation_rules WHERE tenant_id = $1::uuid AND trigger_type = 'stop')
	`, tid)
	return err
}
