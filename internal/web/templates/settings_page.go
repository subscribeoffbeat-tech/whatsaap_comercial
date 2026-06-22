package templates

import (
	"context"
	"fmt"
	"html"
	"io"
	"strings"

	"github.com/a-h/templ"

	mw "whatsapptool/internal/web/middleware"
)

// SettingsViewData holds all editable settings values for the settings page.
type SettingsViewData struct {
	// Connection / Meta status
	WADisplayPhone string
	WADisplayName  string
	WAWABAId       string
	WAAPIVersion   string
	QualityRating  string
	MetaTier       int64

	// Quiet hours
	QuietHoursStart string
	QuietHoursEnd   string
	FreqCapHours    int64

	// Message rates
	MarketingCost float64
	UtilityCost   float64
	AuthCost      float64
	GSTRate       float64

	// Data retention
	RetentionMonths int64

	// Business profile
	BizName     string
	BizAbout    string
	BizAddress  string
	BizIndustry string

	// Opt-out keywords
	StopKeywords []string

	// Active tab (from URL ?tab=...)
	ActiveTab string
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

		// Quality display
		qualityLabel := d.QualityRating
		qualityColorVar := "var(--text-muted)"
		qualityDot := ""
		switch strings.ToUpper(d.QualityRating) {
		case "GREEN", "HIGH":
			qualityLabel = "High"
			qualityColorVar = "var(--success-strong)"
			qualityDot = `<span style="display:inline-block;width:10px;height:10px;border-radius:50%;background:var(--success-strong);margin-right:6px"></span>`
		case "YELLOW", "MEDIUM":
			qualityLabel = "Medium"
			qualityColorVar = "var(--warning-text)"
			qualityDot = `<span style="display:inline-block;width:10px;height:10px;border-radius:50%;background:var(--warning-text);margin-right:6px"></span>`
		case "RED", "LOW":
			qualityLabel = "Low"
			qualityColorVar = "var(--danger-text)"
			qualityDot = `<span style="display:inline-block;width:10px;height:10px;border-radius:50%;background:var(--danger-text);margin-right:6px"></span>`
		default:
			qualityLabel = "Unknown"
		}

		tierLabel := fmt.Sprintf("Tier %d", d.MetaTier)
		tierCap := tierMsgCap(d.MetaTier)

		// Quiet hours bar
		quietBarHTML := buildQuietBar(d.QuietHoursStart, d.QuietHoursEnd)

		// Keywords HTML
		kwHTML := buildKeywordsHTML(d.StopKeywords)

		// Industry options
		industries := []string{"Automotive", "Beauty", "Education", "Entertainment", "Finance",
			"Food & Beverage", "Government", "Healthcare", "Hospitality",
			"Media", "Non-profit", "Real Estate", "Retail", "Technology",
			"Telecommunications", "Travel", "Other"}
		industryOpts := ""
		for _, ind := range industries {
			sel := ""
			if ind == d.BizIndustry {
				sel = ` selected`
			}
			industryOpts += fmt.Sprintf(`<option value="%s"%s>%s</option>`, html.EscapeString(ind), sel, html.EscapeString(ind))
		}

		// Connection fields (read-only display)
		phoneDisplay := d.WADisplayPhone
		if phoneDisplay == "" {
			phoneDisplay = "Not configured"
		}
		wabaDisplay := d.WAWABAId
		if wabaDisplay == "" {
			wabaDisplay = "Not configured"
		}

		_, err := fmt.Fprintf(w, `
<div class="page-wrap">
<div class="page-hd">
  <div>
    <h1 class="screen-title">Settings</h1>
    <p class="screen-subtitle">Configure your WhatsApp Business account</p>
  </div>
</div>
%s
<div class="stt-layout" x-data="{tab:'%s'}">

<!-- Left nav -->
<nav class="card-static stt-nav">
  <button class="stt-nav-link" :class="{active:tab==='connection'}"    @click="tab='connection'">Connection</button>
  <button class="stt-nav-link" :class="{active:tab==='quiet-hours'}"   @click="tab='quiet-hours'">Quiet hours</button>
  <button class="stt-nav-link" :class="{active:tab==='rates'}"         @click="tab='rates'">Rates</button>
  <button class="stt-nav-link" :class="{active:tab==='profile'}"       @click="tab='profile'">Business profile</button>
  <button class="stt-nav-link" :class="{active:tab==='opt-out'}"       @click="tab='opt-out'">Opt-out</button>
</nav>

<!-- Right panel -->
<div class="stt-panel">

<!-- ── Connection ─────────────────────────────────────────────────── -->
<div x-show="tab==='connection'" class="card-static">
  <h2 class="stt-section-title">WhatsApp connection</h2>
  <div class="stt-fields">
    <div class="stt-field">
      <label class="stt-field-label">Phone number</label>
      <div class="stt-field-display">%s</div>
    </div>
    <div class="stt-field">
      <label class="stt-field-label">Business account ID</label>
      <div class="stt-field-display">%s</div>
    </div>
    <div class="stt-field">
      <label class="stt-field-label">API version</label>
      <div class="stt-field-display">%s</div>
    </div>
    <div class="stt-field">
      <label class="stt-field-label">Quality rating</label>
      <div class="stt-field-display" style="color:%s;font-weight:600">%s%s <span style="font-size:13px;font-weight:400;color:var(--text-muted);margin-left:8px">Meta phone quality</span></div>
    </div>
    <div class="stt-field">
      <label class="stt-field-label">Tier</label>
      <div class="stt-field-display">%s (%s)</div>
    </div>
  </div>
  <div style="margin-top:20px;padding-top:16px;border-top:1px solid var(--border-light)">
    <p style="font-size:12.5px;color:var(--text-muted)">Connection parameters are set via environment variables (<code>WA_PHONE_NUMBER_ID</code>, <code>WA_WABA_ID</code>, <code>WA_ACCESS_TOKEN</code>). Quality rating and tier update automatically via Meta webhooks.</p>
  </div>
</div>

<!-- ── Quiet hours ─────────────────────────────────────────────────── -->
<div x-show="tab==='quiet-hours'" class="card-static" style="display:none">
  <h2 class="stt-section-title">Quiet hours</h2>
  <p style="color:var(--text-secondary);font-size:14px;margin:0 0 24px">No outbound messages will be sent during quiet hours.</p>
  <form method="post" action="/settings/sending">
    <div class="stt-time-row">
      <div class="form-group" style="flex:1">
        <label class="form-label" for="s-quiet-start">Start time</label>
        <input id="s-quiet-start" class="form-input" type="time" name="quiet_start" value="%s"
          x-on:change="updateQuietBar($event.target.value, document.getElementById('s-quiet-end').value)">
      </div>
      <div class="form-group" style="flex:1">
        <label class="form-label" for="s-quiet-end">End time</label>
        <input id="s-quiet-end" class="form-input" type="time" name="quiet_end" value="%s"
          x-on:change="updateQuietBar(document.getElementById('s-quiet-start').value, $event.target.value)">
      </div>
    </div>
    %s
    <div style="margin-top:20px">
      <button class="btn btn-primary" type="submit">Save quiet hours</button>
    </div>
  </form>
</div>

<!-- ── Rates ─────────────────────────────────────────────────────── -->
<div x-show="tab==='rates'" class="card-static" style="display:none">
  <h2 class="stt-section-title">Message rates</h2>
  <p style="color:var(--text-secondary);font-size:14px;margin:0 0 20px">Per-message pricing by conversation category (excl. %.0f%% GST).</p>
  <form method="post" action="/settings/costs">
    <div class="stt-rate-list">
      <div class="stt-rate-row">
        <span class="stt-rate-label">Marketing</span>
        <div class="stt-rate-input-wrap">
          <span class="stt-rate-currency">&#8377;</span>
          <input class="form-input stt-rate-input" type="number" step="0.0001" name="marketing" value="%.4f">
          <span class="stt-rate-unit">/ message</span>
        </div>
      </div>
      <div class="stt-rate-row">
        <span class="stt-rate-label">Utility</span>
        <div class="stt-rate-input-wrap">
          <span class="stt-rate-currency">&#8377;</span>
          <input class="form-input stt-rate-input" type="number" step="0.0001" name="utility" value="%.4f">
          <span class="stt-rate-unit">/ message</span>
        </div>
      </div>
      <div class="stt-rate-row">
        <span class="stt-rate-label">Authentication</span>
        <div class="stt-rate-input-wrap">
          <span class="stt-rate-currency">&#8377;</span>
          <input class="form-input stt-rate-input" type="number" step="0.0001" name="auth" value="%.4f">
          <span class="stt-rate-unit">/ message</span>
        </div>
      </div>
      <div class="stt-rate-row">
        <span class="stt-rate-label">Service (incoming)</span>
        <div class="stt-rate-input-wrap">
          <span class="stt-rate-currency">&#8377;</span>
          <input class="form-input stt-rate-input" type="text" value="0.00" disabled style="color:var(--text-muted)">
          <span class="stt-rate-unit">/ message</span>
        </div>
      </div>
    </div>
    <input type="hidden" name="gst_rate" value="%.4f">
    <div style="margin-top:20px">
      <button class="btn btn-primary" type="submit">Save rates</button>
    </div>
  </form>
</div>

<!-- ── Business profile ────────────────────────────────────────────── -->
<div x-show="tab==='profile'" class="card-static" style="display:none">
  <h2 class="stt-section-title">Business profile</h2>
  <form method="post" action="/settings/profile" style="display:flex;flex-direction:column;gap:18px">
    <div class="form-group">
      <label class="form-label" for="s-biz-name">Business name</label>
      <input id="s-biz-name" class="form-input" type="text" name="biz_name" value="%s" placeholder="Your business name">
    </div>
    <div class="form-group">
      <label class="form-label" for="s-biz-about">About</label>
      <textarea id="s-biz-about" class="form-input" name="biz_about" rows="3" placeholder="Short description of your business" style="resize:vertical">%s</textarea>
    </div>
    <div class="form-group">
      <label class="form-label" for="s-biz-address">Address</label>
      <input id="s-biz-address" class="form-input" type="text" name="biz_address" value="%s" placeholder="City, State, Country">
    </div>
    <div class="form-group">
      <label class="form-label" for="s-biz-industry">Industry</label>
      <select id="s-biz-industry" class="form-input" name="biz_industry">
        <option value="">Select industry…</option>
        %s
      </select>
    </div>
    <div>
      <button class="btn btn-primary" type="submit">Save profile</button>
    </div>
  </form>
</div>

<!-- ── Opt-out keywords ────────────────────────────────────────────── -->
<div x-show="tab==='opt-out'" class="card-static" style="display:none">
  <h2 class="stt-section-title">Opt-out keywords</h2>
  <p style="color:var(--text-secondary);font-size:14px;margin:0 0 20px">Contacts who send these words are automatically opted out.</p>
  <div class="stt-kw-list" id="stt-kw-list">
    %s
  </div>
  <form method="post" action="/settings/keywords" class="stt-kw-add">
    <input class="form-input" type="text" name="keyword" placeholder="Add keyword…" style="flex:1">
    <button class="btn btn-primary" type="submit">Add</button>
  </form>
  <p style="font-size:12px;color:var(--text-muted);margin-top:12px">STOP and UNSUBSCRIBE are always enforced (required by Meta) and cannot be removed.</p>
</div>

</div><!-- .stt-panel -->
</div><!-- .stt-layout -->
</div><!-- .page-wrap -->

<script>
function updateQuietBar(start, end) {
  var bar = document.getElementById('quiet-bar');
  if (!bar || !start || !end) return;
  var sh = parseInt(start.split(':')[0], 10);
  var eh = parseInt(end.split(':')[0], 10);
  var label = start.replace(':','') > '1200'
    ? (parseInt(start) > 12 ? (sh-12)+'PM' : sh+'AM')
    : sh+'AM';
  // quiet crosses midnight: start > end
  var label = fmtHour(sh) + ' – ' + fmtHour(eh) + ' IST (quiet)';
  bar.querySelector('.quiet-bar-label').textContent = label;
}
function fmtHour(h) {
  if (h === 0 || h === 24) return '12AM';
  if (h === 12) return '12PM';
  return h > 12 ? (h-12)+'PM' : h+'AM';
}
</script>`,
			savedBanner,
			html.EscapeString(d.ActiveTab),
			html.EscapeString(phoneDisplay),
			html.EscapeString(wabaDisplay),
			html.EscapeString(d.WAAPIVersion),
			qualityColorVar, qualityDot, html.EscapeString(qualityLabel),
			html.EscapeString(tierLabel), html.EscapeString(tierCap),
			// quiet hours
			d.QuietHoursStart, d.QuietHoursEnd,
			quietBarHTML,
			// rates
			d.GSTRate*100,
			d.MarketingCost, d.UtilityCost, d.AuthCost,
			d.GSTRate,
			// profile
			html.EscapeString(d.BizName),
			html.EscapeString(d.BizAbout),
			html.EscapeString(d.BizAddress),
			industryOpts,
			// opt-out keywords
			kwHTML,
		)
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, ShellClose())
		return err
	})
}

func buildQuietBar(start, end string) string {
	sh := parseHour(start)
	eh := parseHour(end)
	label := fmt.Sprintf("%s – %s IST (quiet)", formatHour(sh), formatHour(eh))

	// quiet period percentage (period crosses midnight when sh > eh)
	var quietPct int
	if sh > eh {
		quietPct = ((24 - sh) + eh) * 100 / 24
	} else {
		quietPct = (eh - sh) * 100 / 24
	}
	activePct := 100 - quietPct

	return fmt.Sprintf(`<div class="quiet-bar" id="quiet-bar">
  <div class="quiet-bar-quiet" style="flex:%d">
    <span class="quiet-bar-label">%s</span>
  </div>
  <div class="quiet-bar-active" style="flex:%d"></div>
</div>`, quietPct, html.EscapeString(label), activePct)
}

func buildKeywordsHTML(kws []string) string {
	if len(kws) == 0 {
		return `<p style="color:var(--text-muted);font-size:13px">No keywords configured.</p>`
	}
	var sb strings.Builder
	for _, kw := range kws {
		isRequired := kw == "STOP" || kw == "UNSUBSCRIBE"
		escapedKw := html.EscapeString(kw)
		if isRequired {
			sb.WriteString(fmt.Sprintf(`<span class="stt-kw-chip stt-kw-chip--locked">%s</span>`, escapedKw))
		} else {
			sb.WriteString(fmt.Sprintf(
				`<span class="stt-kw-chip">%s`+
					`<form method="post" action="/settings/keywords/remove" style="display:inline;margin:0">`+
					`<input type="hidden" name="keyword" value="%s">`+
					`<button type="submit" class="stt-kw-remove" title="Remove">&#x2715;</button>`+
					`</form></span>`,
				escapedKw, escapedKw,
			))
		}
	}
	return sb.String()
}

func parseHour(t string) int {
	if len(t) >= 2 {
		h := 0
		for _, c := range t {
			if c == ':' {
				break
			}
			h = h*10 + int(c-'0')
		}
		return h
	}
	return 0
}

func formatHour(h int) string {
	switch {
	case h == 0 || h == 24:
		return "12AM"
	case h == 12:
		return "12PM"
	case h > 12:
		return fmt.Sprintf("%dPM", h-12)
	default:
		return fmt.Sprintf("%dAM", h)
	}
}

func tierMsgCap(tier int64) string {
	caps := map[int64]string{
		0: "250 msg/day", 1: "1K msg/day", 2: "10K msg/day",
		3: "100K msg/day", 4: "unlimited",
	}
	if c, ok := caps[tier]; ok {
		return c
	}
	return "see Meta dashboard"
}
