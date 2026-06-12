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
	pool      *pgxpool.Pool
	jwtSecret []byte
}

func NewOnboardingHandler(pool *pgxpool.Pool, jwtSecret []byte) *OnboardingHandler {
	return &OnboardingHandler{pool: pool, jwtSecret: jwtSecret}
}

func (h *OnboardingHandler) Mount(r chi.Router) {
	r.Get("/", h.Page)
	r.Post("/admin", h.CreateAdmin)
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
	agent, err := db.CreateAdminAgent(r.Context(), h.pool, name, email, hash)
	if err != nil {
		log.Printf("create admin: %v", err)
		templates.OnboardingPage("Email already in use or database error").Render(r.Context(), w)
		return
	}
	// Mark onboarding complete.
	db.SetConfig(r.Context(), h.pool, "onboarding_complete", true) //nolint:errcheck

	token, err := mw.IssueToken(h.jwtSecret, agent.ID, agent.Email, agent.Name, agent.Role)
	if err != nil {
		log.Printf("issue token: %v", err)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	mw.SetSessionCookie(w, token)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
