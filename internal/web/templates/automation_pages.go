package templates

// automation_pages.go — v3: radio-card builder form + redesigned list table.

import (
	"context"
	"fmt"
	"html"
	"io"
	"strings"

	"github.com/a-h/templ"

	"whatsapptool/internal/db"
	mw "whatsapptool/internal/web/middleware"
)

// ── Trigger / action display helpers ─────────────────────────────────────────

func auTriggerLabel(tt string) string {
	switch tt {
	case "keyword":
		return "Keyword"
	case "welcome":
		return "Contact added"
	case "away":
		return "Out of hours"
	case "stop":
		return "Keyword"
	case "no_reply":
		return "No reply"
	case "new_conversation":
		return "New conversation"
	case "opt_out":
		return "Opt-out"
	default:
		return tt
	}
}

func auTriggerBadge(tt string) string {
	cls := "badge au-badge-" + strings.ReplaceAll(tt, "_", "-")
	return fmt.Sprintf(`<span class="%s">%s</span>`, cls, html.EscapeString(auTriggerLabel(tt)))
}

func auKeywordChips(r *db.AutomationRule) string {
	var kws []string
	if r.TriggerType == "stop" {
		kws = []string{"STOP", "UNSUBSCRIBE"}
	} else if r.Keyword != nil && *r.Keyword != "" {
		for _, kw := range strings.Split(*r.Keyword, ",") {
			kw = strings.TrimSpace(kw)
			if kw != "" {
				kws = append(kws, kw)
			}
		}
	}
	if len(kws) == 0 {
		return `<span class="au-dash">—</span>`
	}
	var b strings.Builder
	for _, kw := range kws {
		b.WriteString(`<code class="au-kw-chip">`)
		b.WriteString(html.EscapeString(kw))
		b.WriteString(`</code> `)
	}
	return b.String()
}

func auActionText(r *db.AutomationRule, tmplMap map[string]string) string {
	at := r.ActionType
	if at == "" {
		if r.IsSystem() {
			return "Remove consent + confirm"
		}
		if r.TemplateID != nil {
			at = "send_template"
		} else if r.ResponseText != nil {
			s := *r.ResponseText
			if len([]rune(s)) > 42 {
				s = string([]rune(s)[:42]) + "…"
			}
			return html.EscapeString(s)
		} else {
			return `<span class="au-dash">—</span>`
		}
	}
	switch at {
	case "send_template":
		name := "Unknown template"
		if r.TemplateID != nil {
			if n, ok := tmplMap[*r.TemplateID]; ok {
				name = n
			}
		}
		return "Send template: " + html.EscapeString(name)
	case "assign_agent":
		return "Assign to agent"
	case "add_tag":
		if r.ResponseText != nil && *r.ResponseText != "" {
			return "Add tag: " + html.EscapeString(*r.ResponseText)
		}
		return "Add tag"
	case "remove_consent":
		return "Remove consent"
	case "webhook":
		return "Call webhook"
	default:
		return html.EscapeString(at)
	}
}

// ── Inline SVG icons ──────────────────────────────────────────────────────────

func auSVG(paths string) string {
	return fmt.Sprintf(
		`<svg class="au-icon-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">%s</svg>`,
		paths,
	)
}

const (
	auSVGSearch   = `<circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/>`
	auSVGPersonP  = `<path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><line x1="19" y1="8" x2="19" y2="14"/><line x1="22" y1="11" x2="16" y2="11"/>`
	auSVGClock    = `<circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/>`
	auSVGHourglass = `<path d="M5 22h14M5 2h14M17 22v-4.2a2 2 0 0 0-.6-1.4L12 12l-4.4 4.4A2 2 0 0 0 7 17.8V22M7 2v4.2a2 2 0 0 1 .6 1.4L12 12l4.4-4.4A2 2 0 0 0 17 6.2V2"/>`
	auSVGChat     = `<path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/>`
	auSVGBlock    = `<circle cx="12" cy="12" r="10"/><line x1="4.93" y1="4.93" x2="19.07" y2="19.07"/>`
	auSVGDoc      = `<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/>`
	auSVGPerson   = `<path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/>`
	auSVGTag      = `<path d="M20.59 13.41l-7.17 7.17a2 2 0 0 1-2.83 0L2 12V2h10l8.59 8.59a2 2 0 0 1 0 2.82z"/><line x1="7" y1="7" x2="7.01" y2="7"/>`
	auSVGLink     = `<path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/>`
	auSVGPencil   = `<path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/>`
	auSVGTrash    = `<polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2"/>`
)

func auOptionCard(model, value, iconCls, iconPaths, title, desc string) string {
	fieldName := model + "_type"
	return fmt.Sprintf(`<label class="au-option-card" :class="{'au-option-card--sel': %s==='%s'}">
  <input type="radio" name="%s" value="%s" x-model="%s" class="sr-only">
  <span class="au-option-icon %s">%s</span>
  <span class="au-option-body">
    <span class="au-option-title">%s</span>
    <span class="au-option-desc">%s</span>
  </span>
  <span class="au-option-check" :class="{'au-option-check--on': %s==='%s'}"></span>
</label>`,
		model, value,
		fieldName, value, model,
		iconCls, auSVG(iconPaths),
		html.EscapeString(title), html.EscapeString(desc),
		model, value,
	)
}

// ── AutomationPage — redesigned list ─────────────────────────────────────────

func AutomationPage(agent *mw.AgentClaims, rules []*db.AutomationRule, tmpls []db.Template, flash string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/automation", "Automation", "")); err != nil {
			return err
		}
		if flash != "" {
			if _, err := io.WriteString(w, FlashScript(ToastSuccess, flash)); err != nil {
				return err
			}
		}

		// Count active rules for subtitle
		activeCount := 0
		for _, r := range rules {
			if r.Active {
				activeCount++
			}
		}

		// Build template name lookup map
		tmplMap := make(map[string]string, len(tmpls))
		for _, t := range tmpls {
			tmplMap[t.ID] = t.Name
		}

		subtitle := fmt.Sprintf("%d active rule", activeCount)
		if activeCount != 1 {
			subtitle += "s"
		}

		if _, err := fmt.Fprintf(w, `
<div class="page-wrap">
<div class="page-hd">
<div>
<h1 class="screen-title">Automation rules</h1>
<p class="screen-subtitle">%s</p>
</div>
<a class="btn btn-primary btn-sm" href="/automation/new">+ New rule</a>
</div>
<div class="card-static au-list-card">
<table class="au-table tbl">
<thead><tr>
  <th>Rule</th>
  <th>Trigger</th>
  <th>Keywords / Condition</th>
  <th>Action</th>
  <th>Active</th>
  <th></th>
</tr></thead>
<tbody>`, html.EscapeString(subtitle)); err != nil {
			return err
		}

		if len(rules) == 0 {
			empty := EmptyStateHTML(EmptyIconAutomation, "No rules yet",
				"Create a keyword reply, welcome message, or away message to automate responses.",
				[]EmptyAction{{Label: "New rule", Primary: true, HREF: "/automation/new"}})
			if _, err := fmt.Fprintf(w, `<tr><td colspan="6">%s</td></tr>`, empty); err != nil {
				return err
			}
		} else {
			for _, r := range rules {
				if err := writeAutomationRow(w, r, tmplMap); err != nil {
					return err
				}
			}
		}

		if _, err := io.WriteString(w, `</tbody></table></div></div>`); err != nil {
			return err
		}
		_, err := io.WriteString(w, ShellClose())
		return err
	})
}

func writeAutomationRow(w io.Writer, r *db.AutomationRule, tmplMap map[string]string) error {
	checkedAttr := ""
	if r.Active {
		checkedAttr = " checked"
	}
	activeTitle := "Inactive — click to activate"
	if r.Active {
		activeTitle = "Active — click to deactivate"
	}

	toggleHTML := fmt.Sprintf(`<form method="post" action="/automation/%d/toggle" style="margin:0">
  <button type="submit" class="toggle-wrap au-toggle-btn" aria-label="%s">
    <span class="toggle-sw">
      <input type="checkbox"%s style="pointer-events:none" tabindex="-1">
      <span class="toggle-track"></span>
      <span class="toggle-thumb"></span>
    </span>
  </button>
</form>`, r.ID, html.EscapeString(activeTitle), checkedAttr)

	actionsHTML := ""
	if !r.IsSystem() {
		actionsHTML = fmt.Sprintf(`
<a href="/automation/%d/edit" class="au-icon-btn" title="Edit rule">%s</a>
<button class="au-icon-btn au-icon-btn--danger" title="Delete rule"
  hx-delete="/automation/%d"
  hx-confirm="Delete this rule?"
  hx-target="closest tr"
  hx-swap="outerHTML swap:0.2s">%s</button>`,
			r.ID, auSVG(auSVGPencil),
			r.ID, auSVG(auSVGTrash),
		)
	} else {
		actionsHTML = `<span class="au-system-lbl">System</span>`
	}

	rowCls := ""
	if r.IsSystem() {
		rowCls = ` class="au-system-row"`
	}

	_, err := fmt.Fprintf(w,
		`<tr id="rule-%d"%s>
<td class="au-rule-name">%s</td>
<td>%s</td>
<td class="au-kw-cell">%s</td>
<td class="au-action-cell">%s</td>
<td>%s</td>
<td class="au-row-actions">%s</td>
</tr>`,
		r.ID, rowCls,
		html.EscapeString(r.Name),
		auTriggerBadge(r.TriggerType),
		auKeywordChips(r),
		auActionText(r, tmplMap),
		toggleHTML,
		actionsHTML,
	)
	return err
}

// ── AutomationForm — two-panel radio-card builder ─────────────────────────────

func AutomationForm(agent *mw.AgentClaims, rule *db.AutomationRule, tmpls []db.Template, errMsg string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/automation", "Automation", "")); err != nil {
			return err
		}

		// Compute initial Alpine state values
		triggerType := "keyword"
		actionType := "send_template"
		var nameVal, keywordVal, keywordMatchVal, responseTextVal, templateIDVal string
		noReplyHoursVal := "24"
		activeAttr := " checked"
		formAction := "/automation"
		isEdit := false
		pageTitle := "New rule"

		if rule != nil {
			isEdit = true
			pageTitle = "Edit rule"
			formAction = fmt.Sprintf("/automation/%d", rule.ID)
			nameVal = rule.Name
			triggerType = rule.TriggerType
			keywordMatchVal = rule.KeywordMatch
			if !rule.Active {
				activeAttr = ""
			}
			if rule.Keyword != nil {
				if rule.TriggerType == "no_reply" {
					noReplyHoursVal = *rule.Keyword
				} else {
					keywordVal = *rule.Keyword
				}
			}
			if rule.ResponseText != nil {
				responseTextVal = *rule.ResponseText
			}
			if rule.TemplateID != nil {
				templateIDVal = *rule.TemplateID
			}
			actionType = rule.ActionType
			if actionType == "" {
				if rule.IsSystem() {
					actionType = "remove_consent"
				} else if rule.TemplateID != nil {
					actionType = "send_template"
				} else if rule.ResponseText != nil {
					actionType = "add_tag"
				}
			}
		}
		if keywordMatchVal == "" {
			keywordMatchVal = "exact"
		}

		// x-data for Alpine
		xData := fmt.Sprintf(`{trigger:'%s',action:'%s'}`, triggerType, actionType)

		if _, err := fmt.Fprintf(w, `
<div class="page-wrap">
<div class="page-hd">
  <div>
    <h1 class="screen-title">%s</h1>
    <a class="au-back-link" href="/automation">← Back to rules</a>
  </div>
</div>
%s
<form method="post" action="%s" x-data="%s">`,
			html.EscapeString(pageTitle),
			FormBanner(errMsg),
			html.EscapeString(formAction),
			html.EscapeString(xData),
		); err != nil {
			return err
		}

		// ── Header bar: rule name + active toggle ──────────────────────────
		if _, err := fmt.Fprintf(w, `
<div class="au-form-header card-static">
  <div class="form-group au-name-group">
    <label class="form-label" for="aut-name">Rule name</label>
    <input id="aut-name" class="form-input" type="text" name="name" value="%s" placeholder="e.g. After-hours reply, Keyword ORDER, Welcome message" required>
  </div>
  <div class="form-group au-active-group">
    <label class="form-label">Active</label>
    <label class="toggle-wrap">
      <span class="toggle-sw">
        <input type="checkbox" name="active"%s>
        <span class="toggle-track"></span>
        <span class="toggle-thumb"></span>
      </span>
      <span class="au-active-lbl">Rule fires when matched</span>
    </label>
  </div>
</div>`, html.EscapeString(nameVal), activeAttr); err != nil {
			return err
		}

		// ── Two-panel builder ──────────────────────────────────────────────
		if _, err := io.WriteString(w, `<div class="au-builder">`); err != nil {
			return err
		}

		// ── LEFT: Trigger panel ────────────────────────────────────────────
		if _, err := io.WriteString(w, `
<div class="au-builder-panel card-static">
  <div class="au-panel-hd">
    <span class="au-step-badge">1</span>
    <span class="au-panel-title">When this happens...</span>
  </div>`); err != nil {
			return err
		}

		// Trigger option cards
		triggerCards := []struct{ val, cls, svg, title, desc string }{
			{"keyword", "au-icon--keyword", auSVGSearch,
				"Keyword match", "Contact sends a specific word or phrase"},
			{"welcome", "au-icon--contact", auSVGPersonP,
				"Contact added", "A new contact sends their first message"},
			{"away", "au-icon--hours", auSVGClock,
				"Out of hours", "A message arrives outside business hours"},
			{"no_reply", "au-icon--reply", auSVGHourglass,
				"No reply after...", "You haven't replied within a set time"},
			{"new_conversation", "au-icon--newconv", auSVGChat,
				"New conversation", "A contact starts a new conversation thread"},
			{"opt_out", "au-icon--optout", auSVGBlock,
				"Contact opts out", "Contact sends an opt-out keyword (STOP etc.)"},
		}
		for _, tc := range triggerCards {
			if _, err := io.WriteString(w, auOptionCard("trigger", tc.val, tc.cls, tc.svg, tc.title, tc.desc)); err != nil {
				return err
			}
		}

		// ── Keyword trigger config ─────────────────────────────────────────
		if _, err := fmt.Fprintf(w, `
<div class="au-config-box" x-show="trigger==='keyword'" x-cloak>
  <div class="form-group">
    <label class="form-label">Keywords <span class="form-hint">comma separated, e.g. ORDER, PRICE</span></label>
    <input class="form-input" type="text" name="keyword" :disabled="trigger!=='keyword'" value="%s" placeholder="ORDER, PRICE, INFO">
  </div>
  <div class="form-group">
    <label class="form-label">Match type</label>
    <select class="form-input" name="keyword_match" :disabled="trigger!=='keyword'">
      <option value="exact"%s>Exact word</option>
      <option value="contains"%s>Contains phrase</option>
    </select>
  </div>
</div>`,
			html.EscapeString(keywordVal),
			sel(keywordMatchVal, "exact"), sel(keywordMatchVal, "contains"),
		); err != nil {
			return err
		}

		// ── No reply trigger config ────────────────────────────────────────
		if _, err := fmt.Fprintf(w, `
<div class="au-config-box" x-show="trigger==='no_reply'" x-cloak>
  <div class="form-group">
    <label class="form-label">Trigger after how long without a reply?</label>
    <div class="au-inline-input">
      <input class="form-input" type="number" name="keyword" :disabled="trigger!=='no_reply'" value="%s" min="1" max="168" style="max-width:100px">
      <span class="au-input-suffix">hours</span>
    </div>
  </div>
</div>`, html.EscapeString(noReplyHoursVal)); err != nil {
			return err
		}

		if _, err := io.WriteString(w, `</div>`); err != nil { // close left panel
			return err
		}

		// ── Separator arrow ────────────────────────────────────────────────
		if _, err := io.WriteString(w, `<div class="au-builder-sep"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" width="20" height="20"><polyline points="9 18 15 12 9 6"/></svg></div>`); err != nil {
			return err
		}

		// ── RIGHT: Action panel ────────────────────────────────────────────
		if _, err := io.WriteString(w, `
<div class="au-builder-panel card-static">
  <div class="au-panel-hd">
    <span class="au-step-badge au-step-badge--2">2</span>
    <span class="au-panel-title">Do this...</span>
  </div>`); err != nil {
			return err
		}

		actionCards := []struct{ val, cls, svg, title, desc string }{
			{"send_template", "au-icon--template", auSVGDoc,
				"Send a template", "Send an approved WhatsApp template message"},
			{"assign_agent", "au-icon--agent", auSVGPerson,
				"Assign to agent", "Route the conversation to a team member"},
			{"add_tag", "au-icon--tag", auSVGTag,
				"Add a tag", "Tag the contact for segmentation"},
			{"remove_consent", "au-icon--consent", auSVGBlock,
				"Remove consent", "Opt the contact out of marketing messages"},
			{"webhook", "au-icon--webhook", auSVGLink,
				"Call a webhook", "Send contact data to an external URL"},
		}
		for _, ac := range actionCards {
			if _, err := io.WriteString(w, auOptionCard("action", ac.val, ac.cls, ac.svg, ac.title, ac.desc)); err != nil {
				return err
			}
		}

		// ── Send template config ───────────────────────────────────────────
		if _, err := io.WriteString(w, `
<div class="au-config-box" x-show="action==='send_template'" x-cloak>
  <div class="form-group">
    <label class="form-label">Template</label>
    <select class="form-input" name="template_id" :disabled="action!=='send_template'">
      <option value="">— select a template —</option>`); err != nil {
			return err
		}
		for _, t := range tmpls {
			selected := ""
			if t.ID == templateIDVal {
				selected = " selected"
			}
			if _, err := fmt.Fprintf(w, `<option value="%s"%s>%s</option>`,
				html.EscapeString(t.ID), selected, html.EscapeString(t.Name)); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(w, `</select></div></div>`); err != nil {
			return err
		}

		// ── Add tag config ─────────────────────────────────────────────────
		if _, err := fmt.Fprintf(w, `
<div class="au-config-box" x-show="action==='add_tag'" x-cloak>
  <div class="form-group">
    <label class="form-label">Tag name</label>
    <input class="form-input" type="text" name="response_text" :disabled="action!=='add_tag' && action!=='webhook'" value="%s" placeholder="e.g. hot-lead, vip, support">
  </div>
</div>`, func() string {
			if !isEdit || rule == nil || rule.ActionType != "add_tag" {
				return ""
			}
			return html.EscapeString(responseTextVal)
		}()); err != nil {
			return err
		}

		// ── Webhook config ─────────────────────────────────────────────────
		if _, err := fmt.Fprintf(w, `
<div class="au-config-box" x-show="action==='webhook'" x-cloak>
  <div class="form-group">
    <label class="form-label">Webhook URL</label>
    <input class="form-input" type="url" name="response_text" :disabled="action!=='webhook' && action!=='add_tag'" value="%s" placeholder="https://your-server.com/webhook">
  </div>
</div>`, func() string {
			if !isEdit || rule == nil || rule.ActionType != "webhook" {
				return ""
			}
			return html.EscapeString(responseTextVal)
		}()); err != nil {
			return err
		}

		if _, err := io.WriteString(w, `</div>`); err != nil { // close right panel
			return err
		}

		if _, err := io.WriteString(w, `</div>`); err != nil { // close au-builder
			return err
		}

		// ── Form footer ────────────────────────────────────────────────────
		if _, err := io.WriteString(w, `
<div class="au-form-footer">
  <a class="btn btn-secondary" href="/automation">Cancel</a>
  <button class="btn btn-primary" type="submit">Save rule</button>
</div>
</form>
</div>`); err != nil {
			return err
		}

		_, err := io.WriteString(w, ShellClose())
		return err
	})
}
