package templates

import (
	"context"
	"fmt"
	"html"
	"io"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"whatsapptool/internal/db"
	mw "whatsapptool/internal/web/middleware"
)

// ImportPreviewRow holds one row of the CSV import preview.
type ImportPreviewRow struct {
	Phone string
	Name  string
	Email string
	Error string
	Valid bool
}

func ContactsPage(agent *mw.AgentClaims, tags []db.Tag, industries []string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/contacts", "Contacts", "")); err != nil {
			return err
		}

		// Build industry options for the add-contact modal
		industryOpts := `<option value="">— select industry —</option>`
		for _, ind := range industries {
			industryOpts += fmt.Sprintf(`<option value="%s">%s</option>`, html.EscapeString(ind), html.EscapeString(ind))
		}

		// Build industry list for the collapsible filter
		industryList := ""
		for _, ind := range industries {
			industryList += fmt.Sprintf(`<div style="padding:4px 0;font-size:13px">%s</div>`, html.EscapeString(ind))
		}

		_, err := fmt.Fprintf(w, `
<div class="page-wrap contacts-wrap">
<div class="page-hd">
  <div>
    <div class="screen-title">Contacts</div>
    <div class="screen-subtitle">Manage your contact list and segments</div>
  </div>
  <div style="display:flex;gap:0.5rem;align-items:center">
    <div class="view-toggle" id="view-toggle">
      <button class="view-btn active" id="btn-grid" onclick="setView('grid')" title="Grid view">
        <svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor"><rect x="1" y="1" width="6" height="6" rx="1"/><rect x="9" y="1" width="6" height="6" rx="1"/><rect x="1" y="9" width="6" height="6" rx="1"/><rect x="9" y="9" width="6" height="6" rx="1"/></svg>
      </button>
      <button class="view-btn" id="btn-list" onclick="setView('list')" title="List view">
        <svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor"><rect x="1" y="2" width="14" height="2" rx="1"/><rect x="1" y="7" width="14" height="2" rx="1"/><rect x="1" y="12" width="14" height="2" rx="1"/></svg>
      </button>
    </div>
    <button class="btn btn-secondary btn-sm" onclick="document.getElementById('import-modal').showModal()">Import CSV</button>
    <button class="btn btn-primary btn-sm" onclick="document.getElementById('new-contact-modal').showModal()">+ Add contact</button>
  </div>
</div>

<div style="display:flex;gap:12px;align-items:center;margin-bottom:16px;flex-wrap:wrap">
  <div class="ct-opt-tabs" id="opt-tabs">
    <button class="ct-opt-tab active" onclick="setOptFilter('',event)">All</button>
    <button class="ct-opt-tab" onclick="setOptFilter('true',event)">Opted in</button>
    <button class="ct-opt-tab" onclick="setOptFilter('false',event)">Opted out</button>
  </div>
  <input type="search" placeholder="Search name or phone..."
    hx-get="/contacts/table" hx-trigger="input changed delay:300ms" hx-target="#contacts-table" hx-swap="innerHTML"
    hx-include="#opt-filter-input,#tag-filter-input"
    name="search" class="form-input search-input" style="flex:1;min-width:200px;max-width:360px">
</div>
<input type="hidden" id="opt-filter-input" name="opted_in_filter" value="">
<input type="hidden" id="tag-filter-input" name="tag" value="">

<details style="margin-bottom:8px">
  <summary style="font-size:14px;font-weight:600;padding:10px 0;cursor:pointer;list-style:none;display:flex;align-items:center;justify-content:space-between">
    All tags <span>&#9660;</span>
  </summary>
  <div style="padding:8px 0;font-size:13px;color:var(--text-secondary)">Use the search box above to filter by tag.</div>
</details>

<details style="margin-bottom:16px">
  <summary style="font-size:14px;font-weight:600;padding:10px 0;cursor:pointer;list-style:none;display:flex;align-items:center;justify-content:space-between">
    All industries <span>&#9660;</span>
  </summary>
  <div style="padding:8px 0">%s</div>
</details>

<div id="contacts-table"
  hx-get="/contacts/table"
  hx-trigger="load, contactsUpdated from:body"
  hx-swap="innerHTML">
</div>

<div id="contact-detail"></div>

<dialog id="import-modal">
<h2>Import contacts from CSV</h2>
<form hx-post="/contacts/import/upload" hx-target="#import-steps" hx-swap="innerHTML" enctype="multipart/form-data">
<label class="field"><span>CSV file</span><input type="file" name="csv_file" accept=".csv" required></label>
<div class="modal-btns">
<button class="btn btn-primary btn-sm" type="submit">Upload &amp; map columns</button>
<button class="btn btn-secondary btn-sm" type="button" onclick="document.getElementById('import-modal').close()">Cancel</button>
</div>
</form>
<div id="import-steps"></div>
</dialog>

<dialog id="new-contact-modal" class="modal-dialog">
<div class="modal-gradient-hd" style="background:linear-gradient(135deg,#6c63ff,#a855f7);padding:28px 24px;text-align:center;position:relative;border-radius:12px 12px 0 0">
  <button type="button" onclick="document.getElementById('new-contact-modal').close()"
    style="position:absolute;top:12px;right:16px;background:rgba(255,255,255,0.2);border:none;color:#fff;width:28px;height:28px;border-radius:50%%;cursor:pointer;font-size:16px">&#215;</button>
  <div style="width:60px;height:60px;border-radius:50%%;background:rgba(255,255,255,0.2);display:flex;align-items:center;justify-content:center;margin:0 auto 12px;font-size:24px;color:#fff">?</div>
  <div style="color:#fff;font-size:18px;font-weight:700">Add Contact</div>
  <div style="color:rgba(255,255,255,0.8);font-size:13px;margin-top:4px">Fill in the details to create a new contact</div>
</div>
<div style="padding:20px 24px">
<form hx-post="/contacts" hx-target="body" hx-swap="none" hx-on::after-request="if(event.detail.successful){document.getElementById('new-contact-modal').close();htmx.trigger(document.body,'contactsUpdated')}">
<div class="form-group">
  <label class="form-label">Phone <span style="color:var(--danger)">*</span></label>
  <div style="display:flex;gap:8px">
    <select name="country_code" class="form-input" style="width:110px">
      <option value="+91">+91 IN</option>
      <option value="+1">+1 US</option>
      <option value="+44">+44 GB</option>
      <option value="+971">+971 AE</option>
      <option value="+65">+65 SG</option>
    </select>
    <input class="form-input" type="tel" name="phone_number" required placeholder="9876543210" style="flex:1">
  </div>
</div>
<div class="form-group">
  <label class="form-label">Full name</label>
  <input class="form-input" type="text" name="name" placeholder="Anita Desai">
</div>
<div class="form-group">
  <label class="form-label">Email</label>
  <input class="form-input" type="email" name="email" placeholder="anita@example.com">
</div>
<div style="display:grid;grid-template-columns:1fr 1fr;gap:12px">
  <div class="form-group">
    <label class="form-label">Company</label>
    <input class="form-input" type="text" name="company" placeholder="Acme Pvt Ltd">
  </div>
  <div class="form-group">
    <label class="form-label">Role</label>
    <input class="form-input" type="text" name="role" placeholder="Sales Manager">
  </div>
</div>
<div class="form-group">
  <label class="form-label">Industry</label>
  <select class="form-input" name="industry">%s</select>
</div>
<label style="display:flex;align-items:flex-start;gap:10px;padding:12px;background:var(--success-light,#f0fdf4);border-radius:var(--radius,6px);margin-bottom:16px;cursor:pointer">
  <input type="checkbox" name="opt_in" value="true" style="margin-top:3px">
  <div>
    <div style="font-weight:600;font-size:14px">Opted-in to marketing</div>
    <div style="font-size:12px;color:var(--text-secondary)">Contact has given consent to receive WhatsApp messages</div>
  </div>
</label>
<div style="display:flex;gap:8px;justify-content:flex-end">
  <button class="btn btn-secondary btn-sm" type="button" onclick="document.getElementById('new-contact-modal').close()">Cancel</button>
  <button class="btn btn-primary btn-sm" type="submit">Add contact</button>
</div>
</form>
</div>
</dialog>

</div>

<script>
function setOptFilter(val, evt) {
  document.getElementById('opt-filter-input').value = val;
  document.querySelectorAll('.ct-opt-tab').forEach(function(t){ t.classList.remove('active'); });
  if (evt && evt.target) evt.target.classList.add('active');
  htmx.ajax('GET', '/contacts/table', {target:'#contacts-table', swap:'innerHTML',
    values: {opted_in_filter: val, search: (document.querySelector('[name=search]') || {}).value || ''}});
}
function setView(v) {
  localStorage.setItem('ct_view', v);
  document.getElementById('btn-grid').classList.toggle('active', v === 'grid');
  document.getElementById('btn-list').classList.toggle('active', v === 'list');
  htmx.ajax('GET', '/contacts/table?view=' + v, {target:'#contacts-table', swap:'innerHTML'});
}
</script>`, industryList, industryOpts)
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, ShellClose())
		return err
	})
}

// avatarGradients cycles through a few gradient combos for contact avatars.
var avatarGradients = []string{
	"linear-gradient(135deg,#6c63ff,#a855f7)",
	"linear-gradient(135deg,#0ea5e9,#38bdf8)",
	"linear-gradient(135deg,#10b981,#34d399)",
	"linear-gradient(135deg,#f59e0b,#fbbf24)",
	"linear-gradient(135deg,#ef4444,#f97316)",
	"linear-gradient(135deg,#8b5cf6,#ec4899)",
}

func contactAvatarInitials(name, phone string) string {
	name = strings.TrimSpace(name)
	if name != "" {
		r := []rune(name)
		initials := string(r[0])
		// Try second word initial
		parts := strings.Fields(name)
		if len(parts) > 1 {
			initials += string([]rune(parts[1])[0])
		}
		return strings.ToUpper(initials)
	}
	phone = strings.TrimSpace(phone)
	for _, ch := range phone {
		if ch >= '0' && ch <= '9' {
			return string(ch)
		}
	}
	return "?"
}

func avatarGradient(id string) string {
	if id == "" {
		return avatarGradients[0]
	}
	idx := int(id[0]) % len(avatarGradients)
	return avatarGradients[idx]
}

func ContactTable(contacts []db.Contact, total int, offset, limit int, searchActive bool) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if len(contacts) == 0 {
			msg := "No contacts yet."
			if searchActive {
				msg = "No contacts match your search."
			}
			_, err := fmt.Fprintf(w, `<p class="empty-state">%s</p>`, msg)
			return err
		}

		end := offset + len(contacts)
		_, err := fmt.Fprintf(w, `<div class="contacts-table-wrap">
<div class="table-count">%d–%d of %d</div>
<table class="tbl">
<thead><tr>
  <th>PERSON</th><th>COMPANY</th><th>INDUSTRY</th><th>ROLE</th><th>STATUS</th><th>ACTIONS</th>
</tr></thead>
<tbody>`, offset+1, end, total)
		if err != nil {
			return err
		}

		for _, c := range contacts {
			company := "—"
			if v, ok := c.CustomFields["company"]; ok {
				if s, ok := v.(string); ok && s != "" {
					company = html.EscapeString(s)
				}
			}
			role := "—"
			if v, ok := c.CustomFields["role"]; ok {
				if s, ok := v.(string); ok && s != "" {
					role = html.EscapeString(s)
				}
			}
			industryHTML := "—"
			if c.Industry != "" {
				industryHTML = fmt.Sprintf(`<span class="ind-chip">%s</span>`, html.EscapeString(c.Industry))
			}

			statusHTML := `<span class="badge badge-off">Not opted in</span>`
			if c.OptedIn {
				statusHTML = `<span class="badge badge-on">Opted in</span>`
			}

			initials := contactAvatarInitials(c.Name, c.WAPhone)
			grad := avatarGradient(c.ID)
			displayName := c.Name
			if displayName == "" {
				displayName = c.WAPhone
			}

			if _, err := fmt.Fprintf(w, `<tr>
  <td>
    <div style="display:flex;align-items:center;gap:10px">
      <div style="width:38px;height:38px;border-radius:50%%;background:%s;display:flex;align-items:center;justify-content:center;color:#fff;font-weight:700;font-size:14px;flex-shrink:0">%s</div>
      <div>
        <div style="font-weight:600;font-size:14px">%s</div>
        <div style="font-size:12px;color:var(--text-secondary)">%s</div>
      </div>
    </div>
  </td>
  <td>%s</td>
  <td>%s</td>
  <td>%s</td>
  <td>%s</td>
  <td>
    <div style="display:flex;gap:6px">
      <button class="btn btn-sm btn-secondary"
        hx-get="/contacts/%s" hx-target="#contact-detail" hx-swap="innerHTML"
        title="Edit">&#9998;</button>
      <button class="btn btn-sm btn-danger"
        hx-delete="/contacts/%s"
        hx-confirm="Permanently delete this contact?"
        hx-target="#contacts-table" hx-swap="innerHTML"
        hx-include="#opt-filter-input,#tag-filter-input,[name=search]"
        title="Delete">&#128465;</button>
    </div>
  </td>
</tr>`,
				grad, html.EscapeString(initials),
				html.EscapeString(displayName),
				html.EscapeString(c.WAPhone),
				company,
				industryHTML,
				role,
				statusHTML,
				c.ID,
				c.ID,
			); err != nil {
				return err
			}
		}

		if _, err := io.WriteString(w, `</tbody></table>`); err != nil {
			return err
		}

		// Pagination
		paginationHTML := `<div class="ct-pagination">`
		if offset > 0 {
			prev := offset - limit
			if prev < 0 {
				prev = 0
			}
			paginationHTML += fmt.Sprintf(`<button class="btn btn-secondary btn-sm" hx-get="/contacts/table?offset=%d" hx-target="#contacts-table" hx-swap="innerHTML">&#8592; Previous</button>`, prev)
		}
		if end < total {
			paginationHTML += fmt.Sprintf(`<button class="btn btn-secondary btn-sm" hx-get="/contacts/table?offset=%d" hx-target="#contacts-table" hx-swap="innerHTML">Next &#8594;</button>`, offset+limit)
		}
		paginationHTML += `</div>`
		if _, err := io.WriteString(w, paginationHTML+"</div>"); err != nil {
			return err
		}
		return nil
	})
}

func ContactDetail(c *db.Contact, tags []db.Tag, notes []db.ContactNote, allTags []db.Tag) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		email := "—"
		if c.Email != nil {
			email = *c.Email
		}
		optedIn := "No"
		if c.OptedIn {
			optedIn = "Yes"
		}
		_, err := fmt.Fprintf(w, `
<div class="contact-detail" id="contact-%s">
<div class="detail-hd">
<h2>%s</h2>
<button class="btn btn-sm btn-danger"
  hx-delete="/contacts/%s"
  hx-confirm="Permanently delete this contact?"
  hx-target="#contact-detail"
  hx-swap="innerHTML">Delete</button>
</div>
<dl>
<dt>Phone</dt><dd>%s</dd>
<dt>Email</dt><dd>%s</dd>
<dt>Opted-in</dt><dd>%s</dd>
<dt>Created</dt><dd>%s</dd>
</dl>`,
			c.ID, html.EscapeString(c.Name),
			c.ID,
			html.EscapeString(c.WAPhone), html.EscapeString(email), optedIn,
			c.CreatedAt.Format("02 Jan 2006"),
		)
		if err != nil {
			return err
		}

		if _, err := fmt.Fprintf(w, `<h3>Tags</h3>
<div id="contact-tags-%s">`, c.ID); err != nil {
			return err
		}
		if err := ContactTagList(tags, allTags, c.ID).Render(ctx, w); err != nil {
			return err
		}
		if _, err := io.WriteString(w, `</div>`); err != nil {
			return err
		}

		if _, err := fmt.Fprintf(w, `<h3>Notes</h3>
<div id="contact-notes-%s">`, c.ID); err != nil {
			return err
		}
		if err := ContactNoteList(notes).Render(ctx, w); err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, `</div>
<form hx-post="/contacts/%s/notes" hx-target="#contact-notes-%s" hx-swap="innerHTML">
<label class="field"><span>Add note</span>
<textarea name="body" rows="2" required></textarea></label>
<button class="btn btn-sm btn-secondary" type="submit">Add note</button>
</form>
</div>`, c.ID, c.ID)
		return err
	})
}

func ContactTagList(tags []db.Tag, allTags []db.Tag, contactID string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		tagSet := make(map[int64]bool, len(tags))
		for _, t := range tags {
			tagSet[t.ID] = true
		}
		for _, t := range tags {
			if _, err := fmt.Fprintf(w,
				`<span class="chip" style="background:%s">%s
<button type="button" aria-label="remove tag"
  hx-delete="/contacts/%s/tags/%d"
  hx-target="#contact-tags-%s"
  hx-swap="innerHTML">×</button></span>`,
				html.EscapeString(t.Color), html.EscapeString(t.Name), contactID, t.ID, contactID,
			); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, `<form hx-post="/contacts/%s/tags" hx-target="#contact-tags-%s" hx-swap="innerHTML">
<select name="tag_id">`, contactID, contactID); err != nil {
			return err
		}
		for _, t := range allTags {
			if !tagSet[t.ID] {
				if _, err := fmt.Fprintf(w, `<option value="%d">%s</option>`, t.ID, html.EscapeString(t.Name)); err != nil {
					return err
				}
			}
		}
		_, err := io.WriteString(w, `</select><button class="btn btn-sm btn-secondary" type="submit">Add tag</button></form>`)
		return err
	})
}

func ContactNoteList(notes []db.ContactNote) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if len(notes) == 0 {
			_, err := io.WriteString(w, `<p class="empty-hint">No notes yet.</p>`)
			return err
		}
		for _, n := range notes {
			if _, err := fmt.Fprintf(w,
				`<div class="note"><p>%s</p><span class="note-ts">%s</span></div>`,
				html.EscapeString(n.Body), n.CreatedAt.Format("02 Jan 2006 15:04"),
			); err != nil {
				return err
			}
		}
		return nil
	})
}

func TagManagerList(tags []db.Tag) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, `<ul class="tag-list">`); err != nil {
			return err
		}
		for _, t := range tags {
			if _, err := fmt.Fprintf(w,
				`<li><span class="chip" style="background:%s">%s</span>
<button class="btn btn-sm btn-danger"
  hx-delete="/contacts/tags/%d"
  hx-target="closest li"
  hx-swap="outerHTML"
  hx-confirm="Delete tag %s?">Delete</button></li>`,
				html.EscapeString(t.Color), html.EscapeString(t.Name), t.ID, html.EscapeString(t.Name),
			); err != nil {
				return err
			}
		}
		_, err := fmt.Fprintf(w, `</ul>
<form hx-post="/contacts/tags" hx-target="#tag-manager-list" hx-swap="innerHTML">
<label class="field"><span>Tag name</span><input type="text" name="name" required></label>
<label class="field"><span>Colour</span><input type="color" name="color" value="#0E7A40"></label>
<button class="btn btn-sm btn-primary" type="submit">Create tag</button>
</form>`)
		return err
	})
}

func SegmentList(segs []db.Segment) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if len(segs) == 0 {
			_, err := io.WriteString(w, `<p class="empty-hint">No saved segments.</p>`)
			return err
		}
		_, err := io.WriteString(w, `<ul class="seg-list">`)
		if err != nil {
			return err
		}
		for _, s := range segs {
			if _, err := fmt.Fprintf(w,
				`<li><strong>%s</strong>
<button class="btn btn-sm btn-danger"
  hx-delete="/contacts/segments/%d"
  hx-target="closest li"
  hx-swap="outerHTML"
  hx-confirm="Delete segment?">Delete</button></li>`,
				html.EscapeString(s.Name), s.ID,
			); err != nil {
				return err
			}
		}
		_, err = io.WriteString(w, `</ul>`)
		return err
	})
}

func ImportMapColumns(headers []string, csvData string, rowCount int) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		opts := `<option value="">— skip —</option>`
		for i, h := range headers {
			opts += fmt.Sprintf(`<option value="%d">%s</option>`, i, html.EscapeString(h))
		}

		_, err := fmt.Fprintf(w, `
<div class="import-map">
<p>%d data rows detected. Map the columns:</p>
<form hx-post="/contacts/import/preview" hx-target="#import-steps" hx-swap="innerHTML">
<input type="hidden" name="csv_data" value="%s">
<label class="field"><span>Phone column <span class="req">*</span></span>
<select name="phone_col" required>%s</select></label>
<label class="field"><span>Name column</span>
<select name="name_col">%s</select></label>
<label class="field"><span>Email column</span>
<select name="email_col">%s</select></label>
<label class="field"><span>Tags (comma-separated)</span>
<input type="text" name="tags" placeholder="newsletter, promo"></label>
<label class="field cb">
<input type="checkbox" name="mark_opted_in">
<span>Mark all as opted-in</span></label>
<div class="form-btns">
<button class="btn btn-primary btn-sm" type="submit">Preview import</button>
</div>
</form>
</div>`, rowCount, html.EscapeString(csvData), opts, opts, opts)
		return err
	})
}

func ImportPreview(rows []ImportPreviewRow, total int, csvData string, phoneCol, nameCol, emailCol int, tags string, optedIn bool) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, err := fmt.Fprintf(w, `
<div class="import-preview">
<p>Preview (first %d of %d rows):</p>
<table class="tbl">
<thead><tr><th>Phone</th><th>Name</th><th>Email</th><th>Status</th></tr></thead>
<tbody>`, len(rows), total)
		if err != nil {
			return err
		}
		for _, r := range rows {
			status := `<span class="badge badge-approved">Valid</span>`
			if !r.Valid {
				status = `<span class="badge badge-rejected">` + html.EscapeString(r.Error) + `</span>`
			}
			if _, err := fmt.Fprintf(w, `<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>`,
				html.EscapeString(r.Phone), html.EscapeString(r.Name),
				html.EscapeString(r.Email), status,
			); err != nil {
				return err
			}
		}
		optedInVal := "false"
		if optedIn {
			optedInVal = "true"
		}
		_, err = fmt.Fprintf(w, `</tbody></table>
<form method="post" action="/contacts/import/confirm">
<input type="hidden" name="csv_data" value="%s">
<input type="hidden" name="phone_col" value="%s">
<input type="hidden" name="name_col" value="%s">
<input type="hidden" name="email_col" value="%s">
<input type="hidden" name="tags" value="%s">
<input type="hidden" name="mark_opted_in" value="%s">
<div class="form-btns">
<button class="btn btn-primary btn-sm" type="submit">Confirm import (%d total rows)</button>
</div>
</form>
</div>`,
			html.EscapeString(csvData),
			strconv.Itoa(phoneCol), strconv.Itoa(nameCol), strconv.Itoa(emailCol),
			html.EscapeString(tags), optedInVal, total,
		)
		return err
	})
}

func ImportResult(inserted, skipped, invalidCount int) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, err := fmt.Fprintf(w, `
<div class="import-result">
<p class="success">Import complete.</p>
<dl>
<dt>Inserted</dt><dd>%d</dd>
<dt>Skipped (duplicate phone)</dt><dd>%d</dd>
<dt>Invalid</dt><dd>%d</dd>
</dl>
<a class="btn btn-primary btn-sm" href="/contacts">View contacts</a>
</div>`, inserted, skipped, invalidCount)
		return err
	})
}
