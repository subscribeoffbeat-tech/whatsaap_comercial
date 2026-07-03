package templates

import (
	"context"
	"encoding/base64"
	"fmt"
	"html"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"whatsapptool/internal/db"
	mw "whatsapptool/internal/web/middleware"
)

// tmplVarRe matches {{N}} variable placeholders.
var tmplVarRe = regexp.MustCompile(`\{\{(\d+)\}\}`)

// rejectionDetail maps a Meta template rejection reason code to a plain-English
// explanation of what to fix. Returns "" if the reason isn't a known Meta code
// (e.g. free-text), in which case callers show the raw reason as-is.
func rejectionDetail(reason string) string {
	switch strings.ToUpper(strings.TrimSpace(reason)) {
	case "INVALID_FORMAT":
		return "Formatting not allowed. Common causes: emojis, asterisks (*), or line breaks in the header; a variable like {{1}} with no example value; a variable at the very start or end of the body, or two variables next to each other; or trailing spaces / blank lines at the end."
	case "ABUSIVE_CONTENT":
		return "Flagged as abusive, threatening, or offensive. Revise the wording."
	case "INCORRECT_CATEGORY":
		return "Submitted under the wrong category. Match Marketing / Utility / Authentication to the message's actual intent."
	case "SCAM":
		return "Flagged as a possible scam or phishing — risky links or requests for sensitive info. Remove the flagged content."
	case "TAG_CONTENT_MISMATCH":
		return "The content doesn't match the chosen category. Align the message with its category."
	case "PROMOTIONAL":
		return "A Utility/Authentication template contains promotional content. Use the Marketing category for promos."
	}
	return ""
}

// rejectionReasonHTML renders a rejected template's reason: Meta's exact code
// plus a plain-English fix when the code is recognised, else the raw text.
func rejectionReasonHTML(reason string) string {
	if detail := rejectionDetail(reason); detail != "" {
		return fmt.Sprintf(`<strong>Meta reason: %s</strong><br>%s`,
			html.EscapeString(strings.ToUpper(strings.TrimSpace(reason))), html.EscapeString(detail))
	}
	return html.EscapeString(reason)
}

// extractSortedVars returns the unique {{N}} indices in body, sorted numerically.
func extractSortedVars(body string) []string {
	seen := map[string]bool{}
	var vars []string
	for _, m := range tmplVarRe.FindAllStringSubmatch(body, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			vars = append(vars, m[1])
		}
	}
	sort.Slice(vars, func(i, j int) bool {
		a, _ := strconv.Atoi(vars[i])
		b, _ := strconv.Atoi(vars[j])
		return a < b
	})
	return vars
}

// ── Templates page ────────────────────────────────────────────────────────────

func TemplatesPage(agent *mw.AgentClaims, flash string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/templates", "Templates", "")); err != nil {
			return err
		}
		flashScript := ""
		if flash != "" {
			flashScript = FlashScript(ToastSuccess, flash)
		}
		_, err := fmt.Fprintf(w, `
<div class="page-wrap">
%s
<div class="page-hd">
<div>
<h1 class="screen-title">Templates</h1>
<p class="screen-subtitle">Manage and discover WhatsApp message templates</p>
</div>
<a class="btn btn-primary btn-sm" href="/templates/new">+ New template</a>
</div>

<div class="tmpl-page-tabs" x-data="{tab:'my'}">
<button class="tmpl-page-tab" :class="{active:tab==='my'}" @click="tab='my'"
  hx-get="/templates/gallery" hx-target="#tmpl-main" hx-swap="innerHTML">My Templates</button>
<button class="tmpl-page-tab" :class="{active:tab==='lib'}" @click="tab='lib'"
  hx-get="/templates/library" hx-target="#tmpl-main" hx-swap="innerHTML">&#128218; Template Library</button>
</div>

<div id="tmpl-main"
  hx-get="/templates/gallery"
  hx-trigger="load, templatesUpdated from:body"
  hx-swap="innerHTML">
</div>
</div>`, flashScript)
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, ShellClose())
		return err
	})
}

// ── Gallery partial (My Templates) ───────────────────────────────────────────

func TemplateGallery(tmpls []db.Template) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if len(tmpls) == 0 {
			_, err := io.WriteString(w, EmptyStateHTML(EmptyIconTemplates,
				"No templates yet",
				"Create a new template or browse the sample library.",
				[]EmptyAction{
					{Label: "New template", Primary: true, HREF: "/templates/new"},
				}))
			return err
		}

		// Count per status.
		counts := map[string]int{}
		for _, t := range tmpls {
			counts[t.Status]++
		}

		_, err := io.WriteString(w, `<div x-data="{q:'',cat:'all',status:'all'}" class="tmpl-filter-wrap">`)
		if err != nil {
			return err
		}

		// Search + category bar.
		_, err = io.WriteString(w, `
<div class="tmpl-filter-bar">
<label class="tmpl-search">
<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><path d="m21 21-4.35-4.35"/></svg>
<input type="search" placeholder="Search templates..." x-model.debounce.250ms="q" class="tmpl-search-input">
</label>
<div class="tmpl-cat-tabs">
<button class="tmpl-cat-tab" :class="{active:cat==='all'}" @click="cat='all'" type="button">ALL</button>
<button class="tmpl-cat-tab" :class="{active:cat==='marketing'}" @click="cat='marketing'" type="button">MARKETING</button>
<button class="tmpl-cat-tab" :class="{active:cat==='utility'}" @click="cat='utility'" type="button">UTILITY</button>
</div>
</div>`)
		if err != nil {
			return err
		}

		// Paused pill only appears when Meta has paused a template (uncommon).
		pausedPill := ""
		if counts["paused"] > 0 {
			pausedPill = fmt.Sprintf(
				`<button class="tmpl-status-pill" :class="{active:status==='paused'}" @click="status='paused'" type="button"><span class="sdot sdot--purple"></span> Paused <span class="tmpl-status-count">%d</span></button>`,
				counts["paused"])
		}

		// Status pills with static counts.
		_, err = fmt.Fprintf(w, `
<div class="tmpl-status-pills">
<button class="tmpl-status-pill" :class="{active:status==='all'}" @click="status='all'" type="button">All <span class="tmpl-status-count">%d</span></button>
<button class="tmpl-status-pill" :class="{active:status==='draft'}" @click="status='draft'" type="button"><span class="sdot sdot--gray"></span> Draft <span class="tmpl-status-count">%d</span></button>
<button class="tmpl-status-pill" :class="{active:status==='approved'}" @click="status='approved'" type="button"><span class="sdot sdot--green"></span> Approved <span class="tmpl-status-count">%d</span></button>
<button class="tmpl-status-pill" :class="{active:status==='pending'}" @click="status='pending'" type="button"><span class="sdot sdot--orange"></span> Pending <span class="tmpl-status-count">%d</span></button>
<button class="tmpl-status-pill" :class="{active:status==='rejected'}" @click="status='rejected'" type="button"><span class="sdot sdot--red"></span> Rejected <span class="tmpl-status-count">%d</span></button>
%s
</div>`, len(tmpls), counts["draft"], counts["approved"], counts["pending"], counts["rejected"], pausedPill)
		if err != nil {
			return err
		}

		if _, err = io.WriteString(w, `<div class="tmpl-gallery2">`); err != nil {
			return err
		}

		for _, t := range tmpls {
			bodyText := ""
			var buttons []string
			for _, comp := range t.Components {
				if comp["type"] == "BODY" {
					if text, ok := comp["text"].(string); ok {
						bodyText = text
					}
				}
				if comp["type"] == "BUTTONS" {
					if btns, ok := comp["buttons"].([]any); ok {
						for _, b := range btns {
							if bm, ok := b.(map[string]any); ok {
								if label, ok := bm["text"].(string); ok {
									buttons = append(buttons, label)
								}
							}
						}
					}
				}
			}

			searchScope := t.Name + " " + bodyText

			// Rejection reason block.
			rejHTML := ""
			if t.RejectionReason != nil && *t.RejectionReason != "" {
				rejHTML = fmt.Sprintf(
					`<div class="tmpl-rejection-reason">%s</div>`,
					rejectionReasonHTML(*t.RejectionReason))
			}

			// Button chips.
			btnChips := ""
			for _, b := range buttons {
				btnChips += fmt.Sprintf(`<span class="tmpl-btn-chip">%s</span>`, html.EscapeString(b))
			}
			if btnChips != "" {
				btnChips = `<div class="tmpl-btn-chips">` + btnChips + `</div>`
			}

			// Action buttons.
			var actionBtns string
			switch {
			case t.Status == "draft":
				actionBtns = fmt.Sprintf(
					`<form hx-post="/templates/%s/submit" hx-target="#tmpl-submit-result-%s" hx-swap="innerHTML" style="display:inline">`+
						`<button class="btn btn-sm btn-primary" type="submit">Submit to Meta</button></form>`+
						`<div id="tmpl-submit-result-%s" style="margin-top:6px"></div>`,
					t.ID, t.ID, t.ID)
			case t.Status == "rejected":
				// Rejected templates already exist in Meta, so re-creating fails.
				// The user must edit the content and resubmit (Meta edit endpoint),
				// which the editor's "Save & Resubmit to Meta" button handles.
				actionBtns = fmt.Sprintf(
					`<button class="btn btn-sm btn-primary" hx-get="/templates/%s" hx-target="#tmpl-editor" hx-swap="innerHTML">Edit &amp; Resubmit</button>`,
					t.ID)
			case t.Status == "pending":
				actionBtns = `<div style="font-size:12px;color:var(--text-muted);padding:6px 0 2px">⏳ Submitted to Meta — awaiting review (usually 2–5 min)</div>`
			case t.Status == "paused":
				// Meta paused the template (usually quality). Editing & resubmitting
				// it can reactivate it.
				actionBtns = fmt.Sprintf(
					`<div style="font-size:12px;color:var(--warning-text,#b45309);padding:4px 0 6px">⏸ Paused by Meta (quality) — edit &amp; resubmit to reactivate</div>`+
						`<button class="btn btn-sm btn-primary" hx-get="/templates/%s" hx-target="#tmpl-editor" hx-swap="innerHTML">Edit &amp; Resubmit</button>`,
					t.ID)
			}
			editBtn := fmt.Sprintf(
				`<button class="btn btn-sm btn-secondary" hx-get="/templates/%s" hx-target="#tmpl-editor" hx-swap="innerHTML">Edit</button>`,
				t.ID)

			// Build the Alpine filter expression, then HTML-escape it for the
			// attribute context. searchScope can contain double quotes/apostrophes
			// (e.g. body text like: DM us "CHAT"); without escaping, those would
			// terminate the x-show="..." attribute and break the card's filtering.
			xshowExpr := fmt.Sprintf(
				`(status==='all'||status===%s)&&(cat==='all'||cat===%s)&&(!q||%s.toLowerCase().includes(q.toLowerCase()))`,
				jsLit(t.Status), jsLit(t.Category), jsLit(searchScope))

			_, err = fmt.Fprintf(w,
				`<div class="tmpl-card2" x-show="%s">
<div class="tmpl-card2-hd"><span class="tmpl-card2-name">%s</span><span class="badge badge-cat cat-%s">%s</span>%s</div>
<div class="tmpl-card2-body">%s</div>%s%s%s
<div class="tmpl-card2-ft"><span class="tmpl-card2-meta">%s &middot; %s</span><div class="tmpl-card2-actions">%s</div></div>
</div>`,
				html.EscapeString(xshowExpr),
				html.EscapeString(t.Name),
				t.Category, strings.ToUpper(t.Category),
				BadgeHTML(t.Status, t.Status),
				html.EscapeString(bodyText),
				rejHTML, btnChips, actionBtns,
				html.EscapeString(t.Name), html.EscapeString(t.Language),
				editBtn,
			)
			if err != nil {
				return err
			}
		}

		_, err = io.WriteString(w, `</div>`) // close tmpl-gallery2
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, `<div id="tmpl-editor" aria-live="polite" aria-atomic="false"></div>`)
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, `</div>`) // close Alpine wrapper
		return err
	})
}

// ── Template Library ──────────────────────────────────────────────────────────

type libTmpl struct {
	DisplayName string
	Slug        string
	Category    string   // marketing|utility
	Body        string   // display body with [...] placeholders
	Buttons     []string // CTA button labels
}

type libIndustry struct {
	ID        string
	Label     string
	Templates []libTmpl
}

var libraryData = []libIndustry{
	{
		ID: "ecommerce", Label: "E-commerce",
		Templates: []libTmpl{
			{"Order Confirmed", "order_confirmed", "utility",
				"Hi [...], your order #[...] is confirmed! \U0001f389 Amount: ₹[...]. Delivery by [...].",
				[]string{"Track Order"}},
			{"Shipping Update", "shipping_update", "utility",
				"Your order #[...] is on its way! \U0001f69a Expected by [...]. Live tracking: [...]",
				[]string{"Track Now"}},
			{"Abandoned Cart", "abandoned_cart", "marketing",
				"Hey [...], you left something behind! \U0001f6d2 Your cart is waiting. Complete your order before it expires.",
				[]string{"Complete Order"}},
			{"Flash Sale", "flash_sale", "marketing",
				"⚡ FLASH SALE, [...]! Get [...]% off everything today. No code needed — sale ends at midnight!",
				[]string{"Shop Now"}},
		},
	},
	{
		ID: "realestate", Label: "Real Estate",
		Templates: []libTmpl{
			{"Property Visit Reminder", "property_visit_reminder", "utility",
				"Hi [...], reminder: your property visit at [...] is scheduled for [...] at [...]. See you there!", nil},
			{"New Listing Alert", "new_listing_alert", "marketing",
				"New listing! [...] BHK in [...] at ₹[...] Lac. Perfect for your requirements. Let’s talk!",
				[]string{"View Property"}},
			{"Loan Pre-Approval", "loan_pre_approval", "utility",
				"Hi [...], great news! You’re pre-approved for a home loan up to ₹[...]. Valid till [...].",
				[]string{"Apply Now"}},
		},
	},
	{
		ID: "healthcare", Label: "Healthcare",
		Templates: []libTmpl{
			{"Appointment Reminder", "appointment_reminder", "utility",
				"Hi [...], your appointment with Dr. [...] is on [...] at [...]. Please arrive 10 mins early.",
				[]string{"Confirm", "Reschedule"}},
			{"Lab Results Ready", "lab_results_ready", "utility",
				"Hi [...], your lab reports for [...] are ready. Download or visit us to collect.",
				[]string{"View Report"}},
			{"Health Tip", "health_tip", "marketing",
				"\U0001f48a Weekly health tip for [...]: [...]. Stay healthy and consult us for any concerns.",
				[]string{"Book Consult"}},
		},
	},
	{
		ID: "education", Label: "Education",
		Templates: []libTmpl{
			{"Class Reminder", "class_reminder", "utility",
				"Hi [...], your [...] class starts in 1 hour at [...]. Don’t miss it!", nil},
			{"Fee Due Reminder", "fee_due_reminder", "utility",
				"Hi [...], your fee of ₹[...] for [...] is due on [...]. Pay to avoid late fees.",
				[]string{"Pay Now"}},
			{"Course Launch", "course_launch", "marketing",
				"\U0001f393 New course alert, [...]! [...] is now live. Early bird price: ₹[...]. Hurry, limited seats!",
				[]string{"Enrol Now"}},
		},
	},
	{
		ID: "finance", Label: "Finance",
		Templates: []libTmpl{
			{"Payment Reminder", "payment_reminder", "utility",
				"Dear [...], your payment of ₹[...] is due on [...]. Please pay to avoid late fees.",
				[]string{"Pay Now"}},
			{"Account Statement", "account_statement", "utility",
				"Hi [...], your [...] statement for [...] is ready. Download it below.",
				[]string{"View Statement"}},
			{"Exclusive Offer", "finance_offer", "marketing",
				"Exclusive offer for you, [...]! Apply for a [...] at just [...]% interest. Limited period.",
				[]string{"Apply Now"}},
		},
	},
	{
		ID: "food", Label: "Food & Delivery",
		Templates: []libTmpl{
			{"Order Confirmed", "food_order_confirmed", "utility",
				"Your order from [...] is confirmed! \U0001f37d Estimated delivery: [...]. Track your order here.",
				[]string{"Track Order"}},
			{"Out for Delivery", "out_for_delivery", "utility",
				"Your order is on the way, [...]! \U0001f6f5 ETA: [...] mins. Delivery partner: [...].", nil},
			{"Daily Special", "daily_special", "marketing",
				"\U0001f525 Today’s special for [...]: [...] at just ₹[...]. Order now before it sells out!",
				[]string{"Order Now"}},
		},
	},
	{
		ID: "travel", Label: "Travel",
		Templates: []libTmpl{
			{"Booking Confirmation", "booking_confirmation", "utility",
				"Hi [...], your booking to [...] on [...] is confirmed. Ref: [...]. Have a great trip!",
				[]string{"View Booking"}},
			{"Check-in Reminder", "checkin_reminder", "utility",
				"Your flight to [...] departs tomorrow! ⏰ Check-in is open now. Booking: [...].",
				[]string{"Check In"}},
			{"Travel Offer", "travel_offer", "marketing",
				"✈️ Exclusive deal for [...]: [...] to [...] from ₹[...]. Book by [...] to avail.",
				[]string{"Book Now"}},
		},
	},
}

// libBodyToTemplate converts [...] placeholders to {{1}}, {{2}}, etc.
func libBodyToTemplate(body string) string {
	n := 0
	var result strings.Builder
	for {
		idx := strings.Index(body, "[...]")
		if idx == -1 {
			result.WriteString(body)
			break
		}
		n++
		result.WriteString(body[:idx])
		fmt.Fprintf(&result, "{{%d}}", n)
		body = body[idx+5:]
	}
	return result.String()
}

func TemplateLibrary() templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, `<div class="tmpl-lib-root" x-data="{ind:'ecommerce'}">`); err != nil {
			return err
		}

		// Left sidebar.
		if _, err := io.WriteString(w, `<nav class="tmpl-lib-nav"><p class="tmpl-lib-nav-title">INDUSTRY</p>`); err != nil {
			return err
		}
		for _, ind := range libraryData {
			_, err := fmt.Fprintf(w,
				`<button class="tmpl-lib-nav-item" :class="{active:ind===%s}" @click="ind=%s" type="button">%s</button>`,
				jsLit(ind.ID), jsLit(ind.ID), html.EscapeString(ind.Label))
			if err != nil {
				return err
			}
		}
		if _, err := io.WriteString(w, `</nav>`); err != nil {
			return err
		}

		// Right content.
		if _, err := io.WriteString(w, `<div class="tmpl-lib-content">`); err != nil {
			return err
		}
		for _, ind := range libraryData {
			_, err := fmt.Fprintf(w, `<div x-show="ind===%s">`, jsLit(ind.ID))
			if err != nil {
				return err
			}
			// Section header.
			_, err = fmt.Fprintf(w,
				`<div class="tmpl-lib-hd"><h2>%s <span class="tmpl-lib-count">%d templates</span></h2><p>Click "Use template" to customise and submit for approval</p></div>`,
				html.EscapeString(ind.Label), len(ind.Templates))
			if err != nil {
				return err
			}
			// Template grid.
			if _, err = io.WriteString(w, `<div class="tmpl-lib-grid">`); err != nil {
				return err
			}
			for _, t := range ind.Templates {
				catClass := "cat-" + t.Category
				catLabel := strings.ToUpper(t.Category)

				// Button chips.
				chips := ""
				for _, b := range t.Buttons {
					chips += fmt.Sprintf(`<span class="tmpl-btn-chip">%s</span>`, html.EscapeString(b))
				}
				chipsHTML := ""
				if chips != "" {
					chipsHTML = `<div class="tmpl-btn-chips">` + chips + `</div>`
				}

				// "Use template" URL — convert [...] to {{N}} for the body param.
				useURL := "/templates/new?" + url.Values{
					"name":     {t.Slug},
					"category": {t.Category},
					"body":     {libBodyToTemplate(t.Body)},
				}.Encode()

				_, err = fmt.Fprintf(w,
					`<div class="tmpl-lib-card">
<div class="tmpl-lib-card-hd"><span class="tmpl-lib-card-name">%s</span><span class="badge badge-cat %s">%s</span></div>
<div class="tmpl-lib-card-body">%s</div>%s
<a class="btn btn-primary tmpl-lib-use-btn" href="%s">Use template</a>
</div>`,
					html.EscapeString(t.DisplayName),
					catClass, catLabel,
					html.EscapeString(t.Body),
					chipsHTML,
					html.EscapeString(useURL),
				)
				if err != nil {
					return err
				}
			}
			if _, err = io.WriteString(w, `</div>`); err != nil { // close grid
				return err
			}
			if _, err = io.WriteString(w, `</div>`); err != nil { // close x-show div
				return err
			}
		}
		if _, err := io.WriteString(w, `</div>`); err != nil { // close tmpl-lib-content
			return err
		}
		_, err := io.WriteString(w, `</div>`) // close tmpl-lib-root
		return err
	})
}

// ── Inline editor form (opened by Edit button) ────────────────────────────────

func TemplateEditorForm(t *db.Template) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		header := ""
		headerType := "none"
		hasMedia := false
		mediaHandle := ""
		body := ""
		footer := ""
		var bodyExamples []string
		if t != nil {
			for _, comp := range t.Components {
				switch comp["type"] {
				case "HEADER":
					switch strings.ToUpper(fmt.Sprint(comp["format"])) {
					case "TEXT", "":
						if text, ok := comp["text"].(string); ok && text != "" {
							header = text
							headerType = "text"
						}
					case "IMAGE":
						headerType = "image"
					case "VIDEO":
						headerType = "video"
					case "DOCUMENT":
						headerType = "document"
					}
					if ex, ok := comp["example"].(map[string]any); ok {
						if hh, ok := ex["header_handle"].([]any); ok && len(hh) > 0 {
							hasMedia = true
							if s, ok := hh[0].(string); ok {
								mediaHandle = s
							}
						}
					}
				case "BODY":
					if text, ok := comp["text"].(string); ok {
						body = text
					}
					// Stored variable example values: example.body_text[0] = [v1, v2, ...]
					if ex, ok := comp["example"].(map[string]any); ok {
						if bt, ok := ex["body_text"].([]any); ok && len(bt) > 0 {
							if row, ok := bt[0].([]any); ok {
								for _, v := range row {
									if s, ok := v.(string); ok {
										bodyExamples = append(bodyExamples, s)
									}
								}
							}
						}
					}
				case "FOOTER":
					if text, ok := comp["text"].(string); ok {
						footer = text
					}
				}
			}
		}

		// Build an Alpine examples object literal, zipping sorted variables with
		// their stored example values: {'1':'Rahul','2':'#123'}.
		var exBuilder strings.Builder
		exBuilder.WriteString("{")
		for i, v := range extractSortedVars(body) {
			val := ""
			if i < len(bodyExamples) {
				val = bodyExamples[i]
			}
			if i > 0 {
				exBuilder.WriteString(",")
			}
			exBuilder.WriteString(jsLit(v) + ":" + jsLit(val))
		}
		exBuilder.WriteString("}")
		examplesJS := exBuilder.String()

		name := ""
		language := "en_US"
		category := "marketing"
		id := ""
		status := ""
		rejectionReason := ""
		if t != nil {
			name = t.Name
			language = t.Language
			category = t.Category
			id = t.ID
			status = t.Status
			if t.RejectionReason != nil {
				rejectionReason = *t.RejectionReason
			}
		}

		// When a template was rejected, show why so the user knows what to fix.
		rejBanner := ""
		if status == "rejected" && rejectionReason != "" {
			rejBanner = fmt.Sprintf(
				`<div class="callout callout--danger" style="margin-bottom:12px"><strong>This template was rejected by Meta.</strong><br>%s</div>`,
				rejectionReasonHTML(rejectionReason))
		}

		// A "Save & Resubmit to Meta" button only makes sense for templates that
		// are not already approved or awaiting review.
		resubmitBtn := ""
		if status == "draft" || status == "rejected" || status == "paused" {
			resubmitBtn = `<button class="btn btn-primary btn-sm" type="submit" name="action" value="resubmit">Save &amp; Resubmit to Meta</button>`
		}

		hasMediaJS := "false"
		if hasMedia {
			hasMediaJS = "true"
		}

		// Meta's header_handle encodes the original filename as a base64 segment
		// (handle format: "4:<b64 filename>:<b64 mime>:..."). Decode it so the
		// editor can confirm exactly which file is attached.
		attachedHint := "A header file is already attached. Upload a new one only to replace it."
		if parts := strings.SplitN(mediaHandle, ":", 3); len(parts) >= 2 {
			if dec, derr := base64.StdEncoding.DecodeString(parts[1]); derr == nil && len(dec) > 0 {
				attachedHint = fmt.Sprintf("Attached: %s — upload a new file only to replace it.", html.EscapeString(string(dec)))
			}
		}

		// Alpine state: body text + variable example values + header type/upload.
		xdataEditor := html.EscapeString(fmt.Sprintf(
			`{body:%s,examples:%s,headerType:%s,headerFileName:'',headerPreviewURL:'',hasMedia:%s,`+
				`get vars(){return[...new Set((this.body.match(/\{\{(\d+)\}\}/g)||[]).map(s=>s.replace(/[^0-9]/g,'')))].sort((a,b)=>a-b)},`+
				`clearHeaderFile(){this.headerFileName='';this.headerPreviewURL='';const el=this.$refs.hfile;if(el)el.value=''},`+
				`previewHeaderFile(e){const f=e.target.files[0];if(!f)return;this.headerFileName=f.name;if(this.headerType==='image'){const r=new FileReader();r.onload=(ev)=>{this.headerPreviewURL=ev.target.result};r.readAsDataURL(f)}}}`,
			jsLit(body), examplesJS, jsLit(headerType), hasMediaJS))

		_, err := fmt.Fprintf(w, `
<div class="tmpl-editor">
<h3>Edit template</h3>
%s
<form method="post" action="/templates/%s"
  hx-put="/templates/%s"
  hx-target="#tmpl-main"
  hx-swap="innerHTML"
  x-data="%s"
  enctype="multipart/form-data"
  hx-encoding="multipart/form-data"
  hx-disabled-elt="find button[type='submit']"
  hx-on::response-error="document.getElementById('tmpl-edit-errors').innerHTML=event.detail.xhr.responseText">
<div id="tmpl-edit-errors" role="alert" aria-live="polite"></div>
<label class="field"><span>Name</span>
<input type="text" name="name" value="%s" required pattern="[a-z0-9_]+"></label>
<label class="field"><span>Language</span>
<select name="language">
<option value="en_US"%s>English (US)</option>
<option value="en_GB"%s>English (GB)</option>
</select></label>
<label class="field"><span>Category</span>
<select name="category">
<option value="marketing"%s>Marketing</option>
<option value="utility"%s>Utility</option>
<option value="authentication"%s>Authentication</option>
</select></label>
<div class="field"><span>Header <span style="font-weight:400;font-size:12px;color:var(--text-muted)">(optional)</span></span>
<div style="display:flex;gap:6px;flex-wrap:wrap;margin:6px 0">
<label class="tmpl-hdr-radio" :class="{'tmpl-hdr-radio--on':headerType==='none'}"><input type="radio" name="header_type" value="none" x-model="headerType" @change="clearHeaderFile()" class="sr-only">None</label>
<label class="tmpl-hdr-radio" :class="{'tmpl-hdr-radio--on':headerType==='text'}"><input type="radio" name="header_type" value="text" x-model="headerType" @change="clearHeaderFile()" class="sr-only">Text</label>
<label class="tmpl-hdr-radio" :class="{'tmpl-hdr-radio--on':headerType==='image'}"><input type="radio" name="header_type" value="image" x-model="headerType" @change="clearHeaderFile()" class="sr-only">Image</label>
<label class="tmpl-hdr-radio" :class="{'tmpl-hdr-radio--on':headerType==='video'}"><input type="radio" name="header_type" value="video" x-model="headerType" @change="clearHeaderFile()" class="sr-only">Video</label>
<label class="tmpl-hdr-radio" :class="{'tmpl-hdr-radio--on':headerType==='document'}"><input type="radio" name="header_type" value="document" x-model="headerType" @change="clearHeaderFile()" class="sr-only">Document</label>
</div>
<input type="text" name="header_text" value="%s" x-show="headerType==='text'" x-cloak class="form-input" placeholder="Header text (plain — no emoji, asterisks or line breaks)" maxlength="60">
<div x-show="headerType==='image'||headerType==='video'||headerType==='document'" x-cloak>
<input type="file" x-ref="hfile" name="header_file" @change="previewHeaderFile($event)" :accept="headerType==='image'?'image/jpeg,image/png,image/webp':headerType==='video'?'video/mp4,video/3gpp':'.pdf,.doc,.docx,.ppt,.pptx,.xls,.xlsx,.txt'" style="display:block;margin-top:4px;font-size:13px">
<p x-show="hasMedia&&!headerFileName" x-cloak style="font-size:12px;color:var(--brand-700,#15623f);margin:6px 0 0">🖼 %s</p>
<p x-show="!hasMedia&&!headerFileName" x-cloak style="font-size:12px;color:var(--text-muted);margin:6px 0 0">Upload a sample file — Meta requires it to approve a media header.</p>
<img x-show="headerPreviewURL" :src="headerPreviewURL" x-cloak alt="preview" style="max-height:120px;margin-top:8px;border-radius:6px;display:block">
</div>
</div>
<label class="field"><span>Body text</span>
<textarea name="body" rows="5" x-model="body">%s</textarea></label>
<div class="field" x-show="vars.length>0" x-cloak>
<span>Example values <span style="font-weight:400;font-size:12px;color:var(--text-muted)">— Meta needs a sample for each variable</span></span>
<template x-for="n in vars" :key="n">
<div style="display:flex;align-items:center;gap:8px;margin-top:6px">
<span style="font-family:ui-monospace,monospace;font-size:13px;min-width:42px;color:var(--text-muted);font-weight:600" x-text="'{{'+n+'}}'"></span>
<input type="text" class="form-input" :name="'var_example_'+n" x-model="examples[n]" placeholder="e.g. Rahul" style="flex:1" autocomplete="off">
</div>
</template>
</div>
<label class="field"><span>Footer <span style="font-weight:400;font-size:12px;color:var(--text-muted)">(optional)</span></span>
<input type="text" name="footer" value="%s"></label>
<div class="form-btns">
<button class="btn btn-secondary btn-sm" type="submit" name="action" value="save">Save changes</button>
%s
<button class="btn btn-secondary btn-sm" type="button"
  onclick="document.getElementById('tmpl-editor').innerHTML=''">Cancel</button>
</div>
</form>
</div>`,
			rejBanner,
			id, id,
			xdataEditor,
			html.EscapeString(name),
			sel(language, "en_US"), sel(language, "en_GB"),
			sel(category, "marketing"), sel(category, "utility"), sel(category, "authentication"),
			html.EscapeString(header),
			attachedHint,
			html.EscapeString(body),
			html.EscapeString(footer),
			resubmitBtn,
		)
		return err
	})
}
