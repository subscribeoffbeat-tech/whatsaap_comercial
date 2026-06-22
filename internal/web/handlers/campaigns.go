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
	r.Post("/wizard/basics", h.WizardBasics)
	r.Post("/wizard/template", h.WizardTemplate)
	r.Post("/wizard/vars", h.WizardVars)
	r.Post("/wizard/audience", h.WizardAudience)
	r.Post("/wizard/schedule", h.WizardSchedule)
	r.Post("/", h.Create)
	r.Get("/{id}/report", h.Report)
	r.Get("/{id}/progress", h.ProgressPartial)
	r.Post("/{id}/cancel", h.Cancel)
	r.Post("/{id}/pause", h.Pause)
	r.Get("/{id}/recipients", h.RecipientsList)
}

// ── List ──────────────────────────────────────────────────────────────────────

func (h *CampaignHandler) List(w http.ResponseWriter, r *http.Request) {
	cs, err := db.ListCampaigns(r.Context(), h.pool)
	if err != nil {
		http.Error(w, "load campaigns: "+err.Error(), http.StatusInternalServerError)
		return
	}
	dailyCap := db.DailyCap(r.Context(), h.pool)
	agent := mw.AgentFromCtx(r.Context())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.CampaignsPage(agent, cs, dailyCap).Render(r.Context(), w)
}

// ── Wizard step 1: Basics ─────────────────────────────────────────────────────

func (h *CampaignHandler) NewWizard(w http.ResponseWriter, r *http.Request) {
	agent := mw.AgentFromCtx(r.Context())
	rates := db.LoadRates(r.Context(), h.pool)
	templates.WizardBasicsPage(agent, templates.WizardState{Step: 1, Category: "marketing"}, rates, "").Render(r.Context(), w)
}

func (h *CampaignHandler) WizardBasics(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "parse form", http.StatusBadRequest)
		return
	}
	agent := mw.AgentFromCtx(r.Context())
	rates := db.LoadRates(r.Context(), h.pool)

	name := strings.TrimSpace(r.FormValue("name"))
	category := r.FormValue("category")
	notes := strings.TrimSpace(r.FormValue("notes"))

	switch category {
	case "marketing", "utility", "authentication":
	default:
		category = "marketing"
	}

	state := templates.WizardState{Step: 1, Name: name, Category: category, Notes: notes}

	if name == "" {
		templates.WizardBasicsPage(agent, state, rates, "Campaign name is required.").Render(r.Context(), w)
		return
	}

	// Advance to step 2 (template selection).
	state.Step = 2
	tmplList, _ := db.ListTemplates(r.Context(), h.pool)
	templates.WizardTemplPage(agent, state, approvedTemplates(tmplList), "").Render(r.Context(), w)
}

// ── Wizard step 2: Template ───────────────────────────────────────────────────

func (h *CampaignHandler) WizardTemplate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "parse form", http.StatusBadRequest)
		return
	}
	state, err := templates.DecodeState(r.FormValue("wizard_state"))
	if err != nil {
		http.Redirect(w, r, "/campaigns/new", http.StatusSeeOther)
		return
	}
	agent := mw.AgentFromCtx(r.Context())

	if r.FormValue("action") == "back" {
		rates := db.LoadRates(r.Context(), h.pool)
		state.Step = 1
		templates.WizardBasicsPage(agent, state, rates, "").Render(r.Context(), w)
		return
	}

	templateID := r.FormValue("template_id")
	if templateID == "" {
		tmplList, _ := db.ListTemplates(r.Context(), h.pool)
		templates.WizardTemplPage(agent, state, approvedTemplates(tmplList), "Please select a template to continue.").Render(r.Context(), w)
		return
	}

	tmpl, err := db.GetTemplate(r.Context(), h.pool, templateID)
	if err != nil {
		http.Error(w, "template not found", http.StatusNotFound)
		return
	}

	state.TemplateID = templateID
	varNames := templates.ExtractVarNames(templates.TemplateBodyText(*tmpl))
	state.HasVars = len(varNames) > 0

	if state.HasVars {
		state.Step = 3
		templates.WizardVarsPage(agent, state, *tmpl, varNames).Render(r.Context(), w)
	} else {
		tags, totalOptedIn, _ := db.ListTagsWithOptedInCount(r.Context(), h.pool)
		state.Step = 4
		templates.WizardAudiencePage(agent, state, tags, totalOptedIn, "").Render(r.Context(), w)
	}
}

// ── Wizard step 3: Variables ──────────────────────────────────────────────────

func (h *CampaignHandler) WizardVars(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "parse form", http.StatusBadRequest)
		return
	}
	state, err := templates.DecodeState(r.FormValue("wizard_state"))
	if err != nil {
		http.Redirect(w, r, "/campaigns/new", http.StatusSeeOther)
		return
	}
	agent := mw.AgentFromCtx(r.Context())

	if r.FormValue("action") == "back" {
		tmplList, _ := db.ListTemplates(r.Context(), h.pool)
		state.Step = 2
		templates.WizardTemplPage(agent, state, approvedTemplates(tmplList), "").Render(r.Context(), w)
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
	state.VarMap = varMap
	state.Fallbacks = fallbacks
	state.Step = 4

	tags, totalOptedIn, _ := db.ListTagsWithOptedInCount(r.Context(), h.pool)
	templates.WizardAudiencePage(agent, state, tags, totalOptedIn, "").Render(r.Context(), w)
}

// ── Wizard step 4: Audience ───────────────────────────────────────────────────

func (h *CampaignHandler) WizardAudience(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "parse form", http.StatusBadRequest)
		return
	}
	state, err := templates.DecodeState(r.FormValue("wizard_state"))
	if err != nil {
		http.Redirect(w, r, "/campaigns/new", http.StatusSeeOther)
		return
	}
	agent := mw.AgentFromCtx(r.Context())

	if r.FormValue("action") == "back" {
		if state.HasVars {
			tmpl, _ := db.GetTemplate(r.Context(), h.pool, state.TemplateID)
			if tmpl != nil {
				varNames := templates.ExtractVarNames(templates.TemplateBodyText(*tmpl))
				state.Step = 3
				templates.WizardVarsPage(agent, state, *tmpl, varNames).Render(r.Context(), w)
				return
			}
		}
		tmplList, _ := db.ListTemplates(r.Context(), h.pool)
		state.Step = 2
		templates.WizardTemplPage(agent, state, approvedTemplates(tmplList), "").Render(r.Context(), w)
		return
	}

	useAll := r.FormValue("use_all_contacts") == "1"
	segTagIDs := parseInt64Slice(r.Form["segment_tag_ids"])

	if !useAll && len(segTagIDs) == 0 {
		tags, totalOptedIn, _ := db.ListTagsWithOptedInCount(r.Context(), h.pool)
		templates.WizardAudiencePage(agent, state, tags, totalOptedIn, "Select at least one audience segment.").Render(r.Context(), w)
		return
	}

	if useAll {
		state.UseAllContacts = true
		state.SegmentTagIDs = nil
	} else {
		state.UseAllContacts = false
		state.SegmentTagIDs = segTagIDs
	}

	eligible, report, err := campaigns.BuildAudienceAny(r.Context(), h.pool, state.SegmentTagIDs, state.Category, 24)
	if err != nil {
		http.Error(w, "build audience: "+err.Error(), http.StatusInternalServerError)
		return
	}
	state.EligibleCount = len(eligible)
	state.SkipReport = report

	dailySent, _ := db.DailyMessagesSent(r.Context(), h.pool)
	dailyCap := db.DailyCap(r.Context(), h.pool)
	state.DailySent = int(dailySent)
	state.DailyCap = int(dailyCap)
	state.Step = 5

	templates.WizardSchedulePage(agent, state, "").Render(r.Context(), w)
}

// ── Wizard step 5: Schedule ───────────────────────────────────────────────────

func (h *CampaignHandler) WizardSchedule(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "parse form", http.StatusBadRequest)
		return
	}
	state, err := templates.DecodeState(r.FormValue("wizard_state"))
	if err != nil {
		http.Redirect(w, r, "/campaigns/new", http.StatusSeeOther)
		return
	}
	agent := mw.AgentFromCtx(r.Context())

	if r.FormValue("action") == "back" {
		tags, totalOptedIn, _ := db.ListTagsWithOptedInCount(r.Context(), h.pool)
		state.Step = 4
		templates.WizardAudiencePage(agent, state, tags, totalOptedIn, "").Render(r.Context(), w)
		return
	}

	schedType := r.FormValue("schedule_type")
	if schedType == "" {
		schedType = "now"
	}
	state.ScheduleType = schedType

	if schedType == "scheduled" {
		dtStr := r.FormValue("scheduled_at")
		if dtStr == "" {
			templates.WizardSchedulePage(agent, state, "Please select a date and time.").Render(r.Context(), w)
			return
		}
		t, parseErr := parseLocalDateTime(dtStr)
		if parseErr != nil {
			templates.WizardSchedulePage(agent, state, "Invalid date format.").Render(r.Context(), w)
			return
		}
		if campaigns.IsQuietHours(t) {
			templates.WizardSchedulePage(agent, state, "Quiet hours: 9 pm–9 am IST. Choose a time between 9 am and 9 pm IST.").Render(r.Context(), w)
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
	state.Step = 6

	templates.WizardReviewPage(agent, state, tmpl).Render(r.Context(), w)
}

// ── Wizard step 6: Review / Create ───────────────────────────────────────────

func (h *CampaignHandler) Create(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "parse form", http.StatusBadRequest)
		return
	}
	state, err := templates.DecodeState(r.FormValue("wizard_state"))
	if err != nil {
		http.Redirect(w, r, "/campaigns/new", http.StatusSeeOther)
		return
	}
	agent := mw.AgentFromCtx(r.Context())

	if r.FormValue("action") == "back" {
		state.Step = 5
		templates.WizardSchedulePage(agent, state, "").Render(r.Context(), w)
		return
	}

	ctx := r.Context()
	tmpl, err := db.GetTemplate(ctx, h.pool, state.TemplateID)
	if err != nil {
		http.Error(w, "load template: "+err.Error(), http.StatusInternalServerError)
		return
	}

	eligible, skipReport, err := campaigns.BuildAudienceAny(ctx, h.pool, state.SegmentTagIDs, state.Category, 24)
	if err != nil {
		http.Error(w, "build audience: "+err.Error(), http.StatusInternalServerError)
		return
	}

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

	segTags := state.SegmentTagIDs
	if segTags == nil {
		segTags = []int64{}
	}

	campaign := &db.Campaign{
		Name:              state.Name,
		TemplateID:        tmpl.ID,
		TemplateVariables: state.VarMap,
		Fallbacks:         state.Fallbacks,
		SegmentTags:       segTags,
		ExcludeTags:       []int64{},
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

// ── Report / progress / control ───────────────────────────────────────────────

func (h *CampaignHandler) Report(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	report, err := db.GetCampaignReport(r.Context(), h.pool, id)
	if err != nil {
		http.Error(w, "load report: "+err.Error(), http.StatusInternalServerError)
		return
	}
	failed, _ := db.GetFailedRecipients(r.Context(), h.pool, id)
	hourly, _ := db.GetHourlySendDistribution(r.Context(), h.pool, id)
	agent := mw.AgentFromCtx(r.Context())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.CampaignReportPage(agent, *report, failed, hourly).Render(r.Context(), w)
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
