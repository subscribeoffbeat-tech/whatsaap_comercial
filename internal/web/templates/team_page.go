package templates

import (
	"context"
	"fmt"
	"html"
	"io"
	"strings"
	"time"

	"github.com/a-h/templ"

	"whatsapptool/internal/db"
	mw "whatsapptool/internal/web/middleware"
)

func TeamPage(agents []*db.Agent, limits map[string]*db.AgentLimit, auditLog []*db.AuditEntry, actor *mw.AgentClaims, flash string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(actor, "/team", "Team", "")); err != nil {
			return err
		}

		activeCount := 0
		for _, a := range agents {
			if a.Active && a.InviteToken == nil {
				activeCount++
			}
		}

		flashScript := ""
		if flash != "" {
			flashScript = FlashScript(ToastSuccess, flash)
		}

		_, err := fmt.Fprintf(w, `
<div class="page-wrap">
<div class="page-hd">
  <div>
    <h1 class="screen-title">Team</h1>
    <p class="screen-subtitle">%d active members</p>
  </div>
  <a class="btn btn-primary" href="/team/invite">+ Invite member</a>
</div>
%s
<div class="team-layout">
<div class="card-static">
<table class="tbl">
<thead>
<tr>
  <th>MEMBER</th>
  <th>ROLE</th>
  <th>STATUS</th>
  <th>JOINED</th>
  <th></th>
</tr>
</thead>
<tbody>`, activeCount, flashScript)
		if err != nil {
			return err
		}

		if len(agents) == 0 {
			empty := EmptyStateHTML(EmptyIconContacts, "No team members yet",
				"Invite your first team member to get started.",
				[]EmptyAction{{Label: "Invite member", Primary: true, HREF: "/team/invite"}})
			if _, err := fmt.Fprintf(w, `<tr><td colspan="5">%s</td></tr>`, empty); err != nil {
				return err
			}
		}

		for _, a := range agents {
			// Status indicator
			statusHTML := ""
			if a.Active && a.InviteToken == nil {
				statusHTML = `<span class="team-status team-status--active"><span class="team-dot team-dot--active"></span>active</span>`
			} else if a.InviteToken != nil {
				statusHTML = `<span class="team-status team-status--pending"><span class="team-dot team-dot--pending"></span>pending invite</span>`
			} else {
				statusHTML = `<span class="team-status team-status--inactive"><span class="team-dot team-dot--inactive"></span>inactive</span>`
			}

			selfMark := ""
			if actor != nil && a.ID == actor.ID {
				selfMark = ` <span class="team-self">(you)</span>`
			}

			editBtn := ""
			if actor == nil || a.ID != actor.ID {
				editBtn = fmt.Sprintf(`<button class="team-edit-btn" onclick="openModal('edit-%s',this)">Edit</button>`, a.ID)
			}

			_, err := fmt.Fprintf(w, `<tr>
<td>
  <div class="team-member-cell">
    <div class="team-av %s">%s</div>
    <div>
      <div class="team-member-name">%s%s</div>
      <div class="team-member-email">%s</div>
    </div>
  </div>
</td>
<td>%s</td>
<td>%s</td>
<td class="team-joined">%s</td>
<td class="team-action-cell">%s</td>
</tr>`,
				avatarColor(a.Name), html.EscapeString(twoInitials(a.Name)),
				html.EscapeString(a.Name), selfMark,
				html.EscapeString(a.Email),
				teamRoleBadge(a.Role),
				statusHTML,
				a.CreatedAt.Format("02 Jan 2006"),
				editBtn,
			)
			if err != nil {
				return err
			}

			// Edit modal per member (skip for self)
			if actor == nil || a.ID != actor.ID {
				currentCap := 0
				if limits != nil {
					if lim, ok := limits[a.ID]; ok {
						currentCap = lim.MonthlyMsgCap
					}
				}
				if _, err := io.WriteString(w, buildTeamEditModal(a, currentCap)); err != nil {
					return err
				}
			}
		}

		if _, err := io.WriteString(w, `</tbody></table></div>`); err != nil {
			return err
		}

		// Recent activity sidebar
		if _, err := io.WriteString(w, `<aside class="team-activity"><h3 class="team-act-title">Recent activity</h3><div class="team-act-list">`); err != nil {
			return err
		}

		if len(auditLog) == 0 {
			if _, err := io.WriteString(w, `<p class="team-act-empty">No activity yet.</p>`); err != nil {
				return err
			}
		}

		shown := 0
		for _, e := range auditLog {
			if shown >= 10 {
				break
			}
			shown++
			label := auditActionLabel(e)
			_, err := fmt.Fprintf(w, `<div class="team-act-item">
  <div class="team-act-av %s">%s</div>
  <div class="team-act-body">
    <span class="team-act-name">%s</span> <span class="team-act-desc">%s</span>
    <div class="team-act-time">%s</div>
  </div>
</div>`,
				avatarColor(e.ActorName), html.EscapeString(twoInitials(e.ActorName)),
				html.EscapeString(e.ActorName),
				html.EscapeString(label),
				timeAgo(e.CreatedAt),
			)
			if err != nil {
				return err
			}
		}

		if _, err := io.WriteString(w, `</div></aside></div>`); err != nil {
			return err
		}

		// Invite modal
		inviteBody := `<form hx-post="/team/invite" hx-target="body" hx-swap="none"` +
			` hx-disabled-elt="find button[type='submit']"` +
			` hx-on::after-request="if(event.detail.successful)document.getElementById('invite-modal').close()"` +
			` hx-on::response-error="document.getElementById('inv-form-errors').innerHTML=event.detail.xhr.responseText">` +
			`<div id="inv-form-errors" role="alert" aria-live="polite"></div>` +
			`<div class="form-group"><label class="form-label" for="inv-name">Name</label>` +
			`<input id="inv-name" class="form-input" type="text" name="name" required></div>` +
			`<div class="form-group"><label class="form-label" for="inv-email">Email</label>` +
			`<input id="inv-email" class="form-input" type="email" name="email" required></div>` +
			`<div class="form-group"><label class="form-label" for="inv-role">Role</label>` +
			`<select id="inv-role" class="form-input" name="role">` +
			`<option value="agent">Agent</option>` +
			`<option value="manager">Manager</option>` +
			`<option value="admin">Admin</option>` +
			`</select></div>` +
			`<div class="modal-btns">` +
			`<button class="btn btn-primary btn-sm" type="submit">Send invite</button>` +
			`<button class="btn btn-secondary btn-sm" type="button" onclick="document.getElementById('invite-modal').close()">Cancel</button>` +
			`</div></form>`

		if _, err := io.WriteString(w, ModalShell("invite-modal", "Invite member", inviteBody)+`</div>`); err != nil {
			return err
		}

		_, err = io.WriteString(w, ShellClose())
		return err
	})
}

func teamRoleBadge(role string) string {
	switch role {
	case "admin":
		return `<span class="team-role-badge team-role-badge--admin">Admin</span>`
	case "manager":
		return `<span class="team-role-badge team-role-badge--manager">Manager</span>`
	default:
		return `<span class="team-role-badge team-role-badge--agent">Agent</span>`
	}
}

func buildTeamEditModal(a *db.Agent, currentCap int) string {
	activateHTML := ""
	if a.Active && a.InviteToken == nil {
		activateHTML = fmt.Sprintf(`<form method="post" action="/team/%s/activate" style="display:inline">
  <input type="hidden" name="active" value="false">
  <button class="btn btn-secondary btn-sm" type="submit">Deactivate</button>
</form>`, a.ID)
	} else {
		activateHTML = fmt.Sprintf(`<form method="post" action="/team/%s/activate" style="display:inline">
  <input type="hidden" name="active" value="true">
  <button class="btn btn-primary btn-sm" type="submit">Activate</button>
</form>`, a.ID)
	}

	reinviteHTML := ""
	if a.InviteToken != nil {
		reinviteHTML = fmt.Sprintf(`<form method="post" action="/team/%s/reinvite" style="display:inline">
  <button class="btn btn-secondary btn-sm" type="submit">Resend invite</button>
</form>`, a.ID)
	}

	body := fmt.Sprintf(`
<div class="team-edit-hd">
  <div class="team-av %s" style="width:48px;height:48px;font-size:16px;margin:0 auto 8px">%s</div>
  <div style="font-weight:600;font-size:15px">%s</div>
  <div style="color:var(--text-secondary);font-size:13px">%s</div>
</div>

<form method="post" action="/team/%s/role" class="team-edit-section">
  <div class="form-group" style="margin-bottom:0">
    <label class="form-label">Role</label>
    <select class="form-input" name="role" style="max-width:200px">
      <option value="admin"%s>Admin</option>
      <option value="manager"%s>Manager</option>
      <option value="agent"%s>Agent</option>
    </select>
  </div>
  <div style="margin-top:10px">
    <button class="btn btn-primary btn-sm" type="submit">Save role</button>
  </div>
</form>

<form method="post" action="/team/%s/limits" class="team-edit-section">
  <div class="form-group" style="margin-bottom:0">
    <label class="form-label">Monthly message cap <span style="color:var(--text-muted);font-weight:400">(0 = unlimited)</span></label>
    <input class="form-input" type="number" name="monthly_msg_cap" min="0" value="%d" style="max-width:160px">
  </div>
  <div style="margin-top:10px">
    <button class="btn btn-primary btn-sm" type="submit">Save cap</button>
  </div>
</form>

<div class="team-edit-section">
  <div style="font-size:12px;font-weight:500;text-transform:uppercase;letter-spacing:.04em;color:var(--text-muted);margin-bottom:10px">Status</div>
  <div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap">
    %s
    %s
  </div>
</div>

<div class="team-edit-section team-edit-danger">
  <div style="font-size:12px;font-weight:500;text-transform:uppercase;letter-spacing:.04em;color:var(--text-muted);margin-bottom:10px">Danger zone</div>
  <button class="btn btn-sm" style="background:var(--danger-light);color:var(--danger);border:1px solid var(--danger-light)"
    hx-delete="/team/%s"
    hx-confirm="Remove %s from the team? This cannot be undone."
    hx-swap="none"
    hx-on::after-request="window.location.reload()">Remove from team</button>
</div>`,
		avatarColor(a.Name), html.EscapeString(twoInitials(a.Name)),
		html.EscapeString(a.Name), html.EscapeString(a.Email),
		a.ID,
		sel(a.Role, "admin"), sel(a.Role, "manager"), sel(a.Role, "agent"),
		a.ID, currentCap,
		activateHTML, reinviteHTML,
		a.ID, html.EscapeString(a.Name),
	)

	return ModalShell("edit-"+a.ID, "Edit member", body)
}

func timeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("02 Jan")
	}
}

func auditActionLabel(e *db.AuditEntry) string {
	switch e.Action {
	case "campaign_sent":
		if name, ok := e.Details["name"].(string); ok && name != "" {
			return `sent campaign "` + name + `"`
		}
		return "sent a campaign"
	case "contacts_imported":
		if n, ok := e.Details["count"].(float64); ok {
			return fmt.Sprintf("imported %d contacts", int(n))
		}
		return "imported contacts"
	case "template_created":
		if name, ok := e.Details["name"].(string); ok && name != "" {
			return `created template "` + name + `"`
		}
		return "created a template"
	case "template_submitted":
		if name, ok := e.Details["name"].(string); ok && name != "" {
			return `submitted template "` + name + `" for approval`
		}
		return "submitted a template for approval"
	case "agent_invited":
		if name, ok := e.Details["name"].(string); ok && name != "" {
			return "invited " + name + " to the team"
		}
		return "invited a team member"
	case "agent_role_changed":
		if role, ok := e.Details["new_role"].(string); ok {
			return "changed a member role to " + role
		}
		return "changed a member's role"
	case "agent_activated":
		return "activated a member"
	case "agent_deactivated":
		return "deactivated a member"
	case "agent_deleted":
		return "removed a team member"
	case "settings_updated":
		return "updated settings"
	case "message_sent":
		return "sent a message"
	case "campaign_queued":
		if name, ok := e.Details["name"].(string); ok && name != "" {
			return `queued campaign "` + name + `"`
		}
		return "queued a campaign"
	case "contact_created":
		return "added a contact"
	case "contact_opted_out":
		return "processed opt-out"
	default:
		return strings.ReplaceAll(e.Action, "_", " ")
	}
}

// sel returns " selected" when current == value (used in <select> options).
func sel(current, value string) string {
	if current == value {
		return ` selected`
	}
	return ""
}
