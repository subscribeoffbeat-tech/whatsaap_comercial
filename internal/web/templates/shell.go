// Package templates contains all hand-written templ components.
// shell.go provides the shared app chrome (sidebar + topbar) used by every authenticated page.
package templates

import (
	"fmt"
	"html"

	mw "whatsapptool/internal/web/middleware"
)

// NavItem defines one entry in the sidebar navigation.
type NavItem struct {
	Label  string
	Path   string
	Icon   string // SVG path or emoji stand-in
	Roles  []string // nil = all roles
}

var navItems = []NavItem{
	{Label: "Dashboard",  Path: "/",            Icon: "🏠", Roles: nil},
	{Label: "Inbox",      Path: "/inbox",        Icon: "💬", Roles: nil},
	{Label: "Contacts",   Path: "/contacts",     Icon: "👥", Roles: []string{"admin", "manager"}},
	{Label: "Campaigns",  Path: "/campaigns",    Icon: "📣", Roles: []string{"admin", "manager"}},
	{Label: "Templates",  Path: "/templates",    Icon: "📋", Roles: []string{"admin", "manager"}},
	{Label: "Automation", Path: "/automation",   Icon: "⚡", Roles: []string{"admin", "manager"}},
	{Label: "Analytics",  Path: "/analytics",    Icon: "📊", Roles: nil},
	{Label: "Team",       Path: "/team",         Icon: "👤", Roles: []string{"admin"}},
	{Label: "Settings",   Path: "/settings",     Icon: "⚙️", Roles: []string{"admin"}},
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

// SidebarHTML returns the rendered sidebar HTML for the given agent and active path.
func SidebarHTML(agent *mw.AgentClaims, activePath string) string {
	role := ""
	name := "Guest"
	if agent != nil {
		role = agent.Role
		name = agent.Name
	}

	html := `<aside class="sidebar">
  <div class="sidebar-brand">
    <span class="brand-icon">💬</span>
    <span class="brand-name">WhatsApp Tool</span>
  </div>
  <nav class="sidebar-nav">
`
	for _, item := range navItems {
		if !canSee(item, role) {
			continue
		}
		cls := "nav-item"
		if item.Path == activePath ||
			(activePath != "/" && len(item.Path) > 1 && len(activePath) >= len(item.Path) &&
				activePath[:len(item.Path)] == item.Path) {
			cls += " active"
		}
		html += fmt.Sprintf(`    <a href="%s" class="%s">
      <span class="nav-icon">%s</span>
      <span class="nav-label">%s</span>
    </a>
`, item.Path, cls, item.Icon, item.Label)
	}
	html += `  </nav>
  <div class="sidebar-footer">
    <span class="agent-name">` + escHTML(name) + `</span>
    <form method="POST" action="/logout" style="display:inline">
      <button type="submit" class="logout-btn">Sign out</button>
    </form>
  </div>
</aside>
`
	return html
}

// TopbarHTML returns the topbar HTML.
func TopbarHTML(title, qualityRating string) string {
	dot := ""
	if qualityRating != "" {
		dot = fmt.Sprintf(`<span class="quality-dot quality-%s" title="Meta quality: %s"></span>`,
			escHTML(qualityRating), escHTML(qualityRating))
	}
	return fmt.Sprintf(`<header class="topbar">
  <h1 class="page-title">%s</h1>
  <div class="topbar-right">%s</div>
</header>
`, escHTML(title), dot)
}

// ShellOpen writes the page preamble with sidebar and topbar.
// Call ShellClose to write the closing tags.
func ShellOpen(agent *mw.AgentClaims, activePath, pageTitle, qualityRating string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>%s — WhatsApp Tool</title>
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
%s%s<main class="page-content">
`, escHTML(pageTitle),
		SidebarHTML(agent, activePath),
		TopbarHTML(pageTitle, qualityRating),
	)
}

// ShellClose returns the closing HTML for a shell page.
func ShellClose() string {
	return `</main>
</div>
<div id="toast-region" class="toast-region" role="region" aria-label="Notifications" aria-live="polite"></div>
</body>
</html>`
}

func escHTML(s string) string { return html.EscapeString(s) }
