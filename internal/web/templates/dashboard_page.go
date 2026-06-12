package templates

import (
	"context"
	"fmt"
	"io"

	"github.com/a-h/templ"

	"whatsapptool/internal/db"
	mw "whatsapptool/internal/web/middleware"
)

func DashboardPage(agent *mw.AgentClaims, stats db.DashboardStats, recent []db.Campaign) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/", "Dashboard", "")); err != nil {
			return err
		}

		qualityClass := "stat-green"
		if stats.QualityRating == "yellow" {
			qualityClass = "stat-yellow"
		} else if stats.QualityRating == "red" {
			qualityClass = "stat-red"
		}

		_, err := fmt.Fprintf(w, `
<div class="dash-wrap">
<div class="dash-stats">
<div class="stat-card"><div class="stat-val">%d</div><div class="stat-lbl">Sent today</div></div>
<div class="stat-card"><div class="stat-val">%d</div><div class="stat-lbl">Delivered today</div></div>
<div class="stat-card"><div class="stat-val">%d</div><div class="stat-lbl">Chats waiting</div></div>
<div class="stat-card"><div class="stat-val">₹%.2f</div><div class="stat-lbl">Cost this month</div></div>
<div class="stat-card"><div class="stat-val %s">%s</div><div class="stat-lbl">Quality</div></div>
</div>
<h2>Recent campaigns</h2>
<table class="tbl">
<thead><tr><th>Name</th><th>Status</th><th>Sent</th><th>Delivered</th><th>Failed</th><th>Cost</th></tr></thead>
<tbody>`,
			stats.SentToday, stats.DeliveredToday, stats.ChatsWaiting,
			stats.CostThisMonth, qualityClass, stats.QualityRating)
		if err != nil {
			return err
		}

		for _, c := range recent {
			if _, err := fmt.Fprintf(w,
				`<tr><td><a href="/campaigns/%s/report">%s</a></td><td>%s</td><td>%d</td><td>%d</td><td>%d</td><td>₹%.2f</td></tr>`,
				c.ID, c.Name, c.Status, c.SentCount, c.DeliveredCount, c.FailedCount, c.CostTotalINR,
			); err != nil {
				return err
			}
		}

		if _, err := io.WriteString(w, `</tbody></table></div>`); err != nil {
			return err
		}
		_, err = io.WriteString(w, ShellClose())
		return err
	})
}
