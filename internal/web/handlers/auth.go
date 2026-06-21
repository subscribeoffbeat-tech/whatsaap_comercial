package handlers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"whatsapptool/internal/db"
	mw "whatsapptool/internal/web/middleware"
	"whatsapptool/internal/web/templates"
)

// AuthHandler handles login, logout, and invite acceptance.
type AuthHandler struct {
	pool      *pgxpool.Pool
	jwtSecret []byte
	baseURL   string
}

func NewAuthHandler(pool *pgxpool.Pool, jwtSecret []byte, baseURL string) *AuthHandler {
	return &AuthHandler{pool: pool, jwtSecret: jwtSecret, baseURL: baseURL}
}

func (h *AuthHandler) Mount(r chi.Router) {
	r.Get("/login", h.LoginPage)
	r.Post("/login", h.Login)
	r.Post("/logout", h.Logout)
	r.Get("/invite/{token}", h.InvitePage)
	r.Post("/invite/{token}", h.AcceptInvite)
}

func (h *AuthHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	// If already logged in, redirect home.
	if cookie, err := r.Cookie("wt_session"); err == nil && cookie.Value != "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	flash := r.URL.Query().Get("err")
	next := r.URL.Query().Get("next")
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		next = ""
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.LoginPage(flash, next).Render(r.Context(), w)
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/login?err=invalid+request", http.StatusSeeOther)
		return
	}
	email := strings.TrimSpace(strings.ToLower(r.FormValue("email")))
	password := r.FormValue("password")

	agent, err := db.GetAgentByEmail(r.Context(), h.pool, email)
	if err != nil || !agent.Active || agent.PasswordHash == "" {
		http.Redirect(w, r, "/login?err=Invalid+email+or+password", http.StatusSeeOther)
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(agent.PasswordHash), []byte(password)); err != nil {
		http.Redirect(w, r, "/login?err=Invalid+email+or+password", http.StatusSeeOther)
		return
	}

	token, err := mw.IssueToken(h.jwtSecret, agent.ID, agent.Email, agent.Name, agent.Role)
	if err != nil {
		log.Printf("issue token: %v", err)
		http.Redirect(w, r, "/login?err=Server+error", http.StatusSeeOther)
		return
	}
	mw.SetSessionCookie(w, token)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("TouchLastActive panic: %v", r)
			}
		}()
		db.TouchLastActive(context.Background(), h.pool, agent.ID)
	}()

	next := r.FormValue("next")
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		next = "/"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	mw.ClearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *AuthHandler) InvitePage(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	agent, err := db.GetAgentByInviteToken(r.Context(), h.pool, token)
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		templates.ErrorPage(404, "Invalid invite link", "This invite has expired or is no longer valid.").Render(r.Context(), w)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.InvitePage(agent.Name, token, "").Render(r.Context(), w)
}

func (h *AuthHandler) AcceptInvite(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	agent, err := db.GetAgentByInviteToken(r.Context(), h.pool, token)
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		templates.ErrorPage(404, "Invalid invite link", "This invite has expired or is no longer valid.").Render(r.Context(), w)
		return
	}
	if err := r.ParseForm(); err != nil {
		templates.InvitePage(agent.Name, token, "Invalid request").Render(r.Context(), w)
		return
	}
	password := r.FormValue("password")
	confirm := r.FormValue("confirm")
	if len(password) < 8 {
		templates.InvitePage(agent.Name, token, "Password must be at least 8 characters").Render(r.Context(), w)
		return
	}
	if password != confirm {
		templates.InvitePage(agent.Name, token, "Passwords do not match").Render(r.Context(), w)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		templates.InvitePage(agent.Name, token, "Server error, try again").Render(r.Context(), w)
		return
	}
	if err := db.ActivateInvite(r.Context(), h.pool, agent.ID, string(hash)); err != nil {
		log.Printf("activate invite %s: %v", agent.ID, err)
		templates.InvitePage(agent.Name, token, "Failed to activate account").Render(r.Context(), w)
		return
	}
	jwtToken, _ := mw.IssueToken(h.jwtSecret, agent.ID, agent.Email, agent.Name, agent.Role)
	mw.SetSessionCookie(w, jwtToken)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// GenerateInviteToken creates a 32-char URL-safe random token.
func GenerateInviteToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashPassword is a convenience wrapper used by onboarding.
func HashPassword(plain string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	return string(h), err
}

// InviteExpiry is how long an invite link remains valid.
const InviteExpiry = 72 * time.Hour
