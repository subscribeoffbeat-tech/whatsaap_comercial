package templates

// invite_form_page.go — full-page invite form for admin users.

import (
	"context"
	"fmt"
	"html"
	"io"

	"github.com/a-h/templ"
	mw "whatsapptool/internal/web/middleware"
)

// InviteFormPage renders the two-column "Invite team member" page.
// inviterName is the name of the logged-in admin shown in the email preview.
// errMsg is non-empty when the form has a validation error (re-render with same values).
// prefill values are the form values to keep on error.
func InviteFormPage(
	agent *mw.AgentClaims,
	errMsg string,
	prefillName, prefillEmail, prefillRole, prefillMsg string,
) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/team", "Invite team member", "")); err != nil {
			return err
		}

		inviterName := "Your colleague"
		if agent != nil {
			inviterName = agent.Name
		}
		if prefillRole == "" {
			prefillRole = "agent"
		}

		roleDescs := map[string]string{
			"admin":   "Full access — billing, settings, all features",
			"manager": "Campaigns, contacts, templates, team view",
			"agent":   "Inbox and contact replies only",
		}

		xData := fmt.Sprintf(
			`{role:'%s',invName:'%s',get roleDesc(){return{admin:'Full access — billing, settings, all features',manager:'Campaigns, contacts, templates, team view',agent:'Inbox and contact replies only'}[this.role]||''}}`,
			html.EscapeString(prefillRole),
			html.EscapeString(inviterName),
		)

		if _, err := fmt.Fprintf(w, `
<div class="page-wrap">
<a class="inv-back" href="/team">
  <span class="inv-back-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" width="14" height="14"><polyline points="15 18 9 12 15 6"/></svg></span>
</a>
<div class="inv-page-wrap" x-data="%s">`, html.EscapeString(xData)); err != nil {
			return err
		}

		// ── LEFT: form card ────────────────────────────────────────────────
		if _, err := fmt.Fprintf(w, `
<div class="card-static inv-form-card">
  %s
  <div class="inv-form-hd">
    <div class="inv-form-title">Invite team member</div>
    <div class="inv-form-sub">They'll receive an email to join your workspace</div>
  </div>
  <form method="post" action="/team/invite">
    <div class="inv-row-2">
      <div class="form-group">
        <label class="form-label" for="inv-name">Full name <span class="req">*</span></label>
        <input id="inv-name" class="form-input" type="text" name="name" value="%s" placeholder="Aarav Mehta" required x-model="invName" @input.debounce="invName=$el.value">
      </div>
      <div class="form-group">
        <label class="form-label" for="inv-email">Work email <span class="req">*</span></label>
        <input id="inv-email" class="form-input" type="email" name="email" value="%s" placeholder="aarav@theoffbeat.agency" required>
      </div>
    </div>

    <div class="form-group">
      <div class="inv-role-label">Role</div>
      <div class="inv-role-cards">`,
			FormBanner(errMsg),
			html.EscapeString(prefillName),
			html.EscapeString(prefillEmail),
		); err != nil {
			return err
		}

		// Role radio cards
		type roleCard struct{ val, title, desc string }
		cards := []roleCard{
			{"admin", "Admin", roleDescs["admin"]},
			{"manager", "Manager", roleDescs["manager"]},
			{"agent", "Agent", roleDescs["agent"]},
		}
		for _, rc := range cards {
			if _, err := fmt.Fprintf(w,
				`<label class="au-option-card" :class="{'au-option-card--sel': role==='%s'}">
  <input type="radio" name="role" value="%s" x-model="role" class="sr-only"%s>
  <span class="au-option-body">
    <span class="au-option-title">%s</span>
    <span class="au-option-desc">%s</span>
  </span>
  <span class="au-option-check" :class="{'au-option-check--on': role==='%s'}"></span>
</label>`,
				rc.val, rc.val,
				func() string {
					if rc.val == prefillRole {
						return " checked"
					}
					return ""
				}(),
				html.EscapeString(rc.title),
				html.EscapeString(rc.desc),
				rc.val,
			); err != nil {
				return err
			}
		}

		if _, err := fmt.Fprintf(w, `
      </div>
    </div>

    <div class="form-group">
      <label class="form-label" for="inv-msg">Personal message <span style="font-weight:400;color:var(--text-muted)">(optional)</span></label>
      <textarea id="inv-msg" class="form-input" name="personal_message" rows="3" placeholder="Hey! I'd like you to join our WhatsApp workspace on Offbeat ChatFlow...">%s</textarea>
    </div>

    <div class="inv-form-footer">
      <a href="/team" class="btn btn-secondary">Cancel</a>
      <button type="submit" class="btn btn-primary">Send invite</button>
    </div>
  </form>
</div>`, html.EscapeString(prefillMsg)); err != nil {
			return err
		}

		// ── RIGHT: preview sidebar ─────────────────────────────────────────
		if _, err := io.WriteString(w, `
<aside class="inv-preview">
  <div class="inv-email-card">
    <div class="inv-email-hd">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" width="18" height="18"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>
      Offbeat ChatFlow
    </div>
    <div class="inv-email-body">
      <div class="inv-email-msg">
        <span x-text="(invName||'Your colleague')"></span> has invited you to join their WhatsApp workspace.
      </div>
      <div class="inv-email-cta">Accept invitation →</div>
      <div class="inv-email-expiry">Sent to their email. Expires in 7 days.</div>
    </div>
  </div>
  <div class="inv-role-preview">
    <div class="inv-role-preview-label">Role permissions</div>
    <div class="inv-role-preview-desc" x-text="roleDesc"></div>
  </div>
</aside>`); err != nil {
			return err
		}

		if _, err := io.WriteString(w, `</div></div>`); err != nil { // inv-page-wrap + page-wrap
			return err
		}
		_, err := io.WriteString(w, ShellClose())
		return err
	})
}
