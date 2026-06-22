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
// form field. Serialised as base64-encoded JSON by encodeState/DecodeState.
type WizardState struct {
	Step          int
	Name          string
	SegmentTags   []int64
	ExcludeTags   []int64
	EligibleCount int
	SkipReport    campaigns.SkipReport
	TemplateID    string
	VarMap        map[string]string
	Fallbacks     map[string]string
	ScheduleType  string
	ScheduledAt   *time.Time
	EstCost       float64
	// Tier-cap snapshot captured at the audience step and carried through the wizard.
	DailyCap  int // 90%-of-tier daily cap (from app_config at audience-step time)
	DailySent int // messages already sent today (at audience-step time)
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

func CampaignsPage(agent *mw.AgentClaims, cs []db.Campaign) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/campaigns", "Campaigns", "")); err != nil {
			return err
		}

		// Count by status
		counts := map[string]int{
			"all":       len(cs),
			"running":   0,
			"draft":     0,
			"paused":    0,
			"failed":    0,
			"completed": 0,
			"cancelled": 0,
			"scheduled": 0,
		}
		for _, c := range cs {
			counts[c.Status]++
		}
		// "failed" tab = campaigns where FailedCount > 0 and SentCount == 0
		failedTabCount := 0
		for _, c := range cs {
			if c.FailedCount > 0 && c.SentCount == 0 {
				failedTabCount++
			}
		}
		counts["failed"] = failedTabCount

		_, err := fmt.Fprintf(w, `
<div class="page-wrap">
<div class="page-hd" style="display:flex;align-items:flex-start;justify-content:space-between;margin-bottom:var(--gutter);flex-wrap:wrap;gap:var(--gutter)">
  <div>
    <h1 class="screen-title" style="margin:0 0 4px">Campaigns</h1>
    <div style="color:var(--text-secondary);font-size:14px">Broadcast to opted-in contacts with approved templates</div>
  </div>
  <a class="btn btn-primary" href="/campaigns/new" style="font-size:15px;padding:10px 22px;gap:6px">+ New campaign</a>
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
			// Tab strip + campaign table
			_, err = fmt.Fprintf(w, `
<div x-data="{tab:''}">
<div class="cmp-tabs" style="margin-bottom:var(--gutter)" role="tablist">
  <button class="cmp-tab" role="tab" :class="{active:tab===''}" @click="tab=''">All <span class="cmp-tab-count">%d</span></button>
  <button class="cmp-tab" role="tab" :class="{active:tab==='running'}" @click="tab='running'">Active <span class="cmp-tab-count">%d</span></button>
  <button class="cmp-tab" role="tab" :class="{active:tab==='draft'}" @click="tab='draft'">Draft <span class="cmp-tab-count">%d</span></button>
  <button class="cmp-tab" role="tab" :class="{active:tab==='paused'}" @click="tab='paused'">Paused <span class="cmp-tab-count">%d</span></button>
  <button class="cmp-tab cmp-tab--danger" role="tab" :class="{active:tab==='failed'}" @click="tab='failed'">Failed <span class="cmp-tab-count">%d</span></button>
  <button class="cmp-tab" role="tab" :class="{active:tab==='completed'}" @click="tab='completed'">Completed <span class="cmp-tab-count">%d</span></button>
  <button class="cmp-tab" role="tab" :class="{active:tab==='cancelled'}" @click="tab='cancelled'">Cancelled <span class="cmp-tab-count">%d</span></button>
</div>
<div class="card-static" role="tabpanel" tabindex="0">
<table class="tbl">
<thead><tr>
  <th class="cmp-chev-cell"></th>
  <th>CAMPAIGN</th><th>TEMPLATE</th><th>AUDIENCE</th>
  <th>SENT</th><th>DELIVERED</th><th>FAILED</th>
  <th>STATUS</th><th>COST</th><th>DATE</th>
</tr></thead>
<tbody>`,
				counts["all"], counts["running"], counts["draft"],
				counts["paused"], counts["failed"], counts["completed"], counts["cancelled"],
			)
			if err != nil {
				return err
			}

			for _, c := range cs {
				// Date: prefer scheduled_at, fall back to created_at
				dateStr := c.CreatedAt.Format("02 Jan 2006")
				if c.ScheduledAt != nil {
					dateStr = c.ScheduledAt.Format("02 Jan 2006")
				}

				isFailed := c.FailedCount > 0 && c.SentCount == 0
				xShow := fmt.Sprintf(`tab==='' || tab==='%s'`, c.Status)
				if isFailed {
					xShow += ` || tab==='failed'`
				}

				// Status badge
				badgeVariant := c.Status
				switch c.Status {
				case "running", "completed":
					badgeVariant = "approved"
				case "cancelled", "failed":
					badgeVariant = "rejected"
				case "paused":
					badgeVariant = "paused"
				case "scheduled", "draft":
					badgeVariant = "pending"
				}
				statusBadge := BadgeHTML(badgeVariant, c.Status)

				// Delivered cell: count + delivery rate bar + undelivered count
				dlvCell := `<td class="cmp-td-dlv"><span class="dlv-dash">&#8212;</span></td>`
				if c.SentCount > 0 {
					dlvPct := c.DeliveredCount * 100 / c.SentCount
					dlvFillClass := "dlv-fill--ok"
					if dlvPct >= 70 {
						dlvFillClass = "dlv-fill--good"
					} else if dlvPct < 40 {
						dlvFillClass = "dlv-fill--low"
					}
					notDlv := c.SentCount - c.DeliveredCount
					notDlvHTML := ""
					if notDlv > 0 {
						notDlvHTML = fmt.Sprintf(`<div class="dlv-pending">%d not delivered</div>`, notDlv)
					}
					dlvCell = fmt.Sprintf(
						`<td class="cmp-td-dlv">`+
							`<div class="dlv-count">%d</div>`+
							`<div class="dlv-bar-row">`+
							`<div class="dlv-track"><div class="dlv-fill %s" style="width:%d%%"></div></div>`+
							`<span class="dlv-pct">%d%%</span>`+
							`</div>`+
							`%s`+
							`</td>`,
						c.DeliveredCount, dlvFillClass, dlvPct, dlvPct, notDlvHTML,
					)
				}

				// Failed cell
				failedCell := `<td class="cmp-time">&#8212;</td>`
				if c.FailedCount > 0 {
					failedCell = fmt.Sprintf(`<td class="cmp-td-fail">%d</td>`, c.FailedCount)
				}

				// Action buttons for detail row
				actionsHTML := ""
				if c.Status == "running" {
					actionsHTML += fmt.Sprintf(
						`<form hx-post="/campaigns/%s/pause" hx-swap="none" hx-disabled-elt="find button" style="display:inline"><button class="btn btn-sm btn-secondary" type="submit">&#8214; Pause</button></form> `,
						c.ID)
				}
				if c.Status == "running" || c.Status == "scheduled" {
					actionsHTML += fmt.Sprintf(
						`<form hx-post="/campaigns/%s/cancel" hx-swap="none" hx-disabled-elt="find button" style="display:inline"><button class="btn btn-sm btn-danger-ghost" type="submit">Cancel</button></form>`,
						c.ID)
				}

				// Send progress for detail row
				sendPct := 0
				if c.TotalRecipients > 0 {
					sendPct = (c.SentCount + c.FailedCount + c.SkippedCount) * 100 / c.TotalRecipients
				}
				detailHeaderHTML := ""
				if actionsHTML != "" || c.Status == "running" || c.Status == "paused" {
					detailHeaderHTML = fmt.Sprintf(
						`<div class="cmp-detail-hd">`+
							`<div class="cmp-detail-prog">`+
							`<span class="cmp-detail-prog-label">Send progress</span>`+
							`<div class="prog-wrap"><div class="prog-track"><div class="prog-fill" style="width:%d%%"></div></div>`+
							`<span class="prog-label">%d / %d sent</span></div>`+
							`</div>`+
							`<div class="cmp-action-btns">%s</div>`+
							`</div>`,
						sendPct, c.SentCount+c.FailedCount, c.TotalRecipients, actionsHTML,
					)
				}

				if _, err := fmt.Fprintf(w,
					`<tr x-show="%s" id="cmp-row-%s" class="cmp-row--expand" onclick="toggleCmpDetail('%s')">`+
						`<td class="cmp-chev-cell"><span id="cmp-chev-%s" class="cmp-chev"></span></td>`+
						`<td><a href="/campaigns/%s/report" class="cmp-name" onclick="event.stopPropagation()">%s</a></td>`+
						`<td class="cmp-tmpl">%s</td>`+
						`<td>%d</td>`+
						`<td>%d</td>`+
						`%s`+
						`%s`+
						`<td>%s</td>`+
						`<td>&#8377;%.2f</td>`+
						`<td class="cmp-time">%s</td>`+
						`</tr>`+
						`<tr id="cmp-detail-%s" class="cmp-detail-row" style="display:none">`+
						`<td colspan="10" class="cmp-detail-cell">`+
						`<div data-cmp-id="%s" class="cmp-detail-inner" aria-live="polite">`+
						`%s`+
						`Loading&#8230;</div>`+
						`</td></tr>`,
					xShow, c.ID, c.ID,
					c.ID,
					c.ID, html.EscapeString(c.Name),
					html.EscapeString(c.TemplateName),
					c.TotalRecipients,
					c.SentCount,
					dlvCell,
					failedCell,
					statusBadge,
					c.CostTotalINR,
					dateStr,
					c.ID,
					c.ID,
					detailHeaderHTML,
				); err != nil {
					return err
				}
			}

			if _, err := io.WriteString(w, `</tbody></table>
</div>
</div>

<script>
function toggleCmpDetail(id) {
  var row = document.getElementById('cmp-detail-' + id);
  var chev = document.getElementById('cmp-chev-' + id);
  if (!row) return;
  var open = row.style.display !== 'none';
  row.style.display = open ? 'none' : '';
  if (chev) chev.classList.toggle('open', !open);
  if (!open) {
    var inner = row.querySelector('[data-cmp-id]');
    if (inner && !inner.dataset.loaded) {
      inner.dataset.loaded = '1';
      htmx.ajax('GET', '/campaigns/' + id + '/recipients', {target: inner, swap: 'innerHTML'});
    }
  }
}
</script>`); err != nil {
				return err
			}
		}

		_, err = io.WriteString(w, `</div>`)
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, ShellClose())
		return err
	})
}

// CampaignRecipientRows renders the expandable recipient detail rows for a campaign.
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

// ── Wizard page-level wrappers (include the app shell) ────────────────────────

func WizardNewPage(agent *mw.AgentClaims, tags []db.Tag) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/campaigns", "New Campaign", "")); err != nil {
			return err
		}
		if _, err := io.WriteString(w, `<a href="/" class="wiz-back-link">&#8592; Back to Dashboard</a>`); err != nil {
			return err
		}
		state := WizardState{Step: 1}
		if err := WizardStep1(state, tags).Render(ctx, w); err != nil {
			return err
		}
		_, err := io.WriteString(w, ShellClose())
		return err
	})
}

func WizardStep2Page(agent *mw.AgentClaims, state WizardState, tmpls []db.Template) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/campaigns", "New Campaign", "")); err != nil {
			return err
		}
		if _, err := io.WriteString(w, `<a href="/" class="wiz-back-link">&#8592; Back to Dashboard</a>`); err != nil {
			return err
		}
		if err := WizardStep2(state, tmpls).Render(ctx, w); err != nil {
			return err
		}
		_, err := io.WriteString(w, ShellClose())
		return err
	})
}

func WizardStep2VarsPage(agent *mw.AgentClaims, state WizardState, tmpl db.Template, varNames []string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/campaigns", "New Campaign", "")); err != nil {
			return err
		}
		if _, err := io.WriteString(w, `<a href="/" class="wiz-back-link">&#8592; Back to Dashboard</a>`); err != nil {
			return err
		}
		if err := WizardStep2Vars(state, tmpl, varNames).Render(ctx, w); err != nil {
			return err
		}
		_, err := io.WriteString(w, ShellClose())
		return err
	})
}

func WizardStep2Vars(state WizardState, tmpl db.Template, varNames []string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		bodyText := TemplateBodyText(tmpl)
		// Highlight {{N}} placeholders in the preview.
		highlighted := varPlaceholderRe.ReplaceAllStringFunc(html.EscapeString(bodyText), func(m string) string {
			return `<mark class="var-highlight">` + m + `</mark>`
		})

		_, err := fmt.Fprintf(w, `
<div class="page-wrap">
<div class="wizard">
<div class="wizard-steps">
  <span class="step done">1 Audience</span>
  <span class="step active" aria-current="step">2 Message</span>
  <span class="step">3 Schedule</span>
  <span class="step">4 Review</span>
</div>
<form method="post" action="/campaigns/wizard/message" class="wizard-form">
<input type="hidden" name="wizard_state" value="%s">
<input type="hidden" name="template_id" value="%s">
<input type="hidden" name="vars_confirmed" value="1">
<h2>Step 2 — Map variables</h2>
<p style="font-size:13px;color:var(--text-secondary);margin:0 0 8px">Template: <strong style="color:var(--text)">%s</strong></p>
<div class="var-preview">%s</div>
<div class="var-map-table">`,
			html.EscapeString(encodeState(state)),
			html.EscapeString(tmpl.ID),
			html.EscapeString(tmpl.Name),
			highlighted)
		if err != nil {
			return err
		}

		for _, n := range varNames {
			if _, err := fmt.Fprintf(w, `
<div class="var-row" x-data="{sel:'name',custom:''}">
  <span class="var-label">{{%s}}</span>
  <select x-model="sel" class="form-input var-select">
    <option value="name">Contact name</option>
    <option value="wa_phone">WhatsApp phone</option>
    <option value="email">Email</option>
    <option value="_custom">Custom field…</option>
  </select>
  <input x-show="sel==='_custom'" x-cloak type="text" x-model="custom" placeholder="field key, e.g. order_id" class="form-input var-custom">
  <input type="hidden" name="var_%s" :value="sel==='_custom' ? (custom ? 'custom_fields.'+custom : '') : sel">
  <input type="text" name="fallback_%s" class="form-input var-fallback" placeholder="Fallback (required — used when field is empty)" required>
</div>`, n, n, n); err != nil {
				return err
			}
		}

		_, err = fmt.Fprintf(w, `
</div>
<div class="wizard-btns">
<button class="btn btn-primary" type="submit">Next: schedule &#8594;</button>
<a class="btn btn-secondary" href="/campaigns">Cancel</a>
</div>
</form>
</div>
</div>`)
		return err
	})
}

func WizardStep3Page(agent *mw.AgentClaims, state WizardState) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/campaigns", "New Campaign", "")); err != nil {
			return err
		}
		if _, err := io.WriteString(w, `<a href="/" class="wiz-back-link">&#8592; Back to Dashboard</a>`); err != nil {
			return err
		}
		if err := WizardStep3(state).Render(ctx, w); err != nil {
			return err
		}
		_, err := io.WriteString(w, ShellClose())
		return err
	})
}

func WizardStep3ErrorPage(agent *mw.AgentClaims, state WizardState, errMsg string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/campaigns", "New Campaign", "")); err != nil {
			return err
		}
		if _, err := io.WriteString(w, `<a href="/" class="wiz-back-link">&#8592; Back to Dashboard</a>`); err != nil {
			return err
		}
		if err := WizardStep3Error(state, errMsg).Render(ctx, w); err != nil {
			return err
		}
		_, err := io.WriteString(w, ShellClose())
		return err
	})
}

func WizardStep4Page(agent *mw.AgentClaims, state WizardState, tmpl *db.Template) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/campaigns", "New Campaign", "")); err != nil {
			return err
		}
		if _, err := io.WriteString(w, `<a href="/" class="wiz-back-link">&#8592; Back to Dashboard</a>`); err != nil {
			return err
		}
		if err := WizardStep4(state, tmpl).Render(ctx, w); err != nil {
			return err
		}
		_, err := io.WriteString(w, ShellClose())
		return err
	})
}

// ── Wizard steps ──────────────────────────────────────────────────────────────

func WizardStep1(state WizardState, tags []db.Tag) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		segSet := make(map[int64]bool, len(state.SegmentTags))
		for _, id := range state.SegmentTags {
			segSet[id] = true
		}
		exclSet := make(map[int64]bool, len(state.ExcludeTags))
		for _, id := range state.ExcludeTags {
			exclSet[id] = true
		}

		segChips := ""
		exclChips := ""
		for _, t := range tags {
			segChecked := ""
			if segSet[t.ID] {
				segChecked = " checked"
			}
			exclChecked := ""
			if exclSet[t.ID] {
				exclChecked = " checked"
			}
			chip := fmt.Sprintf(`<label class="tag-pick-item"><input type="checkbox" name="%s" value="%d"%s>%s</label>`,
				"%s", t.ID, "%s", html.EscapeString(t.Name))
			segChips += fmt.Sprintf(`<label class="tag-pick-item"><input type="checkbox" name="segment_tags" value="%d"%s>%s</label>`,
				t.ID, segChecked, html.EscapeString(t.Name))
			exclChips += fmt.Sprintf(`<label class="tag-pick-item"><input type="checkbox" name="exclude_tags" value="%d"%s>%s</label>`,
				t.ID, exclChecked, html.EscapeString(t.Name))
			_ = chip
		}
		emptyHint := ""
		if len(tags) == 0 {
			emptyHint = `<span class="tag-pick-empty">No tags yet — all opted-in contacts will be included</span>`
		}

		_, err := fmt.Fprintf(w, `
<div class="page-wrap">
<div class="wizard">
<div class="wizard-steps">
  <span class="step active" aria-current="step">1 Audience</span>
  <span class="step">2 Message</span>
  <span class="step">3 Schedule</span>
  <span class="step">4 Review</span>
</div>
<form method="post" action="/campaigns/wizard/audience" class="wizard-form">
  <h2>Step 1 — Name &amp; audience</h2>

  <div class="form-group">
    <label class="form-label" for="cmp-name">Campaign name</label>
    <input id="cmp-name" class="form-input" type="text" name="name" value="%s" required placeholder="June promo">
  </div>

  <div class="aud-cols">
    <div class="form-group">
      <label class="form-label">Include tags</label>
      <p class="tag-pick-hint">Contacts must have ALL selected tags. Leave blank for all opted-in.</p>
      <div class="tag-pick-wrap">%s%s</div>
    </div>
    <div class="form-group">
      <label class="form-label">Exclude tags</label>
      <p class="tag-pick-hint">Contacts with ANY of these tags are excluded.</p>
      <div class="tag-pick-wrap">%s%s</div>
    </div>
  </div>

  <div class="wizard-btns">
    <button class="btn btn-primary" type="submit">Next: choose message →</button>
    <a class="btn btn-secondary" href="/campaigns">Cancel</a>
  </div>
</form>
</div>
</div>`,
			html.EscapeString(state.Name),
			segChips, emptyHint,
			exclChips, emptyHint)
		return err
	})
}

// WizardStep2 renders the template-picker with the US-exclusion + freq-cap callout.
func WizardStep2(state WizardState, tmpls []db.Template) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		// Guardrails 4+5: US exclusion and frequency-cap warning callout.
		skipCallout := ""
		if state.SkipReport.USNumber > 0 || state.SkipReport.FreqCap > 0 {
			var msg string
			switch {
			case state.SkipReport.USNumber > 0 && state.SkipReport.FreqCap > 0:
				msg = fmt.Sprintf(
					"%d US (+1) numbers skipped for marketing · %d skipped (frequency cap, last 24h)",
					state.SkipReport.USNumber, state.SkipReport.FreqCap)
			case state.SkipReport.USNumber > 0:
				msg = fmt.Sprintf("%d US (+1) numbers skipped for marketing",
					state.SkipReport.USNumber)
			default:
				msg = fmt.Sprintf("%d contacts skipped (frequency cap, last 24h)",
					state.SkipReport.FreqCap)
			}
			skipCallout = CalloutHTML("warning", msg)
		}

		_, err := fmt.Fprintf(w, `
<div class="page-wrap">
<div class="wizard">
<div class="wizard-steps">
  <span class="step done">1 Audience</span>
  <span class="step active" aria-current="step">2 Message</span>
  <span class="step">3 Schedule</span>
  <span class="step">4 Review</span>
</div>
<form method="post" action="/campaigns/wizard/message" class="wizard-form">
<input type="hidden" name="wizard_state" value="%s">
<h2>Step 2 — Choose template</h2>
<p style="margin:0;font-size:14px;color:var(--text-secondary)">Eligible recipients: <strong style="color:var(--text)">%d</strong></p>
%s
<fieldset class="tmpl-picker">`,
			html.EscapeString(encodeState(state)),
			state.EligibleCount, skipCallout)
		if err != nil {
			return err
		}

		for _, t := range tmpls {
			checked := ""
			if t.ID == state.TemplateID {
				checked = " checked"
			}
			if _, err := fmt.Fprintf(w,
				`<label class="tmpl-option">
<input type="radio" name="template_id" value="%s"%s>
<strong>%s</strong> %s / %s / %s
</label>`,
				t.ID, checked, html.EscapeString(t.Name),
				BadgeHTML(t.Status, t.Status), t.Language, t.Category,
			); err != nil {
				return err
			}
		}

		_, err = fmt.Fprintf(w, `</fieldset>
<div class="wizard-btns">
<button class="btn btn-primary" type="submit">Next: schedule →</button>
<a class="btn btn-secondary" href="/campaigns">Cancel</a>
</div>
</form>
</div>
</div>`)
		return err
	})
}

func WizardStep3(state WizardState) templ.Component {
	return wizardStep3Inner(state, "")
}

func WizardStep3Error(state WizardState, errMsg string) templ.Component {
	return wizardStep3Inner(state, errMsg)
}

// wizardStep3Inner renders the schedule step with an Alpine-driven quiet-hours
// guardrail (guardrail 3): the callout appears and submit is disabled when the
// chosen time falls in the IST quiet window (21:00–09:00).
func wizardStep3Inner(state WizardState, errMsg string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		errHTML := ""
		if errMsg != "" {
			errHTML = CalloutHTML("warning", errMsg)
		}
		_, err := fmt.Fprintf(w, `
<div class="page-wrap">
<div class="wizard">
<div class="wizard-steps">
  <span class="step done">1 Audience</span>
  <span class="step done">2 Message</span>
  <span class="step active" aria-current="step">3 Schedule</span>
  <span class="step">4 Review</span>
</div>
<form method="post" action="/campaigns/wizard/schedule" class="wizard-form"
  x-data="scheduleStep()"
  @submit.prevent="if (!inQuietHours) $el.submit()">
<input type="hidden" name="wizard_state" value="%s">
<h2>Step 3 — Schedule</h2>
%s
<fieldset style="border:none;padding:0;margin:0;display:flex;flex-direction:column;gap:12px">
<label class="radio-row" style="display:flex;align-items:center;gap:8px;font-size:14px;cursor:pointer">
<input type="radio" name="schedule_type" value="now" x-model="schedType" style="accent-color:var(--accent)"> Send now
</label>
<label class="radio-row" style="display:flex;align-items:center;gap:8px;font-size:14px;cursor:pointer">
<input type="radio" name="schedule_type" value="scheduled" x-model="schedType" style="accent-color:var(--accent)"> Schedule for later
</label>
<div class="form-group" x-show="schedType === 'scheduled'">
<label class="form-label" for="sched-at">Date &amp; time (IST)</label>
<input id="sched-at" class="form-input" type="datetime-local" name="scheduled_at" x-model="scheduledAt">
</div>
</fieldset>
<div class="callout callout--warning" x-show="inQuietHours" x-cloak role="alert">
<span aria-hidden="true">⚠</span>
Quiet hours: 9&nbsp;pm–9&nbsp;am IST. Choose a time between 9&nbsp;am and 9&nbsp;pm.
</div>
<div class="wizard-btns">
<button class="btn btn-primary" type="submit"
  :disabled="inQuietHours"
  :aria-disabled="String(inQuietHours)">Next: review →</button>
<a class="btn btn-secondary" href="/campaigns">Cancel</a>
</div>
</form>
</div>
</div>
<script>
function scheduleStep() {
  return {
    schedType: 'now',
    scheduledAt: '',
    get inQuietHours() {
      if (this.schedType !== 'scheduled' || !this.scheduledAt) return false;
      var parts = (this.scheduledAt.split('T')[1] || '').split(':');
      var h = parseInt(parts[0], 10), m = parseInt(parts[1] || '0', 10);
      var total = h * 60 + m;
      return total < 9 * 60 || total >= 21 * 60;
    }
  };
}
</script>`, html.EscapeString(encodeState(state)), errHTML)
		return err
	})
}

// WizardStep4 renders the review step with a tier-cap banner (guardrail 1/2)
// and a confirm-send <dialog> modal. The "Launch campaign" button opens the
// dialog; only the dialog's "Confirm & send" button actually submits the form.
func WizardStep4(state WizardState, tmpl *db.Template) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		schedInfo := "Send immediately"
		if state.ScheduleType == "scheduled" && state.ScheduledAt != nil {
			schedInfo = "Scheduled for " + state.ScheduledAt.Format("02 Jan 2006 15:04 IST")
		}
		tmplName := ""
		tmplCategory := "marketing"
		if tmpl != nil {
			tmplName = tmpl.Name
			tmplCategory = tmpl.Category
		}

		// Guardrail 1: tier-cap banner. Skipped when DailyCap is unknown (0).
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
					"This campaign (%d messages) would exceed your daily cap of %d — it will be blocked at launch. Reduce audience or try tomorrow.",
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

		// The form carries no submit button of its own; the confirm dialog's
		// "Confirm & send" button submits it via form="launch-form".
		confirmInner := fmt.Sprintf(
			`<h2 id="confirm-launch-title">Confirm campaign launch</h2>`+
				`<div class="confirm-summary"><dl>`+
				`<dt>Recipients</dt><dd>%d contacts</dd>`+
				`<dt>Category</dt><dd>%s</dd>`+
				`<dt>Estimated cost</dt><dd>&#8377;%.2f (incl. 18%% GST)</dd>`+
				`</dl></div>`+
				`<p class="confirm-note">This action cannot be undone. %d messages will be queued for delivery.</p>`+
				`<div class="confirm-btns">`+
				`<button type="button" class="btn btn-secondary" onclick="document.getElementById('confirm-launch').close()">Cancel</button>`+
				`<button type="submit" form="launch-form" class="btn btn-primary">Confirm &amp; send to %d contacts</button>`+
				`</div>`,
			state.EligibleCount,
			html.EscapeString(tmplCategory),
			state.EstCost,
			state.EligibleCount,
			state.EligibleCount,
		)
		_, err := fmt.Fprintf(w, `
<div class="page-wrap">
<div class="wizard">
<div class="wizard-steps">
  <span class="step done">1 Audience</span>
  <span class="step done">2 Message</span>
  <span class="step done">3 Schedule</span>
  <span class="step active" aria-current="step">4 Review</span>
</div>
<form id="launch-form" method="post" action="/campaigns" class="wizard-form">
<input type="hidden" name="wizard_state" value="%s">
<h2>Step 4 — Review &amp; launch</h2>
%s
<div style="display:flex;flex-direction:column;gap:0;border:1px solid var(--border);border-radius:var(--radius)">
  <div class="stt-info-row" style="padding:12px 16px"><span class="stt-info-key">Campaign name</span><span class="stt-info-val">%s</span></div>
  <div class="stt-info-row" style="padding:12px 16px"><span class="stt-info-key">Recipients</span><span class="stt-info-val">%d eligible</span></div>
  <div class="stt-info-row" style="padding:12px 16px"><span class="stt-info-key">Template</span><span class="stt-info-val">%s</span></div>
  <div class="stt-info-row" style="padding:12px 16px"><span class="stt-info-key">Category</span><span class="stt-info-val">%s</span></div>
  <div class="stt-info-row" style="padding:12px 16px"><span class="stt-info-key">Scheduling</span><span class="stt-info-val">%s</span></div>
  <div class="stt-info-row" style="padding:12px 16px;border-bottom:none"><span class="stt-info-key">Estimated cost</span><span class="stt-info-val">&#8377;%.2f</span></div>
</div>
<div class="wizard-btns">
<button type="button" class="btn btn-primary"
  onclick="openModal('confirm-launch',this)">Review &amp; launch &#8594;</button>
<a class="btn btn-secondary" href="/campaigns">Cancel</a>
</div>
</form>
</div>
</div>
%s`,
			html.EscapeString(encodeState(state)),
			tierBanner,
			html.EscapeString(state.Name),
			state.EligibleCount,
			html.EscapeString(tmplName),
			html.EscapeString(tmplCategory),
			html.EscapeString(schedInfo),
			state.EstCost,
			ModalShellRaw("confirm-launch", "confirm-dialog", "confirm-launch-title", confirmInner),
		)
		return err
	})
}

func CampaignReportPage(agent *mw.AgentClaims, report db.CampaignReport) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/campaigns", "Campaign Report", "")); err != nil {
			return err
		}
		sched := "—"
		if report.ScheduledAt != nil {
			sched = report.ScheduledAt.Format("02 Jan 2006 15:04")
		}
		_, err := fmt.Fprintf(w, `
<div class="page-wrap">
<div class="page-hd">
<div class="screen-title">%s</div>
%s
</div>
<div class="stat-grid stat-grid--report">
<div class="stat-card"><div class="stat-label">Total recipients</div><div class="stat-value">%d</div></div>
<div class="stat-card"><div class="stat-label">Sent</div><div class="stat-value">%d</div></div>
<div class="stat-card"><div class="stat-label">Delivered</div><div class="stat-value">%d</div></div>
<div class="stat-card"><div class="stat-label">Read</div><div class="stat-value">%d</div></div>
<div class="stat-card"><div class="stat-label">Failed</div><div class="stat-value">%d</div></div>
<div class="stat-card"><div class="stat-label">Clicked</div><div class="stat-value">%d</div></div>
<div class="stat-card"><div class="stat-label">Total cost</div><div class="stat-value">₹%.2f</div></div>
</div>
<div class="card-static" style="display:flex;flex-direction:column;gap:0;margin-top:var(--gutter)">
  <div class="stt-info-row" style="padding:12px 16px"><span class="stt-info-key">Template</span><span class="stt-info-val">%s</span></div>
  <div class="stt-info-row" style="padding:12px 16px"><span class="stt-info-key">Category</span><span class="stt-info-val">%s</span></div>
  <div class="stt-info-row" style="padding:12px 16px;border-bottom:none"><span class="stt-info-key">Scheduled</span><span class="stt-info-val">%s</span></div>
</div>
</div>`,
			html.EscapeString(report.Name),
			BadgeHTML(report.Status, report.Status),
			report.TotalRecipients, report.SentCount, report.DeliveredCount,
			report.ReadCount, report.FailedCount, report.ClickCount,
			report.CostTotalINR,
			html.EscapeString(report.TemplateName),
			report.Category,
			sched,
		)
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, ShellClose())
		return err
	})
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
