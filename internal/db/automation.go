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

// ListRules returns all rules ordered by priority ASC (STOP first by trigger_type).
func ListRules(ctx context.Context, pool *pgxpool.Pool) ([]*AutomationRule, error) {
	rows, err := pool.Query(ctx, `
		SELECT `+ruleColumns+`
		FROM automation_rules
		ORDER BY
		  CASE trigger_type WHEN 'stop' THEN 0 ELSE 1 END,
		  priority ASC, id ASC
	`)
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

// ListActiveRules returns only active rules, ordered for engine evaluation.
func ListActiveRules(ctx context.Context, pool *pgxpool.Pool) ([]*AutomationRule, error) {
	rows, err := pool.Query(ctx, `
		SELECT `+ruleColumns+`
		FROM automation_rules
		WHERE active = TRUE
		ORDER BY
		  CASE trigger_type WHEN 'stop' THEN 0 ELSE 1 END,
		  priority ASC, id ASC
	`)
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
	return scanRule(pool.QueryRow(ctx, `
		SELECT `+ruleColumns+` FROM automation_rules WHERE id = $1
	`, id))
}

// CreateRule inserts a new rule and populates r.ID / timestamps.
func CreateRule(ctx context.Context, pool *pgxpool.Pool, r *AutomationRule) error {
	var actionType *string
	if r.ActionType != "" {
		actionType = &r.ActionType
	}
	return pool.QueryRow(ctx, `
		INSERT INTO automation_rules
		  (name, trigger_type, keyword, keyword_match, template_id, response_text, action_type, active, priority)
		VALUES ($1, $2, $3, $4, $5::uuid, $6, $7, $8, $9)
		RETURNING id, created_at, updated_at
	`, r.Name, r.TriggerType, r.Keyword, r.KeywordMatch,
		r.TemplateID, r.ResponseText, actionType, r.Active, r.Priority,
	).Scan(&r.ID, &r.CreatedAt, &r.UpdatedAt)
}

// UpdateRule updates a non-system rule.
func UpdateRule(ctx context.Context, pool *pgxpool.Pool, r *AutomationRule) error {
	var actionType *string
	if r.ActionType != "" {
		actionType = &r.ActionType
	}
	_, err := pool.Exec(ctx, `
		UPDATE automation_rules
		SET name=$1, trigger_type=$2, keyword=$3, keyword_match=$4,
		    template_id=$5::uuid, response_text=$6, action_type=$7, active=$8, priority=$9, updated_at=NOW()
		WHERE id=$10 AND trigger_type != 'stop'
	`, r.Name, r.TriggerType, r.Keyword, r.KeywordMatch,
		r.TemplateID, r.ResponseText, actionType, r.Active, r.Priority, r.ID)
	return err
}

// ToggleRule enables/disables a non-system rule.
func ToggleRule(ctx context.Context, pool *pgxpool.Pool, id int64, active bool) error {
	_, err := pool.Exec(ctx, `
		UPDATE automation_rules SET active=$1, updated_at=NOW()
		WHERE id=$2 AND trigger_type != 'stop'
	`, active, id)
	return err
}

// DeleteRule removes a non-system rule.
func DeleteRule(ctx context.Context, pool *pgxpool.Pool, id int64) error {
	_, err := pool.Exec(ctx, `
		DELETE FROM automation_rules WHERE id=$1 AND trigger_type != 'stop'
	`, id)
	return err
}

// EnsureStopRule inserts the system STOP rule if it does not yet exist.
func EnsureStopRule(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO automation_rules (name, trigger_type, keyword_match, response_text, active, priority)
		SELECT 'STOP / Opt-out', 'stop', 'exact',
		  'You have been unsubscribed and will no longer receive marketing messages. Reply START to opt back in.',
		  TRUE, 0
		WHERE NOT EXISTS (SELECT 1 FROM automation_rules WHERE trigger_type = 'stop')
	`)
	return err
}
