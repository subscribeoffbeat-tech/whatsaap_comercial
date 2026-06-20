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
		_, err := fmt.Fprintf(w, `
<div class="page-wrap">
<div class="page-hd">
<div>
<h1 class="screen-title">Automation rules</h1>
<p class="screen-subtitle">Keyword replies, welcome messages, away messages, and opt-out handling.</p>
</div>
<a class="btn btn-primary btn-sm" href="/automation/new">+ New rule</a>
</div>
<table class="tbl" id="rules-table">
<thead><tr><th>Name</th><th>Trigger</th><th>Keyword</th><th>Active</th><th>Actions</th></tr></thead>
<tbody>`)
		if err != nil {
			return err
		}
		for _, r := range rules {
			if err := AutomationRuleRow(r, tmpls).Render(ctx, w); err != nil {
				return err
			}
		}
		_, err = io.WriteString(w, `</tbody></table></div>`)
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
<form method="%s" action="%s" class="form-card">
<label class="field"><span>Name</span>
<input type="text" name="name" value="%s" required></label>

<label class="field"><span>Trigger type</span>
<select name="trigger_type">
<option value="keyword"%s>Keyword</option>
<option value="welcome"%s>Welcome (first message)</option>
<option value="away"%s>Away (outside hours)</option>
</select></label>

<label class="field"><span>Keyword</span>
<input type="text" name="keyword" value="%s"></label>

<label class="field"><span>Keyword match</span>
<select name="keyword_match">
<option value="exact"%s>Exact</option>
<option value="contains"%s>Contains</option>
</select></label>

<label class="field"><span>Response text</span>
<textarea name="response_text" rows="3">%s</textarea></label>

<label class="field"><span>Template (optional)</span>
<select name="template_id">
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
		_, err = fmt.Fprintf(w, `</select></label>

<label class="field"><span>Priority (lower = higher priority)</span>
<input type="number" name="priority" value="%d" min="0"></label>

<label class="field cb">
<input type="checkbox" name="active"%s>
<span>Active</span></label>

<div class="form-btns">
<button class="btn btn-primary btn-sm" type="submit">Save rule</button>
<a class="btn btn-secondary btn-sm" href="/automation">Cancel</a>
</div>
</form>
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
		activeLabel := "No"
		if rule.Active {
			activeLabel = "Yes"
		}
		editBtn := ""
		if !rule.IsSystem() {
			editBtn = fmt.Sprintf(`<a class="btn btn-sm btn-secondary" href="/automation/%d/edit">Edit</a>
<button class="btn btn-sm btn-danger"
  hx-delete="/automation/%d"
  hx-confirm="Delete this rule?"
  hx-target="closest tr"
  hx-swap="outerHTML">Delete</button>`,
				rule.ID, rule.ID)
		}
		_, err := fmt.Fprintf(w,
			`<tr id="rule-%d"><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>`,
			rule.ID, html.EscapeString(rule.Name), rule.TriggerType,
			html.EscapeString(keyword), activeLabel, editBtn)
		return err
	})
}
