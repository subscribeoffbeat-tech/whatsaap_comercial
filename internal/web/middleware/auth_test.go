package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testSecret = []byte("test-secret-key-32-bytes-padding!!")

func TestIssueAndParseToken(t *testing.T) {
	token, err := IssueToken(testSecret, "uuid-1", "a@b.com", "Alice", "admin")
	require.NoError(t, err)
	require.NotEmpty(t, token)

	claims, err := parseToken(testSecret, token)
	require.NoError(t, err)
	assert.Equal(t, "uuid-1", claims.ID)
	assert.Equal(t, "admin", claims.Role)
	assert.Equal(t, "a@b.com", claims.Email)
}

func TestParseToken_WrongSecret(t *testing.T) {
	token, _ := IssueToken(testSecret, "uuid-1", "a@b.com", "Alice", "admin")
	_, err := parseToken([]byte("wrong-secret"), token)
	assert.Error(t, err)
}

func TestParseToken_Malformed(t *testing.T) {
	_, err := parseToken(testSecret, "not.a.jwt")
	assert.Error(t, err)

	_, err = parseToken(testSecret, "")
	assert.Error(t, err)
}

func TestRequireAuth_NoCookie(t *testing.T) {
	handler := RequireAuth(testSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/protected", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusSeeOther, rr.Code)
	assert.Contains(t, rr.Header().Get("Location"), "/login")
}

func TestRequireAuth_ValidCookie(t *testing.T) {
	called := false
	handler := RequireAuth(testSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		agent := AgentFromCtx(r.Context())
		assert.NotNil(t, agent)
		assert.Equal(t, "admin", agent.Role)
		w.WriteHeader(http.StatusOK)
	}))
	token, _ := IssueToken(testSecret, "uuid-1", "a@b.com", "Alice", "admin")
	req := httptest.NewRequest("GET", "/protected", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestRequireRole_Allowed(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := RequireRole("admin", "manager")(inner)

	req := httptest.NewRequest("GET", "/admin", nil)
	ctx := context.WithValue(req.Context(), ctxAgentKey, &AgentClaims{Role: "manager"})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req.WithContext(ctx))
	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestRequireRole_Blocked(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := RequireRole("admin")(inner)

	req := httptest.NewRequest("GET", "/admin", nil)
	ctx := context.WithValue(req.Context(), ctxAgentKey, &AgentClaims{Role: "agent"})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req.WithContext(ctx))
	assert.Equal(t, http.StatusForbidden, rr.Code)
}

func TestRequireRole_NoContext(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := RequireRole("admin")(inner)

	req := httptest.NewRequest("GET", "/admin", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusForbidden, rr.Code)
}
