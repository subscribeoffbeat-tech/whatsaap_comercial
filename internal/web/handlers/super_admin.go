package handlers

import (
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"whatsapptool/internal/db"
	mw "whatsapptool/internal/web/middleware"
	"whatsapptool/internal/web/templates"
)

var slugRegex = regexp.MustCompile(`^[a-z0-9-]+$`)

type SuperAdminHandler struct {
	pool *pgxpool.Pool
}

func NewSuperAdminHandler(pool *pgxpool.Pool) *SuperAdminHandler {
	return &SuperAdminHandler{pool: pool}
}

func (h *SuperAdminHandler) Mount(r chi.Router) {
	r.Get("/tenants", h.List)
	r.Post("/tenants", h.Create)
	r.Post("/tenants/{id}/status", h.UpdateStatus)
}

func (h *SuperAdminHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	actor := mw.AgentFromCtx(ctx)

	tenantsList, err := db.ListTenants(ctx, h.pool)
	if err != nil {
		http.Error(w, "failed to load tenants: "+err.Error(), http.StatusInternalServerError)
		return
	}

	flash := r.URL.Query().Get("flash")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.SuperAdminPage(actor, tenantsList, flash).Render(ctx, w)
}

func (h *SuperAdminHandler) Create(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	slug := strings.ToLower(strings.TrimSpace(r.FormValue("slug")))

	if name == "" || slug == "" {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte("Name and slug are required fields."))
		return
	}

	if !slugRegex.MatchString(slug) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte("Slug must contain only lowercase letters, numbers, and hyphens."))
		return
	}

	if slug == "admin" || slug == "www" || slug == "localhost" || slug == "default" {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte("This slug is reserved and cannot be used."))
		return
	}

	ctx := r.Context()
	tenant, err := db.CreateTenant(ctx, h.pool, name, slug)
	if err != nil {
		log.Printf("create tenant failed: %v", err)
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte("Could not create tenant. The slug may already be in use."))
		return
	}

	log.Printf("super-admin created tenant %s (%s)", tenant.Name, tenant.Slug)
	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusOK)
}

func (h *SuperAdminHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}

	status := r.FormValue("status")
	if status != "active" && status != "suspended" {
		http.Error(w, "invalid status", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	err := db.UpdateTenantStatus(ctx, h.pool, id, status)
	if err != nil {
		log.Printf("update tenant status failed: %v", err)
		http.Error(w, "failed to update tenant status: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("super-admin updated tenant %s status to %s", id, status)
	flash := "Tenant workspace status updated successfully."
	http.Redirect(w, r, "/admin/tenants?flash="+flash, http.StatusSeeOther)
}
