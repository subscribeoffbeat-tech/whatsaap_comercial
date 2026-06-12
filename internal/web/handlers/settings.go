package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"whatsapptool/internal/db"
	mw "whatsapptool/internal/web/middleware"
	"whatsapptool/internal/web/templates"
)

// SettingsHandler manages /settings routes (admin only).
type SettingsHandler struct {
	pool *pgxpool.Pool
}

func NewSettingsHandler(pool *pgxpool.Pool) *SettingsHandler {
	return &SettingsHandler{pool: pool}
}

func (h *SettingsHandler) Mount(r chi.Router) {
	r.Get("/", h.Page)
	r.Post("/sending", h.UpdateSending)
	r.Post("/costs", h.UpdateCosts)
	r.Post("/retention", h.UpdateRetention)
}

func (h *SettingsHandler) Page(w http.ResponseWriter, r *http.Request) {
	d := h.loadSettings(r)
	actor := mw.AgentFromCtx(r.Context())
	saved := r.URL.Query().Get("saved") == "1"
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.SettingsPage(d, actor, saved).Render(r.Context(), w)
}

func (h *SettingsHandler) UpdateSending(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	actor := mw.AgentFromCtx(ctx)
	for _, kv := range []struct{ key, form string }{
		{"quiet_hours_start_ist", "quiet_start"},
		{"quiet_hours_end_ist", "quiet_end"},
	} {
		if v := r.FormValue(kv.form); v != "" {
			db.SetConfig(ctx, h.pool, kv.key, v) //nolint:errcheck
		}
	}
	if v := r.FormValue("freq_cap_hours"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			db.SetConfig(ctx, h.pool, "freq_cap_hours", n) //nolint:errcheck
		}
	}
	db.Log(ctx, h.pool, actor.ID, "settings_updated", "config", "sending_rules",
		map[string]any{"quiet_start": r.FormValue("quiet_start"), "quiet_end": r.FormValue("quiet_end"),
			"freq_cap_hours": r.FormValue("freq_cap_hours")})
	http.Redirect(w, r, "/settings?saved=1", http.StatusSeeOther)
}

func (h *SettingsHandler) UpdateCosts(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	actor := mw.AgentFromCtx(ctx)
	for _, kv := range []struct{ key, form string }{
		{"marketing_cost_inr", "marketing"},
		{"utility_cost_inr", "utility"},
		{"auth_cost_inr", "auth"},
		{"gst_rate", "gst_rate"},
	} {
		if v := r.FormValue(kv.form); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				db.SetConfig(ctx, h.pool, kv.key, f) //nolint:errcheck
			}
		}
	}
	db.Log(ctx, h.pool, actor.ID, "settings_updated", "config", "cost_rates",
		map[string]any{"marketing": r.FormValue("marketing"), "utility": r.FormValue("utility"),
			"auth": r.FormValue("auth"), "gst_rate": r.FormValue("gst_rate")})
	http.Redirect(w, r, "/settings?saved=1", http.StatusSeeOther)
}

func (h *SettingsHandler) UpdateRetention(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	actor := mw.AgentFromCtx(ctx)
	if v := r.FormValue("retention_months"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			db.SetConfig(ctx, h.pool, "data_retention_months", n) //nolint:errcheck
		}
	}
	db.Log(ctx, h.pool, actor.ID, "settings_updated", "config", "data_retention",
		map[string]any{"retention_months": r.FormValue("retention_months")})
	http.Redirect(w, r, "/settings?saved=1", http.StatusSeeOther)
}

func (h *SettingsHandler) loadSettings(r *http.Request) templates.SettingsViewData {
	ctx := r.Context()
	d := templates.SettingsViewData{
		QuietHoursStart: "21:00",
		QuietHoursEnd:   "09:00",
		FreqCapHours:    24,
		RetentionMonths: 24,
		MarketingCost:   0.8631,
		UtilityCost:     0.115,
		AuthCost:        0.115,
		GSTRate:         0.18,
	}
	if v, err := db.GetConfigFloat(ctx, h.pool, "marketing_cost_inr"); err == nil {
		d.MarketingCost = v
	}
	if v, err := db.GetConfigFloat(ctx, h.pool, "utility_cost_inr"); err == nil {
		d.UtilityCost = v
	}
	if v, err := db.GetConfigFloat(ctx, h.pool, "auth_cost_inr"); err == nil {
		d.AuthCost = v
	}
	if v, err := db.GetConfigFloat(ctx, h.pool, "gst_rate"); err == nil {
		d.GSTRate = v
	}
	if v, err := db.GetConfigInt(ctx, h.pool, "freq_cap_hours"); err == nil {
		d.FreqCapHours = v
	}
	if v, err := db.GetConfigInt(ctx, h.pool, "data_retention_months"); err == nil {
		d.RetentionMonths = v
	}
	if v, err := db.GetConfigInt(ctx, h.pool, "meta_tier"); err == nil {
		d.MetaTier = v
	}
	qi, _ := db.GetQualityInfo(ctx, h.pool)
	d.QualityRating = qi.QualityRating

	// Quiet hours stored as quoted JSON strings — read raw.
	tryStr := func(key string) string {
		var s string
		h.pool.QueryRow(ctx, `SELECT value#>>'{}' FROM app_config WHERE key=$1`, key).Scan(&s) //nolint:errcheck
		return s
	}
	if v := tryStr("quiet_hours_start_ist"); v != "" {
		d.QuietHoursStart = v
	}
	if v := tryStr("quiet_hours_end_ist"); v != "" {
		d.QuietHoursEnd = v
	}
	var displayName string
	h.pool.QueryRow(ctx, `SELECT value#>>'{}' FROM app_config WHERE key='wa_display_name'`).Scan(&displayName) //nolint:errcheck
	d.WADisplayName = displayName
	return d
}
