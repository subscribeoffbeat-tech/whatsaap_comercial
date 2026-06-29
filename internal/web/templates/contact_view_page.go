package templates

import (
	"context"
	"fmt"
	"html"
	"io"
	"time"

	"github.com/a-h/templ"

	"whatsapptool/internal/db"
	mw "whatsapptool/internal/web/middleware"
)

// ActivityItem is one entry in a contact's activity timeline.
type ActivityItem struct {
	Kind   string // inbound | outbound | campaign | note | created
	Title  string
	Body   string
	Status string
	At     time.Time
}

// activityDot returns the coloured timeline dot for a kind.
func activityDot(kind string) string {
	color := "#cbd5e1"
	switch kind {
	case "inbound":
		color = "#93c5fd"
	case "outbound":
		color = "#34d399"
	case "campaign":
		color = "#c4b5fd"
	case "note":
		color = "#fcd34d"
	}
	return fmt.Sprintf(`<span style="flex-shrink:0;width:30px;height:30px;border-radius:50%%;background:%s33;display:flex;align-items:center;justify-content:center;margin-top:2px"><span style="width:9px;height:9px;border-radius:50%%;background:%s"></span></span>`, color, color)
}

// ContactViewPage renders the full contact profile page with Activity, Campaigns
// and Notes tabs — all backed by real data.
func ContactViewPage(
	agent *mw.AgentClaims,
	c *db.Contact, tags []db.Tag, allTags []db.Tag,
	activity []ActivityItem, campaigns []db.ContactCampaign, notes []db.ContactNote,
) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/contacts", "Contact", "")); err != nil {
			return err
		}

		initials := contactAvatarInitials(c.Name, c.WAPhone)
		grad := avatarGradient(c.ID)
		displayName := c.Name
		if displayName == "" {
			displayName = c.WAPhone
		}
		optedBadge := `<span class="ct-badge ct-badge-off">Not opted in</span>`
		if c.OptedIn {
			optedBadge = `<span class="ct-badge ct-badge-on">Opted in</span>`
		}
		lastMsg := "—"
		if c.LastMessageAt != nil {
			lastMsg = humanizeSince(*c.LastMessageAt)
		}
		consentChecked := ""
		if c.OptedIn {
			consentChecked = " checked"
		}

		// Subtitle under the name: prefer the contact's city (what the forms
		// collect), then fall back to industry, then a dash.
		subLine := ""
		if v, ok := c.CustomFields["city"]; ok {
			if s, ok := v.(string); ok {
				subLine = s
			}
		}
		if subLine == "" {
			subLine = c.Industry
		}
		subLine = orDash(subLine)

		// ── Header ───────────────────────────────────────────────────────────
		if _, err := fmt.Fprintf(w, `
<div class="cv-wrap" x-data="{tab:'activity'}">
<div class="cv-top">
  <a href="/contacts" class="cv-back" aria-label="Back to contacts">&#8592;</a>
  <h1 class="screen-title" style="margin:0">Contact</h1>
  <div class="cv-top-actions">
    <a class="btn btn-secondary btn-sm" href="/contacts/%s/edit-page">&#9998; Edit</a>
    <a class="btn btn-primary btn-sm" href="/inbox">&#9993; Send message</a>
  </div>
</div>
<div class="cv-grid">
<aside class="cv-side">
  <div class="card-static cv-id-card">
    <div class="cv-avatar %s">%s</div>
    <div class="cv-name">%s</div>
    <div class="cv-sub">%s</div>
    <div style="margin-top:8px">%s</div>
  </div>`,
			c.ID,
			html.EscapeString(grad), html.EscapeString(initials),
			html.EscapeString(displayName),
			html.EscapeString(subLine),
			optedBadge,
		); err != nil {
			return err
		}

		// Contact info card (phone, custom fields, last message)
		var infoRows string
		infoRows += cvInfoRow("Phone", c.WAPhone)
		if c.Email != nil && *c.Email != "" {
			infoRows += cvInfoRow("Email", *c.Email)
		}
		if c.Industry != "" {
			infoRows += cvInfoRow("Industry", c.Industry)
		}
		for k, v := range c.CustomFields {
			if s, ok := v.(string); ok && s != "" {
				infoRows += cvInfoRow(titleCase(k), s)
			}
		}
		infoRows += cvInfoRow("Last message", lastMsg)
		if _, err := fmt.Fprintf(w, `
  <div class="card-static cv-info-card">
    <div class="ct-detail-label">CONTACT INFO</div>
    %s
  </div>`, infoRows); err != nil {
			return err
		}

		// Tags card
		if _, err := fmt.Fprintf(w, `
  <div class="card-static cv-tags-card">
    <div class="ct-detail-label">TAGS</div>
    <div id="contact-tags-%s" aria-live="polite">`, c.ID); err != nil {
			return err
		}
		if err := ContactTagList(tags, allTags, c.ID).Render(ctx, w); err != nil {
			return err
		}
		if _, err := io.WriteString(w, `</div></div>`); err != nil {
			return err
		}

		// Marketing consent toggle (functional)
		if _, err := fmt.Fprintf(w, `
  <div class="card-static cv-consent-card">
    <div>
      <div class="cv-consent-title">Marketing consent</div>
      <div class="cv-consent-sub">WhatsApp messaging permission</div>
    </div>
    <label class="cv-switch">
      <input type="checkbox" name="opted_in"%s hx-post="/contacts/%s/consent" hx-trigger="change" hx-swap="none">
      <span class="cv-slider"></span>
    </label>
  </div>
</aside>`, consentChecked, c.ID); err != nil {
			return err
		}

		// ── Right: tabs ──────────────────────────────────────────────────────
		if _, err := io.WriteString(w, `
<section class="cv-main">
<div class="cv-tabs">
  <button class="cv-tab" :class="{'cv-tab--on':tab==='activity'}" @click="tab='activity'" type="button">Activity</button>
  <button class="cv-tab" :class="{'cv-tab--on':tab==='campaigns'}" @click="tab='campaigns'" type="button">Campaigns</button>
  <button class="cv-tab" :class="{'cv-tab--on':tab==='notes'}" @click="tab='notes'" type="button">Notes</button>
</div>`); err != nil {
			return err
		}

		// Activity tab
		if _, err := io.WriteString(w, `<div x-show="tab==='activity'" class="cv-panel"><div class="cv-feed">`); err != nil {
			return err
		}
		if len(activity) == 0 {
			io.WriteString(w, `<p class="cv-empty">No activity yet.</p>`)
		}
		for _, a := range activity {
			if err := writeActivityItem(w, a); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(w, `</div></div>`); err != nil {
			return err
		}

		// Campaigns tab
		if _, err := io.WriteString(w, `<div x-show="tab==='campaigns'" x-cloak class="cv-panel">`); err != nil {
			return err
		}
		if len(campaigns) == 0 {
			io.WriteString(w, `<p class="cv-empty">This contact hasn't been part of any campaign yet.</p>`)
		} else {
			io.WriteString(w, `<table class="tbl cv-camp-tbl"><thead><tr><th>CAMPAIGN</th><th>TEMPLATE</th><th>DATE</th><th>STATUS</th></tr></thead><tbody>`)
			for _, cp := range campaigns {
				tmpl := cp.TemplateName
				if tmpl == "" {
					tmpl = "—"
				}
				fmt.Fprintf(w,
					`<tr><td>%s</td><td><code class="cv-code">%s</code></td><td>%s</td><td>%s</td></tr>`,
					html.EscapeString(cp.CampaignName),
					html.EscapeString(tmpl),
					istFmt(cp.SentAt, "02 Jan 2006"),
					statusPill(cp.Status, cp.SkipReason),
				)
			}
			io.WriteString(w, `</tbody></table>`)
		}
		if _, err := io.WriteString(w, `</div>`); err != nil {
			return err
		}

		// Notes tab
		if _, err := fmt.Fprintf(w, `<div x-show="tab==='notes'" x-cloak class="cv-panel">
<form hx-post="/contacts/%s/notes" hx-target="#contact-notes-%s" hx-swap="innerHTML" hx-on::after-request="this.reset()" style="margin-bottom:14px">
<textarea name="body" rows="3" placeholder="Add an internal note about this contact..." required style="width:100%%;border:1px solid var(--border,#e2e8f0);border-radius:8px;padding:10px;resize:vertical;font:inherit"></textarea>
<div style="text-align:right;margin-top:8px"><button class="btn btn-sm btn-primary" type="submit">Add note</button></div>
</form>
<div id="contact-notes-%s" aria-live="polite">`, c.ID, c.ID, c.ID); err != nil {
			return err
		}
		if err := ContactNoteList(notes).Render(ctx, w); err != nil {
			return err
		}
		if _, err := io.WriteString(w, `</div></div></section></div></div>`); err != nil {
			return err
		}

		_, err := io.WriteString(w, ShellClose())
		return err
	})
}

func writeActivityItem(w io.Writer, a ActivityItem) error {
	when := istFmt(a.At, "2 Jan, 3:04 PM")
	switch a.Kind {
	case "inbound", "outbound":
		bubbleStyle := "background:#f1f5f9"
		align := "flex-start"
		if a.Kind == "outbound" {
			bubbleStyle = "background:#dcfce7"
			align = "flex-start"
		}
		body := a.Body
		if body == "" {
			body = "(media message)"
		}
		_, err := fmt.Fprintf(w,
			`<div class="cv-feed-row">%s<div style="display:flex;flex-direction:column;align-items:%s;flex:1;min-width:0"><div class="cv-bubble" style="%s">%s</div><span class="cv-time">%s · %s</span></div></div>`,
			activityDot(a.Kind), align, bubbleStyle, html.EscapeString(body), msgStatusLabel(a.Status), when)
		return err
	default:
		body := ""
		if a.Body != "" {
			body = `<div class="cv-feed-body">` + html.EscapeString(a.Body) + `</div>`
		}
		_, err := fmt.Fprintf(w,
			`<div class="cv-feed-row">%s<div style="flex:1;min-width:0"><div class="cv-feed-title">%s</div>%s<span class="cv-time">%s</span></div></div>`,
			activityDot(a.Kind), html.EscapeString(a.Title), body, when)
		return err
	}
}

func cvInfoRow(key, val string) string {
	return `<div class="cv-info-row"><span class="cv-info-key">` + html.EscapeString(key) +
		`</span><span class="cv-info-val">` + html.EscapeString(val) + `</span></div>`
}

// statusPill renders a delivery/skip status as a coloured pill.
func statusPill(status, skipReason string) string {
	s := status
	cls := "cv-pill cv-pill--gray"
	switch status {
	case "read":
		cls = "cv-pill cv-pill--green"
	case "delivered":
		cls = "cv-pill cv-pill--blue"
	case "sent":
		cls = "cv-pill cv-pill--gray"
	case "failed":
		cls = "cv-pill cv-pill--red"
	case "skipped":
		cls = "cv-pill cv-pill--amber"
		if skipReason != "" {
			s = "skipped"
		}
	}
	return `<span class="` + cls + `">` + html.EscapeString(s) + `</span>`
}

func msgStatusLabel(s string) string {
	switch s {
	case "read":
		return "Read ✓✓"
	case "delivered":
		return "Delivered ✓✓"
	case "sent":
		return "Sent ✓"
	case "failed":
		return "Failed"
	}
	return "Received"
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] -= 32
	}
	return string(r)
}

// humanizeSince renders the last-message time. (Relative "2h ago" needs the
// current time, which we avoid here; the absolute timestamp is unambiguous.)
func humanizeSince(t time.Time) string {
	return istFmt(t, "02 Jan, 3:04 PM")
}
