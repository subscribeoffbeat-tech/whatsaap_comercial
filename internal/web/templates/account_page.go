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

var timezones = []struct{ v, l string }{
	{"Asia/Kolkata", "Asia/Kolkata (IST, UTC+5:30)"},
	{"Asia/Dubai", "Asia/Dubai (GST, UTC+4)"},
	{"Asia/Singapore", "Asia/Singapore (SGT, UTC+8)"},
	{"Europe/London", "Europe/London (GMT/BST)"},
	{"Europe/Paris", "Europe/Paris (CET, UTC+1)"},
	{"America/New_York", "America/New_York (EST, UTC-5)"},
	{"America/Chicago", "America/Chicago (CST, UTC-6)"},
	{"America/Los_Angeles", "America/Los_Angeles (PST, UTC-8)"},
	{"UTC", "UTC"},
}

func AccountPage(agent *mw.AgentClaims, a *db.Agent, flash string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/account", "My Account", "")); err != nil {
			return err
		}

		// Avatar initials + color
		initials := avatarInitials(a.Name)
		role := a.Role
		if role == "" && agent != nil {
			role = agent.Role
		}
		roleCls := "acct-role-badge"

		// Timezone select options
		tzOpts := ""
		for _, tz := range timezones {
			sel := ""
			if a.Timezone == tz.v {
				sel = ` selected`
			}
			tzOpts += fmt.Sprintf(`<option value="%s"%s>%s</option>`, html.EscapeString(tz.v), sel, html.EscapeString(tz.l))
		}

		// Phone value
		phoneVal := ""
		if a.Phone != nil {
			phoneVal = *a.Phone
		}

		// Flash banner
		flashHTML := ""
		if flash != "" {
			flashHTML = fmt.Sprintf(`<div class="acct-flash">%s</div>`, html.EscapeString(flash))
		}

		// Notification toggles
		notifPref := func(key string) bool {
			v, ok := a.Preferences[key]
			if !ok {
				return true // default on
			}
			b, _ := v.(bool)
			return b
		}
		notifRow := func(key, title, desc string) string {
			checked := ""
			if notifPref(key) {
				checked = ` checked`
			}
			return fmt.Sprintf(`<label class="acct-notif-row">
<div class="acct-notif-info">
  <div class="acct-notif-title">%s</div>
  <div class="acct-notif-desc">%s</div>
</div>
<input class="acct-toggle-cb" type="checkbox" name="%s"%s
  hx-post="/account/notifications"
  hx-include="closest form"
  hx-trigger="change"
  hx-swap="none">
</label>`, html.EscapeString(title), html.EscapeString(desc), html.EscapeString(key), checked)
		}

		_, err := fmt.Fprintf(w, `
<div class="page-wrap acct-wrap">
%s
<div class="acct-hd">
  <div class="screen-title">My Account</div>
  <div class="screen-subtitle">Manage your profile, password and notifications</div>
</div>

<div class="acct-cards">

<!-- Profile card -->
<div class="card acct-card">
  <div class="acct-card-title">Profile</div>
  <div class="acct-profile-hd">
    <div class="acct-av">%s</div>
    <div>
      <div class="acct-name">%s</div>
      <div class="acct-email-disp">%s</div>
      <span class="%s">%s</span>
    </div>
  </div>
  <form hx-post="/account/profile" hx-swap="none"
    hx-on::after-request="if(event.detail.successful)window.__showToast('success','Profile saved')">
    <div class="acct-form-grid">
      <div class="form-group">
        <label class="form-label">Full name</label>
        <input class="form-input" type="text" name="name" value="%s" required>
      </div>
      <div class="form-group">
        <label class="form-label">Email</label>
        <input class="form-input" type="email" name="email" value="%s" required>
      </div>
      <div class="form-group">
        <label class="form-label">Phone</label>
        <input class="form-input" type="tel" name="phone" value="%s" placeholder="+91 98765 43210">
      </div>
      <div class="form-group">
        <label class="form-label">Timezone</label>
        <select class="form-input" name="timezone">%s</select>
      </div>
    </div>
    <div class="acct-form-footer">
      <button class="btn btn-primary" type="submit">Save profile</button>
    </div>
  </form>
</div>

<!-- Notifications card -->
<div class="card acct-card">
  <div class="acct-card-title">Notifications</div>
  <form id="notif-form">
    %s
    %s
    %s
    %s
  </form>
</div>

<!-- Sign out everywhere -->
<div class="card acct-card acct-danger-card">
  <div>
    <div class="acct-danger-title">Sign out everywhere</div>
    <div class="acct-danger-desc">Signs you out of all devices and sessions</div>
  </div>
  <form method="POST" action="/account/signout-all">
    <button class="btn acct-signout-btn" type="submit">Sign out</button>
  </form>
</div>

</div>
</div>
`,
			flashHTML,
			html.EscapeString(initials),
			html.EscapeString(a.Name),
			html.EscapeString(a.Email),
			roleCls, html.EscapeString(role),
			html.EscapeString(a.Name),
			html.EscapeString(a.Email),
			html.EscapeString(phoneVal),
			tzOpts,
			notifRow("notif_new_message", "New message", "Alert when a contact sends a message to inbox"),
			notifRow("notif_campaign_complete", "Campaign complete", "Notify when a campaign finishes sending"),
			notifRow("notif_team_activity", "Team activity", "Updates on what your team members are doing"),
			notifRow("notif_weekly_report", "Weekly report", "Summary email every Monday morning"),
		)
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, ShellClose())
		return err
	})
}
