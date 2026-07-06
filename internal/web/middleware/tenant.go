package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"whatsapptool/internal/db"
)

type tenantCtxKeyType string

const ctxTenantKey tenantCtxKeyType = "tenant"

// TenantFromCtx returns the resolved Tenant struct, if any.
func TenantFromCtx(ctx context.Context) *db.Tenant {
	v, _ := ctx.Value(ctxTenantKey).(*db.Tenant)
	return v
}

// ResolveTenant extracts the subdomain from the host, looks up the tenant in the database,
// and injects both the Tenant object and the tenant ID into the context.
func ResolveTenant(pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract subdomain (e.g. "tenant1" from "tenant1.domain.com:8080")
			host := r.Host
			if ip := strings.Index(host, ":"); ip != -1 {
				host = host[:ip]
			}
			parts := strings.Split(host, ".")

			slug := ""
			// If there's a subdomain (e.g., more than 2 parts or not localhost)
			if len(parts) > 1 && parts[0] != "localhost" && parts[0] != "127" && parts[0] != "www" {
				slug = parts[0]
			}

			var tenant *db.Tenant
			var err error

			if slug == "" {
				// Localhost development or root domain fallback: use Default Tenant
				tenant, err = db.GetTenantBySlug(r.Context(), pool, "default")
			} else {
				tenant, err = db.GetTenantBySlug(r.Context(), pool, slug)
			}

			if err != nil {
				// Tenant not found
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte("<h1>Company Workspace Not Found</h1><p>The requested business workspace does not exist or has been removed.</p>"))
				return
			}

			if tenant.Status == "suspended" {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte("<h1>Workspace Suspended</h1><p>This business workspace is suspended by the platform administrator.</p>"))
				return
			}

			// Inject tenant ID for DB query scoping
			ctx := db.ContextWithTenant(r.Context(), tenant.ID)
			// Inject full tenant struct for handlers/templates
			ctx = context.WithValue(ctx, ctxTenantKey, tenant)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireTenantAuth verifies that the authenticated user belongs to the current tenant.
func RequireTenantAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent := AgentFromCtx(r.Context())
		tenant := TenantFromCtx(r.Context())

		if agent == nil {
			next.ServeHTTP(w, r)
			return
		}

		// Super-admins bypass tenant checks
		if agent.Role == "super_admin" {
			next.ServeHTTP(w, r)
			return
		}

		if tenant != nil && agent.TenantID != "" && agent.TenantID != tenant.ID {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("<h1>Access Denied</h1><p>You do not have access to this business workspace.</p>"))
			return
		}

		next.ServeHTTP(w, r)
	})
}
