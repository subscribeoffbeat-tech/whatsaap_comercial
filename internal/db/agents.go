package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Agent mirrors the agents table.
type Agent struct {
	ID              string
	Name            string
	Email           string
	PasswordHash    string
	Role            string // admin|manager|agent
	Available       bool
	Active          bool
	InviteToken     *string
	InviteExpiresAt *time.Time
	LastActiveAt    *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	// Extended profile fields (migration 011)
	Phone       *string
	Timezone    string
	Preferences map[string]any
}

const agentColumns = `id::text, name, email, password_hash, role, available,
	COALESCE(active, TRUE), invite_token, invite_expires_at, last_active_at,
	created_at, updated_at,
	phone, COALESCE(timezone,'Asia/Kolkata'), COALESCE(preferences::text,'{}')`

func scanAgent(row rowScanner) (*Agent, error) {
	a := &Agent{}
	var inviteToken, phone pgtype.Text
	var inviteExp, lastActive pgtype.Timestamptz
	var prefRaw string
	err := row.Scan(
		&a.ID, &a.Name, &a.Email, &a.PasswordHash, &a.Role, &a.Available,
		&a.Active, &inviteToken, &inviteExp, &lastActive,
		&a.CreatedAt, &a.UpdatedAt,
		&phone, &a.Timezone, &prefRaw,
	)
	if err != nil {
		return nil, err
	}
	if inviteToken.Valid {
		a.InviteToken = &inviteToken.String
	}
	if inviteExp.Valid {
		t := inviteExp.Time
		a.InviteExpiresAt = &t
	}
	if lastActive.Valid {
		t := lastActive.Time
		a.LastActiveAt = &t
	}
	if phone.Valid {
		a.Phone = &phone.String
	}
	a.Preferences = map[string]any{}
	_ = json.Unmarshal([]byte(prefRaw), &a.Preferences)
	return a, nil
}

// GetAgentByEmail returns an agent by email address (for login).
func GetAgentByEmail(ctx context.Context, pool *pgxpool.Pool, email string) (*Agent, error) {
	return scanAgent(pool.QueryRow(ctx, `
		SELECT `+agentColumns+` FROM agents WHERE email = $1
	`, email))
}

// GetAgentByID returns an agent by UUID.
func GetAgentByID(ctx context.Context, pool *pgxpool.Pool, id string) (*Agent, error) {
	return scanAgent(pool.QueryRow(ctx, `
		SELECT `+agentColumns+` FROM agents WHERE id = $1::uuid
	`, id))
}

// GetAgentByInviteToken returns an agent with a valid (non-expired) invite token.
func GetAgentByInviteToken(ctx context.Context, pool *pgxpool.Pool, token string) (*Agent, error) {
	return scanAgent(pool.QueryRow(ctx, `
		SELECT `+agentColumns+` FROM agents
		WHERE invite_token = $1 AND invite_expires_at > NOW()
	`, token))
}

// ListAgents returns all agents ordered by name.
func ListAgents(ctx context.Context, pool *pgxpool.Pool) ([]*Agent, error) {
	rows, err := pool.Query(ctx, `
		SELECT `+agentColumns+` FROM agents ORDER BY name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var agents []*Agent
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

// CountAgents returns the total number of agents (used for onboarding check).
func CountAgents(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var n int64
	err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM agents`).Scan(&n)
	return n, err
}

// CreateInvitedAgent inserts an agent with an invite token (password not yet set).
// The caller must generate a secure random invite_token.
func CreateInvitedAgent(ctx context.Context, pool *pgxpool.Pool, name, email, role, inviteToken string, expiresAt time.Time) (*Agent, error) {
	return scanAgent(pool.QueryRow(ctx, `
		INSERT INTO agents (name, email, password_hash, role, active, invite_token, invite_expires_at)
		VALUES ($1, $2, '', $3, FALSE, $4, $5)
		RETURNING `+agentColumns,
		name, email, role, inviteToken, expiresAt,
	))
}

// CreateAdminAgent creates the first admin account during onboarding.
func CreateAdminAgent(ctx context.Context, pool *pgxpool.Pool, name, email, passwordHash string) (*Agent, error) {
	return scanAgent(pool.QueryRow(ctx, `
		INSERT INTO agents (name, email, password_hash, role, active)
		VALUES ($1, $2, $3, 'admin', TRUE)
		RETURNING `+agentColumns,
		name, email, passwordHash,
	))
}

// ActivateInvite sets the password hash, clears the invite token, and activates the account.
func ActivateInvite(ctx context.Context, pool *pgxpool.Pool, agentID, passwordHash string) error {
	_, err := pool.Exec(ctx, `
		UPDATE agents
		SET password_hash = $1, active = TRUE,
		    invite_token = NULL, invite_expires_at = NULL,
		    updated_at = NOW()
		WHERE id = $2::uuid
	`, passwordHash, agentID)
	return err
}

// UpdateAgentRole changes an agent's role (admin only).
func UpdateAgentRole(ctx context.Context, pool *pgxpool.Pool, agentID, role string) error {
	_, err := pool.Exec(ctx, `
		UPDATE agents SET role = $1, updated_at = NOW() WHERE id = $2::uuid
	`, role, agentID)
	return err
}

// SetAgentActive activates or deactivates an agent.
func SetAgentActive(ctx context.Context, pool *pgxpool.Pool, agentID string, active bool) error {
	_, err := pool.Exec(ctx, `
		UPDATE agents SET active = $1, updated_at = NOW() WHERE id = $2::uuid
	`, active, agentID)
	return err
}

// TouchLastActive updates the last_active_at timestamp.
func TouchLastActive(ctx context.Context, pool *pgxpool.Pool, agentID string) {
	pool.Exec(ctx, `UPDATE agents SET last_active_at = NOW() WHERE id = $1::uuid`, agentID) //nolint:errcheck
}

// DeleteAgent removes an agent record (soft-delete is preferred; use SetAgentActive for deactivation).
func DeleteAgent(ctx context.Context, pool *pgxpool.Pool, agentID string) error {
	_, err := pool.Exec(ctx, `DELETE FROM agents WHERE id = $1::uuid`, agentID)
	return err
}

// IsLastActiveAdmin reports whether the given agent is an active admin AND the
// only one left — used to prevent locking the org out of admin access.
func IsLastActiveAdmin(ctx context.Context, pool *pgxpool.Pool, agentID string) (bool, error) {
	var role string
	var active bool
	if err := pool.QueryRow(ctx,
		`SELECT role, active FROM agents WHERE id = $1::uuid`, agentID,
	).Scan(&role, &active); err != nil {
		return false, err
	}
	if role != "admin" || !active {
		return false, nil
	}
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM agents WHERE role = 'admin' AND active = TRUE`,
	).Scan(&n); err != nil {
		return false, err
	}
	return n <= 1, nil
}

// SetPasswordResetToken stores a 30-minute password reset token for the agent.
func SetPasswordResetToken(ctx context.Context, pool *pgxpool.Pool, agentID, token string, expiresAt time.Time) error {
	_, err := pool.Exec(ctx, `
		UPDATE agents
		SET password_reset_token = $1,
		    password_reset_expires_at = $2,
		    updated_at = NOW()
		WHERE id = $3::uuid
	`, token, expiresAt, agentID)
	return err
}

// GetAgentByResetToken returns the active agent matching a valid (non-expired) reset token.
func GetAgentByResetToken(ctx context.Context, pool *pgxpool.Pool, token string) (*Agent, error) {
	return scanAgent(pool.QueryRow(ctx, `
		SELECT `+agentColumns+` FROM agents
		WHERE password_reset_token = $1
		  AND password_reset_expires_at > NOW()
		  AND active = TRUE
	`, token))
}

// UsePasswordResetToken sets a new password hash and clears the reset token atomically.
func UsePasswordResetToken(ctx context.Context, pool *pgxpool.Pool, agentID, passwordHash string) error {
	_, err := pool.Exec(ctx, `
		UPDATE agents
		SET password_hash = $1,
		    password_reset_token = NULL,
		    password_reset_expires_at = NULL,
		    updated_at = NOW()
		WHERE id = $2::uuid
	`, passwordHash, agentID)
	return err
}

// UpdateInviteToken replaces the invite token for an existing (pending) agent.
func UpdateInviteToken(ctx context.Context, pool *pgxpool.Pool, agentID, token string, expiresAt time.Time) error {
	_, err := pool.Exec(ctx,
		`UPDATE agents SET invite_token = $1, invite_expires_at = $2 WHERE id = $3::uuid`,
		token, expiresAt, agentID)
	return err
}

// UpdateAgentProfile updates name, email, phone, and timezone for the logged-in agent.
func UpdateAgentProfile(ctx context.Context, pool *pgxpool.Pool, agentID, name, email, phone, timezone string) error {
	var phoneVal *string
	if phone != "" {
		phoneVal = &phone
	}
	_, err := pool.Exec(ctx, `
		UPDATE agents SET name=$1, email=$2, phone=$3, timezone=$4, updated_at=NOW()
		WHERE id=$5::uuid
	`, name, email, phoneVal, timezone, agentID)
	return err
}

// UpdateAgentPassword sets a new bcrypt password hash for the agent.
func UpdateAgentPassword(ctx context.Context, pool *pgxpool.Pool, agentID, hash string) error {
	_, err := pool.Exec(ctx, `
		UPDATE agents SET password_hash = $1, updated_at = NOW() WHERE id = $2::uuid
	`, hash, agentID)
	return err
}

// UpdateAgentPreferences stores notification preference toggles as JSONB.
func UpdateAgentPreferences(ctx context.Context, pool *pgxpool.Pool, agentID string, prefs map[string]any) error {
	raw, err := json.Marshal(prefs)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		UPDATE agents SET preferences=$1::jsonb, updated_at=NOW() WHERE id=$2::uuid
	`, string(raw), agentID)
	return err
}
