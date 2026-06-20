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

func TeamPage(agents []*db.Agent, auditLog []*db.AuditEntry, actor *mw.AgentClaims, flash string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(actor, "/team", "Team", "")); err != nil {
			return err
		}

		flashHTML := ""
		if flash != "" {
			flashHTML = `<div class="toast toast--info"><strong>Invite link:</strong> ` + html.EscapeString(flash) + `</div>`
		}

		_, err := fmt.Fprintf(w, `
<div class="page-wrap">
<div class="page-hd">
<div>
<h1 class="screen-title">Team</h1>
<p class="screen-subtitle">Manage your team, roles and monthly limits.</p>
</div>
<button class="btn btn-primary btn-sm" onclick="document.getElementById('invite-modal').showModal()">+ Invite member</button>
</div>
%s

<table class="tbl">
<thead><tr><th>Name</th><th>Email</th><th>Role</th><th>Status</th><th>Actions</th></tr></thead>
<tbody>`, flashHTML)
		if err != nil {
			return err
		}

		for _, a := range agents {
			status := "Active"
			if !a.Active {
				status = "Inactive"
			}
			_, err := fmt.Fprintf(w,
				`<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>
<form method="post" action="/team/%s/role" style="display:inline">
<select name="role" onchange="this.form.submit()">
<option value="admin"%s>Admin</option>
<option value="manager"%s>Manager</option>
<option value="agent"%s>Agent</option>
</select>
</form>`,
				html.EscapeString(a.Name), html.EscapeString(a.Email), a.Role, status,
				a.ID,
				sel(a.Role, "admin"), sel(a.Role, "manager"), sel(a.Role, "agent"),
			)
			if err != nil {
				return err
			}
			if actor != nil && a.ID != actor.ID {
				if a.Active {
					_, err = fmt.Fprintf(w, `<form method="post" action="/team/%s/activate" style="display:inline"><input type="hidden" name="active" value="false"><button class="btn btn-sm btn-secondary">Deactivate</button></form>`, a.ID)
				} else {
					_, err = fmt.Fprintf(w, `<form method="post" action="/team/%s/activate" style="display:inline"><input type="hidden" name="active" value="true"><button class="btn btn-sm btn-primary">Activate</button></form>`, a.ID)
				}
				if err != nil {
					return err
				}
			}
			if _, err := io.WriteString(w, `</td></tr>`); err != nil {
				return err
			}
		}

		if _, err := io.WriteString(w, `</tbody></table>

<h2>Audit log</h2>
<table class="tbl">
<thead><tr><th>Time</th><th>Actor</th><th>Action</th><th>Entity</th></tr></thead>
<tbody>`); err != nil {
			return err
		}

		for _, e := range auditLog {
			entityStr := ""
			if e.EntityType != nil && e.EntityID != nil {
				entityStr = *e.EntityType + "/" + *e.EntityID
			}
			if _, err := fmt.Fprintf(w,
				`<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>`,
				e.CreatedAt.Format("02 Jan 15:04"), html.EscapeString(e.ActorName),
				html.EscapeString(e.Action), html.EscapeString(entityStr),
			); err != nil {
				return err
			}
		}

		if _, err := io.WriteString(w, `</tbody></table>

<dialog id="invite-modal">
<form method="post" action="/team/invite">
<h2>Invite agent</h2>
<label class="field"><span>Name</span><input type="text" name="name" required></label>
<label class="field"><span>Email</span><input type="email" name="email" required></label>
<label class="field"><span>Role</span>
<select name="role">
<option value="agent">Agent</option>
<option value="manager">Manager</option>
<option value="admin">Admin</option>
</select></label>
<div class="modal-btns">
<button class="btn btn-primary btn-sm" type="submit">Send invite</button>
<button class="btn btn-secondary btn-sm" type="button" onclick="document.getElementById('invite-modal').close()">Cancel</button>
</div>
</form>
</dialog>

</div>`); err != nil {
			return err
		}

		_, err = io.WriteString(w, ShellClose())
		return err
	})
}

func sel(current, value string) string {
	if current == value {
		return ` selected`
	}
	return ""
}
