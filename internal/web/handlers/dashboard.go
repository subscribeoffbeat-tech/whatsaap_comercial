package handlers

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"whatsapptool/internal/db"
	mw "whatsapptool/internal/web/middleware"
	"whatsapptool/internal/web/templates"
	"whatsapptool/internal/whatsapp"
)

// DashboardHandler handles the root dashboard page.
type DashboardHandler struct {
	pool     *pgxpool.Pool
	waClient *whatsapp.Client
}

func NewDashboardHandler(pool *pgxpool.Pool, waClient *whatsapp.Client) *DashboardHandler {
	return &DashboardHandler{pool: pool, waClient: waClient}
}

func (h *DashboardHandler) Page(w http.ResponseWriter, r *http.Request) {
	// Refresh phone quality + tier from Meta so the widget shows live data
	// (best-effort, short timeout — never blocks the page).
	h.syncPhoneHealth(r.Context())

	stats, err := db.GetDashboardStats(r.Context(), h.pool)
	if err != nil {
		log.Printf("dashboard stats: %v", err)
	}
	recent, err := db.ListRecentCampaigns(r.Context(), h.pool, 5)
	if err != nil {
		log.Printf("dashboard campaigns: %v", err)
	}
	agent := mw.AgentFromCtx(r.Context())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.DashboardPage(agent, stats, recent).Render(r.Context(), w)
}

// syncPhoneHealth pulls the phone number's quality rating and messaging tier
// from Meta and stores them in app_config, so the dashboard quality widget and
// capacity bar reflect reality. Best-effort with a short timeout.
func (h *DashboardHandler) syncPhoneHealth(ctx context.Context) {
	if h.waClient == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	info, err := h.waClient.GetPhoneNumberInfo(ctx)
	if err != nil {
		log.Printf("dashboard phone-health sync: %v", err)
		return
	}
	if q := strings.ToLower(strings.TrimSpace(info.QualityRating)); q != "" {
		_ = db.SetConfig(ctx, h.pool, "quality_rating", q)
	}
	if tier := tierToNumber(info.MessagingTier); tier > 0 {
		_ = db.SetConfig(ctx, h.pool, "meta_tier", tier)
		// Daily cap = 90% of the tier (hard rule: stop queuing at 90%).
		_ = db.SetConfig(ctx, h.pool, "daily_message_cap", tier*9/10)
	}
}

// tierToNumber maps a Meta messaging_limit_tier string to a per-day number.
func tierToNumber(tier string) int {
	switch strings.ToUpper(strings.TrimSpace(tier)) {
	case "TIER_50":
		return 50
	case "TIER_250":
		return 250
	case "TIER_1K":
		return 1000
	case "TIER_10K":
		return 10000
	case "TIER_100K":
		return 100000
	case "TIER_UNLIMITED":
		return 1000000
	}
	return 0
}
