package handlers

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"whatsapptool/internal/db"
	mw "whatsapptool/internal/web/middleware"
	"whatsapptool/internal/web/templates"
)

type AccountHandler struct {
	pool      *pgxpool.Pool
	jwtSecret []byte
}

func NewAccountHandler(pool *pgxpool.Pool, jwtSecret []byte) *AccountHandler {
	return &AccountHandler{pool: pool, jwtSecret: jwtSecret}
}

func (h *AccountHandler) Mount(r chi.Router) {
	r.Get("/", h.Page)
	r.Post("/profile", h.UpdateProfile)
	r.Post("/notifications", h.UpdateNotifications)
	r.Post("/signout-all", h.SignOutAll)
}

func (h *AccountHandler) Page(w http.ResponseWriter, r *http.Request) {
	agent := mw.AgentFromCtx(r.Context())
	if agent == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	a, err := db.GetAgentByID(r.Context(), h.pool, agent.ID)
	if err != nil {
		log.Printf("account page: get agent: %v", err)
		http.Error(w, "agent not found", http.StatusInternalServerError)
		return
	}
	flash := r.URL.Query().Get("flash")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.AccountPage(agent, a, flash).Render(r.Context(), w); err != nil {
		log.Printf("account page render: %v", err)
	}
}

func (h *AccountHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	agent := mw.AgentFromCtx(r.Context())
	if agent == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	email := strings.TrimSpace(r.FormValue("email"))
	phone := strings.TrimSpace(r.FormValue("phone"))
	timezone := r.FormValue("timezone")
	if name == "" || email == "" {
		http.Error(w, "name and email are required", http.StatusUnprocessableEntity)
		return
	}
	if err := db.UpdateAgentProfile(r.Context(), h.pool, agent.ID, name, email, phone, timezone); err != nil {
		log.Printf("update agent profile: %v", err)
		http.Error(w, "failed to save profile", http.StatusInternalServerError)
		return
	}
	// Re-issue JWT so the topnav shows the new name immediately.
	token, err := mw.IssueToken(h.jwtSecret, agent.ID, email, name, agent.Role)
	if err == nil {
		http.SetCookie(w, &http.Cookie{
			Name:     "wt_session",
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Expires:  time.Now().Add(24 * time.Hour),
		})
	}
	w.WriteHeader(http.StatusOK)
}

func (h *AccountHandler) UpdateNotifications(w http.ResponseWriter, r *http.Request) {
	agent := mw.AgentFromCtx(r.Context())
	if agent == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	keys := []string{"notif_new_message", "notif_campaign_complete", "notif_team_activity", "notif_weekly_report"}
	prefs := map[string]any{}
	for _, k := range keys {
		prefs[k] = r.FormValue(k) == "on"
	}
	if err := db.UpdateAgentPreferences(r.Context(), h.pool, agent.ID, prefs); err != nil {
		log.Printf("update agent preferences: %v", err)
		http.Error(w, "failed to save preferences", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *AccountHandler) SignOutAll(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:    "wt_session",
		Value:   "",
		Path:    "/",
		Expires: time.Unix(0, 0),
		MaxAge:  -1,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
