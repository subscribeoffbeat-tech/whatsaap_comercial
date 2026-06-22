package templates

import (
	"context"
	"fmt"
	"html"
	"io"
	"strings"

	"github.com/a-h/templ"

	"whatsapptool/internal/db"
	mw "whatsapptool/internal/web/middleware"
)

var indianCities = []string{
	"Ahmedabad", "Bengaluru", "Bhopal", "Chennai", "Coimbatore",
	"Delhi", "Faridabad", "Ghaziabad", "Gurugram", "Hyderabad",
	"Indore", "Jaipur", "Kanpur", "Kochi", "Kolkata",
	"Lucknow", "Ludhiana", "Mumbai", "Nagpur", "Nashik",
	"Patna", "Pune", "Rajkot", "Surat", "Thiruvananthapuram",
	"Vadodara", "Varanasi", "Visakhapatnam",
}

func NewContactPage(agent *mw.AgentClaims, tags []db.Tag, errMsg string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/contacts", "New Contact", "")); err != nil {
			return err
		}

		// City options
		cityOpts := `<option value="">Select city...</option>`
		for _, c := range indianCities {
			cityOpts += fmt.Sprintf(`<option value="%s">%s</option>`, html.EscapeString(c), html.EscapeString(c))
		}

		// Tag pill buttons
		tagPillsHTML := ""
		for _, t := range tags {
			tagPillsHTML += fmt.Sprintf(
				`<button type="button" class="nc-tag-pill" :class="{'nc-tag-pill--on':tags.includes('%s')}" @click="toggleTag('%s')">%s</button>`,
				jsStr(t.Name), jsStr(t.Name), html.EscapeString(t.Name),
			)
		}
		if tagPillsHTML == "" {
			tagPillsHTML = `<span style="color:var(--text-secondary);font-size:13px">No tags yet — create tags in the contacts list first.</span>`
		}

		errHTML := ""
		if errMsg != "" {
			errHTML = fmt.Sprintf(`<div class="nc-error-banner">%s</div>`, html.EscapeString(errMsg))
		}

		_, err := fmt.Fprintf(w, `
<div class="page-wrap nc-pg" x-data="{
  name: '',
  phone: '+91',
  city: '',
  optedIn: true,
  tags: [],
  get initials() {
    const p = this.name.trim().split(/\s+/).filter(Boolean);
    if (!p.length) return '?';
    let i = p[0].charAt(0);
    if (p.length > 1) i += p[p.length-1].charAt(0);
    return i.toUpperCase();
  },
  toggleTag(n) {
    const i = this.tags.indexOf(n);
    if (i >= 0) this.tags.splice(i,1); else this.tags.push(n);
  }
}">

%s

<div class="nc-hd">
  <a href="/contacts" class="nc-back-btn">
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none"><path d="M10 3L5 8l5 5" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/></svg>
  </a>
  <div>
    <div class="screen-title">New Contact</div>
    <div class="screen-subtitle">Add a contact to your directory</div>
  </div>
</div>

<div class="nc-body">
  <!-- Form card -->
  <div class="card nc-form-card">
    <form method="POST" action="/contacts/new">

      <!-- Row 1: Name + Phone -->
      <div class="nc-row">
        <div class="form-group">
          <label class="form-label">Full name <span class="nc-req">*</span></label>
          <input class="form-input" type="text" name="name" x-model="name"
            placeholder="Priya Sharma" required autocomplete="off">
        </div>
        <div class="form-group">
          <label class="form-label">WhatsApp number <span class="nc-req">*</span></label>
          <input class="form-input" type="tel" name="phone" x-model="phone"
            placeholder="+91 98765 43210" required>
        </div>
      </div>

      <!-- Row 2: Email + City -->
      <div class="nc-row">
        <div class="form-group">
          <label class="form-label">Email <span class="nc-opt">(optional)</span></label>
          <input class="form-input" type="email" name="email" placeholder="priya@example.com">
        </div>
        <div class="form-group">
          <label class="form-label">City <span class="nc-opt">(optional)</span></label>
          <select class="form-input" name="city" x-model="city">%s</select>
        </div>
      </div>

      <!-- Tags -->
      <div class="form-group">
        <label class="form-label">Tags <span class="nc-opt">(optional)</span></label>
        <div class="nc-tag-pills">%s</div>
        <input type="hidden" name="tags" :value="tags.join(',')">
      </div>

      <!-- Marketing consent -->
      <label class="nc-consent-row">
        <div>
          <div class="nc-consent-title">Marketing consent</div>
          <div class="nc-consent-desc">Contact has opted in to receive WhatsApp messages</div>
        </div>
        <input class="acct-toggle-cb" type="checkbox" name="opt_in" value="on" x-model="optedIn">
      </label>

      <!-- Notes -->
      <div class="form-group" style="margin-top:20px">
        <label class="form-label">Notes <span class="nc-opt">(optional)</span></label>
        <textarea class="form-input nc-notes" name="notes"
          placeholder="Internal notes about this contact..." rows="4"></textarea>
      </div>

      <!-- Footer -->
      <div class="nc-footer">
        <a href="/contacts" class="btn btn-secondary">Cancel</a>
        <button class="btn btn-primary" type="submit"
          :disabled="!name.trim() || phone.trim().length < 5"
          :class="{'btn-disabled':!name.trim() || phone.trim().length < 5}">
          Save contact
        </button>
      </div>

    </form>
  </div>

  <!-- Preview card -->
  <div class="nc-preview-card">
    <div class="nc-preview-label">CONTACT PREVIEW</div>
    <div class="nc-preview-body">
      <div class="nc-preview-av" x-text="initials"></div>
      <div>
        <div class="nc-preview-name" x-text="name.trim() || 'Full name'"></div>
        <div class="nc-preview-city" x-text="city || 'City'"></div>
      </div>
    </div>
    <div class="nc-preview-phone" x-text="phone || '+91'"></div>
    <div class="nc-preview-consent">
      <span class="nc-preview-dot" :class="optedIn ? 'nc-preview-dot--in' : 'nc-preview-dot--out'"></span>
      <span x-text="optedIn ? 'Opted in' : 'Opted out'"
        :style="optedIn ? 'color:var(--accent)' : 'color:var(--danger)'"></span>
    </div>
  </div>
</div>
</div>
`, errHTML, cityOpts, tagPillsHTML)
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, ShellClose())
		return err
	})
}

// jsStr escapes a Go string for safe embedding as a JS string literal (single-quoted).
func jsStr(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return s
}
