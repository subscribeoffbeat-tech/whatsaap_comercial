package templates

import (
	"context"
	"fmt"
	"html"
	"io"
	"strconv"

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

func ContactsPage(agent *mw.AgentClaims, tags []db.Tag) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/contacts", "Contacts", "")); err != nil {
			return err
		}

		tagOpts := ""
		for _, t := range tags {
			tagOpts += fmt.Sprintf(`<option value="%d">%s</option>`, t.ID, html.EscapeString(t.Name))
		}

		_, err := fmt.Fprintf(w, `
<div class="page-wrap contacts-wrap">
<div class="page-hd">
<h1>Contacts</h1>
<button class="btn pri" onclick="document.getElementById('import-modal').showModal()">Import CSV</button>
<button class="btn" onclick="document.getElementById('new-contact-modal').showModal()">Add contact</button>
</div>

<div class="contacts-filters">
<input type="search" placeholder="Search name or phone…"
  hx-get="/contacts/table" hx-trigger="input changed delay:300ms" hx-target="#contacts-table"
  hx-include="[name='tag_filter'],[name='opted_in_filter']"
  name="search" class="search-input">
<select name="tag_filter"
  hx-get="/contacts/table" hx-trigger="change" hx-target="#contacts-table"
  hx-include="[name='search'],[name='opted_in_filter']">
<option value="">All tags</option>
%s
</select>
<select name="opted_in_filter"
  hx-get="/contacts/table" hx-trigger="change" hx-target="#contacts-table"
  hx-include="[name='search'],[name='tag_filter']">
<option value="">All</option>
<option value="true">Opted-in</option>
<option value="false">Opted-out</option>
</select>
</div>

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
<button class="btn pri" type="submit">Upload &amp; map columns</button>
<button class="btn" type="button" onclick="document.getElementById('import-modal').close()">Cancel</button>
</div>
</form>
<div id="import-steps"></div>
</dialog>

<dialog id="new-contact-modal">
<h2>Add contact</h2>
<form hx-post="/contacts" hx-target="body" hx-swap="none">
<label class="field"><span>Phone (E.164, e.g. +919876543210)</span>
<input type="tel" name="phone" required placeholder="+919876543210"></label>
<label class="field"><span>Name</span>
<input type="text" name="name"></label>
<label class="field cb">
<input type="checkbox" name="opt_in" value="true">
<span>Mark as opted-in</span></label>
<div class="modal-btns">
<button class="btn pri" type="submit">Add</button>
<button class="btn" type="button" onclick="document.getElementById('new-contact-modal').close()">Cancel</button>
</div>
</form>
</dialog>
</div>`, tagOpts)
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, ShellClose())
		return err
	})
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

		_, err := fmt.Fprintf(w, `<p class="table-count">Showing %d–%d of %d</p>
<table class="tbl">
<thead><tr><th>Phone</th><th>Name</th><th>Opted-in</th><th>Created</th></tr></thead>
<tbody>`, offset+1, offset+len(contacts), total)
		if err != nil {
			return err
		}
		for _, c := range contacts {
			optedIn := "No"
			if c.OptedIn {
				optedIn = "Yes"
			}
			if _, err := fmt.Fprintf(w,
				`<tr class="clickable" hx-get="/contacts/%s" hx-target="#contact-detail" hx-swap="innerHTML">
<td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>`,
				c.ID, html.EscapeString(c.WAPhone), html.EscapeString(c.Name),
				optedIn, c.CreatedAt.Format("02 Jan 2006"),
			); err != nil {
				return err
			}
		}
		_, err = io.WriteString(w, `</tbody></table>`)
		if err != nil {
			return err
		}

		// Pagination
		if offset > 0 {
			prev := offset - limit
			if prev < 0 {
				prev = 0
			}
			if _, err := fmt.Fprintf(w, `<button class="btn" hx-get="/contacts/table?offset=%d" hx-target="#contacts-table">Previous</button>`, prev); err != nil {
				return err
			}
		}
		if offset+len(contacts) < total {
			if _, err := fmt.Fprintf(w, `<button class="btn" hx-get="/contacts/table?offset=%d" hx-target="#contacts-table">Next</button>`, offset+limit); err != nil {
				return err
			}
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
<button class="btn sm danger"
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
<button class="btn sm" type="submit">Add note</button>
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
		_, err := io.WriteString(w, `</select><button class="btn sm" type="submit">Add tag</button></form>`)
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
<button class="btn sm danger"
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
<button class="btn sm pri" type="submit">Create tag</button>
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
<button class="btn sm danger"
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
<button class="btn pri" type="submit">Preview import</button>
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
<button class="btn pri" type="submit">Confirm import (%d total rows)</button>
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
<a class="btn pri" href="/contacts">View contacts</a>
</div>`, inserted, skipped, invalidCount)
		return err
	})
}
