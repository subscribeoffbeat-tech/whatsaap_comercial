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
	// Role gate: agents must not see any cost data, at either layer.
	// (The handler already returns 0 cost_inr from the DB and nil ByCat/ByCampaign
	// for agents, but we defensively gate the template too so the HTML never
	// contains a ₹ figure, even if field values changed.)
	isAgent := agent != nil && agent.Role == "agent"

	// Overview delivery percentages (safe to pre-compute; no cost involved)
	var delivPct, readPct, failPct float64
	if data.Overview.Sent > 0 {
		delivPct = float64(data.Overview.Delivered) * 100.0 / float64(data.Overview.Sent)
		readPct = float64(data.Overview.Read) * 100.0 / float64(data.Overview.Sent)
		failPct = float64(data.Overview.Failed) * 100.0 / float64(data.Overview.Sent)
	}

	// Bar chart: max daily sent to compute proportional heights
	var maxDay int64 = 1
	for _, d := range data.Days {
		if d.Sent > maxDay {
			maxDay = d.Sent
		}
	}

	// Cost split — only computed/used for admin/manager.
	// ByCat is nil for agents at the data layer; isAgent guard is defense-in-depth.
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

	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/analytics", "Analytics", "")); err != nil {
			return err
		}

		// ── Page header ───────────────────────────────────────────────────────
		_, err := fmt.Fprintf(w, `
<div class="page-wrap">
<div class="page-hd">
<h1>Analytics</h1>
<form method="get" action="/analytics" class="an-date-form">
<input type="date" name="from" value="%s">
<input type="date" name="to" value="%s">
<button class="btn btn--secondary" type="submit">Apply</button>
<a class="btn btn--secondary" href="/analytics/export.csv">Export CSV</a>
</form>
</div>`,
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
		if _, err := fmt.Fprintf(w, `
<div class="an-cards">
<div class="an-card"><div class="an-card-val">%d</div><div class="an-card-lbl">Sent</div></div>
<div class="an-card"><div class="an-card-val">%s</div><div class="an-card-lbl">Delivered</div></div>
<div class="an-card"><div class="an-card-val">%s</div><div class="an-card-lbl">Read</div></div>
<div class="an-card"><div class="an-card-val an-val--danger">%s</div><div class="an-card-lbl">Failed</div></div>`,
			data.Overview.Sent, delivStr, readStr, failStr,
		); err != nil {
			return err
		}
		// Cost card: admin/manager only — must NOT appear in agent's rendered HTML.
		if !isAgent {
			if _, err := fmt.Fprintf(w,
				`<div class="an-card"><div class="an-card-val">₹%.2f</div><div class="an-card-lbl">Cost (INR)</div></div>`,
				data.Overview.CostINR,
			); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(w, `</div>`); err != nil {
			return err
		}

		// ── Empty state ───────────────────────────────────────────────────────
		if data.Overview.Sent == 0 {
			if _, err := io.WriteString(w,
				`<p class="empty-state">No messages sent in this period.</p>`); err != nil {
				return err
			}
		}

		// ── Daily chart + cost split grid ─────────────────────────────────────
		if len(data.Days) > 0 {
			showCostSplit := !isAgent && totalCost > 0
			if showCostSplit {
				if _, err := io.WriteString(w, `<div class="an-grid">`); err != nil {
					return err
				}
			}

			// Messages per day bar chart
			if _, err := io.WriteString(w, `<section class="an-section"><h2>Messages per day</h2><div class="an-chart">`); err != nil {
				return err
			}
			for _, d := range data.Days {
				barPct := int(d.Sent * 100 / maxDay)
				if barPct < 4 {
					barPct = 4
				}
				if _, err := fmt.Fprintf(w,
					`<div class="an-bar-col"><div class="an-bar" style="height:%d%%"></div><div class="an-bar-lbl">%s</div></div>`,
					barPct, d.Day.Format("2/1"),
				); err != nil {
					return err
				}
			}
			if _, err := io.WriteString(w, `</div></section>`); err != nil {
				return err
			}

			// Cost split panel (admin/manager only, non-zero cost only)
			if showCostSplit {
				if _, err := fmt.Fprintf(w, `
<section class="an-section"><h2>Cost split</h2>
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
				if utilityCost > 0 {
					if _, err := fmt.Fprintf(w,
						`<span class="legend-item legend-item--utility">Utility ₹%.2f</span>`, utilityCost); err != nil {
						return err
					}
				}
				if marketingCost > 0 {
					if _, err := fmt.Fprintf(w,
						`<span class="legend-item legend-item--marketing">Marketing ₹%.2f</span>`, marketingCost); err != nil {
						return err
					}
				}
				if authCost > 0 {
					if _, err := fmt.Fprintf(w,
						`<span class="legend-item legend-item--auth">Auth ₹%.2f</span>`, authCost); err != nil {
						return err
					}
				}
				if _, err := io.WriteString(w, `<span class="legend-item">Service free</span></div></section>`); err != nil {
					return err
				}
				if _, err := io.WriteString(w, `</div>`); err != nil { // close an-grid
					return err
				}
			}
		}

		// ── ByCat table (admin/manager only) ──────────────────────────────────
		if !isAgent && len(data.ByCat) > 0 {
			if _, err := io.WriteString(w, `
<section class="an-section"><h2>Cost by category</h2>
<table class="an-table"><thead><tr><th>Category</th><th>Messages</th><th>Cost (INR)</th></tr></thead><tbody>`); err != nil {
				return err
			}
			for _, c := range data.ByCat {
				if _, err := fmt.Fprintf(w,
					`<tr><td>%s</td><td>%d</td><td>₹%.4f</td></tr>`,
					html.EscapeString(c.Category), c.Count, c.CostINR,
				); err != nil {
					return err
				}
			}
			if _, err := io.WriteString(w, `</tbody></table></section>`); err != nil {
				return err
			}
		}

		// ── ByCampaign table (admin/manager only) ─────────────────────────────
		if !isAgent && len(data.ByCampaign) > 0 {
			if _, err := io.WriteString(w, `
<section class="an-section"><h2>Cost by campaign</h2>
<table class="an-table"><thead><tr><th>Campaign</th><th>Messages</th><th>Cost (INR)</th></tr></thead><tbody>`); err != nil {
				return err
			}
			for _, c := range data.ByCampaign {
				if _, err := fmt.Fprintf(w,
					`<tr><td><a href="/campaigns/%s/report">%s</a></td><td>%d</td><td>₹%.4f</td></tr>`,
					c.CampaignID, html.EscapeString(c.CampaignName), c.Count, c.CostINR,
				); err != nil {
					return err
				}
			}
			if _, err := io.WriteString(w, `</tbody></table></section>`); err != nil {
				return err
			}
		}

		// ── Agent stats table ─────────────────────────────────────────────────
		if len(data.AgentStats) > 0 {
			if _, err := io.WriteString(w, `
<section class="an-section"><h2>Agent performance</h2>
<table class="an-table"><thead><tr><th>Agent</th><th>Messages sent</th><th>Convs resolved</th></tr></thead><tbody>`); err != nil {
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

		// ── Quality / tier ────────────────────────────────────────────────────
		if _, err := fmt.Fprintf(w, `
<section class="an-section"><h2>Quality / tier</h2>
<dl>
<dt>Tier</dt><dd>%d</dd>
<dt>Daily cap</dt><dd>%d</dd>
<dt>Sent today</dt><dd>%d</dd>
<dt>Quality</dt><dd>%s</dd>
</dl>
</section>
</div>`,
			data.Quality.Tier, data.Quality.DailyCap,
			data.Quality.SentToday, html.EscapeString(data.Quality.QualityRating),
		); err != nil {
			return err
		}

		_, err = io.WriteString(w, ShellClose())
		return err
	})
}
