package templates

import (
	"fmt"
	mw "whatsapptool/internal/web/middleware"
)

type invRoleCard struct{ val, title, desc string }

var invRoleCards = []invRoleCard{
	{"admin", "Admin", "Full access — billing, settings, all features"},
	{"manager", "Manager", "Campaigns, contacts, templates, team view"},
	{"agent", "Agent", "Inbox and contact replies only"},
}

const invBackSVG = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" width="14" height="14"><polyline points="15 18 9 12 15 6"/></svg>`

func inviterName(agent *mw.AgentClaims) string {
	if agent != nil {
		return agent.Name
	}
	return "Your colleague"
}

func invPrefillRole(r string) string {
	if r == "" {
		return "agent"
	}
	return r
}

// inviteAppScript returns a <script> block that defines inviteApp() for Alpine,
// embedding the server-known prefill values as JS literals.
func inviteAppScript(role, name string) string {
	return fmt.Sprintf(`<script>
function inviteApp(){return{role:'%s',invName:'%s',get roleDesc(){return{admin:'Full access — billing, settings, all features',manager:'Campaigns, contacts, templates, team view',agent:'Inbox and contact replies only'}[this.role]||'';}}}
</script>`, jsStr(role), jsStr(name))
}
