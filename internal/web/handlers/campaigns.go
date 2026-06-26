package handlers

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
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
	"whatsapptool/internal/whatsapp"
)

// CampaignHandler handles all /campaigns routes.
type CampaignHandler struct {
	pool        *pgxpool.Pool
	riverClient *river.Client[pgx.Tx]
	hub         *ws.Hub
	waClient    *whatsapp.Client
}

func NewCampaignHandler(pool *pgxpool.Pool, rc *river.Client[pgx.Tx], hub *ws.Hub, wa *whatsapp.Client) *CampaignHandler {
	return &CampaignHandler{pool: pool, riverClient: rc, hub: hub, waClient: wa}
}

// reviewErr re-renders the review step with an error message (used for
// header-media validation failures), keeping the media-upload field visible.
func (h *CampaignHandler) reviewErr(w http.ResponseWriter, r *http.Request, agent *mw.AgentClaims, state templates.WizardState, tmpl *db.Template, msg string) {
	state.Step = 6
	templates.WizardReviewPage(agent, state, tmpl, msg).Render(r.Context(), w)
}

// freqCapHours returns the admin-configured frequency-cap window (hours),
// defaulting to 24 (the hard rule: 1 marketing message per contact per 24h).
func (h *CampaignHandler) freqCapHours(ctx context.Context) int {
	if v, err := db.GetConfigInt(ctx, h.pool, "freq_cap_hours"); err == nil && v > 0 {
		return int(v)
	}
	return 24
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
	r.Post("/{id}/launch", h.Launch)
	r.Post("/{id}/cancel", h.Cancel)
	r.Post("/{id}/pause", h.Pause)
	r.Get("/{id}/recipients", h.RecipientsList)
	r.Get("/{id}/export", h.Export)
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
	// Image/video/document templates need the real media uploaded before launch.
	state.MediaHeaderFormat = tmpl.HeaderMediaFormat()
	state.HasMediaHeader = state.MediaHeaderFormat != ""

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
			idx := strings.TrimPrefix(k, "var_")
			val := vs[0]
			// "Custom field…" — combine with the typed key into a custom_fields path.
			if val == "__custom__" {
				ck := sanitizeFieldKey(r.FormValue("customkey_" + idx))
				if ck == "" {
					continue // no key typed → leave unmapped; the fallback is used
				}
				val = "custom_fields." + ck
			}
			varMap[idx] = val
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

	eligible, report, err := campaigns.BuildAudienceAny(r.Context(), h.pool, state.SegmentTagIDs, state.Category, h.freqCapHours(r.Context()))
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
		if campaigns.IsQuietHoursCfg(r.Context(), h.pool, t) {
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

	templates.WizardReviewPage(agent, state, tmpl, "").Render(r.Context(), w)
}

// ── Wizard step 6: Review / Create ───────────────────────────────────────────

func (h *CampaignHandler) Create(w http.ResponseWriter, r *http.Request) {
	// The schedule form is multipart when the template needs a header media
	// upload, plain urlencoded otherwise. ParseMultipartForm populates r.Form
	// either way; tolerate ErrNotMultipart so non-media campaigns still work.
	if err := r.ParseMultipartForm(32 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
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

	eligible, skipReport, err := campaigns.BuildAudienceAny(ctx, h.pool, state.SegmentTagIDs, state.Category, h.freqCapHours(ctx))
	if err != nil {
		http.Error(w, "build audience: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// A draft isn't sending now, so the daily cap doesn't apply to it.
	isDraft := r.FormValue("action") == "draft"
	if !isDraft {
		sent, _ := db.DailyMessagesSent(ctx, h.pool)
		if !campaigns.LimitGuardCheck(sent, db.DailyCap(ctx, h.pool)) {
			http.Error(w, "Daily send cap reached. Try again tomorrow.", http.StatusTooManyRequests)
			return
		}
	}

	// Header media: templates with an IMAGE/VIDEO/DOCUMENT header require the
	// owner to upload the actual media now (the approved sample only gets the
	// template approved — each send must carry the real media). Upload it to Meta
	// up front so a bad file fails before we create an orphan campaign; the
	// returned media ID is reused for every recipient.
	mediaFormat := tmpl.HeaderMediaFormat()
	var headerMediaID, headerMediaName string
	var headerMediaBytes []byte
	if mediaFormat != "" {
		file, fh, ferr := r.FormFile("header_media")
		if ferr != nil {
			h.reviewErr(w, r, agent, state, tmpl, "Please upload the "+mediaFormat+" used in this template's header before launching.")
			return
		}
		defer file.Close()

		maxBytes := int64(5 << 20) // image: 5 MB
		switch mediaFormat {
		case "video":
			maxBytes = 16 << 20 // 16 MB
		case "document":
			maxBytes = 100 << 20 // 100 MB
		}
		headerMediaBytes, err = io.ReadAll(io.LimitReader(file, maxBytes+1))
		if err != nil {
			h.reviewErr(w, r, agent, state, tmpl, "Could not read the uploaded file. Please try again.")
			return
		}
		if len(headerMediaBytes) == 0 {
			h.reviewErr(w, r, agent, state, tmpl, "The uploaded file is empty.")
			return
		}
		if int64(len(headerMediaBytes)) > maxBytes {
			h.reviewErr(w, r, agent, state, tmpl, fmt.Sprintf("File is too large for a %s header (max %d MB).", mediaFormat, maxBytes>>20))
			return
		}

		headerMediaName = filepath.Base(fh.Filename)
		mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(headerMediaName)))
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		mid, uerr := h.waClient.UploadMediaStream(ctx, bytes.NewReader(headerMediaBytes), headerMediaName, mimeType)
		if uerr != nil {
			log.Printf("campaign header media upload to Meta failed: %v", uerr)
			h.reviewErr(w, r, agent, state, tmpl, "Uploading the media to WhatsApp failed: "+uerr.Error())
			return
		}
		headerMediaID = mid
	}

	status := "scheduled"
	if state.ScheduleType == "now" {
		status = "running"
	}
	if isDraft {
		status = "draft" // saved for later; not scheduled, not sent
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

	// Persist the header media: store the file on disk (so it can be re-uploaded
	// if Meta's media ID later expires) and save the path + media ID + type on the
	// campaign. The worker reads these at send time. Done before enqueuing so the
	// media ID is present when the first job runs.
	if mediaFormat != "" && headerMediaID != "" {
		storedPath := ""
		dir := filepath.Join("media", "campaigns", campaign.ID)
		if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
			log.Printf("campaign %s: mkdir header media dir: %v", campaign.ID, mkErr)
		} else {
			storedPath = filepath.Join(dir, headerMediaName)
			if wErr := os.WriteFile(storedPath, headerMediaBytes, 0o644); wErr != nil {
				log.Printf("campaign %s: store header media: %v", campaign.ID, wErr)
				storedPath = "" // initial send still works via the media ID
			}
		}
		if sErr := db.SetCampaignHeaderMedia(ctx, h.pool, campaign.ID, storedPath, headerMediaID, mediaFormat); sErr != nil {
			log.Printf("campaign %s: save header media: %v", campaign.ID, sErr)
		}
	}

	if err := db.CreateRecipients(ctx, h.pool, campaign.ID, eligibleIDs, skipReport.Skipped); err != nil {
		http.Error(w, "create recipients: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if skipReport.Total > 0 {
		if _, err := h.pool.Exec(ctx, `UPDATE campaigns SET skipped_count = $2 WHERE id = $1::uuid`,
			campaign.ID, skipReport.Total); err != nil {
			log.Printf("set skipped_count for campaign %s: %v", campaign.ID, err)
		}
	}

	// If everyone was filtered out (e.g. all hit the 24h frequency cap), there's
	// nothing to send — mark the campaign completed so it doesn't sit in
	// "running" forever. The skipped recipients + reasons are in the report.
	if status == "running" && len(eligible) == 0 {
		if err := db.UpdateCampaignStatus(ctx, h.pool, campaign.ID, "completed"); err != nil {
			log.Printf("complete empty campaign %s: %v", campaign.ID, err)
		}
	}

	if status == "running" && len(eligible) > 0 && h.riverClient != nil {
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
	// Recompute counters from the messages table so the report is fresh even if a
	// delivery webhook was missed (best-effort).
	if err := db.SyncCampaignStats(r.Context(), h.pool, id); err != nil {
		log.Printf("report sync stats %s: %v", id, err)
	}
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

// Launch starts a draft campaign: it builds the send jobs for the draft's
// pending recipients and flips it to running (send now). Only drafts can be
// launched this way; anything else is a no-op redirect.
func (h *CampaignHandler) Launch(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	camp, err := db.GetCampaign(ctx, h.pool, id)
	if err != nil {
		http.Error(w, "load campaign: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if camp.Status != "draft" {
		http.Redirect(w, r, "/campaigns/"+id+"/report", http.StatusSeeOther)
		return
	}

	// Respect the daily cap at launch (a draft may sit for days).
	sent, _ := db.DailyMessagesSent(ctx, h.pool)
	if !campaigns.LimitGuardCheck(sent, db.DailyCap(ctx, h.pool)) {
		http.Error(w, "Daily send cap reached. Try again tomorrow.", http.StatusTooManyRequests)
		return
	}

	pending, err := db.ListPendingRecipients(ctx, h.pool, id)
	if err != nil {
		http.Error(w, "list recipients: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if len(pending) == 0 {
		_ = db.UpdateCampaignStatus(ctx, h.pool, id, "completed")
		http.Redirect(w, r, "/campaigns/"+id+"/report", http.StatusSeeOther)
		return
	}
	if err := db.UpdateCampaignStatus(ctx, h.pool, id, "running"); err != nil {
		http.Error(w, "launch: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if h.riverClient != nil {
		if err := campaigns.EnqueueCampaignJobs(ctx, h.pool, h.riverClient, *camp, pending); err != nil {
			log.Printf("launch enqueue %s: %v", id, err)
		}
	}
	http.Redirect(w, r, "/campaigns/"+id+"/report", http.StatusSeeOther)
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

// Export streams the campaign's recipients as CSV. ?type=failed limits it to
// failed/skipped recipients (used by the report's "export failed" button).
func (h *CampaignHandler) Export(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	recipients, err := db.GetCampaignRecipients(r.Context(), h.pool, id, 100000)
	if err != nil {
		http.Error(w, "load recipients: "+err.Error(), http.StatusInternalServerError)
		return
	}
	onlyFailed := r.URL.Query().Get("type") == "failed"
	filename := "campaign-recipients.csv"
	if onlyFailed {
		filename = "campaign-failed.csv"
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))

	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"name", "phone", "status", "reason", "sent_at"})
	for _, rc := range recipients {
		if onlyFailed && rc.Status != "failed" && rc.Status != "skipped" {
			continue
		}
		reason := ""
		if rc.SkipReason != nil {
			reason = *rc.SkipReason
		}
		sentAt := ""
		if rc.SentAt != nil {
			sentAt = rc.SentAt.Format(time.RFC3339)
		}
		_ = cw.Write([]string{rc.Name, rc.WAPhone, rc.Status, reason, sentAt})
	}
	cw.Flush()
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

// sanitizeFieldKey normalises a user-typed custom field key: lowercased, spaces
// and dashes become underscores, and anything outside [a-z0-9_] is dropped. This
// keeps the stored custom_fields path safe and matches CSV-import key handling.
func sanitizeFieldKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '-':
			b.WriteRune('_')
		}
	}
	return b.String()
}

// parseLocalDateTime parses the wizard's datetime-local value as IST wall-clock
// time (the whole app schedules in IST). Without an explicit location time.Parse
// would treat it as UTC, storing a time 5h30m later than the user intended.
func parseLocalDateTime(s string) (time.Time, error) {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		loc = time.FixedZone("IST", 5*60*60+30*60)
	}
	return time.ParseInLocation("2006-01-02T15:04", s, loc)
}
