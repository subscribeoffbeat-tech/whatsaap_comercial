package templates

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"time"

	"github.com/a-h/templ"

	"whatsapptool/internal/campaigns"
	"whatsapptool/internal/db"
	mw "whatsapptool/internal/web/middleware"
)

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
<div style="display:flex;align-items:flex-start;justify-content:space-between;margin-bottom:24px;flex-wrap:wrap;gap:12px">
  <div>
    <h1 style="font-size:28px;font-weight:800;color:var(--text-primary,#111);margin:0 0 4px">Campaigns</h1>
    <div style="color:var(--text-secondary);font-size:14px">Broadcast to opted-in contacts with approved templates</div>
  </div>
  <a class="btn btn-primary" href="/campaigns/new" style="font-size:15px;padding:10px 20px">+ New campaign</a>
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
			// Alpine.js filter tabs + table
			_, err = fmt.Fprintf(w, `
<div x-data="{status:''}">
<div class="tmpl-tabs" style="margin-bottom:16px" role="tablist">
  <button class="tmpl-tab" role="tab" :class="{active:status===''}" :aria-selected="status===''" @click="status=''">All <span class="badge-cnt badge-cnt-success">%d</span></button>
  <button class="tmpl-tab" role="tab" :class="{active:status==='running'}" :aria-selected="status==='running'" @click="status='running'">Active <span class="badge-cnt badge-cnt-success">%d</span></button>
  <button class="tmpl-tab" role="tab" :class="{active:status==='draft'}" :aria-selected="status==='draft'" @click="status='draft'">Draft <span class="badge-cnt badge-cnt-neutral">%d</span></button>
  <button class="tmpl-tab" role="tab" :class="{active:status==='paused'}" :aria-selected="status==='paused'" @click="status='paused'">Paused <span class="badge-cnt badge-cnt-neutral">%d</span></button>
  <button class="tmpl-tab" role="tab" :class="{active:status==='failed'}" :aria-selected="status==='failed'" @click="status='failed'">Failed <span class="badge-cnt badge-cnt-danger">%d</span></button>
  <button class="tmpl-tab" role="tab" :class="{active:status==='completed'}" :aria-selected="status==='completed'" @click="status='completed'">Completed <span class="badge-cnt badge-cnt-success">%d</span></button>
  <button class="tmpl-tab" role="tab" :class="{active:status==='cancelled'}" :aria-selected="status==='cancelled'" @click="status='cancelled'">Cancelled <span class="badge-cnt badge-cnt-purple">%d</span></button>
</div>
<div class="card-static" role="tabpanel" tabindex="0">
<table class="tbl">
<thead><tr>
  <th style="width:32px"></th>
  <th>NAME</th><th>STATUS</th><th>SENT</th><th>DELIVERED</th>
  <th>FAILED</th><th>SUPPRESSED</th><th>COST</th><th>SCHEDULED</th><th>ACTIONS</th><th></th>
</tr></thead>
<tbody>`,
				counts["all"], counts["running"], counts["draft"],
				counts["paused"], counts["failed"], counts["completed"], counts["cancelled"],
			)
			if err != nil {
				return err
			}

			for _, c := range cs {
				sched := "—"
				if c.ScheduledAt != nil {
					sched = c.ScheduledAt.Format("02 Jan 15:04")
				}

				isFailed := c.FailedCount > 0 && c.SentCount == 0
				// Alpine x-show condition
				xShow := fmt.Sprintf(`status==='' || status==='%s'`, c.Status)
				if isFailed {
					xShow += ` || status==='failed'`
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

				// Action buttons
				actionsHTML := ""
				if c.Status == "running" {
					actionsHTML += fmt.Sprintf(
						`<form hx-post="/campaigns/%s/pause" hx-swap="none" hx-disabled-elt="find button" style="display:inline"><button class="btn btn-sm btn-secondary" type="submit">&#8214; Pause</button></form> `,
						c.ID)
				}
				if c.Status == "running" || c.Status == "scheduled" {
					actionsHTML += fmt.Sprintf(
						`<form hx-post="/campaigns/%s/cancel" hx-swap="none" hx-disabled-elt="find button" style="display:inline"><button class="btn btn-sm btn-danger" type="submit">Cancel</button></form>`,
						c.ID)
				}

				// Progress bar
				pct := 0
				if c.TotalRecipients > 0 {
					pct = (c.SentCount + c.FailedCount + c.SkippedCount) * 100 / c.TotalRecipients
				}
				progHTML := fmt.Sprintf(
					`<div class="prog-wrap"><div class="prog-track"><div class="prog-fill" style="width:%d%%"></div></div><span class="prog-label">%d/%d</span></div>`,
					pct, c.SentCount+c.FailedCount, c.TotalRecipients,
				)

				if _, err := fmt.Fprintf(w, `<tr x-show="%s" id="cmp-row-%s">
  <td><button onclick="toggleCmpDetail('%s')" id="cmp-chev-%s" class="btn btn-sm" style="padding:2px 6px;background:transparent;border:1px solid var(--border);font-size:12px">&#9654;</button></td>
  <td><a href="/campaigns/%s/report" style="font-weight:600">%s</a></td>
  <td>%s</td>
  <td>%d</td><td>%d</td>
  <td style="color:var(--danger,#ef4444)">%d</td>
  <td>%d</td>
  <td>&#8377;%.2f</td>
  <td style="font-size:13px;color:var(--text-secondary)">%s</td>
  <td>%s</td>
  <td>%s</td>
</tr>
<tr id="cmp-detail-%s" style="display:none">
  <td colspan="11" style="padding:0">
    <div data-cmp-id="%s" style="padding:12px;background:var(--accent-lighter,#f8fafc);border-top:1px solid var(--border)" aria-live="polite" aria-atomic="false">
      Loading...
    </div>
  </td>
</tr>`,
					xShow, c.ID,
					c.ID, c.ID,
					c.ID, html.EscapeString(c.Name),
					statusBadge,
					c.SentCount, c.DeliveredCount,
					c.FailedCount,
					c.SkippedCount,
					c.CostTotalINR,
					sched,
					actionsHTML,
					progHTML,
					c.ID,
					c.ID,
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
  if (chev) chev.innerHTML = open ? '&#9654;' : '&#9660;';
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
		if _, err := io.WriteString(w, `<table class="tbl" style="font-size:13px"><thead><tr><th>CONTACT</th><th>PHONE</th><th>STATUS</th><th>SENT AT</th><th>NOTE</th></tr></thead><tbody>`); err != nil {
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
		if err := WizardStep2(state, tmpls).Render(ctx, w); err != nil {
			return err
		}
		_, err := io.WriteString(w, ShellClose())
		return err
	})
}

func WizardStep3Page(agent *mw.AgentClaims, state WizardState) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/campaigns", "New Campaign", "")); err != nil {
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
		tagOpts := ""
		for _, t := range tags {
			tagOpts += fmt.Sprintf(`<option value="%d">%s</option>`, t.ID, html.EscapeString(t.Name))
		}
		_, err := fmt.Fprintf(w, `
<div class="wizard">
<div class="wizard-steps">
<span class="step active">1 Audience</span>
<span class="step">2 Message</span>
<span class="step">3 Schedule</span>
<span class="step">4 Review</span>
</div>
<form method="post" action="/campaigns/wizard/audience" class="wizard-form">
<h2>Step 1 — Name &amp; audience</h2>

<label class="field"><span>Campaign name</span>
<input type="text" name="name" value="%s" required placeholder="June promo"></label>

<label class="field"><span>Segment tags (contacts must have ALL selected tags)</span>
<select name="segment_tags" multiple size="4">%s</select></label>

<label class="field"><span>Exclude tags (contacts with ANY of these are excluded)</span>
<select name="exclude_tags" multiple size="4">%s</select></label>

<div class="wizard-btns">
<button class="btn btn-primary" type="submit">Next: choose message →</button>
<a class="btn btn-secondary" href="/campaigns">Cancel</a>
</div>
</form>
</div>`, html.EscapeString(state.Name), tagOpts, tagOpts)
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
<div class="wizard">
<div class="wizard-steps">
<span class="step done">1 Audience</span>
<span class="step active">2 Message</span>
<span class="step">3 Schedule</span>
<span class="step">4 Review</span>
</div>
<form method="post" action="/campaigns/wizard/message" class="wizard-form">
<input type="hidden" name="wizard_state" value="%s">
<h2>Step 2 — Choose template</h2>
<p>Eligible recipients: <strong>%d</strong></p>
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
<div class="wizard">
<div class="wizard-steps">
<span class="step done">1 Audience</span>
<span class="step done">2 Message</span>
<span class="step active">3 Schedule</span>
<span class="step">4 Review</span>
</div>
<form method="post" action="/campaigns/wizard/schedule" class="wizard-form"
  x-data="scheduleStep()"
  @submit.prevent="if (!inQuietHours) $el.submit()">
<input type="hidden" name="wizard_state" value="%s">
<h2>Step 3 — Schedule</h2>
%s
<fieldset>
<label class="field radio-row">
<input type="radio" name="schedule_type" value="now" x-model="schedType"> Send now
</label>
<label class="field radio-row">
<input type="radio" name="schedule_type" value="scheduled" x-model="schedType"> Schedule for later
</label>
<label class="field" x-show="schedType === 'scheduled'">
<span>Date &amp; time (IST)</span>
<input type="datetime-local" name="scheduled_at" x-model="scheduledAt">
</label>
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
<div class="wizard">
<div class="wizard-steps">
<span class="step done">1 Audience</span>
<span class="step done">2 Message</span>
<span class="step done">3 Schedule</span>
<span class="step active">4 Review</span>
</div>
<form id="launch-form" method="post" action="/campaigns" class="wizard-form">
<input type="hidden" name="wizard_state" value="%s">
<h2>Step 4 — Review &amp; launch</h2>
%s
<dl>
<dt>Campaign name</dt><dd>%s</dd>
<dt>Recipients</dt><dd>%d eligible</dd>
<dt>Template</dt><dd>%s</dd>
<dt>Category</dt><dd>%s</dd>
<dt>Scheduling</dt><dd>%s</dd>
<dt>Estimated cost</dt><dd>&#8377;%.2f</dd>
</dl>
<div class="wizard-btns">
<button type="button" class="btn btn-primary"
  onclick="openModal('confirm-launch',this)">Review &amp; launch &#8594;</button>
<a class="btn btn-secondary" href="/campaigns">Cancel</a>
</div>
</form>
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
<div class="stat-row">
<div class="stat-card"><div class="stat-val">%d</div><div class="stat-lbl">Total recipients</div></div>
<div class="stat-card"><div class="stat-val">%d</div><div class="stat-lbl">Sent</div></div>
<div class="stat-card"><div class="stat-val">%d</div><div class="stat-lbl">Delivered</div></div>
<div class="stat-card"><div class="stat-val">%d</div><div class="stat-lbl">Read</div></div>
<div class="stat-card"><div class="stat-val">%d</div><div class="stat-lbl">Failed</div></div>
<div class="stat-card"><div class="stat-val">%d</div><div class="stat-lbl">Clicked</div></div>
<div class="stat-card"><div class="stat-val">₹%.2f</div><div class="stat-lbl">Total cost</div></div>
</div>
<dl>
<dt>Template</dt><dd>%s</dd>
<dt>Category</dt><dd>%s</dd>
<dt>Scheduled</dt><dd>%s</dd>
</dl>
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
