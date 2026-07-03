package templates

import (
	"strings"

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

func acctRole(agent *mw.AgentClaims, a *db.Agent) string {
	if a.Role != "" {
		return a.Role
	}
	if agent != nil {
		return agent.Role
	}
	return ""
}

func acctPhone(a *db.Agent) string {
	if a.Phone != nil {
		return *a.Phone
	}
	return ""
}

func acctNotifPref(a *db.Agent, key string) bool {
	v, ok := a.Preferences[key]
	if !ok {
		return true
	}
	b, _ := v.(bool)
	return b
}

func roleBadgeVariant(role string) BadgeVariant {
	switch role {
	case "admin":
		return BadgeApproved
	case "manager":
		return BadgePending
	default:
		return BadgeNeutral
	}
}

func roleBadgeLabel(role string) string {
	if role == "" {
		return ""
	}
	return strings.ToUpper(string(role[0])) + role[1:]
}
