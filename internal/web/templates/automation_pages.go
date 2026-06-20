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

func AutomationPage(agent *mw.AgentClaims, rules []*db.AutomationRule, tmpls []db.Template) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/automation", "Automation", "")); err != nil {
			return err
		}
		_, err := io.WriteString(w, `
<div class="page-wrap">
<div class="page-hd">
<div>
<h1 class="screen-title">Automation rules</h1>
<p class="screen-subtitle">Keyword replies, welcome messages, away messages, and opt-out handling.</p>
</div>
<a class="btn btn-primary btn-sm" href="/automation/new">+ New rule</a>
</div>

<div x-data="{ trigger: '' }">
<div class="au-tabs">
  <button type="button" class="au-tab" :class="trigger==='' ? 'active' : ''" @click="trigger=''">All</button>
  <button type="button" class="au-tab" :class="trigger==='keyword' ? 'active' : ''" @click="trigger='keyword'">Keyword</button>
  <button type="button" class="au-tab" :class="trigger==='welcome' ? 'active' : ''" @click="trigger='welcome'">Welcome</button>
  <button type="button" class="au-tab" :class="trigger==='away' ? 'active' : ''" @click="trigger='away'">Away</button>
  <button type="button" class="au-tab" :class="trigger==='stop' ? 'active' : ''" @click="trigger='stop'">Stop</button>
</div>
<div class="card-static">
<table class="au-table" id="rules-table">
<thead><tr><th>Name</th><th>Trigger</th><th>Keyword</th><th>Active</th><th>Actions</th></tr></thead>
<tbody>`)
		if err != nil {
			return err
		}
		if len(rules) == 0 {
			if _, err := io.WriteString(w, `<tr><td colspan="5"><div class="empty-state"><div class="empty-icon">&#9889;</div><div class="empty-title">No rules yet</div><div class="empty-desc">Create a keyword reply, welcome message, or away message to automate responses.</div></div></td></tr>`); err != nil {
				return err
			}
		} else {
			for _, r := range rules {
				if err := AutomationRuleRow(r, tmpls).Render(ctx, w); err != nil {
					return err
				}
			}
		}
		_, err = io.WriteString(w, `</tbody></table></div></div></div>`)
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, ShellClose())
		return err
	})
}

func AutomationForm(agent *mw.AgentClaims, rule *db.AutomationRule, tmpls []db.Template) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/automation", "Automation", "")); err != nil {
			return err
		}

		title := "New rule"
		action := "/automation"
		method := "post"
		var name, triggerType, keyword, keywordMatch, responseText, templateID string
		active := true
		priority := 10

		if rule != nil {
			title = "Edit rule"
			action = fmt.Sprintf("/automation/%d", rule.ID)
			method = "post"
			name = rule.Name
			triggerType = rule.TriggerType
			keywordMatch = rule.KeywordMatch
			active = rule.Active
			priority = rule.Priority
			if rule.Keyword != nil {
				keyword = *rule.Keyword
			}
			if rule.ResponseText != nil {
				responseText = *rule.ResponseText
			}
			if rule.TemplateID != nil {
				templateID = *rule.TemplateID
			}
		}
		if keywordMatch == "" {
			keywordMatch = "exact"
		}

		_, err := fmt.Fprintf(w, `
<div class="page-wrap">
<div class="page-hd"><div class="screen-title">%s</div></div>
<div class="card-static au-form-card">
<form method="%s" action="%s">
<div class="form-group">
  <label class="form-label">Name</label>
  <input class="form-input" type="text" name="name" value="%s" required>
</div>

<div class="form-group">
  <label class="form-label">Trigger type</label>
  <select class="form-input" name="trigger_type">
    <option value="keyword"%s>Keyword</option>
    <option value="welcome"%s>Welcome (first message)</option>
    <option value="away"%s>Away (outside hours)</option>
  </select>
</div>

<div class="form-group">
  <label class="form-label">Keyword</label>
  <input class="form-input" type="text" name="keyword" value="%s">
</div>

<div class="form-group">
  <label class="form-label">Keyword match</label>
  <select class="form-input" name="keyword_match">
    <option value="exact"%s>Exact</option>
    <option value="contains"%s>Contains</option>
  </select>
</div>

<div class="form-group">
  <label class="form-label">Response text</label>
  <textarea class="form-input" name="response_text" rows="3">%s</textarea>
</div>

<div class="form-group">
  <label class="form-label">Template (optional)</label>
  <select class="form-input" name="template_id">
    <option value="">— none —</option>`,
			html.EscapeString(title), method, html.EscapeString(action),
			html.EscapeString(name),
			sel(triggerType, "keyword"), sel(triggerType, "welcome"), sel(triggerType, "away"),
			html.EscapeString(keyword),
			sel(keywordMatch, "exact"), sel(keywordMatch, "contains"),
			html.EscapeString(responseText),
		)
		if err != nil {
			return err
		}

		for _, t := range tmpls {
			selected := ""
			if t.ID == templateID {
				selected = " selected"
			}
			if _, err := fmt.Fprintf(w, `<option value="%s"%s>%s</option>`,
				html.EscapeString(t.ID), selected, html.EscapeString(t.Name)); err != nil {
				return err
			}
		}

		activeAttr := ""
		if active {
			activeAttr = " checked"
		}
		_, err = fmt.Fprintf(w, `</select>
</div>

<div class="form-group">
  <label class="form-label">Priority <span style="color:var(--text-muted);font-weight:400">(lower number = higher priority)</span></label>
  <input class="form-input" type="number" name="priority" value="%d" min="0" style="max-width:120px">
</div>

<div class="form-group">
  <label class="form-label">Active</label>
  <label class="toggle-wrap">
    <span class="toggle-sw">
      <input type="checkbox" name="active"%s>
      <span class="toggle-track"></span>
      <span class="toggle-thumb"></span>
    </span>
    <span style="font-size:13px;color:var(--text-secondary)">Rule fires when matched</span>
  </label>
</div>

<div class="form-btns">
  <a class="btn btn-secondary" href="/automation">Cancel</a>
  <button class="btn btn-primary" type="submit">Save rule</button>
</div>
</form>
</div>
</div>`, priority, activeAttr)
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, ShellClose())
		return err
	})
}

func AutomationRuleRow(rule *db.AutomationRule, tmpls []db.Template) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		keyword := "—"
		if rule.Keyword != nil {
			keyword = *rule.Keyword
		}

		// Trigger badge
		triggerBadgeClass := "badge badge-trigger-" + rule.TriggerType
		if rule.IsSystem() {
			triggerBadgeClass = "badge badge-trigger-system"
		}

		// Toggle switch form
		checkedAttr := ""
		if rule.Active {
			checkedAttr = " checked"
		}
		activeTitle := "Inactive — click to activate"
		if rule.Active {
			activeTitle = "Active — click to deactivate"
		}
		toggleHTML := fmt.Sprintf(`<form method="post" action="/automation/%d/toggle" style="margin:0">
  <button type="submit" class="toggle-wrap" title="%s" style="background:none;border:none;cursor:pointer;padding:0">
    <span class="toggle-sw">
      <input type="checkbox"%s style="pointer-events:none">
      <span class="toggle-track"></span>
      <span class="toggle-thumb"></span>
    </span>
  </button>
</form>`, rule.ID, html.EscapeString(activeTitle), checkedAttr)

		// Action buttons (system rules cannot be edited/deleted)
		actionsHTML := ""
		if !rule.IsSystem() {
			actionsHTML = fmt.Sprintf(`<div class="au-actions">
<a class="btn btn-sm btn-secondary" href="/automation/%d/edit">Edit</a>
<button class="btn btn-sm btn-danger"
  hx-delete="/automation/%d"
  hx-confirm="Delete this rule?"
  hx-target="closest tr"
  hx-swap="outerHTML">Delete</button>
</div>`, rule.ID, rule.ID)
		}

		_, err := fmt.Fprintf(w,
			`<tr id="rule-%d" x-show="trigger==='' || trigger==='%s'">
<td style="font-weight:500">%s</td>
<td><span class="%s">%s</span></td>
<td>%s</td>
<td>%s</td>
<td>%s</td>
</tr>`,
			rule.ID, rule.TriggerType,
			html.EscapeString(rule.Name),
			triggerBadgeClass, rule.TriggerType,
			html.EscapeString(keyword),
			toggleHTML,
			actionsHTML,
		)
		return err
	})
}
