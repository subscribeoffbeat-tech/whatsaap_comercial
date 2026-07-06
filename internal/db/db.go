package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect opens a pgx connection pool and verifies connectivity.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

type ctxKey string
const tenantCtxKey ctxKey = "tenant_id"

// ContextWithTenant injects a tenant UUID string into the context.
func ContextWithTenant(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantCtxKey, tenantID)
}

// TenantFromContext extracts the tenant UUID string from the context.
func TenantFromContext(ctx context.Context) string {
	v, _ := ctx.Value(tenantCtxKey).(string)
	return v
}

// TenantParam returns a pointer to the tenant UUID string from context, or nil if unset.
// Useful for SQL scoping: WHERE ($1::text IS NULL OR tenant_id = $1::uuid)
func TenantParam(ctx context.Context) *string {
	v := TenantFromContext(ctx)
	if v == "" {
		return nil
	}
	return &v
}


