package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ── JWT (HS256, cookie-based) ─────────────────────────────────────────────────

const sessionCookie = "wt_session"
const tokenTTL = 24 * time.Hour

// secureCookies controls the Secure flag on the session cookie. Enabled in
// production (HTTPS) via SetSecureCookies; off for local HTTP dev.
var secureCookies = false

// SetSecureCookies enables/disables the Secure flag on session cookies.
func SetSecureCookies(on bool) { secureCookies = on }

var jwtHeaderB64 = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

// AgentClaims is stored in the JWT payload and injected into request context.
type AgentClaims struct {
	ID    string `json:"sub"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	Exp   int64  `json:"exp"`
}

type ctxKey string

const ctxAgentKey ctxKey = "agent"

// IssueToken creates a signed JWT for the given claims.
func IssueToken(secret []byte, id, email, name, role string) (string, error) {
	claims := AgentClaims{
		ID:    id,
		Email: email,
		Name:  name,
		Role:  role,
		Exp:   time.Now().Add(tokenTTL).Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	b64payload := base64.RawURLEncoding.EncodeToString(payload)
	msg := jwtHeaderB64 + "." + b64payload
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(msg))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return msg + "." + sig, nil
}

// parseToken validates a JWT and returns its claims.
func parseToken(secret []byte, token string) (*AgentClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid token")
	}
	msg := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(msg))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return nil, errors.New("invalid signature")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var claims AgentClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return nil, err
	}
	if time.Now().Unix() > claims.Exp {
		return nil, errors.New("token expired")
	}
	return &claims, nil
}

// SetSessionCookie writes the JWT into an httponly cookie.
func SetSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(tokenTTL.Seconds()),
	})
}

// ClearSessionCookie removes the session cookie.
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

// AgentFromCtx returns the agent claims stored in the request context, or nil.
func AgentFromCtx(ctx context.Context) *AgentClaims {
	v, _ := ctx.Value(ctxAgentKey).(*AgentClaims)
	return v
}

// ── Middleware ────────────────────────────────────────────────────────────────

// RequireAuth validates the session cookie. On failure it redirects to /login.
func RequireAuth(secret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(sessionCookie)
			if err != nil {
				http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
				return
			}
			claims, err := parseToken(secret, cookie.Value)
			if err != nil {
				ClearSessionCookie(w)
				http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
				return
			}
			ctx := context.WithValue(r.Context(), ctxAgentKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// SecurityHeaders sets baseline security response headers on every request.
// (HSTS is left to the TLS-terminating reverse proxy.)
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// CSRFGuard rejects state-changing requests (POST/PUT/PATCH/DELETE) whose Origin
// (or Referer) host doesn't match the request host — a lightweight CSRF defense
// layered on top of the SameSite=Lax session cookie. exemptPrefixes lists paths
// that must skip the check (e.g. the Meta webhook, which is cross-origin).
func CSRFGuard(exemptPrefixes ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			default:
				next.ServeHTTP(w, r)
				return
			}
			for _, p := range exemptPrefixes {
				if strings.HasPrefix(r.URL.Path, p) {
					next.ServeHTTP(w, r)
					return
				}
			}
			// Determine the source host from Origin, falling back to Referer.
			src := r.Header.Get("Origin")
			if src == "" {
				src = r.Header.Get("Referer")
			}
			if src != "" {
				if u, err := url.Parse(src); err != nil || u.Host != r.Host {
					http.Error(w, "cross-site request blocked", http.StatusForbidden)
					return
				}
			}
			// No Origin/Referer (e.g. some native clients): fall through to the
			// SameSite=Lax cookie protection.
			next.ServeHTTP(w, r)
		})
	}
}

// RequireRole allows only agents whose role is in the given list.
// forbidden is called (instead of a plain 403) when access is denied —
// pass a handler that renders a branded error page.
func RequireRole(forbidden http.HandlerFunc, roles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(roles))
	for _, r := range roles {
		allowed[r] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			agent := AgentFromCtx(r.Context())
			if agent == nil || !allowed[agent.Role] {
				forbidden(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
