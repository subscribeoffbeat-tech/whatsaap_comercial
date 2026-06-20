package templates

import (
	"context"
	"fmt"
	"html"
	"io"

	"github.com/a-h/templ"

	"whatsapptool/internal/db"
	mw "whatsapptool/internal/web/middleware"
)

func TemplatesPage(agent *mw.AgentClaims) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/templates", "Templates", "")); err != nil {
			return err
		}
		_, err := io.WriteString(w, `
<div class="page-wrap">
<div class="page-hd">
<div>
<h1 class="screen-title">WhatsApp Templates</h1>
<p class="screen-subtitle">Browse approved templates and the sample library.</p>
</div>
<button class="btn btn-primary btn-sm"
  onclick="document.getElementById('new-tmpl-modal').showModal()">+ New template</button>
</div>

<div class="tmpl-tabs">
  <button class="tmpl-tab active"
    hx-get="/templates/gallery" hx-target="#tmpl-gallery" hx-swap="innerHTML"
    onclick="document.querySelectorAll('.tmpl-tab').forEach(t=>t.classList.remove('active'));this.classList.add('active')">All</button>
  <button class="tmpl-tab"
    hx-get="/templates/gallery?status=approved" hx-target="#tmpl-gallery" hx-swap="innerHTML"
    onclick="document.querySelectorAll('.tmpl-tab').forEach(t=>t.classList.remove('active'));this.classList.add('active')">Approved</button>
  <button class="tmpl-tab"
    hx-get="/templates/gallery?status=pending" hx-target="#tmpl-gallery" hx-swap="innerHTML"
    onclick="document.querySelectorAll('.tmpl-tab').forEach(t=>t.classList.remove('active'));this.classList.add('active')">Pending</button>
  <button class="tmpl-tab"
    hx-get="/templates/gallery?status=draft" hx-target="#tmpl-gallery" hx-swap="innerHTML"
    onclick="document.querySelectorAll('.tmpl-tab').forEach(t=>t.classList.remove('active'));this.classList.add('active')">Drafts</button>
  <button class="tmpl-tab"
    hx-get="/templates/gallery?status=rejected" hx-target="#tmpl-gallery" hx-swap="innerHTML"
    onclick="document.querySelectorAll('.tmpl-tab').forEach(t=>t.classList.remove('active'));this.classList.add('active')">Rejected</button>
  <button class="tmpl-tab tmpl-tab-lib"
    hx-get="/templates/library" hx-target="#tmpl-gallery" hx-swap="innerHTML"
    onclick="document.querySelectorAll('.tmpl-tab').forEach(t=>t.classList.remove('active'));this.classList.add('active')">&#128218; Library</button>
</div>

<div id="tmpl-gallery"
  hx-get="/templates/gallery"
  hx-trigger="load, templatesUpdated from:body"
  hx-swap="innerHTML">
</div>

<dialog id="new-tmpl-modal">
<form method="post" action="/templates" hx-post="/templates" hx-target="#tmpl-gallery" hx-swap="innerHTML">
<h2>New template</h2>
<label class="field"><span>Name (snake_case)</span>
<input type="text" name="name" required pattern="[a-z0-9_]+" placeholder="promo_offer"></label>
<label class="field"><span>Language</span>
<select name="language">
<option value="en_US">English (US)</option>
<option value="en_GB">English (GB)</option>
</select></label>
<label class="field"><span>Category</span>
<select name="category">
<option value="marketing">Marketing</option>
<option value="utility">Utility</option>
<option value="authentication">Authentication</option>
</select></label>
<label class="field"><span>Body text (use {{1}}, {{2}} for variables)</span>
<textarea name="body" rows="4" required></textarea></label>
<div class="modal-btns">
<button class="btn btn-primary btn-sm" type="submit">Create &amp; save</button>
<button class="btn btn-secondary btn-sm" type="button" onclick="document.getElementById('new-tmpl-modal').close()">Cancel</button>
</div>
</form>
</dialog>
</div>`)
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, ShellClose())
		return err
	})
}

func TemplateGallery(tmpls []db.Template) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if len(tmpls) == 0 {
			_, err := io.WriteString(w, `<div class="empty-state"><div class="empty-icon">&#9889;</div><div class="empty-title">No templates</div><div class="empty-desc">Create a new template or browse the library.</div></div>`)
			return err
		}
		if _, err := io.WriteString(w, `<div class="tmpl-gallery">`); err != nil {
			return err
		}
		for _, t := range tmpls {
			statusClass := "badge-" + t.Status
			catClass := "cat-" + t.Category

			// Extract body preview from components
			bodyPreview := ""
			for _, comp := range t.Components {
				if comp["type"] == "BODY" {
					if text, ok := comp["text"].(string); ok {
						bodyPreview = text
					}
				}
			}

			if _, err := fmt.Fprintf(w,
				`<div class="tmpl-card">
<div class="tmpl-card-hd">
<span class="tmpl-name">%s</span>
<span class="badge badge-cat %s">%s</span>
<span class="badge %s">%s</span>
</div>
<div class="tmpl-body-preview">%s</div>
<div class="tmpl-card-actions">`,
				html.EscapeString(t.Name),
				catClass, t.Category,
				statusClass, t.Status,
				html.EscapeString(bodyPreview),
			); err != nil {
				return err
			}

			// Edit button opens the editor form
			if _, err := fmt.Fprintf(w,
				`<button class="btn btn-sm btn-secondary"
  hx-get="/templates/%s"
  hx-target="#tmpl-editor"
  hx-swap="innerHTML">Edit</button>`,
				t.ID,
			); err != nil {
				return err
			}

			// Submit to Meta button for pending/rejected templates
			if t.Status == "pending" || t.Status == "rejected" || t.Status == "draft" {
				if _, err := fmt.Fprintf(w,
					`<form method="post" action="/templates/%s/submit" style="display:inline">
<button class="btn btn-sm btn-primary" type="submit">Submit to Meta</button></form>`, t.ID,
				); err != nil {
					return err
				}
			}

			// Delete button
			if _, err := fmt.Fprintf(w,
				`<form method="post" action="/templates/%s" hx-delete="/templates/%s" hx-confirm="Delete template?" hx-target="closest .tmpl-card" hx-swap="outerHTML" style="display:inline">
<button class="btn btn-sm btn-danger" type="submit">Delete</button></form>`,
				t.ID, t.ID,
			); err != nil {
				return err
			}

			if _, err := io.WriteString(w, `</div></div>`); err != nil {
				return err
			}
		}
		_, err := io.WriteString(w, `</div><div id="tmpl-editor"></div>`)
		return err
	})
}

// libraryTemplate is a sample template shown in the Library tab.
type libraryTemplate struct {
	Name     string
	Category string
	Industry string
	Body     string
}

var libraryTemplates = []libraryTemplate{
	{"welcome_new_customer", "utility", "All industries", "Hi {{1}}, welcome! We're thrilled to have you. Reply STOP to opt out."},
	{"order_confirmation", "utility", "Retail & E-commerce", "Hi {{1}}, your order #{{2}} has been confirmed. Expected delivery: {{3}}."},
	{"appointment_reminder", "utility", "Healthcare & Services", "Hi {{1}}, reminder: your appointment is on {{2}} at {{3}}. Reply to reschedule."},
	{"payment_due_reminder", "utility", "Finance & Real Estate", "Hi {{1}}, your payment of ₹{{2}} is due on {{3}}. Pay now to avoid late fees."},
	{"flash_sale_alert", "marketing", "Retail & FMCG", "Hi {{1}}, flash sale! Get {{2}}% off on all products today only. Shop now: {{3}}"},
	{"reengagement_offer", "marketing", "All industries", "Hi {{1}}, we miss you! Here's a special {{2}}% discount just for you. Valid till {{3}}."},
	{"support_ticket_opened", "utility", "Technology", "Hi {{1}}, ticket #{{2}} has been opened. Our team will respond within {{3}} hours."},
	{"feedback_request", "marketing", "All industries", "Hi {{1}}, how was your experience with {{2}}? Reply with a rating 1-5. Your feedback matters!"},
}

func TemplateLibrary() templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, `<div class="lib-cards">`); err != nil {
			return err
		}
		for _, t := range libraryTemplates {
			catClass := "cat-" + t.Category
			useURL := fmt.Sprintf("/templates/new?name=%s&category=%s&body=%s",
				html.EscapeString(t.Name), html.EscapeString(t.Category), html.EscapeString(t.Body))
			if _, err := fmt.Fprintf(w,
				`<div class="lib-card">
<div class="lib-card-hd">
<span class="lib-card-name">%s</span>
<span class="badge badge-cat %s">%s</span>
</div>
<div class="lib-card-industry">%s</div>
<div class="lib-card-body">%s</div>
<a class="btn btn-sm btn-secondary" href="%s">&#9999; Edit &amp; create draft</a>
</div>`,
				html.EscapeString(t.Name),
				catClass, t.Category,
				html.EscapeString(t.Industry),
				html.EscapeString(t.Body),
				useURL,
			); err != nil {
				return err
			}
		}
		_, err := io.WriteString(w, `</div>`)
		return err
	})
}

func TemplateEditorForm(t *db.Template) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		body := ""
		if t != nil {
			for _, comp := range t.Components {
				if comp["type"] == "BODY" {
					if text, ok := comp["text"].(string); ok {
						body = text
					}
				}
			}
		}

		name := ""
		language := "en_US"
		category := "marketing"
		id := ""
		if t != nil {
			name = t.Name
			language = t.Language
			category = t.Category
			id = t.ID
		}

		_, err := fmt.Fprintf(w, `
<div class="tmpl-editor">
<h3>Edit template</h3>
<form method="post" action="/templates/%s"
  hx-put="/templates/%s"
  hx-target="#tmpl-gallery"
  hx-swap="innerHTML">
<label class="field"><span>Name</span>
<input type="text" name="name" value="%s" required pattern="[a-z0-9_]+"></label>
<label class="field"><span>Language</span>
<select name="language">
<option value="en_US"%s>English (US)</option>
<option value="en_GB"%s>English (GB)</option>
</select></label>
<label class="field"><span>Category</span>
<select name="category">
<option value="marketing"%s>Marketing</option>
<option value="utility"%s>Utility</option>
<option value="authentication"%s>Authentication</option>
</select></label>
<label class="field"><span>Body text</span>
<textarea name="body" rows="5">%s</textarea></label>
<div class="form-btns">
<button class="btn btn-primary btn-sm" type="submit">Save changes</button>
</div>
</form>
</div>`,
			id, id,
			html.EscapeString(name),
			sel(language, "en_US"), sel(language, "en_GB"),
			sel(category, "marketing"), sel(category, "utility"), sel(category, "authentication"),
			html.EscapeString(body),
		)
		return err
	})
}
