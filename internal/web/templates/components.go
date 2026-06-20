package templates

import (
	"fmt"
	"html"
)

// ── Toast ─────────────────────────────────────────────────────────────────────

// ToastKind is the semantic level of a notification toast.
type ToastKind string

const (
	ToastSuccess ToastKind = "success"
	ToastError   ToastKind = "error"
	ToastInfo    ToastKind = "info"
	ToastWarning ToastKind = "warning"
)

const (
	toastSVGSuccess = `<svg class="ti" viewBox="0 0 18 18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 9.5l4 4 8-9"/></svg>`
	toastSVGError   = `<svg class="ti" viewBox="0 0 18 18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="9" cy="9" r="7"/><path d="M9 5.5v4M9 12.4v.1"/></svg>`
	toastSVGInfo    = `<svg class="ti" viewBox="0 0 18 18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="9" cy="9" r="7"/><path d="M9 8.5v4M9 5.6v.1"/></svg>`
	toastSVGWarning = `<svg class="ti" viewBox="0 0 18 18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M9 2.5l7 12.5H2L9 2.5z"/><path d="M9 7.5v3M9 13.3v.1"/></svg>`
)

// ToastHTML returns a single toast element.
//
// actionLabel and actionURL are optional; pass empty strings to omit.
// success and info toasts auto-dismiss after 5 s via Alpine x-init.
// error and warning toasts persist until the user clicks ×.
func ToastHTML(kind ToastKind, msg, actionLabel, actionURL string) string {
	var icon, role string
	var autoDismiss bool
	switch kind {
	case ToastSuccess:
		icon, role, autoDismiss = toastSVGSuccess, "status", true
	case ToastError:
		icon, role, autoDismiss = toastSVGError, "alert", false
	case ToastInfo:
		icon, role, autoDismiss = toastSVGInfo, "status", true
	case ToastWarning:
		icon, role, autoDismiss = toastSVGWarning, "alert", false
	default:
		icon, role, autoDismiss = toastSVGInfo, "status", true
	}

	actHTML := ""
	if actionLabel != "" {
		if actionURL != "" {
			actHTML = fmt.Sprintf(`<a class="act" href="%s">%s</a>`,
				html.EscapeString(actionURL), html.EscapeString(actionLabel))
		} else {
			actHTML = fmt.Sprintf(`<button class="act" type="button">%s</button>`,
				html.EscapeString(actionLabel))
		}
	}

	initAttr := ""
	if autoDismiss {
		initAttr = ` x-init="setTimeout(() => $el.remove(), 5000)"`
	}

	return fmt.Sprintf(
		`<div class="toast toast--%s" role="%s" x-data="{}"%s>`+
			`%s`+
			`<div class="body"><div class="msg">%s</div>%s</div>`+
			`<button class="x" type="button" aria-label="Dismiss" @click="$el.closest('.toast').remove()">×</button>`+
			`</div>`,
		string(kind), role, initAttr,
		icon,
		html.EscapeString(msg),
		actHTML,
	)
}

// ToastFragment wraps a toast in an hx-swap-oob="afterbegin" div so it can
// be appended to any HTMX response and land at the top of #toast-region.
//
// Usage in a handler:
//
//	fmt.Fprint(w, templates.ToastFragment(templates.ToastSuccess, "Saved.", "", ""))
func ToastFragment(kind ToastKind, msg, actionLabel, actionURL string) string {
	return `<div id="toast-region" hx-swap-oob="afterbegin">` +
		ToastHTML(kind, msg, actionLabel, actionURL) +
		`</div>`
}

// ── Empty states ──────────────────────────────────────────────────────────────

// EmptyAction is one button or link in an empty state.
type EmptyAction struct {
	Label    string
	Primary  bool
	HREF     string // renders as <a> when set
	HXGet    string // HTMX GET when no HREF
	HXTarget string // hx-target for HXGet (defaults to "closest .screen")
	AtClick  string // Alpine @click handler (e.g. "openImport()")
}

// EmptyStateHTML returns the centered empty-state markup.
// iconSVG must be a trusted server-side SVG literal (not user input).
func EmptyStateHTML(iconSVG, title, body string, actions []EmptyAction) string {
	actHTML := ""
	for _, a := range actions {
		cls := "btn btn--secondary"
		if a.Primary {
			cls = "btn btn--primary"
		}
		label := html.EscapeString(a.Label)
		switch {
		case a.HREF != "":
			actHTML += fmt.Sprintf(`<a href="%s" class="%s">%s</a>`,
				html.EscapeString(a.HREF), cls, label)
		case a.HXGet != "":
			tgt := a.HXTarget
			if tgt == "" {
				tgt = "closest .screen"
			}
			actHTML += fmt.Sprintf(`<button class="%s" hx-get="%s" hx-target="%s">%s</button>`,
				cls, html.EscapeString(a.HXGet), html.EscapeString(tgt), label)
		case a.AtClick != "":
			actHTML += fmt.Sprintf(`<button class="%s" type="button" @click="%s">%s</button>`,
				cls, html.EscapeString(a.AtClick), label)
		default:
			actHTML += fmt.Sprintf(`<button class="%s" type="button">%s</button>`, cls, label)
		}
	}
	return fmt.Sprintf(
		`<div class="empty">%s<h3>%s</h3><p>%s</p><div class="actions">%s</div></div>`,
		iconSVG,
		html.EscapeString(title),
		html.EscapeString(body),
		actHTML,
	)
}

// Pre-built SVG icons for empty states (24×24, stroke="currentColor").
const (
	EmptyIconContacts   = `<svg class="ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M19 8v6M22 11h-6"/></svg>`
	EmptyIconCampaigns  = `<svg class="ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 11l18-7-7 18-2.5-7.5L3 11z"/></svg>`
	EmptyIconTemplates  = `<svg class="ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="4" y="3" width="16" height="18" rx="2"/><path d="M8 8h8M8 12h8M8 16h5"/></svg>`
	EmptyIconInbox      = `<svg class="ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M21 11.5a8.4 8.4 0 0 1-9 8.4L3 21l1.1-5A8.4 8.4 0 1 1 21 11.5z"/></svg>`
	EmptyIconSearch     = `<svg class="ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="11" cy="11" r="7"/><path d="M21 21l-4-4"/></svg>`
	EmptyIconAutomation = `<svg class="ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z"/></svg>`
)
