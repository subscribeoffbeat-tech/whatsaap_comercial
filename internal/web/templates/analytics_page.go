package templates

import (
	"context"
	"fmt"
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
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/analytics", "Analytics", "")); err != nil {
			return err
		}

		_, err := fmt.Fprintf(w, `
<div class="page-wrap">
<div class="page-hd">
<h1>Analytics</h1>
<form method="get" action="/analytics" class="date-range-form">
<input type="date" name="from" value="%s">
<input type="date" name="to" value="%s">
<button class="btn" type="submit">Apply</button>
<a class="btn" href="/analytics/export.csv">Export CSV</a>
</form>
</div>

<section class="card">
<h2>Overview</h2>
<div class="stat-row">
<div class="stat-card"><div class="stat-val">%d</div><div class="stat-lbl">Sent</div></div>
<div class="stat-card"><div class="stat-val">%d</div><div class="stat-lbl">Delivered</div></div>
<div class="stat-card"><div class="stat-val">%d</div><div class="stat-lbl">Read</div></div>
<div class="stat-card"><div class="stat-val">%d</div><div class="stat-lbl">Failed</div></div>
<div class="stat-card"><div class="stat-val">₹%.2f</div><div class="stat-lbl">Cost (INR)</div></div>
</div>
</section>`,
			data.From.Format("2006-01-02"), data.To.Format("2006-01-02"),
			data.Overview.Sent, data.Overview.Delivered, data.Overview.Read,
			data.Overview.Failed, data.Overview.CostINR)
		if err != nil {
			return err
		}

		if len(data.ByCat) > 0 {
			if _, err := io.WriteString(w, `<section class="card"><h2>Cost by category</h2><table class="tbl"><thead><tr><th>Category</th><th>Messages</th><th>Cost (INR)</th></tr></thead><tbody>`); err != nil {
				return err
			}
			for _, c := range data.ByCat {
				if _, err := fmt.Fprintf(w, `<tr><td>%s</td><td>%d</td><td>₹%.4f</td></tr>`, c.Category, c.Count, c.CostINR); err != nil {
					return err
				}
			}
			if _, err := io.WriteString(w, `</tbody></table></section>`); err != nil {
				return err
			}
		}

		if len(data.ByCampaign) > 0 {
			if _, err := io.WriteString(w, `<section class="card"><h2>Cost by campaign</h2><table class="tbl"><thead><tr><th>Campaign</th><th>Messages</th><th>Cost (INR)</th></tr></thead><tbody>`); err != nil {
				return err
			}
			for _, c := range data.ByCampaign {
				if _, err := fmt.Fprintf(w, `<tr><td><a href="/campaigns/%s/report">%s</a></td><td>%d</td><td>₹%.4f</td></tr>`, c.CampaignID, c.CampaignName, c.Count, c.CostINR); err != nil {
					return err
				}
			}
			if _, err := io.WriteString(w, `</tbody></table></section>`); err != nil {
				return err
			}
		}

		if len(data.AgentStats) > 0 {
			if _, err := io.WriteString(w, `<section class="card"><h2>Agent performance</h2><table class="tbl"><thead><tr><th>Agent</th><th>Messages sent</th><th>Convs resolved</th></tr></thead><tbody>`); err != nil {
				return err
			}
			for _, s := range data.AgentStats {
				if _, err := fmt.Fprintf(w, `<tr><td>%s</td><td>%d</td><td>%d</td></tr>`, s.AgentName, s.MessagesSent, s.ConvsResolved); err != nil {
					return err
				}
			}
			if _, err := io.WriteString(w, `</tbody></table></section>`); err != nil {
				return err
			}
		}

		if _, err := fmt.Fprintf(w, `
<section class="card">
<h2>Quality / tier</h2>
<dl>
<dt>Tier</dt><dd>%d</dd>
<dt>Daily cap</dt><dd>%d</dd>
<dt>Sent today</dt><dd>%d</dd>
<dt>Quality</dt><dd>%s</dd>
</dl>
</section>
</div>`, data.Quality.Tier, data.Quality.DailyCap, data.Quality.SentToday, data.Quality.QualityRating); err != nil {
			return err
		}

		_, err = io.WriteString(w, ShellClose())
		return err
	})
}
