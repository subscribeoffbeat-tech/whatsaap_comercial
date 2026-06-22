package handlers

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"whatsapptool/internal/db"
	"whatsapptool/internal/email"
	mw "whatsapptool/internal/web/middleware"
	"whatsapptool/internal/web/templates"
)

// TeamHandler manages /team routes (admin only).
type TeamHandler struct {
	pool      *pgxpool.Pool
	jwtSecret []byte
	baseURL   string
	mailer    *email.Sender
}

func NewTeamHandler(pool *pgxpool.Pool, jwtSecret []byte, baseURL string, mailer *email.Sender) *TeamHandler {
	return &TeamHandler{pool: pool, jwtSecret: jwtSecret, baseURL: baseURL, mailer: mailer}
}

func (h *TeamHandler) Mount(r chi.Router) {
	r.Get("/", h.List)
	r.Get("/invite", h.InviteForm)
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
	flash := r.URL.Query().Get("flash")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.TeamPage(agents, limits, auditLog, actor, flash).Render(r.Context(), w)
}

func (h *TeamHandler) InviteForm(w http.ResponseWriter, r *http.Request) {
	actor := mw.AgentFromCtx(r.Context())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.InviteFormPage(actor, "", "", "", "agent", "").Render(r.Context(), w)
}

func (h *TeamHandler) Invite(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	emailAddr := strings.TrimSpace(strings.ToLower(r.FormValue("email")))
	role := r.FormValue("role")
	personalMsg := strings.TrimSpace(r.FormValue("personal_message"))
	actor := mw.AgentFromCtx(r.Context())

	if name == "" || emailAddr == "" || role == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnprocessableEntity)
		templates.InviteFormPage(actor, "Name, email and role are all required.", name, emailAddr, role, personalMsg).Render(r.Context(), w)
		return
	}
	if role != "admin" && role != "manager" && role != "agent" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnprocessableEntity)
		templates.InviteFormPage(actor, "Invalid role selected.", name, emailAddr, role, personalMsg).Render(r.Context(), w)
		return
	}

	token, err := GenerateInviteToken()
	if err != nil {
		log.Printf("generate invite token: %v", err)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		templates.InviteFormPage(actor, "Failed to generate invite link — please try again.", name, emailAddr, role, personalMsg).Render(r.Context(), w)
		return
	}

	expires := time.Now().Add(7 * 24 * time.Hour)
	invited, err := db.CreateInvitedAgent(r.Context(), h.pool, name, emailAddr, role, token, expires)
	if err != nil {
		log.Printf("create invited agent: %v", err)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnprocessableEntity)
		templates.InviteFormPage(actor, "Could not create the invite — the email may already be registered.", name, emailAddr, role, personalMsg).Render(r.Context(), w)
		return
	}

	inviterName := "A colleague"
	if actor != nil {
		inviterName = actor.Name
	}
	inviteURL := h.baseURL + "/invite/" + token

	if h.mailer != nil {
		go h.sendInviteEmail(emailAddr, name, inviterName, role, inviteURL, personalMsg)
	} else {
		log.Printf("WARN: SMTP not configured; invite link for %s: %s", emailAddr, inviteURL)
	}

	if actor != nil {
		db.Log(r.Context(), h.pool, actor.ID, "agent_invited", "agent", invited.ID,
			map[string]any{"name": name, "email": emailAddr, "role": role})
	}

	flash := "Invite sent to " + emailAddr + "."
	if h.mailer == nil {
		flash = "Invite created (SMTP not configured — check server logs for the link)."
	}
	http.Redirect(w, r, "/team?flash="+url.QueryEscape(flash), http.StatusSeeOther)
}

func (h *TeamHandler) sendInviteEmail(to, name, inviterName, role, inviteURL, personalMsg string) {
	roleDesc := map[string]string{
		"admin":   "full access (billing, settings, all features)",
		"manager": "campaigns, contacts, templates and team view",
		"agent":   "inbox and contact replies",
	}[role]

	greeting := ""
	if personalMsg != "" {
		greeting = personalMsg + "\n\n"
	}

	subject := inviterName + " has invited you to Offbeat ChatFlow"
	body := "Hi " + name + ",\n\n" +
		greeting +
		inviterName + " has invited you to join Offbeat ChatFlow as " + role + " (" + roleDesc + ").\n\n" +
		"Click the link below to accept your invitation and set your password:\n\n" +
		inviteURL + "\n\n" +
		"This link expires in 7 days. If you did not expect this, you can safely ignore it.\n\n" +
		"-- Offbeat ChatFlow"

	if err := h.mailer.Send(to, subject, body); err != nil {
		log.Printf("invite email send failed for %s: %v", to, err)
	} else {
		log.Printf("invite email sent to %s", to)
	}
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
	expires := time.Now().Add(7 * 24 * time.Hour)
	if err := db.UpdateInviteToken(r.Context(), h.pool, id, token, expires); err != nil {
		log.Printf("reinvite %s: %v", id, err)
		http.Redirect(w, r, "/team", http.StatusSeeOther)
		return
	}

	agentRec, agentErr := db.GetAgentByID(r.Context(), h.pool, id)
	if agentErr == nil && h.mailer != nil {
		actor := mw.AgentFromCtx(r.Context())
		inviterName := "A colleague"
		if actor != nil {
			inviterName = actor.Name
		}
		inviteURL := h.baseURL + "/invite/" + token
		go h.sendInviteEmail(agentRec.Email, agentRec.Name, inviterName, agentRec.Role, inviteURL, "")
	}

	destEmail := "the team member"
	if agentErr == nil {
		destEmail = agentRec.Email
	}
	http.Redirect(w, r, "/team?flash="+url.QueryEscape("Invite link refreshed and resent to "+destEmail+"."), http.StatusSeeOther)
}
