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
		_, err := fmt.Fprintf(w, `
<div class="page-wrap">
<div class="page-hd">
<div>
<h1 class="screen-title">WhatsApp Templates</h1>
<p class="screen-subtitle">Browse approved templates and the sample library.</p>
</div>
<button class="btn btn-primary btn-sm"
  onclick="document.getElementById('new-tmpl-modal').showModal()">+ New template</button>
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
			_, err := io.WriteString(w, `<p class="empty-state">No templates yet. Create one to get started.</p>`)
			return err
		}
		if _, err := io.WriteString(w, `<table class="tbl"><thead><tr><th>Name</th><th>Language</th><th>Category</th><th>Status</th><th>Actions</th></tr></thead><tbody>`); err != nil {
			return err
		}
		for _, t := range tmpls {
			statusClass := "badge-" + t.Status
			if _, err := fmt.Fprintf(w,
				`<tr><td>%s</td><td>%s</td><td>%s</td><td><span class="badge %s">%s</span></td><td>
<button class="btn btn-sm btn-secondary"
  hx-get="/templates/%s"
  hx-target="#tmpl-editor"
  hx-swap="innerHTML">Edit</button>`,
				html.EscapeString(t.Name), t.Language, t.Category,
				statusClass, t.Status, t.ID,
			); err != nil {
				return err
			}
			if t.Status == "pending" || t.Status == "rejected" {
				if _, err := fmt.Fprintf(w,
					`<form method="post" action="/templates/%s/submit" style="display:inline">
<button class="btn btn-sm btn-primary" type="submit">Submit to Meta</button></form>`, t.ID,
				); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintf(w,
				`<form method="post" action="/templates/%s" hx-delete="/templates/%s" hx-confirm="Delete template?" style="display:inline">
<button class="btn btn-sm btn-danger" type="submit">Delete</button></form>
</td></tr>`, t.ID, t.ID,
			); err != nil {
				return err
			}
		}
		_, err := io.WriteString(w, `</tbody></table><div id="tmpl-editor"></div>`)
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
