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

func SuperAdminPage(actor *mw.AgentClaims, tenants []*db.Tenant, flash string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(actor, "/admin/tenants", "Platform Admin", "")); err != nil {
			return err
		}

		flashScript := ""
		if flash != "" {
			flashScript = FlashScript(ToastSuccess, flash)
		}

		_, err := fmt.Fprintf(w, `
<div class="page-wrap">
<div class="page-hd">
  <div>
    <h1 class="screen-title">Workspace Tenants</h1>
    <p class="screen-subtitle">%d workspace accounts</p>
  </div>
  <button class="btn btn-primary" onclick="openModal('create-tenant-modal',this)">+ Create tenant</button>
</div>
%s
<div class="team-layout">
<div class="card-static" style="flex: 2">
<table class="tbl">
<thead>
<tr>
  <th>NAME</th>
  <th>SLUG (SUBDOMAIN)</th>
  <th>STATUS</th>
  <th>CREATED AT</th>
  <th>ACTIONS</th>
</tr>
</thead>
<tbody>`, len(tenants), flashScript)
		if err != nil {
			return err
		}

		for _, t := range tenants {
			statusHTML := ""
			suspendBtnText := "Suspend"
			suspendActionStatus := "suspended"
			suspendBtnClass := "btn-secondary"
			if t.Status == "suspended" {
				statusHTML = `<span class="team-status team-status--inactive"><span class="team-dot team-dot--inactive"></span>suspended</span>`
				suspendBtnText = "Activate"
				suspendActionStatus = "active"
				suspendBtnClass = "btn-primary"
			} else {
				statusHTML = `<span class="team-status team-status--active"><span class="team-dot team-dot--active"></span>active</span>`
			}

			_, err := fmt.Fprintf(w, `<tr>
<td><strong>%s</strong></td>
<td><code>%s</code></td>
<td>%s</td>
<td>%s</td>
<td>
  <form method="post" action="/admin/tenants/%s/status" style="display:inline;margin:0">
    <input type="hidden" name="status" value="%s">
    <button type="submit" class="btn %s btn-sm">%s</button>
  </form>
</td>
</tr>`,
				html.EscapeString(t.Name),
				html.EscapeString(t.Slug),
				statusHTML,
				t.CreatedAt.In(istLoc).Format("02 Jan 2006 15:04"),
				t.ID,
				suspendActionStatus,
				suspendBtnClass,
				suspendBtnText,
			)
			if err != nil {
				return err
			}
		}

		if _, err := io.WriteString(w, `</tbody></table></div>`); err != nil {
			return err
		}

		// Create tenant modal
		createBody := `<form hx-post="/admin/tenants" hx-target="body" hx-swap="none"` +
			` hx-disabled-elt="find button[type='submit']"` +
			` hx-on::after-request="if(event.detail.successful)window.location.reload()"` +
			` hx-on::response-error="document.getElementById('tenant-form-errors').innerHTML=event.detail.xhr.responseText">` +
			`<div id="tenant-form-errors" role="alert" aria-live="polite"></div>` +
			`<div class="form-group"><label class="form-label" for="t-name">Workspace Name</label>` +
			`<input id="t-name" class="form-input" type="text" name="name" placeholder="e.g. Acme Corp" required></div>` +
			`<div class="form-group"><label class="form-label" for="t-slug">Subdomain Slug</label>` +
			`<input id="t-slug" class="form-input" type="text" name="slug" placeholder="e.g. acme" required></div>` +
			`<div class="modal-btns">` +
			`<button class="btn btn-primary btn-sm" type="submit">Create workspace</button>` +
			`<button class="btn btn-secondary btn-sm" type="button" onclick="document.getElementById('create-tenant-modal').close()">Cancel</button>` +
			`</div></form>`

		if _, err := io.WriteString(w, ModalShell("create-tenant-modal", "Create Workspace Tenant", createBody)+`</div>`); err != nil {
			return err
		}

		_, err = io.WriteString(w, ShellClose())
		return err
	})
}
