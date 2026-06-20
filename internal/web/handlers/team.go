package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"whatsapptool/internal/db"
	mw "whatsapptool/internal/web/middleware"
	"whatsapptool/internal/web/templates"
)

// TeamHandler manages /team routes (admin only).
type TeamHandler struct {
	pool      *pgxpool.Pool
	jwtSecret []byte
	baseURL   string
}

func NewTeamHandler(pool *pgxpool.Pool, jwtSecret []byte, baseURL string) *TeamHandler {
	return &TeamHandler{pool: pool, jwtSecret: jwtSecret, baseURL: baseURL}
}

func (h *TeamHandler) Mount(r chi.Router) {
	r.Get("/", h.List)
	r.Post("/invite", h.Invite)
	r.Post("/{id}/role", h.UpdateRole)
	r.Post("/{id}/activate", h.SetActive)
	r.Post("/{id}/limits", h.SetLimits)
	r.Post("/{id}/reinvite", h.Reinvite)
	r.Delete("/{id}", h.Delete)
}

func (h *TeamHandler) List(w http.ResponseWriter, r *http.Request) {
	agents, err := db.ListAgents(r.Context(), h.pool)
	if err != nil {
		http.Error(w, "load agents: "+err.Error(), http.StatusInternalServerError)
		return
	}
	limits, _ := db.ListAgentLimits(r.Context(), h.pool)
	auditLog, _ := db.ListAuditLog(r.Context(), h.pool, "", 50, 0)
	actor := mw.AgentFromCtx(r.Context())
	flash := r.URL.Query().Get("invite_url")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.TeamPage(agents, limits, auditLog, actor, flash).Render(r.Context(), w)
}

func (h *TeamHandler) Invite(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, templates.FormBanner("Invalid form submission."))
		return
	}
	name := r.FormValue("name")
	email := r.FormValue("email")
	role := r.FormValue("role")
	if name == "" || email == "" || role == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnprocessableEntity)
		fmt.Fprint(w, templates.FormBanner("Name, email and role are all required."))
		return
	}
	token, err := GenerateInviteToken()
	if err != nil {
		log.Printf("generate invite token: %v", err)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, templates.FormBanner("Failed to generate invite link — please try again."))
		return
	}
	expires := time.Now().Add(InviteExpiry)
	invited, err := db.CreateInvitedAgent(r.Context(), h.pool, name, email, role, token, expires)
	if err != nil {
		log.Printf("create invited agent: %v", err)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, templates.FormBanner("Could not create the invite — the email may already be registered."))
		return
	}
	actor := mw.AgentFromCtx(r.Context())
	db.Log(r.Context(), h.pool, actor.ID, "agent_invited", "agent", invited.ID,
		map[string]any{"name": name, "email": email, "role": role})

	inviteURL := h.baseURL + "/invite/" + token
	w.Header().Set("HX-Redirect", "/team?invite_url="+inviteURL)
	w.WriteHeader(http.StatusOK)
}

func (h *TeamHandler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	role := r.FormValue("role")
	if err := db.UpdateAgentRole(r.Context(), h.pool, id, role); err != nil {
		log.Printf("update role %s: %v", id, err)
	}
	actor := mw.AgentFromCtx(r.Context())
	db.Log(r.Context(), h.pool, actor.ID, "agent_role_changed", "agent", id,
		map[string]any{"new_role": role})
	http.Redirect(w, r, "/team", http.StatusSeeOther)
}

func (h *TeamHandler) SetActive(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	active := r.FormValue("active") == "true"
	if err := db.SetAgentActive(r.Context(), h.pool, id, active); err != nil {
		log.Printf("set active %s: %v", id, err)
	}
	actor := mw.AgentFromCtx(r.Context())
	action := "agent_deactivated"
	if active {
		action = "agent_activated"
	}
	db.Log(r.Context(), h.pool, actor.ID, action, "agent", id, nil)
	http.Redirect(w, r, "/team", http.StatusSeeOther)
}

func (h *TeamHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	actor := mw.AgentFromCtx(r.Context())
	if actor != nil && actor.ID == id {
		http.Error(w, "cannot delete yourself", http.StatusBadRequest)
		return
	}
	if err := db.DeleteAgent(r.Context(), h.pool, id); err != nil {
		log.Printf("delete agent %s: %v", id, err)
	}
	db.Log(r.Context(), h.pool, actor.ID, "agent_deleted", "agent", id, nil)
	// hx-swap="outerHTML" on the <tr> — OOB toast fires, empty body removes the row.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, templates.ToastFragment(templates.ToastSuccess, "Member removed.", "", ""))
}

func (h *TeamHandler) SetLimits(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	cap := 0
	if v := r.FormValue("monthly_msg_cap"); v != "" {
		if n, err2 := strconv.Atoi(v); err2 == nil && n >= 0 {
			cap = n
		}
	}
	if err := db.SetAgentMsgCap(r.Context(), h.pool, id, cap); err != nil {
		log.Printf("set agent limits %s: %v", id, err)
	}
	http.Redirect(w, r, "/team", http.StatusSeeOther)
}

func (h *TeamHandler) Reinvite(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	token, err := GenerateInviteToken()
	if err != nil {
		log.Printf("generate reinvite token: %v", err)
		http.Redirect(w, r, "/team", http.StatusSeeOther)
		return
	}
	expires := time.Now().Add(InviteExpiry)
	if err := db.UpdateInviteToken(r.Context(), h.pool, id, token, expires); err != nil {
		log.Printf("reinvite %s: %v", id, err)
		http.Redirect(w, r, "/team", http.StatusSeeOther)
		return
	}
	inviteURL := h.baseURL + "/invite/" + token
	http.Redirect(w, r, "/team?invite_url="+inviteURL, http.StatusSeeOther)
}
