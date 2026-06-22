package templates

import (
	"context"
	"fmt"
	"html"
	"io"
	"time"

	"github.com/a-h/templ"

	"whatsapptool/internal/db"
	mw "whatsapptool/internal/web/middleware"
)

// AnalyticsData bundles all data needed for the analytics page.
type AnalyticsData struct {
	From       time.Time
	To         time.Time
	Overview   db.OverviewStats
	Days       []db.DayStat
	ByCat      []db.CategoryCost
	ByCampaign []db.CampaignCost
	Quality    db.QualityInfo
	AgentStats []db.AgentStat
}

func AnalyticsPage(agent *mw.AgentClaims, data AnalyticsData) templ.Component {
	isAgent := agent != nil && agent.Role == "agent"

	// Overview percentages
	var delivPct, readPct, failPct float64
	if data.Overview.Sent > 0 {
		delivPct = float64(data.Overview.Delivered) * 100.0 / float64(data.Overview.Sent)
		readPct = float64(data.Overview.Read) * 100.0 / float64(data.Overview.Sent)
		failPct = float64(data.Overview.Failed) * 100.0 / float64(data.Overview.Sent)
	}

	// Bar chart: max daily sent for proportional heights
	var maxDay int64 = 1
	for _, d := range data.Days {
		if d.Sent > maxDay {
			maxDay = d.Sent
		}
	}

	// Build day lookup and find date range
	type dayCounts struct{ sent, delivered, failed int64 }
	dayMap := map[string]dayCounts{}
	var chartFrom, chartTo time.Time
	for i, d := range data.Days {
		dayMap[d.Day.Format("2006-01-02")] = dayCounts{d.Sent, d.Delivered, d.Failed}
		if i == 0 || d.Day.Before(chartFrom) {
			chartFrom = d.Day
		}
		if i == 0 || d.Day.After(chartTo) {
			chartTo = d.Day
		}
	}

	// Cost split (admin/manager only)
	var totalCost, utilityCost, marketingCost, authCost float64
	if !isAgent {
		for _, c := range data.ByCat {
			totalCost += c.CostINR
			switch c.Category {
			case "utility":
				utilityCost = c.CostINR
			case "marketing":
				marketingCost = c.CostINR
			case "authentication":
				authCost = c.CostINR
			}
		}
	}
	splitBase := utilityCost + marketingCost + authCost
	var utilPct, mktPct, authPct float64
	if splitBase > 0 {
		utilPct = utilityCost / splitBase * 100
		mktPct = marketingCost / splitBase * 100
		authPct = authCost / splitBase * 100
	}

	// Daily usage bar
	var usagePct int64
	if data.Quality.DailyCap > 0 {
		usagePct = data.Quality.SentToday * 100 / data.Quality.DailyCap
		if usagePct > 100 {
			usagePct = 100
		}
	}

	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/analytics", "Analytics", "")); err != nil {
			return err
		}

		// ── Page header ───────────────────────────────────────────────────────
		_, err := fmt.Fprintf(w, `
<div class="page-wrap">
<div class="page-hd">
  <div>
    <h1 class="screen-title">Analytics</h1>
    <p class="screen-subtitle">Delivery performance and cost breakdown.</p>
  </div>
  <form method="get" action="/analytics" class="an-date-form">
    <input type="date" name="from" value="%s">
    <span class="an-date-arrow">→</span>
    <input type="date" name="to" value="%s">
    <button class="btn btn-primary btn-sm" type="submit">Apply</button>
    <a class="btn btn-secondary btn-sm an-export-btn" href="/analytics/export.csv">
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
      Export CSV
    </a>
  </form>
</div>
<div class="an-body">`,
			data.From.Format("2006-01-02"), data.To.Format("2006-01-02"))
		if err != nil {
			return err
		}

		// ── Overview stat cards ───────────────────────────────────────────────
		delivStr, readStr, failStr := "—", "—", "—"
		if data.Overview.Sent > 0 {
			delivStr = fmt.Sprintf("%.1f%%", delivPct)
			readStr = fmt.Sprintf("%.1f%%", readPct)
			failStr = fmt.Sprintf("%.1f%%", failPct)
		}
		failStyle := ""
		if failPct > 0 {
			failStyle = ` style="color:var(--danger)"`
		}
		if _, err := fmt.Fprintf(w, `
<div class="stat-grid an-stat-grid">
  <div class="stat-card">
    <div class="stat-label">Sent</div>
    <div class="stat-value">%d</div>
    <div class="stat-sub">messages dispatched</div>
  </div>
  <div class="stat-card">
    <div class="stat-label">Delivered</div>
    <div class="stat-value">%s</div>
    <div class="stat-sub">of messages sent</div>
  </div>
  <div class="stat-card">
    <div class="stat-label">Read</div>
    <div class="stat-value">%s</div>
    <div class="stat-sub">of messages sent</div>
  </div>
  <div class="stat-card">
    <div class="stat-label">Failed</div>
    <div class="stat-value"%s>%s</div>
    <div class="stat-sub">delivery failures</div>
  </div>`,
			data.Overview.Sent, delivStr, readStr, failStyle, failStr,
		); err != nil {
			return err
		}
		if !isAgent {
			if _, err := fmt.Fprintf(w,
				`  <div class="stat-card">
    <div class="stat-label">Cost</div>
    <div class="stat-value">₹%.2f</div>
    <div class="stat-sub">incl. 18%% GST</div>
  </div>`,
				data.Overview.CostINR,
			); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(w, `</div>`); err != nil {
			return err
		}

		// ── Main two-column grid: chart + sidebar ─────────────────────────────
		if _, err := io.WriteString(w, `<div class="an-main-grid">`); err != nil {
			return err
		}

		// Chart section
		if _, err := io.WriteString(w, `<section class="an-section an-chart-section">
<div class="an-chart-hd">
  <h2 class="an-section-title">Messages per day</h2>
  <div class="an-chart-legend">
    <span class="an-leg-item"><span class="an-leg-dot an-leg-dot--sent"></span>Sent</span>
    <span class="an-leg-item"><span class="an-leg-dot an-leg-dot--delivered"></span>Delivered</span>
    <span class="an-leg-item"><span class="an-leg-dot an-leg-dot--failed"></span>Failed</span>
  </div>
</div>
<div class="an-chart">`); err != nil {
			return err
		}

		const maxBarPx = int64(160)

		if len(data.Days) == 0 {
			if _, err := io.WriteString(w, `<div class="an-chart-empty">No messages in this period.</div>`); err != nil {
				return err
			}
		} else {
			for cur := chartFrom; !cur.After(chartTo); cur = cur.AddDate(0, 0, 1) {
				key := cur.Format("2006-01-02")
				dc := dayMap[key]
				label := cur.Format("2/1")
				if dc.sent > 0 {
					sentPx := dc.sent * maxBarPx / maxDay
					if sentPx < 4 {
						sentPx = 4
					}
					delivPx := dc.delivered * maxBarPx / maxDay
					if delivPx < 2 && dc.delivered > 0 {
						delivPx = 2
					}
					failPx := dc.failed * maxBarPx / maxDay
					if failPx < 2 && dc.failed > 0 {
						failPx = 2
					}
					failBar := ""
					if dc.failed > 0 {
						failBar = fmt.Sprintf(`<div class="an-bar-wrap"><span class="an-cnt an-cnt--failed">%d</span><div class="an-bar an-bar--failed" style="height:%dpx"></div></div>`, dc.failed, failPx)
					}
					if _, err := fmt.Fprintf(w,
						`<div class="an-bar-col"><div class="an-bar-group"><div class="an-bar-wrap"><span class="an-cnt an-cnt--sent">%d</span><div class="an-bar an-bar--sent" style="height:%dpx"></div></div><div class="an-bar-wrap"><span class="an-cnt an-cnt--delivered">%d</span><div class="an-bar an-bar--delivered" style="height:%dpx"></div></div>%s</div><div class="an-bar-lbl">%s</div></div>`,
						dc.sent, sentPx, dc.delivered, delivPx, failBar, label,
					); err != nil {
						return err
					}
				} else {
					if _, err := fmt.Fprintf(w,
						`<div class="an-bar-col an-bar-col--empty"><div class="an-bar-lbl">%s</div></div>`,
						label,
					); err != nil {
						return err
					}
				}
			}
		}

		if _, err := io.WriteString(w, `</div></section>`); err != nil {
			return err
		}

		// Right sidebar: cost split (admin/manager) + quality/tier (always)
		if _, err := io.WriteString(w, `<aside class="an-sidebar">`); err != nil {
			return err
		}

		// Cost split card
		if !isAgent && totalCost > 0 {
			if _, err := fmt.Fprintf(w, `<section class="an-section an-cost-card">
<h2 class="an-section-title">Cost split</h2>
<div class="an-cost-total">₹%.2f <span class="an-cost-period">this period</span></div>
<div class="cost-split-bar">
  <div class="split-seg split-seg--utility" style="width:%.1f%%"></div>
  <div class="split-seg split-seg--marketing" style="width:%.1f%%"></div>
  <div class="split-seg split-seg--auth" style="width:%.1f%%"></div>
</div>
<div class="cost-split-legend">`,
				totalCost, utilPct, mktPct, authPct,
			); err != nil {
				return err
			}
			if marketingCost > 0 {
				fmt.Fprintf(w, `<span class="legend-item legend-item--marketing">Marketing ₹%.2f</span>`, marketingCost) //nolint:errcheck
			}
			if utilityCost > 0 {
				fmt.Fprintf(w, `<span class="legend-item legend-item--utility">Utility ₹%.2f</span>`, utilityCost) //nolint:errcheck
			}
			if authCost > 0 {
				fmt.Fprintf(w, `<span class="legend-item legend-item--auth">Auth ₹%.2f</span>`, authCost) //nolint:errcheck
			}
			fmt.Fprintf(w, `<span class="legend-item legend-item--service">Service free</span>`) //nolint:errcheck
			if _, err := io.WriteString(w, `</div></section>`); err != nil {
				return err
			}
		}

		// Quality / tier card (always visible)
		capLabel := fmt.Sprintf("%d msg/day", data.Quality.DailyCap)
		if _, err := fmt.Fprintf(w, `<section class="an-section an-quality-card">
<h2 class="an-section-title">Quality / tier</h2>
<div class="an-qual-list">
  <div class="an-qual-row"><span class="an-qual-lbl">Tier</span><span class="an-qual-val">%d</span></div>
  <div class="an-qual-row"><span class="an-qual-lbl">Daily cap</span><span class="an-qual-val an-qual-val--bold">%s</span></div>
  <div class="an-qual-row"><span class="an-qual-lbl">Sent today</span><span class="an-qual-val">%d</span></div>
  <div class="an-qual-row"><span class="an-qual-lbl">Quality</span><span class="an-qual-val an-qual-val--muted">%s</span></div>
  <div class="an-qual-row"><span class="an-qual-lbl">Today's usage</span><span class="an-qual-val">%d / %d</span></div>
  <div class="an-usage-bar"><div class="an-usage-fill" style="width:%d%%"></div></div>
</div>
</section>`,
			data.Quality.Tier,
			capLabel,
			data.Quality.SentToday,
			html.EscapeString(data.Quality.QualityRating),
			data.Quality.SentToday, data.Quality.DailyCap,
			usagePct,
		); err != nil {
			return err
		}

		if _, err := io.WriteString(w, `</aside></div>`); err != nil { // close an-main-grid
			return err
		}

		// ── Bottom: cost tables side-by-side (admin/manager only) ─────────────
		if !isAgent && (len(data.ByCat) > 0 || len(data.ByCampaign) > 0) {
			if _, err := io.WriteString(w, `<div class="an-bottom-grid">`); err != nil {
				return err
			}

			// Cost by category
			if len(data.ByCat) > 0 {
				if _, err := io.WriteString(w, `<section class="an-section">
<h2 class="an-section-title">Cost by category</h2>
<table class="an-table">
<thead><tr><th>CATEGORY</th><th>MESSAGES</th><th>COST (INR)</th></tr></thead>
<tbody>`); err != nil {
					return err
				}
				for _, c := range data.ByCat {
					dotClass := anCatDotClass(c.Category)
					muted := ""
					if c.CostINR == 0 {
						muted = ` class="an-muted"`
					}
					if _, err := fmt.Fprintf(w,
						`<tr%s><td><span class="an-cat-dot %s"></span>%s</td><td>%d</td><td>₹%.4f</td></tr>`,
						muted, dotClass, html.EscapeString(catLabel(c.Category)), c.Count, c.CostINR,
					); err != nil {
						return err
					}
				}
				if _, err := io.WriteString(w, `</tbody></table></section>`); err != nil {
					return err
				}
			}

			// Cost by campaign
			if len(data.ByCampaign) > 0 {
				if _, err := io.WriteString(w, `<section class="an-section">
<h2 class="an-section-title">Cost by campaign</h2>
<table class="an-table">
<thead><tr><th>CAMPAIGN</th><th>MESSAGES</th><th>COST (INR)</th></tr></thead>
<tbody>`); err != nil {
					return err
				}
				for _, c := range data.ByCampaign {
					if _, err := fmt.Fprintf(w,
						`<tr><td><a class="an-camp-link" href="/campaigns/%s/report">%s</a></td><td>%d</td><td>₹%.4f</td></tr>`,
						c.CampaignID, html.EscapeString(c.CampaignName), c.Count, c.CostINR,
					); err != nil {
						return err
					}
				}
				if _, err := io.WriteString(w, `</tbody></table></section>`); err != nil {
					return err
				}
			}

			if _, err := io.WriteString(w, `</div>`); err != nil { // close an-bottom-grid
				return err
			}
		}

		// ── Agent performance ─────────────────────────────────────────────────
		if len(data.AgentStats) > 0 {
			if _, err := io.WriteString(w, `<section class="an-section">
<h2 class="an-section-title">Agent performance</h2>
<table class="an-table">
<thead><tr><th>AGENT</th><th>MESSAGES SENT</th><th>CONVS RESOLVED</th></tr></thead>
<tbody>`); err != nil {
				return err
			}
			for _, s := range data.AgentStats {
				if _, err := fmt.Fprintf(w,
					`<tr><td>%s</td><td>%d</td><td>%d</td></tr>`,
					html.EscapeString(s.AgentName), s.MessagesSent, s.ConvsResolved,
				); err != nil {
					return err
				}
			}
			if _, err := io.WriteString(w, `</tbody></table></section>`); err != nil {
				return err
			}
		}

		if _, err := io.WriteString(w, `</div></div>`); err != nil { // close an-body + page-wrap
			return err
		}

		_, err = io.WriteString(w, ShellClose())
		return err
	})
}

func anCatDotClass(cat string) string {
	switch cat {
	case "marketing":
		return "an-dot--marketing"
	case "utility":
		return "an-dot--utility"
	case "authentication":
		return "an-dot--auth"
	case "service":
		return "an-dot--service"
	default:
		return "an-dot--service"
	}
}

func catLabel(cat string) string {
	switch cat {
	case "marketing":
		return "Marketing"
	case "utility":
		return "Utility"
	case "authentication":
		return "Authentication"
	case "service":
		return "Service"
	default:
		return cat
	}
}
