package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"whatsapptool/internal/config"
	"whatsapptool/internal/db"
	mw "whatsapptool/internal/web/middleware"
	"whatsapptool/internal/web/templates"
)

// SettingsHandler manages /settings routes (admin only).
type SettingsHandler struct {
	pool *pgxpool.Pool
	wa   config.WAConfig
}

func NewSettingsHandler(pool *pgxpool.Pool, wa config.WAConfig) *SettingsHandler {
	return &SettingsHandler{pool: pool, wa: wa}
}

func (h *SettingsHandler) Mount(r chi.Router) {
	r.Get("/", h.Page)
	r.Post("/sending", h.UpdateSending)
	r.Post("/costs", h.UpdateCosts)
	r.Post("/retention", h.UpdateRetention)
	r.Post("/profile", h.UpdateProfile)
	r.Post("/keywords", h.AddKeyword)
	r.Post("/keywords/remove", h.RemoveKeyword)
}

// ── Page ─────────────────────────────────────────────────────────────────────

func (h *SettingsHandler) Page(w http.ResponseWriter, r *http.Request) {
	d := h.loadSettings(r)
	actor := mw.AgentFromCtx(r.Context())
	saved := r.URL.Query().Get("saved") == "1"
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.SettingsPage(d, actor, saved).Render(r.Context(), w)
}

// ── Sending rules ─────────────────────────────────────────────────────────────

func (h *SettingsHandler) UpdateSending(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	tid := db.TenantFromContext(ctx)
	actor := mw.AgentFromCtx(ctx)
	for _, kv := range []struct{ key, form string }{
		{"quiet_hours_start_ist", "quiet_start"},
		{"quiet_hours_end_ist", "quiet_end"},
	} {
		if v := r.FormValue(kv.form); v != "" {
			db.SetConfig(ctx, h.pool, tid, kv.key, v) //nolint:errcheck
		}
	}
	if v := r.FormValue("freq_cap_hours"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			db.SetConfig(ctx, h.pool, tid, "freq_cap_hours", n) //nolint:errcheck
		}
	}
	db.Log(ctx, h.pool, actor.ID, "settings_updated", "config", "sending_rules",
		map[string]any{"quiet_start": r.FormValue("quiet_start"), "quiet_end": r.FormValue("quiet_end"),
			"freq_cap_hours": r.FormValue("freq_cap_hours")})
	http.Redirect(w, r, "/settings?tab=quiet-hours&saved=1", http.StatusSeeOther)
}

// ── Costs ─────────────────────────────────────────────────────────────────────

func (h *SettingsHandler) UpdateCosts(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	tid := db.TenantFromContext(ctx)
	actor := mw.AgentFromCtx(ctx)
	for _, kv := range []struct{ key, form string }{
		{"marketing_cost_inr", "marketing"},
		{"utility_cost_inr", "utility"},
		{"auth_cost_inr", "auth"},
		{"gst_rate", "gst_rate"},
	} {
		if v := r.FormValue(kv.form); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				db.SetConfig(ctx, h.pool, tid, kv.key, f) //nolint:errcheck
			}
		}
	}
	db.Log(ctx, h.pool, actor.ID, "settings_updated", "config", "cost_rates",
		map[string]any{"marketing": r.FormValue("marketing"), "utility": r.FormValue("utility"),
			"auth": r.FormValue("auth"), "gst_rate": r.FormValue("gst_rate")})
	http.Redirect(w, r, "/settings?tab=rates&saved=1", http.StatusSeeOther)
}

// ── Data retention ────────────────────────────────────────────────────────────

func (h *SettingsHandler) UpdateRetention(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	tid := db.TenantFromContext(ctx)
	actor := mw.AgentFromCtx(ctx)
	if v := r.FormValue("retention_months"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			db.SetConfig(ctx, h.pool, tid, "data_retention_months", n) //nolint:errcheck
		}
	}
	db.Log(ctx, h.pool, actor.ID, "settings_updated", "config", "data_retention",
		map[string]any{"retention_months": r.FormValue("retention_months")})
	http.Redirect(w, r, "/settings?tab=connection&saved=1", http.StatusSeeOther)
}

// ── Business profile ──────────────────────────────────────────────────────────

func (h *SettingsHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	tid := db.TenantFromContext(ctx)
	actor := mw.AgentFromCtx(ctx)
	for _, kv := range []struct{ key, form string }{
		{"biz_name", "biz_name"},
		{"biz_about", "biz_about"},
		{"biz_address", "biz_address"},
		{"biz_industry", "biz_industry"},
	} {
		db.SetConfig(ctx, h.pool, tid, kv.key, r.FormValue(kv.form)) //nolint:errcheck
	}
	db.Log(ctx, h.pool, actor.ID, "settings_updated", "config", "business_profile", nil)
	http.Redirect(w, r, "/settings?tab=profile&saved=1", http.StatusSeeOther)
}

// ── Opt-out keywords ──────────────────────────────────────────────────────────

func (h *SettingsHandler) AddKeyword(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	kw := strings.ToUpper(strings.TrimSpace(r.FormValue("keyword")))
	if kw != "" {
		kws := loadStopKeywords(r, h.pool)
		if !containsKeyword(kws, kw) {
			kws = append(kws, kw)
			saveStopKeywords(r, h.pool, kws)
		}
	}
	http.Redirect(w, r, "/settings?tab=opt-out&saved=1", http.StatusSeeOther)
}

func (h *SettingsHandler) RemoveKeyword(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	kw := strings.ToUpper(strings.TrimSpace(r.FormValue("keyword")))
	// STOP and UNSUBSCRIBE are mandated by Meta — cannot remove them.
	if kw == "STOP" || kw == "UNSUBSCRIBE" {
		http.Redirect(w, r, "/settings?tab=opt-out", http.StatusSeeOther)
		return
	}
	kws := loadStopKeywords(r, h.pool)
	var filtered []string
	for _, k := range kws {
		if k != kw {
			filtered = append(filtered, k)
		}
	}
	saveStopKeywords(r, h.pool, filtered)
	http.Redirect(w, r, "/settings?tab=opt-out&saved=1", http.StatusSeeOther)
}

// ── Load helpers ──────────────────────────────────────────────────────────────

var defaultStopKeywords = []string{"STOP", "UNSUBSCRIBE", "OPT OUT", "OPTOUT", "OPT-OUT", "QUIT", "CANCEL", "END"}

func loadStopKeywords(r *http.Request, pool *pgxpool.Pool) []string {
	var raw string
	tid := db.TenantFromContext(r.Context())
	pool.QueryRow(r.Context(), `SELECT value#>>'{}' FROM app_config WHERE tenant_id = $2::uuid AND key=$1`, "stop_keywords", tid).Scan(&raw) //nolint:errcheck
	if raw == "" {
		return defaultStopKeywords
	}
	var kws []string
	if err := json.Unmarshal([]byte(raw), &kws); err != nil {
		return defaultStopKeywords
	}
	return kws
}

func saveStopKeywords(r *http.Request, pool *pgxpool.Pool, kws []string) {
	b, _ := json.Marshal(kws)
	tid := db.TenantFromContext(r.Context())
	pool.QueryRow(r.Context(), `INSERT INTO app_config (tenant_id, key, value) VALUES ($2::uuid, 'stop_keywords', $1::jsonb)
		ON CONFLICT (tenant_id, key) DO UPDATE SET value=$1::jsonb, updated_at=NOW()`, string(b), tid).Scan() //nolint:errcheck
}

func containsKeyword(kws []string, kw string) bool {
	for _, k := range kws {
		if k == kw {
			return true
		}
	}
	return false
}

func (h *SettingsHandler) loadSettings(r *http.Request) templates.SettingsViewData {
	ctx := r.Context()
	tid := db.TenantFromContext(ctx)
	d := templates.SettingsViewData{
		QuietHoursStart: "21:00",
		QuietHoursEnd:   "09:00",
		FreqCapHours:    24,
		RetentionMonths: 24,
		MarketingCost:   0.8631,
		UtilityCost:     0.115,
		AuthCost:        0.115,
		GSTRate:         0.18,
		WAWABAId:        h.wa.WABAID,
		WAAPIVersion:    "v23.0",
		StopKeywords:    defaultStopKeywords,
	}

	_, _ = db.GetConfigString(ctx, h.pool, tid, "whatsapp_phone_number_id")
	wabaID, _ := db.GetConfigString(ctx, h.pool, tid, "whatsapp_waba_id")
	if wabaID != "" {
		d.WAWABAId = wabaID
	}

	if v, err := db.GetConfigFloat(ctx, h.pool, tid, "marketing_cost_inr"); err == nil {
		d.MarketingCost = v
	}
	if v, err := db.GetConfigFloat(ctx, h.pool, tid, "utility_cost_inr"); err == nil {
		d.UtilityCost = v
	}
	if v, err := db.GetConfigFloat(ctx, h.pool, tid, "auth_cost_inr"); err == nil {
		d.AuthCost = v
	}
	if v, err := db.GetConfigFloat(ctx, h.pool, tid, "gst_rate"); err == nil {
		d.GSTRate = v
	}
	if v, err := db.GetConfigInt(ctx, h.pool, tid, "freq_cap_hours"); err == nil {
		d.FreqCapHours = v
	}
	if v, err := db.GetConfigInt(ctx, h.pool, tid, "data_retention_months"); err == nil {
		d.RetentionMonths = v
	}
	if v, err := db.GetConfigInt(ctx, h.pool, tid, "meta_tier"); err == nil {
		d.MetaTier = v
	}
	qi, _ := db.GetQualityInfo(ctx, h.pool)
	d.QualityRating = qi.QualityRating

	tryStr := func(key string) string {
		var s string
		h.pool.QueryRow(ctx, `SELECT value#>>'{}' FROM app_config WHERE tenant_id = $2::uuid AND key=$1`, key, tid).Scan(&s) //nolint:errcheck
		return s
	}
	if v := tryStr("quiet_hours_start_ist"); v != "" {
		d.QuietHoursStart = v
	}
	if v := tryStr("quiet_hours_end_ist"); v != "" {
		d.QuietHoursEnd = v
	}
	d.WADisplayName = tryStr("wa_display_name")
	d.WADisplayPhone = tryStr("wa_display_phone")
	d.BizName = tryStr("biz_name")
	d.BizAbout = tryStr("biz_about")
	d.BizAddress = tryStr("biz_address")
	d.BizIndustry = tryStr("biz_industry")

	// Stop keywords from DB
	if raw := tryStr("stop_keywords"); raw != "" {
		var kws []string
		if err := json.Unmarshal([]byte(raw), &kws); err == nil && len(kws) > 0 {
			d.StopKeywords = kws
		}
	}

	d.ActiveTab = r.URL.Query().Get("tab")
	if d.ActiveTab == "" {
		d.ActiveTab = "connection"
	}
	return d
}
