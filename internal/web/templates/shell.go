// Package templates contains all hand-written templ components.
// shell.go provides the shared app chrome (topnav) used by every authenticated page.
package templates

import (
	"fmt"
	"html"
	"strings"

	mw "whatsapptool/internal/web/middleware"
)

// NavItem defines one entry in the topnav navigation.
type NavItem struct {
	Label string
	Path  string
	Icon  string   // kept for compatibility; not used in topnav pills
	Roles []string // nil = all roles
}

var navItems = []NavItem{
	{Label: "Dashboard", Path: "/", Icon: "🏠", Roles: nil},
	{Label: "Inbox", Path: "/inbox", Icon: "💬", Roles: nil},
	{Label: "Contacts", Path: "/contacts", Icon: "👥", Roles: []string{"admin", "manager"}},
	{Label: "Campaigns", Path: "/campaigns", Icon: "📣", Roles: []string{"admin", "manager"}},
	{Label: "Templates", Path: "/templates", Icon: "📋", Roles: []string{"admin", "manager"}},
	{Label: "Automation", Path: "/automation", Icon: "⚡", Roles: []string{"admin", "manager"}},
	{Label: "Analytics", Path: "/analytics", Icon: "📊", Roles: nil},
	{Label: "Team", Path: "/team", Icon: "👤", Roles: []string{"admin"}},
	{Label: "Settings", Path: "/settings", Icon: "⚙️", Roles: []string{"admin"}},
}

// canSee returns true if the agent's role can see the nav item.
func canSee(item NavItem, role string) bool {
	if len(item.Roles) == 0 {
		return true
	}
	for _, r := range item.Roles {
		if r == role {
			return true
		}
	}
	return false
}

func navPillsHTML(agent *mw.AgentClaims, activePath string) string {
	role := ""
	if agent != nil {
		role = agent.Role
	}
	var s string
	for _, item := range navItems {
		if !canSee(item, role) {
			continue
		}
		cls := "topnav-pill"
		if activePath == item.Path ||
			(item.Path != "/" && strings.HasPrefix(activePath, item.Path)) {
			cls += " active"
		}
		s += fmt.Sprintf(`<a href="%s" class="%s">%s</a>
`, item.Path, cls, item.Label)
	}
	return s
}

func qualityBadgeHTML(qualityRating string) string {
	if qualityRating == "" {
		return ""
	}
	cls := "quality-" + strings.ToLower(qualityRating)
	return fmt.Sprintf(`<span class="badge badge-neutral" title="Meta quality: %s" style="gap:6px">
        <span class="quality-dot %s"></span>%s
      </span>`,
		escHTML(qualityRating),
		escHTML(cls),
		escHTML(qualityRating),
	)
}

func avatarInitials(name string) string {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return "?"
	}
	r0 := []rune(parts[0])
	if len(parts) == 1 {
		return strings.ToUpper(string(r0[:1]))
	}
	r1 := []rune(parts[len(parts)-1])
	return strings.ToUpper(string(r0[:1]) + string(r1[:1]))
}

func avatarDropdownHTML(agent *mw.AgentClaims) string {
	name := "Guest"
	email := ""
	if agent != nil {
		name = agent.Name
		email = agent.Email
	}
	initials := avatarInitials(name)
	return fmt.Sprintf(`<div class="dropdown" x-data="{open:false}">
  <button class="topnav-avatar" title="%s" @click="open=!open" @click.outside="open=false">%s</button>
  <div class="dropdown-menu" x-show="open" x-cloak style="min-width:180px">
    <span class="dropdown-item" style="font-size:12px;color:var(--text-muted);cursor:default;line-height:1.4">
      <strong>%s</strong><br>%s
    </span>
    <div class="dropdown-divider"></div>
    <form method="POST" action="/logout" style="margin:0">
      <button type="submit" class="dropdown-item" style="width:100%%;text-align:left">
 Sign out</button>
    </form>
  </div>
</div>`,
		escHTML(name),
		escHTML(initials),
		escHTML(name),
		escHTML(email), // email shown below name in dropdown
	)
}

// ShellOpen writes the page preamble with topnav.
// Call ShellClose to write the closing tags.
func ShellOpen(agent *mw.AgentClaims, activePath, pageTitle, qualityRating string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>%s — Offbeat ChatFlow</title>
<link rel="preconnect" href="https://fonts.googleapis.com"/>
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin/>
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=DM+Sans:wght@400;500;600;700&display=swap"/>
<link rel="stylesheet" href="/static/tokens.css"/>
<link rel="stylesheet" href="/static/app.css"/>
<link rel="stylesheet" href="/static/inbox.css"/>
<link rel="stylesheet" href="/static/contacts.css"/>
<link rel="stylesheet" href="/static/templates.css"/>
<link rel="stylesheet" href="/static/campaigns.css"/>
<link rel="stylesheet" href="/static/automation.css"/>
<link rel="stylesheet" href="/static/analytics.css"/>
<script src="https://unpkg.com/htmx.org@2.0.3" defer></script>
<script src="https://unpkg.com/alpinejs@3.14.3/dist/cdn.min.js" defer></script>
</head>
<body>
<div class="app-layout">
<nav class="topnav">
  <a class="topnav-logo" href="/">
    <img src="/static/logo.png" class="topnav-logo-img" alt="Offbeat ChatFlow">
    <span class="topnav-logo-text">Offbeat ChatFlow</span>
  </a>
  <div class="topnav-pills">
    %s
  </div>
  <div class="topnav-spacer"></div>
  <div class="topnav-actions">
    %s
    <div class="topnav-divider"></div>
    %s
  </div>
</nav>
<div class="main-area">
<div class="screen">
`,
		escHTML(pageTitle),
		navPillsHTML(agent, activePath),
		qualityBadgeHTML(qualityRating),
		avatarDropdownHTML(agent),
	)
}

// ShellClose returns the closing HTML for a shell page.
func ShellClose() string {
	return `</div>
</div>
</div>
<div id="toast-region" class="toast-region" role="region" aria-label="Notifications" aria-live="polite"></div>
</body>
</html>`
}

func escHTML(s string) string { return html.EscapeString(s) }
