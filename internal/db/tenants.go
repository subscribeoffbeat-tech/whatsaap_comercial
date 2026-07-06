package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Tenant mirrors the tenants table.
type Tenant struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Status    string    `json:"status"` // active|suspended|trial
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// GetTenantBySlug fetches a tenant by their slug.
func GetTenantBySlug(ctx context.Context, pool *pgxpool.Pool, slug string) (*Tenant, error) {
	var t Tenant
	err := pool.QueryRow(ctx, `
		SELECT id::text, name, slug, status, created_at, updated_at
		FROM tenants
		WHERE slug = $1
	`, slug).Scan(&t.ID, &t.Name, &t.Slug, &t.Status, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get tenant by slug: %w", err)
	}
	return &t, nil
}

// GetTenantByID fetches a tenant by UUID.
func GetTenantByID(ctx context.Context, pool *pgxpool.Pool, id string) (*Tenant, error) {
	var t Tenant
	err := pool.QueryRow(ctx, `
		SELECT id::text, name, slug, status, created_at, updated_at
		FROM tenants
		WHERE id = $1::uuid
	`, id).Scan(&t.ID, &t.Name, &t.Slug, &t.Status, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get tenant by id: %w", err)
	}
	return &t, nil
}

// CreateTenant inserts a new tenant and populates ID/timestamps.
func CreateTenant(ctx context.Context, pool *pgxpool.Pool, name, slug string) (*Tenant, error) {
	var t Tenant
	err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, slug)
		VALUES ($1, $2)
		RETURNING id::text, name, slug, status, created_at, updated_at
	`, name, slug).Scan(&t.ID, &t.Name, &t.Slug, &t.Status, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create tenant: %w", err)
	}
	return &t, nil
}

// ListTenants lists all tenants.
func ListTenants(ctx context.Context, pool *pgxpool.Pool) ([]*Tenant, error) {
	rows, err := pool.Query(ctx, `
		SELECT id::text, name, slug, status, created_at, updated_at
		FROM tenants
		ORDER BY name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Tenant
	for rows.Next() {
		var t Tenant
		if err := rows.Scan(&t.ID, &t.Name, &t.Slug, &t.Status, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

// UpdateTenantStatus activates or suspends a tenant.
func UpdateTenantStatus(ctx context.Context, pool *pgxpool.Pool, id, status string) error {
	_, err := pool.Exec(ctx, `
		UPDATE tenants SET status = $1, updated_at = NOW() WHERE id = $2::uuid
	`, status, id)
	return err
}

// LookupTenantByPhoneID resolves a tenant ID from a WhatsApp phone_number_id stored in app_config.
func LookupTenantByPhoneID(ctx context.Context, pool *pgxpool.Pool, phoneID string) (string, error) {
	var tenantID string
	err := pool.QueryRow(ctx, `
		SELECT tenant_id::text
		FROM app_config
		WHERE key = 'whatsapp_phone_number_id' AND value = to_jsonb($1::text)
	`, phoneID).Scan(&tenantID)
	return tenantID, err
}

