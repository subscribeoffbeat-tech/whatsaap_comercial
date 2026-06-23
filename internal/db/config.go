package db

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ConfigRates holds per-category message costs (excluding GST).
type ConfigRates struct {
	Marketing float64
	Utility   float64
	Auth      float64
	GSTRate   float64
}

// GetConfigString reads a string app_config value by key (JSON-decoded).
func GetConfigString(ctx context.Context, pool *pgxpool.Pool, key string) (string, error) {
	var raw json.RawMessage
	if err := pool.QueryRow(ctx,
		`SELECT value FROM app_config WHERE key = $1`, key,
	).Scan(&raw); err != nil {
		return "", err
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("parse config %q as string: %w", key, err)
	}
	return s, nil
}

// GetConfigFloat reads a numeric app_config value by key.
func GetConfigFloat(ctx context.Context, pool *pgxpool.Pool, key string) (float64, error) {
	var raw json.RawMessage
	if err := pool.QueryRow(ctx,
		`SELECT value FROM app_config WHERE key = $1`, key,
	).Scan(&raw); err != nil {
		return 0, fmt.Errorf("get_config %q: %w", key, err)
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return f, nil
	}
	// Handle quoted string form e.g. '"0.8631"'
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strconv.ParseFloat(s, 64)
	}
	return 0, fmt.Errorf("parse config %q as float", key)
}

// GetConfigInt reads an integer app_config value by key.
func GetConfigInt(ctx context.Context, pool *pgxpool.Pool, key string) (int64, error) {
	var raw json.RawMessage
	if err := pool.QueryRow(ctx,
		`SELECT value FROM app_config WHERE key = $1`, key,
	).Scan(&raw); err != nil {
		return 0, fmt.Errorf("get_config %q: %w", key, err)
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, fmt.Errorf("parse config %q as int: %w", key, err)
	}
	return n, nil
}

// GetConfigBool reads a boolean app_config value by key.
func GetConfigBool(ctx context.Context, pool *pgxpool.Pool, key string) (bool, error) {
	var raw json.RawMessage
	if err := pool.QueryRow(ctx,
		`SELECT value FROM app_config WHERE key = $1`, key,
	).Scan(&raw); err != nil {
		return false, fmt.Errorf("get_config %q: %w", key, err)
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return false, fmt.Errorf("parse config %q as bool: %w", key, err)
	}
	return b, nil
}

// GetStopKeywords returns the admin-configured opt-out keywords from app_config
// (key "stop_keywords"), or nil if unset. These are merged with the mandatory
// built-in keywords by the STOP matcher.
func GetStopKeywords(ctx context.Context, pool *pgxpool.Pool) []string {
	var raw json.RawMessage
	if err := pool.QueryRow(ctx,
		`SELECT value FROM app_config WHERE key = 'stop_keywords'`,
	).Scan(&raw); err != nil {
		return nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil
	}
	return list
}

// SetConfig upserts a config value by key.
func SetConfig(ctx context.Context, pool *pgxpool.Pool, key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO app_config (key, value)
		VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE
		  SET value = EXCLUDED.value, updated_at = NOW()
	`, key, json.RawMessage(raw))
	return err
}

// LoadRates returns the current per-message cost rates from app_config.
// Falls back to the CLAUDE.md defaults if any key is missing.
func LoadRates(ctx context.Context, pool *pgxpool.Pool) ConfigRates {
	r := ConfigRates{Marketing: 0.8631, Utility: 0.115, Auth: 0.115, GSTRate: 0.18}
	if v, err := GetConfigFloat(ctx, pool, "marketing_cost_inr"); err == nil {
		r.Marketing = v
	}
	if v, err := GetConfigFloat(ctx, pool, "utility_cost_inr"); err == nil {
		r.Utility = v
	}
	if v, err := GetConfigFloat(ctx, pool, "auth_cost_inr"); err == nil {
		r.Auth = v
	}
	if v, err := GetConfigFloat(ctx, pool, "gst_rate"); err == nil {
		r.GSTRate = v
	}
	return r
}

// DailyCap returns the daily message cap (90% of current tier).
func DailyCap(ctx context.Context, pool *pgxpool.Pool) int64 {
	if v, err := GetConfigInt(ctx, pool, "daily_message_cap"); err == nil {
		return v
	}
	return 900 // fallback: 90% of tier-1000
}

// DailyMessagesSent counts outbound messages sent since midnight IST today.
// The cap window is anchored to the IST business day (and Meta's local-day tier
// reset for this single-tenant India deployment), not UTC midnight (05:30 IST).
func DailyMessagesSent(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var n int64
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM messages
		WHERE direction = 'outbound'
		  AND created_at >= ((now() AT TIME ZONE 'Asia/Kolkata')::date) AT TIME ZONE 'Asia/Kolkata'
	`).Scan(&n)
	return n, err
}

// FreqCapHours returns the configured marketing frequency-cap window in hours
// (config key freq_cap_hours), defaulting to 24.
func FreqCapHours(ctx context.Context, pool *pgxpool.Pool) int {
	if v, err := GetConfigInt(ctx, pool, "freq_cap_hours"); err == nil && v > 0 {
		return int(v)
	}
	return 24
}
