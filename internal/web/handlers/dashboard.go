package handlers

import (
	"log"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"whatsapptool/internal/db"
	mw "whatsapptool/internal/web/middleware"
	"whatsapptool/internal/web/templates"
)

// DashboardHandler handles the root dashboard page.
type DashboardHandler struct {
	pool *pgxpool.Pool
}

func NewDashboardHandler(pool *pgxpool.Pool) *DashboardHandler {
	return &DashboardHandler{pool: pool}
}

func (h *DashboardHandler) Page(w http.ResponseWriter, r *http.Request) {
	stats, err := db.GetDashboardStats(r.Context(), h.pool)
	if err != nil {
		log.Printf("dashboard stats: %v", err)
	}
	recent, err := db.ListCampaigns(r.Context(), h.pool)
	if err != nil {
		log.Printf("dashboard campaigns: %v", err)
	}
	if len(recent) > 5 {
		recent = recent[:5]
	}
	agent := mw.AgentFromCtx(r.Context())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.DashboardPage(agent, stats, recent).Render(r.Context(), w)
}
