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

func CampaignsPage(agent *mw.AgentClaims, cs []db.Campaign) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/campaigns", "Campaigns", "")); err != nil {
			return err
		}
		_, err := fmt.Fprintf(w, `
<div class="page-wrap">
<div class="page-hd">
<h1>Campaigns</h1>
<a class="btn pri" href="/campaigns/new">New campaign</a>
</div>`)
		if err != nil {
			return err
		}

		if len(cs) == 0 {
			if _, err := io.WriteString(w, `<p class="empty-state">No campaigns yet. Click "New campaign" to get started.</p>`); err != nil {
				return err
			}
		} else {
			if _, err := io.WriteString(w, `<table class="tbl">
<thead><tr><th>Name</th><th>Status</th><th>Sent</th><th>Delivered</th><th>Failed</th><th>Cost</th><th>Scheduled</th><th></th></tr></thead>
<tbody>`); err != nil {
				return err
			}
			for _, c := range cs {
				sched := "—"
				if c.ScheduledAt != nil {
					sched = c.ScheduledAt.Format("02 Jan 15:04")
				}
				cancelBtn := ""
				if c.Status == "running" || c.Status == "scheduled" {
					cancelBtn = fmt.Sprintf(`<form method="post" action="/campaigns/%s/cancel" style="display:inline"><button class="btn sm danger" type="submit">Cancel</button></form>`, c.ID)
				}
				if _, err := fmt.Fprintf(w,
					`<tr>
<td><a href="/campaigns/%s/report">%s</a></td>
<td>%s</td><td>%d</td><td>%d</td><td>%d</td>
<td>₹%.2f</td><td>%s</td>
<td>
<div id="prog-%s"
  hx-get="/campaigns/%s/progress"
  hx-trigger="%s"
  hx-swap="outerHTML">
</div>
%s
</td>
</tr>`,
					c.ID, html.EscapeString(c.Name),
					c.Status, c.SentCount, c.DeliveredCount, c.FailedCount,
					c.CostTotalINR, sched,
					c.ID, c.ID,
					progressTrigger(c.Status),
					cancelBtn,
				); err != nil {
					return err
				}
			}
			if _, err := io.WriteString(w, `</tbody></table>`); err != nil {
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

func progressTrigger(status string) string {
	if status == "running" {
		return `every 5s`
	}
	return `load once`
}

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
<button class="btn pri" type="submit">Next: choose message →</button>
<a class="btn" href="/campaigns">Cancel</a>
</div>
</form>
</div>`, html.EscapeString(state.Name), tagOpts, tagOpts)
		return err
	})
}

func WizardStep2(state WizardState, tmpls []db.Template) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
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
<p>Eligible recipients: <strong>%d</strong>
(skipped: %d US numbers, %d freq-capped)</p>

<fieldset class="tmpl-picker">`, html.EscapeString(encodeState(state)),
			state.EligibleCount, state.SkipReport.USNumber, state.SkipReport.FreqCap)
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
<strong>%s</strong> <span class="badge badge-%s">%s</span> / %s / %s
</label>`,
				t.ID, checked, html.EscapeString(t.Name),
				t.Status, t.Status, t.Language, t.Category,
			); err != nil {
				return err
			}
		}

		_, err = fmt.Fprintf(w, `</fieldset>
<div class="wizard-btns">
<button class="btn pri" type="submit">Next: schedule →</button>
<a class="btn" href="/campaigns">Cancel</a>
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

func wizardStep3Inner(state WizardState, errMsg string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		errHTML := ""
		if errMsg != "" {
			errHTML = `<p class="err">` + html.EscapeString(errMsg) + `</p>`
		}
		_, err := fmt.Fprintf(w, `
<div class="wizard">
<div class="wizard-steps">
<span class="step done">1 Audience</span>
<span class="step done">2 Message</span>
<span class="step active">3 Schedule</span>
<span class="step">4 Review</span>
</div>
<form method="post" action="/campaigns/wizard/schedule" class="wizard-form">
<input type="hidden" name="wizard_state" value="%s">
<h2>Step 3 — Schedule</h2>
%s
<fieldset>
<label class="field">
<input type="radio" name="schedule_type" value="now" checked> Send now
</label>
<label class="field">
<input type="radio" name="schedule_type" value="scheduled"> Schedule for later
</label>
<label class="field">
<span>Date &amp; time (IST, 9am–9pm only)</span>
<input type="datetime-local" name="scheduled_at">
</label>
</fieldset>
<div class="wizard-btns">
<button class="btn pri" type="submit">Next: review →</button>
<a class="btn" href="/campaigns">Cancel</a>
</div>
</form>
</div>`, html.EscapeString(encodeState(state)), errHTML)
		return err
	})
}

func WizardStep4(state WizardState, tmpl *db.Template) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		schedInfo := "Send immediately"
		if state.ScheduleType == "scheduled" && state.ScheduledAt != nil {
			schedInfo = "Scheduled for " + state.ScheduledAt.Format("02 Jan 2006 15:04 IST")
		}
		tmplName := ""
		if tmpl != nil {
			tmplName = tmpl.Name
		}
		_, err := fmt.Fprintf(w, `
<div class="wizard">
<div class="wizard-steps">
<span class="step done">1 Audience</span>
<span class="step done">2 Message</span>
<span class="step done">3 Schedule</span>
<span class="step active">4 Review</span>
</div>
<form method="post" action="/campaigns" class="wizard-form">
<input type="hidden" name="wizard_state" value="%s">
<h2>Step 4 — Review &amp; launch</h2>
<dl>
<dt>Campaign name</dt><dd>%s</dd>
<dt>Recipients</dt><dd>%d eligible</dd>
<dt>Template</dt><dd>%s</dd>
<dt>Scheduling</dt><dd>%s</dd>
<dt>Estimated cost</dt><dd>₹%.2f</dd>
</dl>
<div class="wizard-btns">
<button class="btn pri" type="submit">Launch campaign</button>
<a class="btn" href="/campaigns">Cancel</a>
</div>
</form>
</div>`,
			html.EscapeString(encodeState(state)),
			html.EscapeString(state.Name),
			state.EligibleCount,
			html.EscapeString(tmplName),
			html.EscapeString(schedInfo),
			state.EstCost,
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
<h1>%s</h1>
<span class="badge badge-%s">%s</span>
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
			report.Status, report.Status,
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
