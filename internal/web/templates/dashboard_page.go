package templates

import (
	"context"
	"fmt"
	"html"
	"io"

	"github.com/a-h/templ"

	"whatsapptool/internal/db"
	mw "whatsapptool/internal/web/middleware"
)

func DashboardPage(agent *mw.AgentClaims, stats db.DashboardStats, recent []db.Campaign) templ.Component {
	// Quality dot: map rating string to the CSS class that EXISTS in app.css
	qualityDotClass := "quality-green"
	qualityLabel := "High quality"
	if stats.QualityRating == "yellow" {
		qualityDotClass = "quality-yellow"
		qualityLabel = "Medium quality"
	} else if stats.QualityRating == "red" {
		qualityDotClass = "quality-red"
		qualityLabel = "Low quality — action required"
	}

	// Tier-cap progress bar: same DailyCap source as WizardAudience + LimitGuardCheck
	var capPct int
	if stats.DailyCap > 0 {
		capPct = int(stats.SentToday * 100 / stats.DailyCap)
		if capPct > 100 {
			capPct = 100
		}
	}

	// Agents must not see cost data (defense-in-depth; handler already zeros CostThisMonth)
	isAgent := agent != nil && agent.Role == "agent"

	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/", "Dashboard", "")); err != nil {
			return err
		}

		// ── Page header ───────────────────────────────────────────────────────────
		if _, err := io.WriteString(w, `
<div class="page-wrap">
<div class="page-hd">
<div>
<div class="screen-title">Dashboard</div>
<div class="screen-subtitle">Overview of your WhatsApp channel</div>
</div>
<div style="display:flex;gap:0.5rem;flex-wrap:wrap;">
<a class="btn btn-secondary btn-sm" href="/contacts/import">Import contacts</a>
<a class="btn btn-primary btn-sm" href="/campaigns/new">+ New campaign</a>
</div>
</div>`); err != nil {
			return err
		}

		// ── Stat cards (use .an-card / .an-card-val / .an-card-lbl from analytics.css) ──
		if _, err := io.WriteString(w, `<div class="an-cards">`); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w,
			`<div class="an-card"><div class="an-card-val">%d</div><div class="an-card-lbl">Sent today</div></div>`,
			stats.SentToday,
		); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w,
			`<div class="an-card"><div class="an-card-val">%d</div><div class="an-card-lbl">Delivered today</div></div>`,
			stats.DeliveredToday,
		); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w,
			`<div class="an-card"><div class="an-card-val">%d</div><div class="an-card-lbl">Chats waiting</div></div>`,
			stats.ChatsWaiting,
		); err != nil {
			return err
		}
		if !isAgent {
			if _, err := fmt.Fprintf(w,
				`<div class="an-card"><div class="an-card-val">₹%.2f</div><div class="an-card-lbl">Cost this month</div></div>`,
				stats.CostThisMonth,
			); err != nil {
				return err
			}
		}
		// Quality card: colored dot + label — uses .quality-dot + .quality-green/yellow/red from app.css
		if _, err := fmt.Fprintf(w,
			`<div class="an-card"><div class="an-card-val" style="font-size:1rem;gap:0.4rem;"><span class="quality-dot %s"></span>%s</div><div class="an-card-lbl">Quality</div></div>`,
			qualityDotClass, html.EscapeString(qualityLabel),
		); err != nil {
			return err
		}
		if _, err := io.WriteString(w, `</div>`); err != nil {
			return err
		}

		// ── Tier-cap progress bar ─────────────────────────────────────────────────
		if stats.DailyCap > 0 {
			fillClass := "tier-bar-fill"
			if capPct >= 90 {
				fillClass = "tier-bar-fill tier-bar-fill--warn"
			}
			if _, err := fmt.Fprintf(w, `
<div class="tier-bar-row">
<span class="tier-bar-lbl">Daily cap</span>
<div class="tier-bar-wrap"><div class="%s" style="width:%d%%"></div></div>
<span class="tier-bar-lbl">%d / %d</span>
</div>`, fillClass, capPct, stats.SentToday, stats.DailyCap); err != nil {
				return err
			}
		}

		// ── Quick-actions panel ───────────────────────────────────────────────────
		if _, err := io.WriteString(w, `
<div class="dash-actions">
<a class="btn btn-primary" href="/campaigns/new">New campaign</a>
<a class="btn btn-secondary" href="/inbox">Open inbox</a>
<a class="btn btn-secondary" href="/contacts/import">Import contacts</a>
<a class="btn btn-secondary" href="/templates/new">New template</a>
</div>`); err != nil {
			return err
		}

		// ── Recent campaigns ──────────────────────────────────────────────────────
		if _, err := io.WriteString(w, `
<section class="an-section">
<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:0.75rem;">
<h2 style="margin:0;font-size:1rem;font-weight:600;color:var(--text-strong)">Recent campaigns</h2>
<a class="btn btn-secondary btn-sm" href="/campaigns">View all</a>
</div>`); err != nil {
			return err
		}

		if len(recent) == 0 {
			if _, err := io.WriteString(w, `<p class="empty-state">No campaigns yet. <a href="/campaigns/new">Create one →</a></p>`); err != nil {
				return err
			}
		} else {
			if _, err := io.WriteString(w, `<table class="an-table"><thead><tr><th>Name</th><th>Status</th><th>Sent</th><th>Delivered</th><th>Failed</th>`); err != nil {
				return err
			}
			if !isAgent {
				if _, err := io.WriteString(w, `<th>Cost</th>`); err != nil {
					return err
				}
			}
			if _, err := io.WriteString(w, `</tr></thead><tbody>`); err != nil {
				return err
			}
			for _, c := range recent {
				if _, err := fmt.Fprintf(w,
					`<tr><td><a href="/campaigns/%s/report">%s</a></td><td>%s</td><td>%d</td><td>%d</td><td>%d</td>`,
					html.EscapeString(c.ID), html.EscapeString(c.Name), html.EscapeString(c.Status),
					c.SentCount, c.DeliveredCount, c.FailedCount,
				); err != nil {
					return err
				}
				if !isAgent {
					if _, err := fmt.Fprintf(w, `<td>₹%.2f</td>`, c.CostTotalINR); err != nil {
						return err
					}
				}
				if _, err := io.WriteString(w, `</tr>`); err != nil {
					return err
				}
			}
			if _, err := io.WriteString(w, `</tbody></table>`); err != nil {
				return err
			}
		}

		if _, err := io.WriteString(w, `</section></div>`); err != nil {
			return err
		}
		_, err := io.WriteString(w, ShellClose())
		return err
	})
}
