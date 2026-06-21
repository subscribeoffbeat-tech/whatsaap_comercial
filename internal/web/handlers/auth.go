package handlers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"whatsapptool/internal/db"
	"whatsapptool/internal/email"
	mw "whatsapptool/internal/web/middleware"
	"whatsapptool/internal/web/templates"
)

// AuthHandler handles login, logout, invite acceptance, and password reset.
type AuthHandler struct {
	pool      *pgxpool.Pool
	jwtSecret []byte
	baseURL   string
	mailer    *email.Sender // nil when SMTP is not configured
}

func NewAuthHandler(pool *pgxpool.Pool, jwtSecret []byte, baseURL string, mailer *email.Sender) *AuthHandler {
	return &AuthHandler{pool: pool, jwtSecret: jwtSecret, baseURL: baseURL, mailer: mailer}
}

func (h *AuthHandler) Mount(r chi.Router) {
	r.Get("/login", h.LoginPage)
	r.Post("/login", h.Login)
	r.Post("/logout", h.Logout)
	r.Get("/invite/{token}", h.InvitePage)
	r.Post("/invite/{token}", h.AcceptInvite)
	r.Get("/forgot-password", h.ForgotPasswordPage)
	r.Post("/forgot-password", h.ForgotPassword)
	r.Get("/reset-password", h.ResetPasswordPage)
	r.Post("/reset-password", h.ResetPassword)
}

func (h *AuthHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("wt_session"); err == nil && cookie.Value != "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	flash := r.URL.Query().Get("err")
	msg := r.URL.Query().Get("msg")
	next := r.URL.Query().Get("next")
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		next = ""
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.LoginPage(flash, msg, next).Render(r.Context(), w)
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

func (h *AuthHandler) ForgotPasswordPage(w http.ResponseWriter, r *http.Request) {
	msg := r.URL.Query().Get("msg")
	msgType := r.URL.Query().Get("type")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.ForgotPasswordPage(msg, msgType).Render(r.Context(), w)
}

func (h *AuthHandler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/forgot-password?msg=Invalid+request&type=error", http.StatusSeeOther)
		return
	}
	emailAddr := strings.TrimSpace(strings.ToLower(r.FormValue("email")))
	if emailAddr == "" {
		http.Redirect(w, r, "/forgot-password?msg=Please+enter+your+email+address&type=error", http.StatusSeeOther)
		return
	}

	// Always show the same success message regardless of whether the email exists (prevents user enumeration).
	successURL := "/forgot-password?msg=If+that+email+is+registered%2C+a+reset+link+has+been+sent.+Check+your+inbox.&type=success"

	agent, err := db.GetAgentByEmail(r.Context(), h.pool, emailAddr)
	if err != nil || !agent.Active || agent.PasswordHash == "" {
		http.Redirect(w, r, successURL, http.StatusSeeOther)
		return
	}

	token, err := GenerateInviteToken()
	if err != nil {
		log.Printf("generate reset token: %v", err)
		http.Redirect(w, r, successURL, http.StatusSeeOther)
		return
	}

	expiresAt := time.Now().Add(30 * time.Minute)
	if err := db.SetPasswordResetToken(r.Context(), h.pool, agent.ID, token, expiresAt); err != nil {
		log.Printf("set reset token for %s: %v", agent.ID, err)
		http.Redirect(w, r, successURL, http.StatusSeeOther)
		return
	}

	if h.mailer != nil {
		resetLink := h.baseURL + "/reset-password?token=" + url.QueryEscape(token)
		subject := "Reset your Offbeat ChatFlow password"
		body := "Hi " + agent.Name + ",\n\n" +
			"Someone requested a password reset for your Offbeat ChatFlow account.\n\n" +
			"Click the link below to set a new password:\n\n" +
			resetLink + "\n\n" +
			"This link expires in 30 minutes.\n\n" +
			"If you did not request this, you can safely ignore this email — your password will not change.\n\n" +
			"— Offbeat ChatFlow"
		go func() {
			if err := h.mailer.Send(agent.Email, subject, body); err != nil {
				log.Printf("reset email send failed for agent %s: %v", agent.ID, err)
			}
		}()
	} else {
		log.Printf("WARN: SMTP not configured; skipping reset email for agent %s", agent.ID)
	}

	http.Redirect(w, r, successURL, http.StatusSeeOther)
}

func (h *AuthHandler) ResetPasswordPage(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		templates.ErrorPage(400, "Invalid reset link", "The reset link is missing. Please request a new one.").Render(r.Context(), w)
		return
	}
	if _, err := db.GetAgentByResetToken(r.Context(), h.pool, token); err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		templates.ErrorPage(400, "Link expired", "This password reset link has expired or already been used. Please request a new one.").Render(r.Context(), w)
		return
	}
	flash := r.URL.Query().Get("err")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.ResetPasswordPage(token, flash).Render(r.Context(), w)
}

func (h *AuthHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/login?err=Invalid+request", http.StatusSeeOther)
		return
	}
	token := r.FormValue("token")
	password := r.FormValue("password")
	confirm := r.FormValue("confirm")

	agent, err := db.GetAgentByResetToken(r.Context(), h.pool, token)
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		templates.ErrorPage(400, "Link expired", "This password reset link has expired or already been used. Please request a new one from the login page.").Render(r.Context(), w)
		return
	}

	resetURL := "/reset-password?token=" + url.QueryEscape(token)
	if len(password) < 8 {
		http.Redirect(w, r, resetURL+"&err=Password+must+be+at+least+8+characters", http.StatusSeeOther)
		return
	}
	if password != confirm {
		http.Redirect(w, r, resetURL+"&err=Passwords+do+not+match", http.StatusSeeOther)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		http.Redirect(w, r, resetURL+"&err=Server+error%2C+try+again", http.StatusSeeOther)
		return
	}
	if err := db.UsePasswordResetToken(r.Context(), h.pool, agent.ID, string(hash)); err != nil {
		log.Printf("use reset token for %s: %v", agent.ID, err)
		http.Redirect(w, r, resetURL+"&err=Server+error%2C+try+again", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/login?msg=Password+reset+successfully.+Please+sign+in+with+your+new+password.", http.StatusSeeOther)
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
