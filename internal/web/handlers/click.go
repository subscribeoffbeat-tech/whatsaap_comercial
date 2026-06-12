package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgx "github.com/jackc/pgx/v5"

	"whatsapptool/internal/campaigns"
)

// ClickHandler handles /c/{short_code} — log the click and redirect.
type ClickHandler struct {
	pool *pgxpool.Pool
}

func NewClickHandler(pool *pgxpool.Pool) *ClickHandler {
	return &ClickHandler{pool: pool}
}

func (h *ClickHandler) Mount(r chi.Router) {
	r.Get("/{short_code}", h.Redirect)
}

func (h *ClickHandler) Redirect(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "short_code")
	if code == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	target, err := campaigns.RecordClick(r.Context(), h.pool, code)
	if err != nil {
		if err == pgx.ErrNoRows {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, target, http.StatusFound)
}
