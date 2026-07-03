package templates

import (
	"context"
	"fmt"
	"html"
	"io"
	"strings"

	"github.com/a-h/templ"
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
	toastSVGSuccess = `<svg class="ti" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M4 12.7l5.3 5.3 10.7-12"/></svg>`
	toastSVGError   = `<svg class="ti" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="9"/><path d="M12 7.3v5.4M12 16.5v.15"/></svg>`
	toastSVGInfo    = `<svg class="ti" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="9"/><path d="M12 11.3v5.4M12 7.5v.15"/></svg>`
	toastSVGWarning = `<svg class="ti" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 3.3l9.3 16.7H2.7z"/><path d="M12 10v4M12 17.7v.15"/></svg>`
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
		cls := "btn btn-secondary"
		if a.Primary {
			cls = "btn btn-primary"
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

// ── Form error helpers (G3) ───────────────────────────────────────────────────

// FieldError returns a small inline error message for a single form field.
func FieldError(msg string) string {
	if msg == "" {
		return ""
	}
	return fmt.Sprintf(`<p class="field-err" role="alert">%s</p>`, html.EscapeString(msg))
}

// FlashScript returns an inline <script> that fires window.__showToast on
// DOMContentLoaded.  Use this for redirect-flow flash messages so the toast
// appears after the next full-page render.
func FlashScript(kind ToastKind, msg string) string {
	return fmt.Sprintf(
		`<script>document.addEventListener('DOMContentLoaded',function(){window.__showToast&&window.__showToast('%s','%s')})</script>`,
		string(kind),
		// single-quote-safe: escape ' so the inline JS string stays valid
		strings.ReplaceAll(html.EscapeString(msg), "'", `\'`),
	)
}

// FormBanner returns a full-width error banner for form-level errors.
func FormBanner(msg string) string {
	if msg == "" {
		return ""
	}
	return fmt.Sprintf(
		`<div class="callout callout--danger" role="alert" aria-live="assertive"><span aria-hidden="true">⚠</span> %s</div>`,
		html.EscapeString(msg),
	)
}

// ── Badge helper (G5) ─────────────────────────────────────────────────────────

// BadgeHTML returns a status badge span.
// variant maps to a CSS class: badge-success, badge-warning, badge-danger,
// badge-info, badge-neutral, badge-primary, badge-approved, badge-pending,
// badge-rejected, badge-on, badge-off, or any badge-* class in app.css.
func BadgeHTML(variant, label string) string {
	return fmt.Sprintf(`<span class="badge badge-%s">%s</span>`,
		html.EscapeString(variant), html.EscapeString(label))
}

// ── Callout helper ────────────────────────────────────────────────────────────

// CalloutHTML returns a contextual callout box.
// kind is one of: info, warning, danger, success — maps to callout--{kind} CSS class.
// msg is a plain string and will be HTML-escaped.
func CalloutHTML(kind, msg string) string {
	icon := "ℹ"
	role := "status"
	switch kind {
	case "warning":
		icon = "⚠"
		role = "alert"
	case "danger":
		icon = "⛔"
		role = "alert"
	case "success":
		icon = "✓"
	}
	return fmt.Sprintf(
		`<div class="callout callout--%s" role="%s"><span aria-hidden="true">%s</span> %s</div>`,
		html.EscapeString(kind), role, icon, html.EscapeString(msg),
	)
}

// ── Modal shell helper (G4) ───────────────────────────────────────────────────

// ModalShell returns a fully-accessible <dialog> wrapper.
// id must be unique on the page. title is shown as the dialog heading.
// bodyHTML is trusted server-side HTML (not user input).
// Open with: document.getElementById(id).showModal()
// Clicking the backdrop (outside the dialog box) closes the dialog automatically.
func ModalShell(id, title, bodyHTML string) string {
	titleID := id + "-title"
	return fmt.Sprintf(
		`<dialog id="%s" aria-modal="true" aria-labelledby="%s" onclick="if(event.target===this)this.close()">`+
			`<h2 id="%s" style="margin:0 0 16px;font-size:17px;font-weight:700">%s</h2>`+
			`%s`+
			`</dialog>`,
		html.EscapeString(id),
		html.EscapeString(titleID),
		html.EscapeString(titleID),
		html.EscapeString(title),
		bodyHTML,
	)
}

// ModalShellRaw is like ModalShell but accepts the full inner HTML directly,
// for dialogs that need custom headers (gradient bands, summary DL layouts, etc.).
// The caller must include an element with id=labelledByID somewhere in innerHTML.
// dialogClass is optional; pass "" for no class attribute.
func ModalShellRaw(id, dialogClass, labelledByID, innerHTML string) string {
	clsAttr := ""
	if dialogClass != "" {
		clsAttr = fmt.Sprintf(` class="%s"`, html.EscapeString(dialogClass))
	}
	return fmt.Sprintf(
		`<dialog id="%s"%s aria-modal="true" aria-labelledby="%s" onclick="if(event.target===this)this.close()">%s</dialog>`,
		html.EscapeString(id),
		clsAttr,
		html.EscapeString(labelledByID),
		innerHTML,
	)
}

// ── Skeleton loader (G1b) ─────────────────────────────────────────────────────

// SkeletonRows returns n placeholder rows shown while a table loads via HTMX.
func SkeletonRows(n int) string {
	row := `<div class="skeleton-row">` +
		`<div class="skeleton" style="width:36px;height:36px;border-radius:50%;flex-shrink:0"></div>` +
		`<div style="flex:1;display:flex;flex-direction:column;gap:6px">` +
		`<div class="skeleton" style="height:13px;width:55%"></div>` +
		`<div class="skeleton" style="height:11px;width:35%"></div>` +
		`</div>` +
		`<div class="skeleton" style="height:20px;width:60px;border-radius:20px"></div>` +
		`</div>`
	out := `<div style="padding:8px 0">`
	for i := 0; i < n; i++ {
		out += row
	}
	return out + `</div>`
}

// ExportIconSVG is the canonical download/export icon (24×24 viewBox, 14×14 element).
// Used in campaigns report and analytics export buttons.
const ExportIconSVG = `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>`

// ── Canonical Batch-0 Components ─────────────────────────────────────────────
// Create-only. Adopted page-by-page in Phase-3 batches. Zero existing call
// sites are changed in this file. Each component returns templ.Component so
// it can be used directly with @ syntax in .templ files.
//
// Intentional Button exclusions (do NOT migrate to Button):
//   - .tn-tool-btn  (template-body format toolbar): text-label insert shortcuts
//     with no structural relationship to the generic Button; stays bespoke.
//   - .auth-pwd-toggle: absolutely-positioned eye icon inside a password wrapper;
//     its layout context is not generic.

// ── Button ────────────────────────────────────────────────────────────────────

// ButtonVariant sets the visual style of a Button.
type ButtonVariant string

const (
	BtnPrimary   ButtonVariant = "primary"
	BtnSecondary ButtonVariant = "secondary"
	BtnDanger    ButtonVariant = "danger"
	BtnGhost     ButtonVariant = "ghost" // icon-only action / toolbar buttons
)

// ButtonSize sets the padding tier of a Button.
type ButtonSize string

const (
	BtnMd ButtonSize = ""   // default padding
	BtnSm ButtonSize = "sm" // compact (btn-sm)
)

// Button renders a <button> (href=="") or <a> (href non-empty) with the canonical
// .btn style family. icon is a trusted SVG string (e.g. ExportIconSVG) or "".
// label is the visible text; pass "" for icon-only (provide aria-label in attrs).
// Icon-only buttons (icon!="" && label=="") automatically receive .btn-icon sizing.
// attrs carries per-call attributes: hx-*, @click, onclick, type, x-model, etc.
func Button(variant ButtonVariant, size ButtonSize, href, icon, label string, attrs templ.Attributes) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		cls := "btn btn-" + string(variant)
		if size != "" {
			cls += " btn-" + string(size)
		}
		if icon != "" && label == "" {
			cls += " btn-icon"
		}
		inner := icon // trusted server-side SVG literal
		if label != "" {
			inner += html.EscapeString(label)
		}
		a := renderAttrs(attrs)
		var out string
		if href != "" {
			out = fmt.Sprintf(`<a class="%s" href="%s"%s>%s</a>`, cls, html.EscapeString(href), a, inner)
		} else {
			out = fmt.Sprintf(`<button class="%s"%s>%s</button>`, cls, a, inner)
		}
		_, err := io.WriteString(w, out)
		return err
	})
}

// ── StatusBadge ───────────────────────────────────────────────────────────────

// BadgeVariant is the semantic tier of a status badge.
type BadgeVariant string

const (
	BadgeApproved BadgeVariant = "approved" // green  — completed, approved, active
	BadgePending  BadgeVariant = "pending"  // yellow — scheduled, submitted, pending
	BadgeSending  BadgeVariant = "sending"  // blue   — running / in-flight
	BadgeDanger   BadgeVariant = "danger"   // red    — failed, rejected
	BadgeWarning  BadgeVariant = "warning"  // amber  — paused
	BadgeNeutral  BadgeVariant = "neutral"  // gray   — draft, cancelled, unknown
)

// StatusBadge renders <span class="badge badge-{variant}">{label}</span>.
// This is the canonical templ.Component form; it replaces BadgeHTML in templ pages.
// BadgeHTML (string function) is kept for fmt.Fprintf pages until they migrate.
func StatusBadge(variant BadgeVariant, label string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, err := io.WriteString(w, BadgeHTML(string(variant), label))
		return err
	})
}

// CampaignBadge maps a campaign DB status string to the canonical StatusBadge.
// Centralises the variant-mapping logic previously inline in campaigns_pages.go.
func CampaignBadge(status string) templ.Component {
	var v BadgeVariant
	var label string
	switch status {
	case "running":
		v, label = BadgeSending, "sending"
	case "completed":
		v, label = BadgeApproved, "completed"
	case "paused":
		v, label = BadgeWarning, "paused"
	case "scheduled":
		v, label = BadgePending, "scheduled"
	case "cancelled":
		v, label = BadgeNeutral, "cancelled"
	default: // draft, unknown
		v, label = BadgeNeutral, status
	}
	return StatusBadge(v, label)
}

// TemplateBadge maps a template DB status string to the canonical StatusBadge.
func TemplateBadge(status string) templ.Component {
	var v BadgeVariant
	switch status {
	case "approved":
		v = BadgeApproved
	case "rejected":
		v = BadgeDanger
	case "submitted":
		v = BadgePending
	default: // draft, unknown
		v = BadgeNeutral
	}
	return StatusBadge(v, status)
}

// ── TagPill ───────────────────────────────────────────────────────────────────

// TagPillMode controls whether TagPill renders as a static span or interactive button.
type TagPillMode string

const (
	TagDisplay TagPillMode = "display" // <span> read-only; replaces ct-tag-badge
	TagToggle  TagPillMode = "toggle"  // <button type="button">; replaces nc-tag-pill / ct-tag-pill
)

// TagColor is a token-scoped colour variant for a tag pill.
// Inline hex is forbidden; only these named variants are allowed (map to CSS tokens).
type TagColor string

const (
	TagColorDefault TagColor = ""       // no modifier — CSS base style applies
	TagColorGreen   TagColor = "green"  // --accent-lighter bg, --accent text/border
	TagColorBlue    TagColor = "blue"   // --info-light bg, --info text/border
	TagColorAmber   TagColor = "amber"  // --warning-light bg, --warning-text text
	TagColorRed     TagColor = "red"    // --danger-light bg, --danger text
	TagColorPurple  TagColor = "purple" // --primary-light bg, --primary-dark text
)

// TagPill renders a tag label in display (read-only) or toggle (interactive) mode.
// color: token-scoped colour variant; TagColorDefault = base CSS style, no modifier.
// active: for TagToggle, pre-marks as selected (adds tag-pill--on modifier).
// attrs: per-call attributes (@click, onclick, :class, data-*, etc.).
func TagPill(mode TagPillMode, label string, color TagColor, active bool, attrs templ.Attributes) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		cls := "tag-pill"
		if color != TagColorDefault {
			cls += " tag-pill--" + string(color)
		}
		if active {
			cls += " tag-pill--on"
		}
		a := renderAttrs(attrs)
		var out string
		if mode == TagDisplay {
			out = fmt.Sprintf(`<span class="%s"%s>%s</span>`, cls, a, html.EscapeString(label))
		} else {
			out = fmt.Sprintf(`<button type="button" class="%s"%s>%s</button>`, cls, a, html.EscapeString(label))
		}
		_, err := io.WriteString(w, out)
		return err
	})
}

// TagChip renders a removable tag badge with an embedded hx-delete × button.
// Replaces the ct-tag-chip + ct-tag-remove compound in the contact detail panel.
// color uses the same TagColor enum as TagPill; TagColorDefault = base chip style.
// No inline style attributes are emitted — color resolves to a CSS class only.
// deleteURL is the hx-delete endpoint; htmxTarget is the hx-target selector.
func TagChip(label string, color TagColor, deleteURL, htmxTarget string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		cls := "tag-chip"
		if color != TagColorDefault {
			cls += " tag-chip--" + string(color)
		}
		out := fmt.Sprintf(
			`<span class="%s">%s`+
				`<button class="tag-chip-remove" type="button" aria-label="Remove tag"`+
				` hx-delete="%s" hx-target="%s" hx-swap="innerHTML">&#215;</button>`+
				`</span>`,
			cls, html.EscapeString(label),
			html.EscapeString(deleteURL), html.EscapeString(htmxTarget),
		)
		_, err := io.WriteString(w, out)
		return err
	})
}

// ── ConsentIndicator ──────────────────────────────────────────────────────────

// ConsentForm selects the visual shape of a ConsentIndicator.
type ConsentForm string

const (
	ConsentDot   ConsentForm = "dot"   // small circle + adjacent text span
	ConsentBadge ConsentForm = "badge" // full pill with background colour + text
)

// ConsentIndicator renders an opted-in / opted-out indicator.
//
// ConsentDot emits <span class="consent-dot consent-dot--{in|out}"> + text span.
// Replaces ct-consent-dot in the contacts table. The Alpine nc-preview-dot in
// new_contact_page.go is NOT replaced here (reactive :class binding required), but
// it shares the same consent-dot--in/out class names so one CSS rule covers both.
//
// ConsentBadge emits <span class="badge consent-badge--{on|off}">{label}</span>.
// Replaces ct-badge-on / ct-badge-off in the contact detail panel header.
func ConsentIndicator(form ConsentForm, optedIn bool) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		var out string
		if form == ConsentDot {
			mod, label, textStyle := "--out", "Opted out", "color:var(--danger)"
			if optedIn {
				mod, label, textStyle = "--in", "Opted in", "color:var(--accent)"
			}
			out = fmt.Sprintf(
				`<span class="consent-dot consent-dot%s"></span><span style="%s">%s</span>`,
				mod, textStyle, label,
			)
		} else {
			if optedIn {
				out = `<span class="badge consent-badge--on">Opted in</span>`
			} else {
				out = `<span class="badge consent-badge--off">Not opted in</span>`
			}
		}
		_, err := io.WriteString(w, out)
		return err
	})
}

// ── Toggle ────────────────────────────────────────────────────────────────────

// Toggle renders a CSS-only styled toggle using the .acct-toggle-cb checkbox pattern.
// name sets the input's name and id (used for <label for="..."> association).
// checked sets the initial checked state.
// attrs carries per-call attributes: x-model, hx-post, hx-trigger, hx-swap,
// hx-target, hx-include, value, etc.
// Wrapping in a <label> and providing visible label text is the caller's responsibility.
func Toggle(name string, checked bool, attrs templ.Attributes) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		checkedAttr := ""
		if checked {
			checkedAttr = " checked"
		}
		out := fmt.Sprintf(
			`<input class="acct-toggle-cb" type="checkbox" id="%s" name="%s"%s%s>`,
			html.EscapeString(name), html.EscapeString(name), checkedAttr, renderAttrs(attrs),
		)
		_, err := io.WriteString(w, out)
		return err
	})
}

// ── renderAttrs ───────────────────────────────────────────────────────────────

// renderAttrs converts a templ.Attributes map to an HTML attribute string.
// bool true → bare attribute name (checked, disabled); bool false → omitted.
// All other values are rendered as key="html-escaped-value".
// Map iteration order is non-deterministic; callers must not depend on attr order.
func renderAttrs(attrs templ.Attributes) string {
	if len(attrs) == 0 {
		return ""
	}
	var sb strings.Builder
	for k, v := range attrs {
		if b, ok := v.(bool); ok {
			if b {
				sb.WriteByte(' ')
				sb.WriteString(k)
			}
			continue
		}
		sb.WriteString(fmt.Sprintf(` %s="%s"`, k, html.EscapeString(fmt.Sprint(v))))
	}
	return sb.String()
}

// ── Pre-built SVG icons for empty states (24×24, stroke="currentColor"). ─────
const (
	EmptyIconContacts   = `<svg class="ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M19 8v6M22 11h-6"/></svg>`
	EmptyIconCampaigns  = `<svg class="ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 11l18-7-7 18-2.5-7.5L3 11z"/></svg>`
	EmptyIconTemplates  = `<svg class="ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="4" y="3" width="16" height="18" rx="2"/><path d="M8 8h8M8 12h8M8 16h5"/></svg>`
	EmptyIconInbox      = `<svg class="ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M21 11.5a8.4 8.4 0 0 1-9 8.4L3 21l1.1-5A8.4 8.4 0 1 1 21 11.5z"/></svg>`
	EmptyIconSearch     = `<svg class="ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="11" cy="11" r="7"/><path d="M21 21l-4-4"/></svg>`
	EmptyIconAutomation = `<svg class="ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z"/></svg>`
)
