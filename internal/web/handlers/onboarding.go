package handlers

import (
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"whatsapptool/internal/db"
	mw "whatsapptool/internal/web/middleware"
	"whatsapptool/internal/web/templates"
)

// OnboardingHandler shows the first-run setup wizard.
// It is mounted at /onboarding and requires NO auth.
type OnboardingHandler struct {
	pool        *pgxpool.Pool
	jwtSecret   []byte
	waPhoneID   string // WA_PHONE_NUMBER_ID (env-only, never stored in DB)
	waToken     string // WA_ACCESS_TOKEN (env-only, never stored in DB)
}

func NewOnboardingHandler(pool *pgxpool.Pool, jwtSecret []byte, waPhoneID, waToken string) *OnboardingHandler {
	return &OnboardingHandler{pool: pool, jwtSecret: jwtSecret, waPhoneID: waPhoneID, waToken: waToken}
}

func (h *OnboardingHandler) Mount(r chi.Router) {
	r.Get("/", h.Page)
	r.Post("/admin", h.CreateAdmin)
	r.Get("/setup", h.SetupPage)
}

func (h *OnboardingHandler) Page(w http.ResponseWriter, r *http.Request) {
	// If agents already exist, redirect to login.
	n, err := db.CountAgents(r.Context(), h.pool)
	if err == nil && n > 0 {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	flash := r.URL.Query().Get("err")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.OnboardingPage(flash).Render(r.Context(), w)
}

func (h *OnboardingHandler) CreateAdmin(w http.ResponseWriter, r *http.Request) {
	// Still guard against being called when agents exist.
	n, _ := db.CountAgents(r.Context(), h.pool)
	if n > 0 {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		templates.OnboardingPage("Invalid request").Render(r.Context(), w)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	email := strings.TrimSpace(strings.ToLower(r.FormValue("email")))
	password := r.FormValue("password")
	confirm := r.FormValue("confirm")

	if name == "" || email == "" {
		templates.OnboardingPage("Name and email are required").Render(r.Context(), w)
		return
	}
	if len(password) < 8 {
		templates.OnboardingPage("Password must be at least 8 characters").Render(r.Context(), w)
		return
	}
	if password != confirm {
		templates.OnboardingPage("Passwords do not match").Render(r.Context(), w)
		return
	}
	hash, err := HashPassword(password)
	if err != nil {
		log.Printf("hash password: %v", err)
		templates.OnboardingPage("Server error, please try again").Render(r.Context(), w)
		return
	}
	agent, err := db.CreateAdminAgent(r.Context(), h.pool, name, email, hash, "00000000-0000-0000-0000-000000000000")
	if err != nil {
		log.Printf("create admin: %v", err)
		templates.OnboardingPage("Email already in use or database error").Render(r.Context(), w)
		return
	}
	// Mark onboarding step-1 complete and issue session.
	db.SetConfig(r.Context(), h.pool, "00000000-0000-0000-0000-000000000000", "onboarding_complete", true) //nolint:errcheck

	token, err := mw.IssueToken(h.jwtSecret, agent.ID, agent.Email, agent.Name, agent.Role, "00000000-0000-0000-0000-000000000000")
	if err != nil {
		log.Printf("issue token: %v", err)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	mw.SetSessionCookie(w, token)
	// Continue to step 2: WhatsApp connection checklist.
	http.Redirect(w, r, "/onboarding/setup", http.StatusSeeOther)
}

func (h *OnboardingHandler) SetupPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	status := templates.OnboardingStatus{
		// WA credentials are env-only — just check if they're non-empty
		WaConnected: h.waPhoneID != "" && h.waToken != "",
	}
	tid := db.TenantFromContext(ctx)
	if tid == "" {
		tid = "00000000-0000-0000-0000-000000000000"
	}
	// Read per-item completion flags from app_config (set via Meta webhooks or admin settings)
	if v, err := db.GetConfigBool(ctx, h.pool, tid, "wa_pin_set"); err == nil {
		status.PinSet = v
	}
	if v, err := db.GetConfigBool(ctx, h.pool, tid, "display_name_approved"); err == nil {
		status.DisplayNameOK = v
	}
	if v, err := db.GetConfigBool(ctx, h.pool, tid, "business_profile_complete"); err == nil {
		status.BusinessProfileOK = v
	}
	if v, err := db.GetConfigBool(ctx, h.pool, tid, "business_verified"); err == nil {
		status.BusinessVerified = v
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.OnboardingSetupPage(status).Render(ctx, w)
}
