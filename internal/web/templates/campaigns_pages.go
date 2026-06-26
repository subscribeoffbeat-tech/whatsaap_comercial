package templates

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"

	"whatsapptool/internal/campaigns"
	"whatsapptool/internal/db"
	mw "whatsapptool/internal/web/middleware"
)

var varPlaceholderRe = regexp.MustCompile(`\{\{(\d+)\}\}`)

// TemplateBodyText returns the body text from the template's components array.
func TemplateBodyText(tmpl db.Template) string {
	for _, comp := range tmpl.Components {
		t, _ := comp["type"].(string)
		if strings.EqualFold(t, "BODY") {
			text, _ := comp["text"].(string)
			return text
		}
	}
	return ""
}

// ExtractVarNames returns sorted unique variable indices found in body text, e.g. ["1","2"].
func ExtractVarNames(body string) []string {
	seen := map[string]bool{}
	for _, m := range varPlaceholderRe.FindAllStringSubmatch(body, -1) {
		seen[m[1]] = true
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := strconv.Atoi(out[i])
		b, _ := strconv.Atoi(out[j])
		return a < b
	})
	return out
}

// WizardState carries campaign-creation state across wizard steps via a hidden
// form field. Serialised as base64-encoded JSON.
type WizardState struct {
	Step              int
	Name              string
	Category          string // marketing | utility | authentication (from Basics step)
	Notes             string // internal notes (from Basics step)
	HasVars           bool   // template has {{N}} placeholders
	TemplateID        string
	HasMediaHeader    bool   // template has an IMAGE/VIDEO/DOCUMENT header (needs upload)
	MediaHeaderFormat string // image | video | document (when HasMediaHeader)
	VarMap            map[string]string
	Fallbacks         map[string]string
	UseAllContacts    bool    // audience: all opted-in contacts (no tag filter)
	SegmentTagIDs     []int64 // audience: selected tag IDs (union); nil = all
	EligibleCount     int
	SkipReport        campaigns.SkipReport
	ScheduleType      string
	ScheduledAt       *time.Time
	EstCost           float64
	DailyCap          int
	DailySent         int
}

func encodeState(s WizardState) string {
	b, _ := json.Marshal(s)
	return base64.StdEncoding.EncodeToString(b)
}

// DecodeState deserialises a WizardState from a hidden form field value.
func DecodeState(s string) (WizardState, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return WizardState{}, fmt.Errorf("decode state: %w", err)
	}
	var state WizardState
	if err := json.Unmarshal(b, &state); err != nil {
		return WizardState{}, fmt.Errorf("unmarshal state: %w", err)
	}
	return state, nil
}

// ── Campaign list ─────────────────────────────────────────────────────────────

func CampaignsPage(agent *mw.AgentClaims, cs []db.Campaign, dailyCap int64) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/campaigns", "Campaigns", "")); err != nil {
			return err
		}

		_, err := io.WriteString(w, `
<div class="page-wrap">
<div class="page-hd">
<div><h1 class="screen-title">Campaigns</h1><p class="screen-subtitle">Broadcast messages to your contacts</p></div>
<a class="btn btn-primary btn-sm" href="/campaigns/new">+ New campaign</a>
</div>`)
		if err != nil {
			return err
		}

		if len(cs) == 0 {
			if _, err := io.WriteString(w, EmptyStateHTML(
				EmptyIconCampaigns,
				"No campaigns yet",
				"Broadcast to your opted-in contacts with a Meta-approved template.",
				[]EmptyAction{{Label: "New campaign", Primary: true, HREF: "/campaigns/new"}},
			)); err != nil {
				return err
			}
		} else {
			// Status tabs: map DB statuses to display tabs.
			counts := map[string]int{"all": len(cs), "completed": 0, "sending": 0, "scheduled": 0, "paused": 0}
			for _, c := range cs {
				switch c.Status {
				case "completed":
					counts["completed"]++
				case "running":
					counts["sending"]++
				case "scheduled":
					counts["scheduled"]++
				case "paused":
					counts["paused"]++
				}
			}

			_, err = fmt.Fprintf(w, `
<div x-data="{tab:'all'}" class="cmp-list-wrap">
<div class="cmp-tabs" role="tablist">
<button class="cmp-tab" role="tab" :class="{active:tab==='all'}" @click="tab='all'" type="button">All</button>
<button class="cmp-tab" role="tab" :class="{active:tab==='completed'}" @click="tab='completed'" type="button">Completed</button>
<button class="cmp-tab" role="tab" :class="{active:tab==='sending'}" @click="tab='sending'" type="button">Sending</button>
<button class="cmp-tab" role="tab" :class="{active:tab==='scheduled'}" @click="tab='scheduled'" type="button">Scheduled</button>
<button class="cmp-tab" role="tab" :class="{active:tab==='paused'}" @click="tab='paused'" type="button">Paused</button>
</div>
<div class="card-static cmp-table-card">
<table class="tbl cmp-tbl">
<thead><tr>
<th>CAMPAIGN</th><th>TEMPLATE</th><th>AUDIENCE</th><th>SENT</th>
<th>READ RATE</th><th>STATUS</th><th>COST</th><th>DATE</th>
</tr></thead>
<tbody>`)
			if err != nil {
				return err
			}

			for _, c := range cs {
				// Map status to tab.
				tabStatus := c.Status
				switch c.Status {
				case "running":
					tabStatus = "sending"
				}
				xShow := fmt.Sprintf(`tab==='all'||tab===%s`, jsLit(tabStatus))

				// Date: prefer scheduled, then completed, then created.
				dateStr := c.CreatedAt.Format("02 Jan 2006")
				if c.CompletedAt != nil {
					dateStr = c.CompletedAt.Format("02 Jan 2006")
				}
				if c.ScheduledAt != nil {
					dateStr = c.ScheduledAt.Format("02 Jan 2006")
				}

				// Status badge.
				badgeVariant := c.Status
				badgeLabel := c.Status
				switch c.Status {
				case "running":
					badgeVariant, badgeLabel = "sending", "sending"
				case "completed":
					badgeVariant = "approved"
				case "paused":
					badgeVariant = "warning"
				case "scheduled":
					badgeVariant = "pending"
				case "cancelled", "draft":
					badgeVariant = "neutral"
				}

				// Read rate cell.
				readRateCell := `<td class="cmp-td-rate"><span class="cmp-dash">—</span></td>`
				if c.SentCount > 0 && c.ReadCount > 0 {
					pct := c.ReadCount * 100 / c.SentCount
					readRateCell = fmt.Sprintf(
						`<td class="cmp-td-rate"><div class="cmp-rate-bar"><div class="cmp-rate-fill" style="width:%d%%"></div></div><span class="cmp-rate-pct">%d%%</span></td>`,
						pct, pct)
				}

				// Cost: show "—" when zero.
				costStr := fmt.Sprintf("&#8377;%.2f", c.CostTotalINR)
				if c.CostTotalINR == 0 {
					costStr = `<span class="cmp-dash">—</span>`
				}

				_, err = fmt.Fprintf(w,
					`<tr x-show="%s">
<td><a href="/campaigns/%s/report" class="cmp-name">%s</a></td>
<td class="cmp-tmpl"><code>%s</code></td>
<td class="cmp-num">%s</td>
<td class="cmp-num">%s</td>
%s
<td>%s</td>
<td class="cmp-cost">%s</td>
<td class="cmp-date">%s</td>
</tr>`,
					xShow,
					c.ID, html.EscapeString(c.Name),
					html.EscapeString(c.TemplateName),
					fmtNum(c.TotalRecipients),
					fmtNum(c.SentCount),
					readRateCell,
					BadgeHTML(badgeVariant, badgeLabel),
					costStr,
					dateStr,
				)
				if err != nil {
					return err
				}
			}

			_, err = io.WriteString(w, `</tbody></table></div>`)
			if err != nil {
				return err
			}

			// Tier info bar.
			_, err = fmt.Fprintf(w,
				`<div class="cmp-tier-bar">
<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="10"/><path d="M12 8h.01M12 12v4"/></svg>
Your current tier allows <strong>%s messages / day</strong>. Increase your messaging limits by maintaining high quality ratings.
</div>`, fmtNum(int(dailyCap)))
			if err != nil {
				return err
			}

			if _, err = io.WriteString(w, `</div>`); err != nil { // close cmp-list-wrap
				return err
			}
		}

		_, err = io.WriteString(w, `</div>`) // page-wrap
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, ShellClose())
		return err
	})
}

// CampaignRecipientRows renders the expandable recipient detail rows.
func CampaignRecipientRows(recipients []db.CampaignRecipient) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if len(recipients) == 0 {
			_, err := io.WriteString(w, `<p style="font-size:13px;color:var(--text-secondary);padding:8px 0">No recipients found.</p>`)
			return err
		}
		if _, err := io.WriteString(w, `<table class="rpt-table"><thead><tr><th>CONTACT</th><th>PHONE</th><th>STATUS</th><th>SENT AT</th><th>NOTE</th></tr></thead><tbody>`); err != nil {
			return err
		}
		for _, r := range recipients {
			name := r.Name
			if name == "" {
				name = r.WAPhone
			}
			sentAt := "—"
			if r.SentAt != nil {
				sentAt = r.SentAt.Format("02 Jan 15:04")
			}
			note := "—"
			if r.SkipReason != nil && *r.SkipReason != "" {
				note = html.EscapeString(*r.SkipReason)
			}
			if _, err := fmt.Fprintf(w, `<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>`,
				html.EscapeString(name),
				html.EscapeString(r.WAPhone),
				BadgeHTML(r.Status, r.Status),
				sentAt,
				note,
			); err != nil {
				return err
			}
		}
		_, err := io.WriteString(w, `</tbody></table>`)
		return err
	})
}

func progressTrigger(status string) string {
	if status == "running" {
		return `every 5s`
	}
	return `load once`
}

func CampaignProgressBar(campaign db.Campaign) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		pct := 0
		if campaign.TotalRecipients > 0 {
			pct = (campaign.SentCount + campaign.FailedCount + campaign.SkippedCount) * 100 / campaign.TotalRecipients
		}
		_, err := fmt.Fprintf(w,
			`<div id="prog-%s" class="progress-bar"
  hx-get="/campaigns/%s/progress"
  hx-trigger="%s"
  hx-swap="outerHTML">
<div class="progress-fill" style="width:%d%%"></div>
<span>%d / %d</span>
</div>`,
			campaign.ID, campaign.ID, progressTrigger(campaign.Status),
			pct, campaign.SentCount+campaign.FailedCount, campaign.TotalRecipients,
		)
		return err
	})
}

func CampaignReportPage(agent *mw.AgentClaims, report db.CampaignReport, failed []db.FailedRecipient, hourly []db.HourlyCount) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/campaigns", "Campaign — "+report.Name, "")); err != nil {
			return err
		}

		// ── Header ──────────────────────────────────────────────────────────
		dateStr := report.CreatedAt.Format("02 Jan 2006")
		if report.CompletedAt != nil {
			dateStr = report.CompletedAt.Format("02 Jan 2006")
		}
		if report.ScheduledAt != nil {
			dateStr = report.ScheduledAt.Format("02 Jan 2006")
		}

		badgeVariant := report.Status
		switch report.Status {
		case "completed":
			badgeVariant = "approved"
		case "running":
			badgeVariant = "sending"
		case "paused":
			badgeVariant = "warning"
		case "scheduled":
			badgeVariant = "pending"
		}

		_, err := fmt.Fprintf(w, `
<div class="page-wrap rpt-wrap">
<div class="rpt-hd">
<div class="rpt-hd-left">
<a href="/campaigns" class="rpt-back"><svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="m15 18-6-6 6-6"/></svg></a>
<div>
<div class="rpt-title">%s %s</div>
<div class="rpt-sub">%s &nbsp;<code class="rpt-tmpl-slug">%s</code></div>
</div>
</div>
<a class="btn btn-secondary btn-sm" href="/campaigns/%s/export">`+ExportIconSVG+`
Export
</a>
</div>`,
			html.EscapeString(report.Name),
			BadgeHTML(badgeVariant, report.Status),
			dateStr,
			html.EscapeString(report.TemplateName),
			report.ID,
		)
		if err != nil {
			return err
		}

		// ── Stat cards ───────────────────────────────────────────────────────
		dlvPct := ""
		if report.SentCount > 0 {
			dlvPct = fmt.Sprintf("%d%% of sent", report.DeliveredCount*100/report.SentCount)
		}
		readPct := ""
		if report.SentCount > 0 {
			readPct = fmt.Sprintf("%d%% read rate", report.ReadCount*100/report.SentCount)
		}

		// Skipped contacts (blocked before send, e.g. frequency cap) are shown as
		// a card only when present, so a campaign where nothing reached anyone is
		// obviously flagged rather than looking like a clean success.
		skipCard := ""
		if report.SkippedCount > 0 {
			skipCard = fmt.Sprintf(
				`<div class="rpt-stat-card rpt-stat-card--fail"><div class="rpt-stat-label">SKIPPED</div><div class="rpt-stat-val rpt-stat-val--fail">%s</div><div class="rpt-stat-sub">blocked before send</div></div>`,
				fmtNum(report.SkippedCount))
		}

		_, err = fmt.Fprintf(w, `
<div class="rpt-stat-row">
<div class="rpt-stat-card"><div class="rpt-stat-label">TARGETED</div><div class="rpt-stat-val">%s</div><div class="rpt-stat-sub">total recipients</div></div>
<div class="rpt-stat-card"><div class="rpt-stat-label">SENT</div><div class="rpt-stat-val">%s</div><div class="rpt-stat-sub">messages dispatched</div></div>
<div class="rpt-stat-card"><div class="rpt-stat-label">DELIVERED</div><div class="rpt-stat-val">%s</div><div class="rpt-stat-sub">%s</div></div>
<div class="rpt-stat-card rpt-stat-card--read"><div class="rpt-stat-label">READ</div><div class="rpt-stat-val rpt-stat-val--read">%s</div><div class="rpt-stat-sub">%s</div></div>
<div class="rpt-stat-card rpt-stat-card--fail"><div class="rpt-stat-label">FAILED</div><div class="rpt-stat-val rpt-stat-val--fail">%s</div><div class="rpt-stat-sub">not delivered</div></div>
%s
</div>`,
			fmtNum(report.TotalRecipients),
			fmtNum(report.SentCount),
			fmtNum(report.DeliveredCount), dlvPct,
			fmtNum(report.ReadCount), readPct,
			fmtNum(report.FailedCount),
			skipCard,
		)
		if err != nil {
			return err
		}

		// ── Two-column body: left (funnel + chart) | right (info + rate) ───
		if _, err = io.WriteString(w, `<div class="rpt-body">`); err != nil {
			return err
		}

		// Left column.
		if _, err = io.WriteString(w, `<div class="rpt-left">`); err != nil {
			return err
		}

		// Delivery funnel.
		type funnelRow struct {
			Label string
			Count int
			Total int
		}
		funnelRows := []funnelRow{
			{"Targeted", report.TotalRecipients, report.TotalRecipients},
			{"Sent", report.SentCount, report.TotalRecipients},
			{"Delivered", report.DeliveredCount, report.TotalRecipients},
			{"Read", report.ReadCount, report.TotalRecipients},
		}
		if _, err = io.WriteString(w, `<div class="rpt-card rpt-funnel-card"><div class="rpt-card-hd"><span class="rpt-card-title">Delivery funnel</span><span class="rpt-card-sub">showing drop-off at each stage</span></div>`); err != nil {
			return err
		}
		for i, row := range funnelRows {
			pct := 100
			if row.Total > 0 {
				pct = row.Count * 100 / row.Total
			}
			barClass := "rpt-funnel-bar--base"
			switch i {
			case 1:
				barClass = "rpt-funnel-bar--sent"
			case 2:
				barClass = "rpt-funnel-bar--dlv"
			case 3:
				barClass = "rpt-funnel-bar--read"
			}
			_, err = fmt.Fprintf(w,
				`<div class="rpt-funnel-row">
<span class="rpt-funnel-label">%s</span>
<div class="rpt-funnel-track"><div class="rpt-funnel-bar %s" style="width:%d%%"><span class="rpt-funnel-count">%s</span></div></div>
<span class="rpt-funnel-pct">%d%%</span>
</div>`, row.Label, barClass, pct, fmtNum(row.Count), pct)
			if err != nil {
				return err
			}
		}
		if _, err = io.WriteString(w, `</div>`); err != nil { // funnel-card
			return err
		}

		// Hourly send distribution.
		if _, err = io.WriteString(w, `<div class="rpt-card rpt-dist-card"><div class="rpt-card-hd"><span class="rpt-card-title">Send distribution</span><span class="rpt-card-sub">by hour (IST)</span></div>`); err != nil {
			return err
		}
		if len(hourly) == 0 {
			_, err = io.WriteString(w, `<div class="rpt-dist-empty">No send data yet</div>`)
		} else {
			maxCount := 0
			for _, h := range hourly {
				if h.Count > maxCount {
					maxCount = h.Count
				}
			}
			_, err = io.WriteString(w, `<div class="rpt-dist-chart">`)
			if err != nil {
				return err
			}
			for _, h := range hourly {
				heightPct := 0
				if maxCount > 0 {
					heightPct = h.Count * 100 / maxCount
				}
				label := fmt.Sprintf("%dAM", h.Hour)
				if h.Hour == 0 {
					label = "12AM"
				} else if h.Hour == 12 {
					label = "12PM"
				} else if h.Hour > 12 {
					label = fmt.Sprintf("%dPM", h.Hour-12)
				}
				_, err = fmt.Fprintf(w,
					`<div class="rpt-dist-col"><div class="rpt-dist-bar-wrap"><div class="rpt-dist-bar" style="height:%d%%"></div></div><span class="rpt-dist-lbl">%s</span></div>`,
					heightPct, label)
				if err != nil {
					return err
				}
			}
			_, err = io.WriteString(w, `</div>`)
		}
		if err != nil {
			return err
		}
		if _, err = io.WriteString(w, `<div class="rpt-dist-hint">Peak sending in first 2 hours after launch</div></div>`); err != nil {
			return err
		}

		if _, err = io.WriteString(w, `</div>`); err != nil { // rpt-left
			return err
		}

		// Right column.
		if _, err = io.WriteString(w, `<div class="rpt-right">`); err != nil {
			return err
		}

		// Campaign info card.
		audienceStr := fmt.Sprintf("%s contacts", fmtNum(report.TotalRecipients))
		_, err = fmt.Fprintf(w, `
<div class="rpt-card rpt-info-card">
<div class="rpt-card-title" style="margin-bottom:12px">Campaign info</div>
<div class="rpt-info-row"><span class="rpt-info-key">Template</span><code class="rpt-info-tmpl">%s</code></div>
<div class="rpt-info-row"><span class="rpt-info-key">Audience</span><span class="rpt-info-val">%s</span></div>
<div class="rpt-info-row"><span class="rpt-info-key">Sent</span><span class="rpt-info-val">%s</span></div>
<div class="rpt-info-row"><span class="rpt-info-key">Date</span><span class="rpt-info-val">%s</span></div>
<div class="rpt-info-row rpt-info-row--last"><span class="rpt-info-key">Cost</span><span class="rpt-info-val">&#8377;%.2f</span></div>
</div>`,
			html.EscapeString(report.TemplateName),
			audienceStr,
			fmtNum(report.SentCount),
			dateStr,
			report.CostTotalINR,
		)
		if err != nil {
			return err
		}

		// Read rate card.
		readRate := 0
		if report.SentCount > 0 {
			readRate = report.ReadCount * 100 / report.SentCount
		}
		rateLabel := "Below average"
		rateStar := false
		if readRate >= 65 {
			rateLabel = "Excellent — above 65% industry avg"
			rateStar = true
		} else if readRate >= 30 {
			rateLabel = "Average — above 30% industry avg"
		}
		starHTML := ""
		if rateStar {
			starHTML = `&#11088; `
		}
		_, err = fmt.Fprintf(w, `
<div class="rpt-card rpt-rate-card">
<div class="rpt-card-title">Read rate</div>
<div class="rpt-rate-big">%d%%</div>
<div class="rpt-rate-track"><div class="rpt-rate-fill" style="width:%d%%"></div></div>
<div class="rpt-rate-label">%s%s</div>
</div>`, readRate, readRate, starHTML, rateLabel)
		if err != nil {
			return err
		}

		// Failure breakdown (right sidebar).
		if report.FailedCount > 0 {
			failCounts := map[string]int{}
			for _, fr := range failed {
				failCounts[fr.FailCategory]++
			}
			_, err = io.WriteString(w, `<div class="rpt-card rpt-fail-card"><div class="rpt-card-title">Failure breakdown</div>`)
			if err != nil {
				return err
			}
			type failCat struct {
				key   string
				label string
				dot   string
			}
			cats := []failCat{
				{"opted_out", "Opted out", "sdot--orange"},
				{"invalid_number", "Invalid number", "sdot--gray"},
				{"limit_reached", "Limit reached", "sdot--purple"},
				{"blocked", "Blocked", "sdot--red"},
				{"rejected", "Rejected", "sdot--darkred"},
			}
			for _, cat := range cats {
				if n := failCounts[cat.key]; n > 0 {
					_, err = fmt.Fprintf(w,
						`<div class="rpt-fail-row"><span class="sdot %s"></span><span class="rpt-fail-label">%s</span><span class="rpt-fail-count">%d</span></div>`,
						cat.dot, cat.label, n)
					if err != nil {
						return err
					}
				}
			}
			if _, err = io.WriteString(w, `</div>`); err != nil {
				return err
			}
		}

		if _, err = io.WriteString(w, `</div>`); err != nil { // rpt-right
			return err
		}
		if _, err = io.WriteString(w, `</div>`); err != nil { // rpt-body
			return err
		}

		// ── Failed & undelivered contacts ────────────────────────────────────
		if len(failed) > 0 {
			// Compute counts per category.
			failCats := map[string]int{}
			for _, fr := range failed {
				failCats[fr.FailCategory]++
			}

			_, err = fmt.Fprintf(w, `
<div class="rpt-card rpt-failed-section" x-data="{cat:'all'}">
<div class="rpt-failed-hd">
<div><span class="rpt-card-title">Failed &amp; undelivered contacts</span> <span class="rpt-failed-total">%d total</span></div>
<a class="btn btn-secondary btn-sm" href="/campaigns/%s/export?type=failed">`+ExportIconSVG+`
Export list</a>
</div>`, len(failed), report.ID)
			if err != nil {
				return err
			}

			// Filter pills.
			type pill struct {
				key   string
				label string
				dot   string
			}
			pills := []pill{{"all", fmt.Sprintf("All %d", len(failed)), ""}}
			for _, k := range []struct{ key, label, dot string }{
				{"opted_out", "Opted out", "sdot--orange"},
				{"invalid_number", "Invalid number", "sdot--gray"},
				{"limit_reached", "Limit reached", "sdot--purple"},
				{"blocked", "Blocked", "sdot--red"},
				{"rejected", "Rejected", "sdot--darkred"},
			} {
				if n := failCats[k.key]; n > 0 {
					pills = append(pills, pill{k.key, fmt.Sprintf("%s %d", k.label, n), k.dot})
				}
			}

			_, err = io.WriteString(w, `<div class="rpt-failed-pills">`)
			if err != nil {
				return err
			}
			for _, p := range pills {
				dotHTML := ""
				if p.dot != "" {
					dotHTML = fmt.Sprintf(`<span class="sdot %s"></span>`, p.dot)
				}
				_, err = fmt.Fprintf(w,
					`<button class="rpt-fail-pill" :class="{active:cat===%s}" @click="cat=%s" type="button">%s%s</button>`,
					jsLit(p.key), jsLit(p.key), dotHTML, html.EscapeString(p.label))
				if err != nil {
					return err
				}
			}
			if _, err = io.WriteString(w, `</div>`); err != nil {
				return err
			}

			// Failed contacts table.
			if _, err = io.WriteString(w, `
<table class="tbl rpt-failed-tbl">
<thead><tr><th>CONTACT</th><th>PHONE NUMBER</th><th>FAILURE REASON</th><th>CATEGORY</th><th>TIME</th></tr></thead>
<tbody>`); err != nil {
				return err
			}

			catBadgeMap := map[string][2]string{
				"opted_out":      {"badge-warning", "Opted out"},
				"invalid_number": {"badge-neutral", "Invalid number"},
				"limit_reached":  {"badge-purple", "Limit reached"},
				"blocked":        {"badge-danger", "Blocked"},
				"rejected":       {"badge-danger", "Rejected"},
			}

			for _, fr := range failed {
				initials := contactInitials(fr.Name)
				avatarColor := avatarColor(fr.Name)
				timeStr := "—"
				if fr.FailedAt != nil {
					timeStr = fr.FailedAt.Format("3:04 PM")
				}
				catInfo := catBadgeMap[fr.FailCategory]
				catBadge := fmt.Sprintf(`<span class="badge %s">%s</span>`, catInfo[0], catInfo[1])
				xShow := fmt.Sprintf(`cat==='all'||cat===%s`, jsLit(fr.FailCategory))

				_, err = fmt.Fprintf(w,
					`<tr x-show="%s">
<td><div class="rpt-contact-cell"><div class="rpt-av" style="background:%s">%s</div><span>%s</span></div></td>
<td class="rpt-phone">%s</td>
<td class="rpt-fail-reason">%s</td>
<td>%s</td>
<td class="rpt-time">%s</td>
</tr>`,
					xShow,
					avatarColor, initials,
					html.EscapeString(fr.Name),
					html.EscapeString(fr.WAPhone),
					html.EscapeString(humanizeFailReason(fr.FailReason, fr.FailCategory)),
					catBadge,
					timeStr,
				)
				if err != nil {
					return err
				}
			}

			if _, err = io.WriteString(w, `</tbody></table></div>`); err != nil {
				return err
			}
		}

		_, err = io.WriteString(w, `</div>`) // page-wrap
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, ShellClose())
		return err
	})
}

// contactInitials returns up to 2 uppercase initials from a name.
func contactInitials(name string) string {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return "?"
	}
	if len(parts) == 1 {
		r := []rune(parts[0])
		if len(r) > 0 {
			return strings.ToUpper(string(r[0]))
		}
	}
	r0 := []rune(parts[0])
	r1 := []rune(parts[len(parts)-1])
	if len(r0) > 0 && len(r1) > 0 {
		return strings.ToUpper(string(r0[0]) + string(r1[0]))
	}
	return strings.ToUpper(string(r0[0]))
}

// humanizeFailReason converts a raw skip_reason to a readable message.
// Recognised Meta delivery error codes get a precise explanation.
func humanizeFailReason(reason, category string) string {
	r := strings.ToLower(reason)
	switch {
	case strings.Contains(r, "frequency cap") || strings.Contains(r, "last 24h"):
		return "Skipped — already received a marketing message in the last 24h (frequency cap)"
	case strings.Contains(r, "us (+1)") || strings.Contains(r, "+1) number"):
		return "Skipped — US (+1) number, excluded from marketing"
	case strings.Contains(r, "opted out"):
		// Send-time skip: the contact opted out (STOP) or was blocked after the
		// campaign was queued. Checked before the generic "blocked" case below,
		// which refers to the user blocking the business number on WhatsApp.
		return "Skipped — contact opted out (or was blocked) before send"
	case strings.Contains(r, "131049"):
		return "Blocked by Meta's per-user marketing limit (healthy-ecosystem cap) — try a smaller send or space out marketing messages"
	case strings.Contains(r, "131026"):
		return "Undeliverable — recipient can't receive this message (not on WhatsApp, or can't receive this template type)"
	case strings.Contains(r, "131047"):
		return "Re-engagement required — outside the 24-hour window with no template"
	case strings.Contains(r, "131048"):
		return "Spam-rate limit hit for this number"
	case strings.Contains(r, "131031") || strings.Contains(r, "blocked"):
		return "User has blocked the business number"
	case strings.Contains(r, "470") || strings.Contains(r, "re-engagement"):
		return "Message failed — outside the allowed messaging window"
	}
	switch category {
	case "opted_out":
		return "Contact opted out before send"
	case "limit_reached":
		return "Daily messaging limit reached"
	case "blocked":
		return "User has blocked the business number"
	case "invalid_number":
		if strings.Contains(r, "deactivat") || strings.Contains(r, "ported") {
			return "Number deactivated or ported"
		}
		return "Number not registered on WhatsApp"
	default:
		if reason != "" {
			return reason
		}
		return "Message rejected"
	}
}

// fmtNum formats an int with comma separators.
func fmtNum(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	s := fmt.Sprintf("%d", n)
	var b strings.Builder
	for i, r := range s {
		rem := len(s) - i
		if i > 0 && rem%3 == 0 {
			b.WriteRune(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ── Wizard helpers ────────────────────────────────────────────────────────────

var wizStepLabels = []string{"Basics", "Template", "Variables", "Audience", "Schedule", "Review"}

// wizOpen writes the wizard shell: app shell + sidebar + form open tag.
func wizOpen(w io.Writer, agent *mw.AgentClaims, state WizardState, formAction, errMsg string) error {
	if _, err := io.WriteString(w, ShellOpen(agent, "/campaigns", "New Campaign", "")); err != nil {
		return err
	}
	// Sidebar
	sidebar := `<nav class="wiz-fp-nav" aria-label="Wizard steps"><div class="wiz-fp-nav-title">New Campaign</div>`
	for i, label := range wizStepLabels {
		n := i + 1
		cls := "wiz-fp-step"
		numHTML := fmt.Sprintf(`%d`, n)
		if n < state.Step {
			cls += " wiz-fp-step--done"
			numHTML = `&#10003;`
		} else if n == state.Step {
			cls += " wiz-fp-step--active"
		}
		sidebar += fmt.Sprintf(`<div class="%s"><span class="wiz-fp-step-num">%s</span><span class="wiz-fp-step-label">%s</span></div>`, cls, numHTML, label)
	}
	sidebar += `</nav>`

	// Dots
	dots := `<div class="wiz-fp-dots" aria-hidden="true">`
	for i := range wizStepLabels {
		n := i + 1
		if n <= state.Step {
			dots += `<span class="wiz-fp-dot wiz-fp-dot--done"></span>`
		} else {
			dots += `<span class="wiz-fp-dot"></span>`
		}
	}
	dots += `</div>`

	errBanner := ""
	if errMsg != "" {
		errBanner = `<div class="callout callout--warning" role="alert" style="margin-bottom:0">` + html.EscapeString(errMsg) + `</div>`
	}

	// Only the final review form (which posts to /campaigns → Create) carries the
	// header-media file upload, so only it needs a multipart body. Earlier steps
	// use ParseForm (urlencoded) and must NOT be multipart.
	enctype := ""
	if state.HasMediaHeader && formAction == "/campaigns" {
		enctype = ` enctype="multipart/form-data"`
	}

	_, err := fmt.Fprintf(w, `
<div class="wiz-fp-root">
%s
<form id="wiz-form" method="post" action="%s"%s class="wiz-fp-body" autocomplete="off">
<input type="hidden" name="wizard_state" value="%s">
<div class="wiz-fp-inner" id="wiz-inner">
`,
		sidebar,
		html.EscapeString(formAction),
		enctype,
		html.EscapeString(encodeState(state)),
	)
	if err != nil {
		return err
	}
	if errBanner != "" {
		if _, err := fmt.Fprintf(w, `<div style="grid-column:1/-1;padding:16px 32px 0">%s</div>`, errBanner); err != nil {
			return err
		}
	}
	return nil
}

// wizClose writes the footer nav bar and closes all tags.
// disabledExpr is an Alpine expression for :disabled (empty = always enabled).
// onclickExpr makes the continue button type=button with the given onclick
// instead of a form submit (used for the review step's modal trigger).
func wizClose(w io.Writer, state WizardState, backLabel, continueLabel, disabledExpr, onclickExpr string) error {
	dots := `<div class="wiz-fp-dots" aria-hidden="true">`
	for i := range wizStepLabels {
		n := i + 1
		if n <= state.Step {
			dots += `<span class="wiz-fp-dot wiz-fp-dot--done"></span>`
		} else {
			dots += `<span class="wiz-fp-dot"></span>`
		}
	}
	dots += `</div>`

	backBtn := ""
	if state.Step == 1 {
		backBtn = `<a class="btn btn-secondary" href="/campaigns">Cancel</a>`
	} else {
		backBtn = `<button class="btn btn-secondary" type="submit" name="action" value="back">&#8592; ` + html.EscapeString(backLabel) + `</button>`
	}

	var continueBtn string
	if onclickExpr != "" {
		attr := ""
		if disabledExpr != "" {
			attr = ` :disabled="` + disabledExpr + `"`
		}
		continueBtn = fmt.Sprintf(`<button class="btn btn-primary" type="button" onclick="%s"%s>%s</button>`,
			html.EscapeString(onclickExpr), attr, html.EscapeString(continueLabel))
	} else {
		attr := ""
		if disabledExpr != "" {
			attr = ` :disabled="` + disabledExpr + `"`
		}
		continueBtn = fmt.Sprintf(`<button class="btn btn-primary" type="submit" name="action" value="next"%s>%s</button>`,
			attr, html.EscapeString(continueLabel))
	}

	_, err := fmt.Fprintf(w, `
</div><!-- wiz-fp-inner -->
<div class="wiz-fp-footer">
%s
%s
%s
</div>
</form>
</div><!-- wiz-fp-root -->
`,
		backBtn, dots, continueBtn,
	)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, ShellClose())
	return err
}

// ── Wizard step 1: Basics ─────────────────────────────────────────────────────

func WizardBasicsPage(agent *mw.AgentClaims, state WizardState, rates db.ConfigRates, errMsg string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := wizOpen(w, agent, state, "/campaigns/wizard/basics", errMsg); err != nil {
			return err
		}

		mktRate := fmtRate(rates.Marketing, rates.GSTRate)
		utlRate := fmtRate(rates.Utility, rates.GSTRate)
		authRate := fmtRate(rates.Auth, rates.GSTRate)

		mktSel := selAttr(state.Category, "marketing")
		utlSel := selAttr(state.Category, "utility")
		authSel := selAttr(state.Category, "authentication")

		_, err := fmt.Fprintf(w, `
<div class="wiz-fp-form wiz-fp-form--full">
<div class="wiz-fp-hd">
  <h2>Campaign basics</h2>
  <p>Name your campaign and set its type.</p>
</div>

<div class="field">
  <label for="cmp-name">Campaign name <span style="color:var(--danger)">*</span></label>
  <input id="cmp-name" type="text" name="name" value="%s" required placeholder="e.g. Diwali Sale 2026" autofocus>
</div>

<div>
  <label class="form-label" style="margin-bottom:10px;display:block">Campaign type</label>
  <div class="ctype-cards">
    <label class="ctype-card">
      <input class="ctype-card-radio" type="radio" name="category" value="marketing"%s>
      <div class="ctype-card-body">
        <div class="ctype-card-name">Marketing</div>
        <div class="ctype-card-desc">Promotions, offers, announcements</div>
      </div>
      <span class="ctype-card-price">%s</span>
    </label>
    <label class="ctype-card">
      <input class="ctype-card-radio" type="radio" name="category" value="utility"%s>
      <div class="ctype-card-body">
        <div class="ctype-card-name">Utility</div>
        <div class="ctype-card-desc">Order updates, reminders, alerts</div>
      </div>
      <span class="ctype-card-price">%s</span>
    </label>
    <label class="ctype-card">
      <input class="ctype-card-radio" type="radio" name="category" value="authentication"%s>
      <div class="ctype-card-body">
        <div class="ctype-card-name">Authentication</div>
        <div class="ctype-card-desc">OTPs and verification codes</div>
      </div>
      <span class="ctype-card-price">%s</span>
    </label>
  </div>
</div>

<div class="field">
  <label for="cmp-notes">Internal notes <span style="color:var(--text-secondary);font-weight:400">(optional)</span></label>
  <textarea id="cmp-notes" name="notes" rows="3" placeholder="Why this campaign, target goal, internal reference&#8230;">%s</textarea>
</div>
</div>
`,
			html.EscapeString(state.Name),
			mktSel, mktRate,
			utlSel, utlRate,
			authSel, authRate,
			html.EscapeString(state.Notes),
		)
		if err != nil {
			return err
		}

		return wizClose(w, state, "Back", "Continue →", "", "")
	})
}

// ── Wizard step 2: Template ───────────────────────────────────────────────────

type wizTmplJS struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Category string     `json:"category"`
	Status   string     `json:"status"`
	Body     string     `json:"body"`
	Footer   string     `json:"footer"`
	VarCount int        `json:"varCount"`
	BtnCount int        `json:"btnCount"`
	Buttons  []wizBtnJS `json:"buttons"`
}
type wizBtnJS struct {
	Text string `json:"text"`
	Type string `json:"type"`
}

func WizardTemplPage(agent *mw.AgentClaims, state WizardState, tmpls []db.Template, errMsg string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := wizOpen(w, agent, state, "/campaigns/wizard/template", errMsg); err != nil {
			return err
		}

		// Build JS data for Alpine template picker.
		var jsData []wizTmplJS
		for _, t := range tmpls {
			body := TemplateBodyText(t)
			varCount := len(ExtractVarNames(body))
			var footer string
			var btns []wizBtnJS
			for _, comp := range t.Components {
				switch fmt.Sprintf("%v", comp["type"]) {
				case "FOOTER":
					footer, _ = comp["text"].(string)
				case "BUTTONS":
					if bArr, ok := comp["buttons"].([]any); ok {
						for _, b := range bArr {
							if bm, ok := b.(map[string]any); ok {
								btns = append(btns, wizBtnJS{
									Text: fmt.Sprintf("%v", bm["text"]),
									Type: fmt.Sprintf("%v", bm["type"]),
								})
							}
						}
					}
				}
			}
			if btns == nil {
				btns = []wizBtnJS{}
			}
			jsData = append(jsData, wizTmplJS{
				ID:       t.ID,
				Name:     t.Name,
				Category: t.Category,
				Status:   t.Status,
				Body:     body,
				Footer:   footer,
				VarCount: varCount,
				BtnCount: len(btns),
				Buttons:  btns,
			})
		}
		jsonBytes, _ := json.Marshal(jsData)

		// Default filter tab = category chosen in basics.
		initCat := state.Category
		if initCat == "" {
			initCat = "all"
		}

		xdata := `{q:'',cat:` + jsLit(initCat) + `,sel:` + jsLit(state.TemplateID) + `,tl:` + string(jsonBytes) + `,` +
			`get ft(){return this.tl.filter(t=>(this.cat==='all'||t.category===this.cat)&&(!this.q||t.name.toLowerCase().includes(this.q.toLowerCase())||t.body.toLowerCase().includes(this.q.toLowerCase())))},` +
			`get st(){return this.tl.find(t=>t.id===this.sel)||null}}`

		_, err := fmt.Fprintf(w, `
<div class="wiz-fp-form" x-data="%s">
<div class="wiz-fp-hd">
  <h2>Select a template</h2>
  <p>Choose an approved WhatsApp message template.</p>
</div>

<div class="tmpl-pick-search">
  <input type="text" x-model="q" placeholder="Search templates&#8230;" class="form-input" style="width:100%%">
</div>
<div class="tmpl-pick-tabs">
  <button type="button" class="tmpl-pick-tab" :class="{active:cat==='all'}" @click="cat='all'">All</button>
  <button type="button" class="tmpl-pick-tab" :class="{active:cat==='marketing'}" @click="cat='marketing'">Marketing</button>
  <button type="button" class="tmpl-pick-tab" :class="{active:cat==='utility'}" @click="cat='utility'">Utility</button>
  <button type="button" class="tmpl-pick-tab" :class="{active:cat==='authentication'}" @click="cat='authentication'">Authentication</button>
</div>

<div class="tmpl-pick-list">
  <template x-if="ft.length===0">
    <div style="padding:24px 0;text-align:center;color:var(--text-secondary);font-size:14px">No templates match your filter.</div>
  </template>
  <template x-for="t in ft" :key="t.id">
    <label class="tmpl-pick-item" :class="{selected:sel===t.id}">
      <input type="radio" name="template_id" :value="t.id" x-model="sel" style="accent-color:var(--accent);margin-top:3px;flex-shrink:0">
      <div class="tmpl-pick-right">
        <div class="tmpl-pick-name" x-text="t.name"></div>
        <div class="tmpl-pick-body" x-text="t.body.replace(/\{\{(\d+)\}\}/g,'[...]')"></div>
        <div class="tmpl-pick-meta">
          <code x-text="t.name" style="font-size:11px;color:var(--text-secondary)"></code>
          <span x-show="t.varCount>0" x-text="t.varCount+' variable'+(t.varCount>1?'s':'')"></span>
        </div>
      </div>
    </label>
  </template>
</div>
</div>

<div class="wiz-fp-aside">
  <div x-show="!st" class="wiz-aside-placeholder">
    <div class="wiz-aside-placeholder-icon">&#128172;</div>
    <span>Select a template to preview</span>
  </div>
  <template x-if="st">
    <div>
      <div class="aside-label">PREVIEW</div>
      <div class="wiz-wa-wrap">
        <div class="wiz-wa-hd">
          <div class="wiz-wa-av">B</div>
          <div>
            <div class="wiz-wa-biz">Your Business</div>
            <div class="wiz-wa-sub">Business Account</div>
          </div>
        </div>
        <div class="wiz-wa-body">
          <div class="wiz-wa-bub" x-text="st.body.replace(/\{\{(\d+)\}\}/g,'[...]')"></div>
          <div x-show="st.footer" class="wiz-wa-footer" x-text="st.footer"></div>
          <div class="wiz-wa-ts">10:24 AM &#10003;&#10003;</div>
        </div>
        <template x-if="st.buttons.length">
          <div class="wiz-wa-btns">
            <template x-for="btn in st.buttons" :key="btn.text">
              <div class="wiz-wa-btn" x-text="btn.text"></div>
            </template>
          </div>
        </template>
      </div>
      <div class="tmpl-info-card" style="margin-top:12px">
        <div class="tmpl-info-label">TEMPLATE INFO</div>
        <div class="tmpl-info-row"><span class="tmpl-info-key">Category</span><span class="tmpl-info-val" x-text="st.category.charAt(0).toUpperCase()+st.category.slice(1)"></span></div>
        <div class="tmpl-info-row"><span class="tmpl-info-key">Variables</span><span class="tmpl-info-val" x-text="st.varCount"></span></div>
        <div class="tmpl-info-row"><span class="tmpl-info-key">Buttons</span><span class="tmpl-info-val" x-text="st.btnCount"></span></div>
      </div>
    </div>
  </template>
</div>
`,
			html.EscapeString(xdata),
		)
		if err != nil {
			return err
		}

		return wizClose(w, state, "Back", "Continue →", "!sel", "")
	})
}

// ── Wizard step 3: Variables ──────────────────────────────────────────────────

// varFieldOption is one selectable contact/agency field for a template variable.
type varFieldOption struct{ Value, Label string }

// varFieldGroups lists the fields offered for template-variable mapping. Agency
// fields resolve to custom_fields.<key>; the "__custom__" option lets the user
// type any custom field key of their own.
var varFieldGroups = []struct {
	Label   string
	Options []varFieldOption
}{
	{"Contact", []varFieldOption{
		{"first_name", "First name"},
		{"name", "Full name"},
		{"wa_phone", "WhatsApp phone"},
		{"email", "Email"},
		{"industry", "Industry / Vertical"},
		{"custom_fields.city", "City"},
	}},
	{"Agency fields", []varFieldOption{
		{"custom_fields.brand", "Brand / Company"},
		{"custom_fields.designation", "Designation"},
		{"custom_fields.service", "Service / Package"},
		{"custom_fields.account_manager", "Account Manager"},
		{"custom_fields.campaign", "Campaign name"},
		{"custom_fields.platform", "Platform"},
		{"custom_fields.ad_budget", "Ad budget"},
		{"custom_fields.reporting_month", "Reporting month"},
		{"custom_fields.invoice_amount", "Invoice amount"},
		{"custom_fields.due_date", "Due date"},
		{"custom_fields.payment_link", "Payment link"},
	}},
}

func knownVarField(value string) bool {
	for _, g := range varFieldGroups {
		for _, o := range g.Options {
			if o.Value == value {
				return true
			}
		}
	}
	return false
}

// varFieldSelect renders the grouped field <select> for variable index n, plus a
// "Custom field…" text input shown (via Alpine) when the custom option is chosen.
func varFieldSelect(n, existingVar string) string {
	selected := "first_name"
	customKey := ""
	isCustom := false
	if existingVar != "" {
		if knownVarField(existingVar) {
			selected = existingVar
		} else {
			isCustom = true
			selected = "__custom__"
			customKey = strings.TrimPrefix(existingVar, "custom_fields.")
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<div x-data="{ f: %s }">`, jsLit(selected))
	fmt.Fprintf(&b, `<select name="var_%s" class="form-input" style="font-size:13px" x-model="f">`, html.EscapeString(n))
	for _, g := range varFieldGroups {
		fmt.Fprintf(&b, `<optgroup label="%s">`, html.EscapeString(g.Label))
		for _, o := range g.Options {
			sel := ""
			if o.Value == selected {
				sel = " selected"
			}
			fmt.Fprintf(&b, `<option value="%s"%s>%s</option>`, html.EscapeString(o.Value), sel, html.EscapeString(o.Label))
		}
		b.WriteString(`</optgroup>`)
	}
	customSel := ""
	if isCustom {
		customSel = " selected"
	}
	fmt.Fprintf(&b, `<option value="__custom__"%s>Custom field…</option></select>`, customSel)
	fmt.Fprintf(&b,
		`<input type="text" name="customkey_%s" class="form-input" style="margin-top:6px;font-size:13px" `+
			`placeholder="your field key, e.g. brand" value="%s" x-show="f==='__custom__'" x-cloak>`,
		html.EscapeString(n), html.EscapeString(customKey))
	b.WriteString(`</div>`)
	return b.String()
}

func WizardVarsPage(agent *mw.AgentClaims, state WizardState, tmpl db.Template, varNames []string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := wizOpen(w, agent, state, "/campaigns/wizard/vars", ""); err != nil {
			return err
		}

		body := TemplateBodyText(tmpl)

		// Build Alpine x-data for the preview.
		varsInit := "{"
		for i, n := range varNames {
			if i > 0 {
				varsInit += ","
			}
			existing := ""
			if state.Fallbacks != nil {
				existing = state.Fallbacks[n]
			}
			varsInit += jsLit(n) + ":" + jsLit(existing)
		}
		varsInit += "}"

		xdata := `{vars:` + varsInit + `,body:` + jsLit(body) + `,` +
			`get filled(){return Object.values(this.vars).filter(v=>v.trim()).length},` +
			`get total(){return Object.keys(this.vars).length},` +
			`get preview(){return this.body.replace(/\{\{(\d+)\}\}/g,(_,n)=>this.vars[n]?'['+this.vars[n]+']':'[...]')}` +
			`}`

		_, err := fmt.Fprintf(w, `
<div class="wiz-fp-form" x-data="%s">
<div class="wiz-fp-hd">
  <h2>Fill in variables</h2>
  <p>Set default values for your template placeholders. These will be personalised per contact at send time.</p>
</div>

<div class="wiz-var-tmpl-badge" style="display:flex;align-items:center;gap:8px;padding:10px 14px;background:var(--bg-surface);border:1px solid var(--border);border-radius:var(--radius);font-size:13px">
  <svg width="14" height="14" viewBox="0 0 16 16" fill="none" style="flex-shrink:0"><rect x="1" y="1" width="14" height="14" rx="3" stroke="currentColor" stroke-width="1.5"/><path d="M4 5h8M4 8h6M4 11h4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>
  <strong>%s</strong>
  <code style="color:var(--text-secondary);font-size:11px;margin-left:auto">%s</code>
</div>

<div class="wiz-var-rows">
`,
			html.EscapeString(xdata),
			html.EscapeString(tmpl.Name),
			html.EscapeString(tmpl.Name),
		)
		if err != nil {
			return err
		}

		for _, n := range varNames {
			existingVar := ""
			existingFallback := ""
			if state.VarMap != nil {
				existingVar = state.VarMap[n]
			}
			if state.Fallbacks != nil {
				existingFallback = state.Fallbacks[n]
			}

			sampleVal := "Customer"
			if n == "2" {
				sampleVal = "Your Business"
			} else if n == "3" {
				sampleVal = "tomorrow"
			}

			if _, err := fmt.Fprintf(w, `
<div class="wiz-var-row">
  <div class="wiz-var-label">
    <span class="wiz-var-num">%s</span>
    <span class="wiz-var-heading">Variable %s</span>
  </div>
  <div class="field" style="margin:0">
    <label style="font-size:12px;color:var(--text-secondary)">Contact field to use</label>
    %s
  </div>
  <div class="field" style="margin:0">
    <label style="font-size:12px;color:var(--text-secondary)">Fallback (used when field is empty)</label>
    <div class="wiz-var-input-row">
      <input type="text" name="fallback_%s" class="form-input"
        placeholder="e.g. %s"
        value="%s"
        required
        @input="vars[%s]=$event.target.value">
      <button type="button" class="btn btn-sm btn-secondary"
        @click="$el.previousElementSibling.value=%s;vars[%s]=%s">Use sample</button>
    </div>
  </div>
  <div class="wiz-var-mapsto">Maps to <code>{{%s}}</code> in the template</div>
</div>`,
				n, n,
				varFieldSelect(n, existingVar),
				n,
				html.EscapeString(sampleVal),
				html.EscapeString(existingFallback),
				jsLit(n),
				jsLit(sampleVal), jsLit(n), jsLit(sampleVal),
				n,
			); err != nil {
				return err
			}
		}

		_, err = fmt.Fprintf(w, `
</div>

<div class="callout callout--info" style="font-size:13px">
  &#9432; These are fallback values. If a contact has a matching field in their profile, that value will be used instead.
</div>
</div>

<div class="wiz-fp-aside">
  <div class="aside-label">PREVIEW</div>
  <div class="wiz-wa-wrap">
    <div class="wiz-wa-hd">
      <div class="wiz-wa-av">B</div>
      <div>
        <div class="wiz-wa-biz">Your Business</div>
        <div class="wiz-wa-sub">Business Account</div>
      </div>
    </div>
    <div class="wiz-wa-body">
      <div class="wiz-wa-bub" x-text="preview"></div>
      <div class="wiz-wa-ts">10:24 AM &#10003;&#10003;</div>
    </div>
  </div>
  <div class="fill-progress-card">
    <div class="fill-progress-label">FILL PROGRESS</div>
    <div class="fill-progress-track">
      <div class="fill-progress-fill" :style="'width:'+(total?Math.round(filled/total*100):0)+'%%'"></div>
    </div>
    <div class="fill-progress-count" x-text="filled+'/'+total"></div>
  </div>
</div>
`)
		if err != nil {
			return err
		}

		return wizClose(w, state, "Back", "Continue →", "", "")
	})
}

// ── Wizard step 4: Audience ───────────────────────────────────────────────────

func WizardAudiencePage(agent *mw.AgentClaims, state WizardState, tags []db.TagWithCount, totalOptedIn int, errMsg string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := wizOpen(w, agent, state, "/campaigns/wizard/audience", errMsg); err != nil {
			return err
		}

		// Build Alpine tagCounts map and initial tagSel map.
		tagCountsJS := "{"
		tagSelJS := "{"
		selTagSet := map[int64]bool{}
		for _, id := range state.SegmentTagIDs {
			selTagSet[id] = true
		}
		for i, tc := range tags {
			if i > 0 {
				tagCountsJS += ","
				tagSelJS += ","
			}
			key := jsLit(strconv.FormatInt(tc.ID, 10))
			tagCountsJS += key + ":" + strconv.Itoa(tc.OptedInCount)
			tagSelJS += key + ":" + strconv.FormatBool(selTagSet[tc.ID])
		}
		tagCountsJS += "}"
		tagSelJS += "}"

		allSel := "false"
		if state.UseAllContacts {
			allSel = "true"
		}

		xdata := `{tagCounts:` + tagCountsJS + `,tagSel:` + tagSelJS + `,allSel:` + allSel + `,` +
			`get reach(){if(this.allSel)return ` + strconv.Itoa(totalOptedIn) + `;` +
			`let s=0;for(const[k,v]of Object.entries(this.tagSel)){if(v)s+=this.tagCounts[k]||0}return s}` +
			`}`

		tierNote := ""
		if state.DailyCap > 0 {
			tierNote = fmt.Sprintf(`<div class="tier-info">&#9432; Tier limit: <strong>%d</strong> msgs/day. This campaign will queue if it exceeds your daily limit.</div>`, state.DailyCap)
		}

		_, err := fmt.Fprintf(w, `
<div class="wiz-fp-form" x-data="%s">
<div class="wiz-fp-hd">
  <h2>Select audience</h2>
  <p>Choose which contact groups will receive this campaign.</p>
</div>

<div class="seg-list">
  <label class="seg-item">
    <input type="checkbox" name="use_all_contacts" value="1" x-model="allSel" @change="if(allSel){tagSel=Object.fromEntries(Object.keys(tagSel).map(k=>[k,false]))}">
    <div class="seg-item-body">
      <div class="seg-item-name">All contacts</div>
      <div class="seg-item-desc">All opted-in contacts in your directory</div>
    </div>
    <div class="seg-item-count">%d<span>contacts</span></div>
  </label>
`,
			html.EscapeString(xdata),
			totalOptedIn,
		)
		if err != nil {
			return err
		}

		for _, tc := range tags {
			idStr := strconv.FormatInt(tc.ID, 10)
			if _, err := fmt.Fprintf(w, `
  <label class="seg-item" :class="{'seg-item--selected':tagSel[%s]}">
    <input type="checkbox" name="segment_tag_ids" value="%s"
      x-model="tagSel[%s]"
      @change="if(tagSel[%s])allSel=false">
    <div class="seg-item-body">
      <div class="seg-item-name">%s</div>
      <div class="seg-item-desc">Contacts tagged %s</div>
    </div>
    <div class="seg-item-count" x-text="tagCounts[%s]"><span>contacts</span></div>
  </label>`,
				jsLit(idStr), idStr,
				jsLit(idStr),
				jsLit(idStr),
				html.EscapeString(tc.Name),
				html.EscapeString(tc.Name),
				jsLit(idStr),
			); err != nil {
				return err
			}
		}

		_, err = fmt.Fprintf(w, `
</div>
</div>

<div class="wiz-fp-aside">
  <div class="reach-summary">
    <div class="reach-summary-title">Reach summary</div>
    <div class="reach-count" x-text="reach">0</div>
    <div class="reach-label">contacts selected</div>
  </div>
  %s
</div>
`, tierNote)
		if err != nil {
			return err
		}

		return wizClose(w, state, "Back", "Continue →", "reach===0", "")
	})
}

// ── Wizard step 5: Schedule ───────────────────────────────────────────────────

func WizardSchedulePage(agent *mw.AgentClaims, state WizardState, errMsg string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := wizOpen(w, agent, state, "/campaigns/wizard/schedule", errMsg); err != nil {
			return err
		}

		initSched := "now"
		if state.ScheduleType == "scheduled" {
			initSched = "scheduled"
		}

		_, err := fmt.Fprintf(w, `
<div class="wiz-fp-form"
  x-data="schedWiz()"
  @submit.prevent="if(!inQH)$el.closest('form').submit()">
<div class="wiz-fp-hd">
  <h2>Schedule</h2>
  <p>Choose when to send this campaign.</p>
</div>

<div class="field">
  <div class="ctype-cards">
    <label class="ctype-card">
      <input class="ctype-card-radio" type="radio" name="schedule_type" value="now" x-model="sched">
      <div class="ctype-card-body">
        <div class="ctype-card-name">Send now</div>
        <div class="ctype-card-desc">Queued immediately after you confirm</div>
      </div>
    </label>
    <label class="ctype-card">
      <input class="ctype-card-radio" type="radio" name="schedule_type" value="scheduled" x-model="sched">
      <div class="ctype-card-body">
        <div class="ctype-card-name">Schedule for later</div>
        <div class="ctype-card-desc">Pick a date and time (IST, 9 am–9 pm)</div>
      </div>
    </label>
  </div>
</div>

<div class="field" x-show="sched==='scheduled'" x-cloak>
  <label for="sched-at">Date &amp; time (IST)</label>
  <input id="sched-at" type="datetime-local" name="scheduled_at" x-model="schedAt" class="form-input">
</div>

<div class="callout callout--warning" x-show="inQH" x-cloak role="alert">
  &#9888; Quiet hours: 9&nbsp;pm–9&nbsp;am IST. Choose a time between 9&nbsp;am and 9&nbsp;pm.
</div>

<div class="wiz-sched-summary" style="padding:16px;background:var(--bg-surface);border:1px solid var(--border);border-radius:var(--radius);font-size:13px">
  <div style="display:flex;justify-content:space-between;margin-bottom:6px">
    <span style="color:var(--text-secondary)">Eligible recipients</span>
    <strong>%d</strong>
  </div>
  <div style="display:flex;justify-content:space-between">
    <span style="color:var(--text-secondary)">Estimated cost</span>
    <strong>&#8377;%.2f</strong>
  </div>
</div>
</div>

<div class="wiz-fp-aside">
  <div class="aside-label">SEND SUMMARY</div>
  <div style="display:flex;flex-direction:column;gap:12px;font-size:13px">
    <div style="display:flex;justify-content:space-between"><span style="color:var(--text-secondary)">Campaign</span><strong>%s</strong></div>
    <div style="display:flex;justify-content:space-between"><span style="color:var(--text-secondary)">Recipients</span><strong>%d</strong></div>
    <div style="display:flex;justify-content:space-between"><span style="color:var(--text-secondary)">Category</span><strong>%s</strong></div>
  </div>
</div>

<script>
function schedWiz() {
  return {
    sched: %s,
    schedAt: '',
    get inQH() {
      if (this.sched !== 'scheduled' || !this.schedAt) return false;
      var parts = (this.schedAt.split('T')[1] || '').split(':');
      var h = parseInt(parts[0], 10), m = parseInt(parts[1] || '0', 10);
      var total = h * 60 + m;
      return total < 9 * 60 || total >= 21 * 60;
    }
  };
}
</script>
`,
			state.EligibleCount,
			state.EstCost,
			html.EscapeString(state.Name),
			state.EligibleCount,
			html.EscapeString(state.Category),
			jsLit(initSched),
		)
		if err != nil {
			return err
		}

		return wizClose(w, state, "Back", "Continue →", "inQH", "")
	})
}

// ── Wizard step 6: Review ─────────────────────────────────────────────────────

func WizardReviewPage(agent *mw.AgentClaims, state WizardState, tmpl *db.Template, errMsg string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := wizOpen(w, agent, state, "/campaigns", errMsg); err != nil {
			return err
		}

		// Header-media upload card (image/video/document templates only). The real
		// media must be supplied at send time — the approved sample only got the
		// template approved.
		mediaUpload := ""
		if state.HasMediaHeader {
			accept := "image/*"
			switch state.MediaHeaderFormat {
			case "video":
				accept = "video/*"
			case "document":
				accept = ".pdf,.doc,.docx,.xls,.xlsx,.ppt,.pptx,application/pdf"
			}
			mediaUpload = fmt.Sprintf(`
<div class="card-static" style="display:flex;flex-direction:column;gap:8px;margin-top:16px;border:1px solid var(--accent,#2563eb)">
  <label for="header_media" style="font-weight:600">Header %s <span style="color:var(--danger,#dc2626)">*</span></label>
  <p style="font-size:13px;color:var(--text-secondary);margin:0">
    This template has a %s header. Upload the %s to send with every message in this campaign.
  </p>
  <input type="file" id="header_media" name="header_media" accept="%s" required
         style="font-size:13px;padding:8px;border:1px dashed var(--border);border-radius:var(--radius);background:var(--bg-surface)">
</div>`,
				html.EscapeString(state.MediaHeaderFormat),
				html.EscapeString(state.MediaHeaderFormat),
				html.EscapeString(state.MediaHeaderFormat),
				accept,
			)
		}

		schedInfo := "Send immediately"
		if state.ScheduleType == "scheduled" && state.ScheduledAt != nil {
			schedInfo = "Scheduled for " + state.ScheduledAt.Format("02 Jan 2006 15:04 IST")
		}
		tmplName := ""
		tmplCategory := state.Category
		if tmpl != nil {
			tmplName = tmpl.Name
			tmplCategory = tmpl.Category
		}

		// Tier banner
		tierBanner := ""
		if state.DailyCap > 0 {
			projected := state.DailySent + state.EligibleCount
			remaining := state.DailyCap - state.DailySent
			if remaining < 0 {
				remaining = 0
			}
			pct := projected * 100 / state.DailyCap
			switch {
			case projected >= state.DailyCap:
				tierBanner = CalloutHTML("danger", fmt.Sprintf(
					"This campaign (%d messages) would exceed your daily cap of %d — it will be blocked at launch.",
					state.EligibleCount, state.DailyCap))
			case pct >= 70:
				tierBanner = CalloutHTML("warning", fmt.Sprintf(
					"Adding %d messages brings today's projected total to %d%% of your %d daily cap (%d sent so far).",
					state.EligibleCount, pct, state.DailyCap, state.DailySent))
			default:
				tierBanner = CalloutHTML("info", fmt.Sprintf(
					"Daily cap: %d sent · %d remaining · this campaign: %d messages.",
					state.DailySent, remaining, state.EligibleCount))
			}
		}

		// Confirm dialog
		confirmInner := fmt.Sprintf(
			`<h2 id="confirm-launch-title" style="margin:0 0 16px;font-size:17px">Confirm campaign launch</h2>`+
				`<dl class="confirm-dl">`+
				`<dt>Campaign</dt><dd>%s</dd>`+
				`<dt>Recipients</dt><dd>%d contacts</dd>`+
				`<dt>Category</dt><dd>%s</dd>`+
				`<dt>Est. cost</dt><dd>&#8377;%.2f (incl. 18%% GST)</dd>`+
				`</dl>`+
				`<p style="font-size:13px;color:var(--text-secondary);margin:12px 0 0">%d messages will be queued. This cannot be undone.</p>`+
				`<div class="confirm-btns">`+
				`<button type="button" class="btn btn-secondary" onclick="document.getElementById('confirm-launch').close()">Cancel</button>`+
				`<button type="submit" form="wiz-form" name="action" value="next" class="btn btn-primary">Confirm &amp; send to %d contacts</button>`+
				`</div>`,
			html.EscapeString(state.Name),
			state.EligibleCount,
			html.EscapeString(tmplCategory),
			state.EstCost,
			state.EligibleCount,
			state.EligibleCount,
		)

		_, err := fmt.Fprintf(w, `
<div class="wiz-fp-form">
<div class="wiz-fp-hd">
  <h2>Review &amp; launch</h2>
  <p>Check the details before sending.</p>
</div>
%s
<div class="card-static" style="display:flex;flex-direction:column">
  <div class="stt-info-row"><span class="stt-info-key">Campaign name</span><span class="stt-info-val">%s</span></div>
  <div class="stt-info-row"><span class="stt-info-key">Template</span><span class="stt-info-val">%s</span></div>
  <div class="stt-info-row"><span class="stt-info-key">Category</span><span class="stt-info-val">%s</span></div>
  <div class="stt-info-row"><span class="stt-info-key">Recipients</span><span class="stt-info-val">%d eligible</span></div>
  <div class="stt-info-row"><span class="stt-info-key">Scheduling</span><span class="stt-info-val">%s</span></div>
  <div class="stt-info-row" style="border-bottom:none"><span class="stt-info-key">Estimated cost</span><span class="stt-info-val">&#8377;%.2f</span></div>
</div>
%s
</div>

<div class="wiz-fp-aside">
  <div class="aside-label">LAUNCH</div>
  <p style="font-size:13px;color:var(--text-secondary);line-height:1.5">
    Clicking <strong>Launch campaign</strong> will open a confirmation dialog before anything is sent.
  </p>
  %s
</div>
%s
`,
			tierBanner,
			html.EscapeString(state.Name),
			html.EscapeString(tmplName),
			html.EscapeString(tmplCategory),
			state.EligibleCount,
			html.EscapeString(schedInfo),
			state.EstCost,
			mediaUpload,
			CalloutHTML("info", fmt.Sprintf("%.0f messages will be sent to opted-in contacts.", float64(state.EligibleCount))),
			ModalShellRaw("confirm-launch", "confirm-dialog", "confirm-launch-title", confirmInner),
		)
		if err != nil {
			return err
		}

		return wizClose(w, state, "Back", "Launch campaign →", "", "openModal('confirm-launch',this)")
	})
}

// ── Misc helpers ──────────────────────────────────────────────────────────────

func fmtRate(base, gst float64) string {
	if base == 0 {
		return "&#8212;"
	}
	return fmt.Sprintf("&#8377;%.4f/msg", base*(1+gst))
}

func selAttr(current, value string) string {
	if current == value {
		return " checked"
	}
	return ""
}
