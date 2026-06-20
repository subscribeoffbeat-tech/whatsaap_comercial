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
			savedBanner = `<div class="toast toast--success">Settings saved.</div>`
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
<section class="card">
<h2>Sending rules</h2>
<form method="post" action="/settings/sending">
<label class="field"><span>Quiet hours start (IST)</span>
<input type="time" name="quiet_start" value="%s"></label>
<label class="field"><span>Quiet hours end (IST)</span>
<input type="time" name="quiet_end" value="%s"></label>
<label class="field"><span>Frequency cap (hours)</span>
<input type="number" name="freq_cap_hours" value="%d" min="1" max="168"></label>
<button class="btn btn-primary btn-sm" type="submit">Save sending rules</button>
</form>
</section>

<section class="card">
<h2>Message costs (INR, excl. GST)</h2>
<form method="post" action="/settings/costs">
<label class="field"><span>Marketing / message</span>
<input type="number" step="0.0001" name="marketing" value="%.4f"></label>
<label class="field"><span>Utility / message</span>
<input type="number" step="0.0001" name="utility" value="%.4f"></label>
<label class="field"><span>Auth / message</span>
<input type="number" step="0.0001" name="auth" value="%.4f"></label>
<label class="field"><span>GST rate</span>
<input type="number" step="0.01" name="gst_rate" value="%.4f"></label>
<button class="btn btn-primary btn-sm" type="submit">Save costs</button>
</form>
</section>

<section class="card">
<h2>Data retention</h2>
<form method="post" action="/settings/retention">
<label class="field"><span>Retention (months)</span>
<input type="number" name="retention_months" value="%d" min="1" max="84"></label>
<button class="btn btn-primary btn-sm" type="submit">Save retention</button>
</form>
</section>

<section class="card">
<h2>Meta status</h2>
<dl>
<dt>Tier</dt><dd>%d</dd>
<dt>Quality</dt><dd>%s</dd>
<dt>WhatsApp display name</dt><dd>%s</dd>
</dl>
</section>
</div>`,
			savedBanner,
			d.QuietHoursStart, d.QuietHoursEnd, d.FreqCapHours,
			d.MarketingCost, d.UtilityCost, d.AuthCost, d.GSTRate,
			d.RetentionMonths,
			d.MetaTier, d.QualityRating, d.WADisplayName)
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, ShellClose())
		return err
	})
}
