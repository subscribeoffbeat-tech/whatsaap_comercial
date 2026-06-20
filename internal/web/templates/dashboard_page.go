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
	qualityDotClass := "quality-green"
	qualityLabel := "High"
	if stats.QualityRating == "yellow" {
		qualityDotClass = "quality-yellow"
		qualityLabel = "Medium"
	} else if stats.QualityRating == "red" {
		qualityDotClass = "quality-red"
		qualityLabel = "Low"
	}

	var capPct int
	if stats.DailyCap > 0 {
		capPct = int(stats.SentToday * 100 / stats.DailyCap)
		if capPct > 100 {
			capPct = 100
		}
	}

	isAgent := agent != nil && agent.Role == "agent"

	fillClass := "tier-bar-fill"
	if capPct >= 90 {
		fillClass = "tier-bar-fill tier-bar-fill--warn"
	}

	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/", "Dashboard", "")); err != nil {
			return err
		}

		// ── Page header ───────────────────────────────────────────────────────────
		if _, err := io.WriteString(w, `
<div class="page-wrap">
<div class="page-hd">
<div>
<h1 class="screen-title">Dashboard</h1>
<p class="screen-subtitle">Overview of your WhatsApp channel</p>
</div>
<div style="display:flex;gap:0.5rem;flex-wrap:wrap">
<a class="btn btn-secondary btn-sm" href="/contacts/import">Import contacts</a>
<a class="btn btn-primary btn-sm" href="/campaigns/new">+ New campaign</a>
</div>
</div>`); err != nil {
			return err
		}

		// ── Stat cards ────────────────────────────────────────────────────────────
		if _, err := io.WriteString(w, `<div class="stat-grid">`); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w,
			`<div class="stat-card"><div class="stat-label">Sent today</div><div class="stat-value">%d</div><div class="stat-sub">messages dispatched</div></div>`,
			stats.SentToday,
		); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w,
			`<div class="stat-card"><div class="stat-label">Delivered</div><div class="stat-value">%d</div><div class="stat-sub">confirmed by Meta</div></div>`,
			stats.DeliveredToday,
		); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w,
			`<div class="stat-card"><div class="stat-label">Chats waiting</div><div class="stat-value">%d</div><div class="stat-sub">open conversations</div></div>`,
			stats.ChatsWaiting,
		); err != nil {
			return err
		}
		if !isAgent {
			if _, err := fmt.Fprintf(w,
				`<div class="stat-card"><div class="stat-label">Cost this month</div><div class="stat-value">₹%.2f</div><div class="stat-sub">incl. 18%% GST</div></div>`,
				stats.CostThisMonth,
			); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w,
			`<div class="stat-card"><div class="stat-label">Quality</div><div class="stat-value" style="font-size:1.5rem;display:flex;align-items:center;gap:0.4rem"><span class="quality-dot %s"></span>%s</div><div class="stat-sub">Meta phone quality</div></div>`,
			qualityDotClass, html.EscapeString(qualityLabel),
		); err != nil {
			return err
		}
		if _, err := io.WriteString(w, `</div>`); err != nil {
			return err
		}

		// ── Daily send capacity card ──────────────────────────────────────────────
		if _, err := fmt.Fprintf(w, `
<div class="card-static" style="margin-bottom:24px">
<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:14px">
  <div style="font-size:15px;font-weight:600;color:var(--text-strong)">Daily send capacity</div>
  <div style="font-size:13px;color:var(--text-secondary)">%d / %d messages</div>
</div>
<div class="tier-bar-wrap"><div class="%s" style="width:%d%%"></div></div>
<div style="display:flex;justify-content:space-between;font-size:12px;color:var(--text-muted);margin-top:6px">
  <span>%d</span><span>%d%% used</span><span>%d</span>
</div>
</div>`,
			stats.SentToday, stats.DailyCap,
			fillClass, capPct,
			stats.SentToday, capPct, stats.DailyCap,
		); err != nil {
			return err
		}

		// ── Two-column layout ─────────────────────────────────────────────────────
		if _, err := io.WriteString(w, `
<div style="display:grid;grid-template-columns:1fr 280px;gap:24px;align-items:start">`); err != nil {
			return err
		}

		// ── Left: Recent campaigns ────────────────────────────────────────────────
		if _, err := io.WriteString(w, `
<div class="card-static">
<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:14px">
  <h2 style="margin:0;font-size:15px;font-weight:600;color:var(--text-strong)">Recent campaigns</h2>
  <a class="btn btn-secondary btn-sm" href="/campaigns">View all</a>
</div>`); err != nil {
			return err
		}

		if len(recent) == 0 {
			if _, err := io.WriteString(w, EmptyStateHTML(EmptyIconCampaigns,
				"No campaigns yet",
				"Launch your first broadcast to get started.",
				[]EmptyAction{{Label: "New campaign", Primary: true, HREF: "/campaigns/new"}},
			)); err != nil {
				return err
			}
		} else {
			if _, err := io.WriteString(w, `<table class="an-table tbl"><thead><tr>
<th>CAMPAIGN</th><th>STATUS</th><th>SENT</th><th>DELIVERED</th><th>FAILED</th>`); err != nil {
				return err
			}
			if !isAgent {
				if _, err := io.WriteString(w, `<th>COST</th>`); err != nil {
					return err
				}
			}
			if _, err := io.WriteString(w, `</tr></thead><tbody>`); err != nil {
				return err
			}
			for _, c := range recent {
				statusBadge := BadgeHTML(c.Status, c.Status)
				if _, err := fmt.Fprintf(w,
					`<tr><td><a href="/campaigns/%s/report" style="font-weight:500">%s</a></td><td>%s</td><td>%d</td><td>%d</td><td>%d</td>`,
					html.EscapeString(c.ID), html.EscapeString(c.Name),
					statusBadge, c.SentCount, c.DeliveredCount, c.FailedCount,
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

		if _, err := io.WriteString(w, `</div>`); err != nil { // close left card-static
			return err
		}

		// ── Right: Quick actions + Account health ─────────────────────────────────
		if _, err := io.WriteString(w, `<div style="display:flex;flex-direction:column;gap:16px">`); err != nil {
			return err
		}

		// Quick actions card
		if _, err := io.WriteString(w, `
<div class="card-static">
<h2 style="margin:0 0 14px;font-size:15px;font-weight:600;color:var(--text-strong)">Quick actions</h2>
<div style="display:flex;flex-direction:column;gap:8px">
<a class="btn btn-primary" href="/campaigns/new" style="text-align:center">+ New campaign</a>
<a class="btn btn-secondary btn-sm" href="/inbox" style="text-align:center">Open inbox</a>
<a class="btn btn-secondary btn-sm" href="/contacts/import" style="text-align:center">Import contacts</a>
<a class="btn btn-secondary btn-sm" href="/templates" style="text-align:center">New template</a>
</div>
</div>`); err != nil {
			return err
		}

		// Account health card
		if _, err := fmt.Fprintf(w, `
<div class="card-static">
<h2 style="margin:0 0 14px;font-size:15px;font-weight:600;color:var(--text-strong)">Account health</h2>
<dl style="margin:0;display:flex;flex-direction:column;gap:8px">
<div style="display:flex;justify-content:space-between;font-size:13px">
  <dt style="color:var(--text-secondary)">Quality</dt>
  <dd style="margin:0;display:flex;align-items:center;gap:4px;font-weight:500">
    <span class="quality-dot %s"></span>%s
  </dd>
</div>
<div style="display:flex;justify-content:space-between;font-size:13px">
  <dt style="color:var(--text-secondary)">Daily cap</dt>
  <dd style="margin:0;font-weight:500">%d messages</dd>
</div>
<div style="display:flex;justify-content:space-between;font-size:13px">
  <dt style="color:var(--text-secondary)">Sent today</dt>
  <dd style="margin:0;font-weight:500">%d</dd>
</div>
</dl>
</div>`,
			qualityDotClass, html.EscapeString(qualityLabel),
			stats.DailyCap, stats.SentToday,
		); err != nil {
			return err
		}

		// close right column + two-column grid + page-wrap
		if _, err := io.WriteString(w, `</div></div></div>`); err != nil {
			return err
		}

		_, err := io.WriteString(w, ShellClose())
		return err
	})
}
