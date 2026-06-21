package templates

import (
	"context"
	"fmt"
	"io"

	"github.com/a-h/templ"

	mw "whatsapptool/internal/web/middleware"
)

// SettingsViewData holds all editable settings values for the settings page.
type SettingsViewData struct {
	QuietHoursStart string
	QuietHoursEnd   string
	FreqCapHours    int64
	RetentionMonths int64
	MarketingCost   float64
	UtilityCost     float64
	AuthCost        float64
	GSTRate         float64
	MetaTier        int64
	QualityRating   string
	WADisplayName   string
}

func SettingsPage(d SettingsViewData, actor *mw.AgentClaims, saved bool) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(actor, "/settings", "Settings", "")); err != nil {
			return err
		}

		savedBanner := ""
		if saved {
			savedBanner = FlashScript(ToastSuccess, "Settings saved.")
		}

		qualityColor := "var(--text-muted)"
		switch d.QualityRating {
		case "GREEN":
			qualityColor = "var(--success-strong)"
		case "YELLOW":
			qualityColor = "var(--warning-text)"
		case "RED":
			qualityColor = "var(--danger-text)"
		}

		_, err := fmt.Fprintf(w, `
<div class="page-wrap">
<div class="page-hd">
  <div>
    <h1 class="screen-title">Settings</h1>
    <p class="screen-subtitle">Sending rules, cost rates, and account configuration.</p>
  </div>
</div>
%s
<div class="g-grid g-split-3-9" style="align-items:start">

<nav class="card-static" style="position:sticky;top:24px;padding:16px">
  <p class="stt-nav-label">Settings</p>
  <a class="stt-nav-link" href="#sending">Sending rules</a>
  <a class="stt-nav-link" href="#costs">Message costs</a>
  <a class="stt-nav-link" href="#retention">Data retention</a>
  <a class="stt-nav-link" href="#meta">Meta status</a>
</nav>

<div style="display:flex;flex-direction:column;gap:var(--gutter)">

<section class="card" id="sending">
  <div class="card-header">
    <h2 class="card-title">Sending rules</h2>
  </div>
  <form method="post" action="/settings/sending" style="display:flex;flex-direction:column;gap:16px">
    <div class="form-group">
      <label class="form-label" for="s-quiet-start">Quiet hours start (IST)</label>
      <input id="s-quiet-start" class="form-input" type="time" name="quiet_start" value="%s">
    </div>
    <div class="form-group">
      <label class="form-label" for="s-quiet-end">Quiet hours end (IST)</label>
      <input id="s-quiet-end" class="form-input" type="time" name="quiet_end" value="%s">
    </div>
    <div class="form-group">
      <label class="form-label" for="s-freq-cap">Frequency cap (hours)</label>
      <input id="s-freq-cap" class="form-input" type="number" name="freq_cap_hours" value="%d" min="1" max="168">
    </div>
    <div>
      <button class="btn btn-primary btn-sm" type="submit">Save sending rules</button>
    </div>
  </form>
</section>

<section class="card" id="costs">
  <div class="card-header">
    <h2 class="card-title">Message costs <span style="font-size:12px;font-weight:500;color:var(--text-muted)">(INR, excl. GST)</span></h2>
  </div>
  <form method="post" action="/settings/costs" style="display:flex;flex-direction:column;gap:16px">
    <div class="form-group">
      <label class="form-label" for="s-mkt">Marketing / message</label>
      <input id="s-mkt" class="form-input" type="number" step="0.0001" name="marketing" value="%.4f">
    </div>
    <div class="form-group">
      <label class="form-label" for="s-util">Utility / message</label>
      <input id="s-util" class="form-input" type="number" step="0.0001" name="utility" value="%.4f">
    </div>
    <div class="form-group">
      <label class="form-label" for="s-auth">Auth / message</label>
      <input id="s-auth" class="form-input" type="number" step="0.0001" name="auth" value="%.4f">
    </div>
    <div class="form-group">
      <label class="form-label" for="s-gst">GST rate</label>
      <input id="s-gst" class="form-input" type="number" step="0.01" name="gst_rate" value="%.4f">
    </div>
    <div>
      <button class="btn btn-primary btn-sm" type="submit">Save costs</button>
    </div>
  </form>
</section>

<section class="card" id="retention">
  <div class="card-header">
    <h2 class="card-title">Data retention</h2>
  </div>
  <form method="post" action="/settings/retention" style="display:flex;flex-direction:column;gap:16px">
    <div class="form-group">
      <label class="form-label" for="s-ret">Retention period (months)</label>
      <input id="s-ret" class="form-input" type="number" name="retention_months" value="%d" min="1" max="84">
      <p style="font-size:12px;color:var(--text-muted);margin:2px 0 0">Conversations and messages older than this are purged.</p>
    </div>
    <div>
      <button class="btn btn-primary btn-sm" type="submit">Save retention</button>
    </div>
  </form>
</section>

<section class="card" id="meta">
  <div class="card-header">
    <h2 class="card-title">Meta status</h2>
  </div>
  <div style="display:flex;flex-direction:column;gap:0">
    <div class="stt-info-row">
      <span class="stt-info-key">WhatsApp display name</span>
      <span class="stt-info-val">%s</span>
    </div>
    <div class="stt-info-row">
      <span class="stt-info-key">Messaging tier</span>
      <span class="stt-info-val">Tier %d</span>
    </div>
    <div class="stt-info-row" style="border-bottom:none">
      <span class="stt-info-key">Quality rating</span>
      <span class="stt-info-val" style="color:%s;font-weight:600">%s</span>
    </div>
  </div>
</section>

</div>
</div>
</div>`,
			savedBanner,
			d.QuietHoursStart, d.QuietHoursEnd, d.FreqCapHours,
			d.MarketingCost, d.UtilityCost, d.AuthCost, d.GSTRate,
			d.RetentionMonths,
			d.WADisplayName,
			d.MetaTier, qualityColor, d.QualityRating)
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, ShellClose())
		return err
	})
}
