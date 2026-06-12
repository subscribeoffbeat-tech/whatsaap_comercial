package handlers

import (
	"encoding/csv"
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

// AnalyticsHandler handles /analytics routes.
type AnalyticsHandler struct {
	pool *pgxpool.Pool
}

func NewAnalyticsHandler(pool *pgxpool.Pool) *AnalyticsHandler {
	return &AnalyticsHandler{pool: pool}
}

func (h *AnalyticsHandler) Mount(r chi.Router) {
	r.Get("/", h.Page)
	r.Get("/export.csv", h.ExportCSV)
}

func (h *AnalyticsHandler) Page(w http.ResponseWriter, r *http.Request) {
	from, to := parseDateRange(r)
	ctx := r.Context()
	agent := mw.AgentFromCtx(ctx)

	var overview db.OverviewStats
	var byCat []db.CategoryCost
	var byCamp []db.CampaignCost
	var agentStats []db.AgentStat

	if agent.Role == "agent" {
		// Agents see only their own sent-message stats; no cost data.
		overview, _ = db.GetAgentOverviewStats(ctx, h.pool, from, to, agent.ID)
		allStats, err := db.AgentPerformance(ctx, h.pool, from, to)
		if err != nil {
			log.Printf("analytics agents: %v", err)
		}
		for _, s := range allStats {
			if s.AgentID == agent.ID {
				agentStats = []db.AgentStat{s}
				break
			}
		}
		// byCat and byCamp remain nil — cost sections won't render
	} else {
		var err error
		overview, err = db.GetOverviewStats(ctx, h.pool, from, to)
		if err != nil {
			log.Printf("analytics overview: %v", err)
		}
		byCat, err = db.CostByCategory(ctx, h.pool, from, to)
		if err != nil {
			log.Printf("analytics cost by cat: %v", err)
		}
		byCamp, err = db.CostByCampaign(ctx, h.pool, from, to)
		if err != nil {
			log.Printf("analytics cost by campaign: %v", err)
		}
		agentStats, err = db.AgentPerformance(ctx, h.pool, from, to)
		if err != nil {
			log.Printf("analytics agents: %v", err)
		}
	}

	days, err := db.MessagesPerDay(ctx, h.pool, from, to)
	if err != nil {
		log.Printf("analytics per-day: %v", err)
	}
	qi, err := db.GetQualityInfo(ctx, h.pool)
	if err != nil {
		log.Printf("analytics quality: %v", err)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.AnalyticsPage(agent, templates.AnalyticsData{
		From:       from,
		To:         to,
		Overview:   overview,
		Days:       days,
		ByCat:      byCat,
		ByCampaign: byCamp,
		Quality:    qi,
		AgentStats: agentStats,
	}).Render(ctx, w)
}

func (h *AnalyticsHandler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	from, to := parseDateRange(r)
	byCat, err := db.CostByCategory(r.Context(), h.pool, from, to)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	byCamp, err := db.CostByCampaign(r.Context(), h.pool, from, to)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}

	filename := fmt.Sprintf("analytics_%s_%s.csv",
		from.Format("2006-01-02"), to.Format("2006-01-02"))
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")

	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"section", "name", "count", "cost_inr"})
	for _, c := range byCat {
		_ = cw.Write([]string{"category", c.Category, strconv.FormatInt(c.Count, 10), fmt.Sprintf("%.4f", c.CostINR)})
	}
	for _, c := range byCamp {
		_ = cw.Write([]string{"campaign", c.CampaignName, strconv.FormatInt(c.Count, 10), fmt.Sprintf("%.4f", c.CostINR)})
	}
	cw.Flush()
}

// parseDateRange reads ?from=YYYY-MM-DD&to=YYYY-MM-DD from the query string.
// Defaults to the last 30 days.
func parseDateRange(r *http.Request) (from, to time.Time) {
	to = time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour) // end of today
	from = to.Add(-30 * 24 * time.Hour)

	if s := r.URL.Query().Get("from"); s != "" {
		if t, err := time.Parse("2006-01-02", s); err == nil {
			from = t.UTC()
		}
	}
	if s := r.URL.Query().Get("to"); s != "" {
		if t, err := time.Parse("2006-01-02", s); err == nil {
			to = t.UTC().Add(24 * time.Hour) // inclusive end
		}
	}
	return
}
