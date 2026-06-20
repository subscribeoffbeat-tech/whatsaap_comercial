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

func TeamPage(agents []*db.Agent, limits map[string]*db.AgentLimit, auditLog []*db.AuditEntry, actor *mw.AgentClaims, flash string) templ.Component {
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

<div x-data="{role:''}">
<div class="tmpl-tabs" style="margin-bottom:16px">
  <button class="tmpl-tab" :class="{ active: role==='' }" @click="role=''">All</button>
  <button class="tmpl-tab" :class="{ active: role==='admin' }" @click="role='admin'">Admin</button>
  <button class="tmpl-tab" :class="{ active: role==='manager' }" @click="role='manager'">Manager</button>
  <button class="tmpl-tab" :class="{ active: role==='agent' }" @click="role='agent'">Member</button>
</div>

<div class="card-static" style="margin-bottom:24px">
<table>
<thead>
<tr><th>Name</th><th>Email</th><th>Role</th><th>Monthly limit</th><th>Status</th><th>Actions</th></tr>
</thead>
<tbody>`, flashHTML)
		if err != nil {
			return err
		}

		for _, a := range agents {
			status := `<span class="badge badge-approved">Active</span>`
			if !a.Active {
				// Check if it's a pending invite (has no password hash → invite_token set)
				if a.InviteToken != nil {
					status = `<span class="badge badge-pending">Pending invite</span>`
				} else {
					status = `<span class="badge badge-rejected">Inactive</span>`
				}
			}

			// Monthly limit display
			limitStr := "No limit"
			if limits != nil {
				if lim, ok := limits[a.ID]; ok && lim.MonthlyMsgCap > 0 {
					limitStr = fmt.Sprintf("%d msg/mo", lim.MonthlyMsgCap)
				}
			}
			currentCap := 0
			if limits != nil {
				if lim, ok := limits[a.ID]; ok {
					currentCap = lim.MonthlyMsgCap
				}
			}

			// Role badge (inline select for changing role)
			roleBadge := ""
			switch a.Role {
			case "admin":
				roleBadge = `<span class="badge badge-approved" style="margin-right:4px">Admin</span>`
			case "manager":
				roleBadge = `<span class="badge badge-pending" style="margin-right:4px">Manager</span>`
			default:
				roleBadge = `<span class="badge" style="margin-right:4px">Member</span>`
			}

			// Self marker
			selfMark := ""
			if actor != nil && a.ID == actor.ID {
				selfMark = ` <span style="font-size:11px;color:var(--text-muted)">(you)</span>`
			}

			_, err := fmt.Fprintf(w,
				`<tr x-show="role==='' || role==='%s'">
<td style="font-weight:500">%s%s</td>
<td style="color:var(--text-secondary);font-size:13px">%s</td>
<td>%s<form method="post" action="/team/%s/role" style="display:inline;margin-left:6px">
  <select name="role" onchange="this.form.submit()"
    style="font-size:12px;padding:2px 6px;border:1px solid var(--border);border-radius:var(--radius-sm);background:var(--bg-input)">
    <option value="admin"%s>Admin</option>
    <option value="manager"%s>Manager</option>
    <option value="agent"%s>Member</option>
  </select>
</form></td>
<td>%s
  <button class="btn btn-secondary btn-sm" style="margin-left:6px;font-size:11px"
    onclick="document.getElementById('lim-%s').showModal()">Set</button>
</td>
<td>%s</td>
<td style="display:flex;gap:5px;align-items:center;flex-wrap:wrap;min-width:200px">`,
				a.Role,
				html.EscapeString(a.Name), selfMark,
				html.EscapeString(a.Email),
				roleBadge, a.ID,
				sel(a.Role, "admin"), sel(a.Role, "manager"), sel(a.Role, "agent"),
				html.EscapeString(limitStr), a.ID,
				status,
			)
			if err != nil {
				return err
			}

			// Action buttons
			if actor == nil || a.ID != actor.ID {
				if a.Active {
					if _, err := fmt.Fprintf(w,
						`<form method="post" action="/team/%s/activate" style="display:inline">
<input type="hidden" name="active" value="false">
<button class="btn btn-sm btn-secondary">Deactivate</button>
</form>`,
						a.ID); err != nil {
						return err
					}
				} else if a.InviteToken != nil {
					// Pending invite: show Copy link + Re-invite + Delete
					inviteLink := ""
					if flash != "" {
						inviteLink = flash
					}
					if _, err := fmt.Fprintf(w,
						`<button class="btn btn-secondary btn-sm"
  onclick="navigator.clipboard.writeText('%s').then(()=>alert('Copied!'))">Copy link</button>
<form method="post" action="/team/%s/reinvite" style="display:inline">
  <button class="btn btn-secondary btn-sm" title="Generate a new invite link">Re-invite</button>
</form>
<form method="post" action="/team/%s/activate" style="display:inline">
  <input type="hidden" name="active" value="true">
  <button class="btn btn-secondary btn-sm">Activate</button>
</form>`,
						html.EscapeString(inviteLink), a.ID, a.ID); err != nil {
						return err
					}
				} else {
					if _, err := fmt.Fprintf(w,
						`<form method="post" action="/team/%s/activate" style="display:inline">
<input type="hidden" name="active" value="true">
<button class="btn btn-primary btn-sm">Activate</button>
</form>`,
						a.ID); err != nil {
						return err
					}
				}

				// Delete button (always shown for non-self)
				if _, err := fmt.Fprintf(w,
					`<button class="btn btn-sm" style="background:var(--danger-light);color:var(--danger);border:1px solid var(--danger-light)"
  onclick="if(confirm('Delete %s from the team?')) { fetch('/team/%s', {method:'DELETE'}).then(()=>location.reload()) }">
  Delete</button>`,
					html.EscapeString(a.Name), a.ID); err != nil {
					return err
				}
			}

			if _, err := io.WriteString(w, `</td></tr>`); err != nil {
				return err
			}

			// Limit dialog for this agent
			if _, err := fmt.Fprintf(w, `
<dialog id="lim-%s">
<form method="post" action="/team/%s/limits">
<h2 style="margin-top:0;font-size:16px">Monthly limit — %s</h2>
<div class="form-group">
  <label class="form-label">Monthly message cap <span style="color:var(--text-muted);font-weight:400">(0 = unlimited)</span></label>
  <input class="form-input" type="number" name="monthly_msg_cap" min="0" value="%d" placeholder="0 = unlimited" style="max-width:160px">
</div>
<div class="modal-btns">
  <button class="btn btn-primary btn-sm" type="submit">Save</button>
  <button class="btn btn-secondary btn-sm" type="button" onclick="document.getElementById('lim-%s').close()">Cancel</button>
</div>
</form>
</dialog>`,
				a.ID, a.ID, html.EscapeString(a.Name), currentCap, a.ID); err != nil {
				return err
			}
		}

		if _, err := io.WriteString(w, `</tbody></table></div></div>

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
<h2>Invite member</h2>
<div class="form-group">
  <label class="form-label">Name</label>
  <input class="form-input" type="text" name="name" required>
</div>
<div class="form-group">
  <label class="form-label">Email</label>
  <input class="form-input" type="email" name="email" required>
</div>
<div class="form-group">
  <label class="form-label">Role</label>
  <select class="form-input" name="role">
    <option value="agent">Member</option>
    <option value="manager">Manager</option>
    <option value="admin">Admin</option>
  </select>
</div>
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
