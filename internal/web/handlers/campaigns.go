package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"whatsapptool/internal/campaigns"
	"whatsapptool/internal/db"
	mw "whatsapptool/internal/web/middleware"
	"whatsapptool/internal/web/templates"
	"whatsapptool/internal/web/ws"
)

// CampaignHandler handles all /campaigns routes.
type CampaignHandler struct {
	pool        *pgxpool.Pool
	riverClient *river.Client[pgx.Tx]
	hub         *ws.Hub
}

func NewCampaignHandler(pool *pgxpool.Pool, rc *river.Client[pgx.Tx], hub *ws.Hub) *CampaignHandler {
	return &CampaignHandler{pool: pool, riverClient: rc, hub: hub}
}

func (h *CampaignHandler) Mount(r chi.Router) {
	r.Get("/", h.List)
	r.Get("/new", h.NewWizard)
	r.Post("/wizard/audience", h.WizardAudience)
	r.Post("/wizard/message", h.WizardMessage)
	r.Post("/wizard/schedule", h.WizardSchedule)
	r.Post("/", h.Create)
	r.Get("/{id}/report", h.Report)
	r.Get("/{id}/progress", h.ProgressPartial)
	r.Post("/{id}/cancel", h.Cancel)
	r.Post("/{id}/pause", h.Pause)
	r.Get("/{id}/recipients", h.RecipientsList)
}

func (h *CampaignHandler) List(w http.ResponseWriter, r *http.Request) {
	cs, err := db.ListCampaigns(r.Context(), h.pool)
	if err != nil {
		http.Error(w, "load campaigns: "+err.Error(), http.StatusInternalServerError)
		return
	}
	agent := mw.AgentFromCtx(r.Context())
	templates.CampaignsPage(agent, cs).Render(r.Context(), w)
}

func (h *CampaignHandler) NewWizard(w http.ResponseWriter, r *http.Request) {
	tags, _ := db.ListTags(r.Context(), h.pool)
	agent := mw.AgentFromCtx(r.Context())
	templates.WizardNewPage(agent, tags).Render(r.Context(), w)
}

func (h *CampaignHandler) WizardAudience(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "parse form", http.StatusBadRequest)
		return
	}

	agent := mw.AgentFromCtx(r.Context())

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		tags, _ := db.ListTags(r.Context(), h.pool)
		templates.WizardNewPage(agent, tags).Render(r.Context(), w)
		return
	}

	segTags := parseInt64Slice(r.Form["segment_tags"])
	exclTags := parseInt64Slice(r.Form["exclude_tags"])

	// Use "marketing" as the conservative audience filter (most restrictive).
	eligible, report, err := campaigns.BuildAudience(r.Context(), h.pool, segTags, exclTags, "marketing", 24)
	if err != nil {
		http.Error(w, "build audience: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Snapshot daily cap and today's sent count for the tier-cap banner (step 4).
	dailySent, _ := db.DailyMessagesSent(r.Context(), h.pool)
	dailyCap := db.DailyCap(r.Context(), h.pool)

	state := templates.WizardState{
		Step:          2,
		Name:          name,
		SegmentTags:   segTags,
		ExcludeTags:   exclTags,
		EligibleCount: len(eligible),
		SkipReport:    report,
		DailySent:     int(dailySent),
		DailyCap:      int(dailyCap),
	}

	tmplList, _ := db.ListTemplates(r.Context(), h.pool)
	templates.WizardStep2Page(agent, state, approvedTemplates(tmplList)).Render(r.Context(), w)
}

func (h *CampaignHandler) WizardMessage(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "parse form", http.StatusBadRequest)
		return
	}
	state, err := templates.DecodeState(r.FormValue("wizard_state"))
	if err != nil {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}

	agent := mw.AgentFromCtx(r.Context())

	templateID := r.FormValue("template_id")
	if templateID == "" {
		tmplList, _ := db.ListTemplates(r.Context(), h.pool)
		state.Step = 2
		templates.WizardStep2Page(agent, state, approvedTemplates(tmplList)).Render(r.Context(), w)
		return
	}

	varMap := map[string]string{}
	fallbacks := map[string]string{}
	for k, vs := range r.Form {
		if strings.HasPrefix(k, "var_") && len(vs) > 0 && vs[0] != "" {
			varMap[strings.TrimPrefix(k, "var_")] = vs[0]
		}
		if strings.HasPrefix(k, "fallback_") && len(vs) > 0 && vs[0] != "" {
			fallbacks[strings.TrimPrefix(k, "fallback_")] = vs[0]
		}
	}

	state.Step = 3
	state.TemplateID = templateID
	state.VarMap = varMap
	state.Fallbacks = fallbacks

	templates.WizardStep3Page(agent, state).Render(r.Context(), w)
}

func (h *CampaignHandler) WizardSchedule(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "parse form", http.StatusBadRequest)
		return
	}
	state, err := templates.DecodeState(r.FormValue("wizard_state"))
	if err != nil {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}

	agent := mw.AgentFromCtx(r.Context())

	schedType := r.FormValue("schedule_type")
	if schedType == "" {
		schedType = "now"
	}
	state.ScheduleType = schedType

	if schedType == "scheduled" {
		dtStr := r.FormValue("scheduled_at")
		if dtStr == "" {
			templates.WizardStep3ErrorPage(agent, state, "Please select a date and time.").Render(r.Context(), w)
			return
		}
		t, parseErr := parseLocalDateTime(dtStr)
		if parseErr != nil {
			templates.WizardStep3ErrorPage(agent, state, "Invalid date format.").Render(r.Context(), w)
			return
		}
		if campaigns.IsQuietHours(t) {
			templates.WizardStep3ErrorPage(agent, state, "Quiet hours: 9pm–9am IST. Choose a time between 9am and 9pm IST.").Render(r.Context(), w)
			return
		}
		state.ScheduledAt = &t
	}

	tmpl, tmplErr := db.GetTemplate(r.Context(), h.pool, state.TemplateID)
	if tmplErr != nil {
		http.Error(w, "load template: "+tmplErr.Error(), http.StatusInternalServerError)
		return
	}

	rates := db.LoadRates(r.Context(), h.pool)
	state.EstCost = campaigns.CalcTotalCost(tmpl.Category, state.EligibleCount, rates)
	state.Step = 4

	templates.WizardStep4Page(agent, state, tmpl).Render(r.Context(), w)
}

func (h *CampaignHandler) Create(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "parse form", http.StatusBadRequest)
		return
	}
	state, err := templates.DecodeState(r.FormValue("wizard_state"))
	if err != nil {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	tmpl, err := db.GetTemplate(ctx, h.pool, state.TemplateID)
	if err != nil {
		http.Error(w, "load template: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Re-compute audience to get fresh list (state may be minutes old).
	eligible, skipReport, err := campaigns.BuildAudience(ctx, h.pool, state.SegmentTags, state.ExcludeTags, tmpl.Category, 24)
	if err != nil {
		http.Error(w, "build audience: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Hard stop at daily cap.
	sent, _ := db.DailyMessagesSent(ctx, h.pool)
	if !campaigns.LimitGuardCheck(sent, db.DailyCap(ctx, h.pool)) {
		http.Error(w, "Daily send cap reached. Try again tomorrow.", http.StatusTooManyRequests)
		return
	}

	status := "scheduled"
	if state.ScheduleType == "now" {
		status = "running"
	}

	totalRecipients := len(eligible) + skipReport.Total
	eligibleIDs := make([]string, len(eligible))
	for i, e := range eligible {
		eligibleIDs[i] = e.ContactID
	}

	campaign := &db.Campaign{
		Name:              state.Name,
		TemplateID:        tmpl.ID,
		TemplateVariables: state.VarMap,
		Fallbacks:         state.Fallbacks,
		SegmentTags:       state.SegmentTags,
		ExcludeTags:       state.ExcludeTags,
		Status:            status,
		ScheduledAt:       state.ScheduledAt,
		TotalRecipients:   totalRecipients,
	}
	if err := db.CreateCampaign(ctx, h.pool, campaign); err != nil {
		http.Error(w, "create campaign: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := db.CreateRecipients(ctx, h.pool, campaign.ID, eligibleIDs, nil); err != nil {
		http.Error(w, "create recipients: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if status == "running" && h.riverClient != nil {
		pendingRecips, rErr := db.ListPendingRecipients(ctx, h.pool, campaign.ID)
		if rErr != nil {
			log.Printf("list pending for campaign %s: %v", campaign.ID, rErr)
		} else {
			fullCampaign := db.Campaign{
				ID:                campaign.ID,
				TemplateName:      tmpl.Name,
				TemplateLanguage:  tmpl.Language,
				Category:          tmpl.Category,
				TemplateVariables: state.VarMap,
				Fallbacks:         state.Fallbacks,
			}
			if err := campaigns.EnqueueCampaignJobs(ctx, h.pool, h.riverClient, fullCampaign, pendingRecips); err != nil {
				log.Printf("enqueue campaign %s: %v", campaign.ID, err)
			}
		}
	}

	http.Redirect(w, r, "/campaigns", http.StatusSeeOther)
}

func (h *CampaignHandler) Report(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	report, err := db.GetCampaignReport(r.Context(), h.pool, id)
	if err != nil {
		http.Error(w, "load report: "+err.Error(), http.StatusInternalServerError)
		return
	}
	agent := mw.AgentFromCtx(r.Context())
	templates.CampaignReportPage(agent, *report).Render(r.Context(), w)
}

func (h *CampaignHandler) ProgressPartial(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	c, err := db.GetCampaign(r.Context(), h.pool, id)
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	templates.CampaignProgressBar(*c).Render(r.Context(), w)
}

func (h *CampaignHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := db.UpdateCampaignStatus(r.Context(), h.pool, id, "cancelled"); err != nil {
		http.Error(w, "cancel: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, templates.ToastFragment(templates.ToastSuccess, "Campaign cancelled.", "", ""))
}

func (h *CampaignHandler) Pause(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := db.UpdateCampaignStatus(r.Context(), h.pool, id, "paused"); err != nil {
		http.Error(w, "pause: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, templates.ToastFragment(templates.ToastSuccess, "Campaign paused.", "", ""))
}

func (h *CampaignHandler) RecipientsList(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	recipients, err := db.GetCampaignRecipients(r.Context(), h.pool, id, 50)
	if err != nil {
		http.Error(w, "load recipients: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.CampaignRecipientRows(recipients).Render(r.Context(), w)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func approvedTemplates(ts []db.Template) []db.Template {
	var out []db.Template
	for _, t := range ts {
		if t.Status == "approved" {
			out = append(out, t)
		}
	}
	return out
}

func parseInt64Slice(ss []string) []int64 {
	var out []int64
	for _, s := range ss {
		for _, part := range strings.Split(s, ",") {
			part = strings.TrimSpace(part)
			if n, err := strconv.ParseInt(part, 10, 64); err == nil {
				out = append(out, n)
			}
		}
	}
	return out
}

func parseLocalDateTime(s string) (time.Time, error) {
	return time.Parse("2006-01-02T15:04", s)
}
