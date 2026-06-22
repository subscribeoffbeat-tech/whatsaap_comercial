package templates

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/url"
	"strings"

	"github.com/a-h/templ"

	"whatsapptool/internal/db"
	mw "whatsapptool/internal/web/middleware"
)

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

		// Status pills with static counts.
		_, err = fmt.Fprintf(w, `
<div class="tmpl-status-pills">
<button class="tmpl-status-pill" :class="{active:status==='all'}" @click="status='all'" type="button">All <span class="tmpl-status-count">%d</span></button>
<button class="tmpl-status-pill" :class="{active:status==='approved'}" @click="status='approved'" type="button"><span class="sdot sdot--green"></span> Approved <span class="tmpl-status-count">%d</span></button>
<button class="tmpl-status-pill" :class="{active:status==='pending'}" @click="status='pending'" type="button"><span class="sdot sdot--orange"></span> Pending <span class="tmpl-status-count">%d</span></button>
<button class="tmpl-status-pill" :class="{active:status==='rejected'}" @click="status='rejected'" type="button"><span class="sdot sdot--red"></span> Rejected <span class="tmpl-status-count">%d</span></button>
</div>`, len(tmpls), counts["approved"], counts["pending"], counts["rejected"])
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
					`<div class="tmpl-rejection-reason"><strong>Rejection reason:</strong> %s</div>`,
					html.EscapeString(*t.RejectionReason))
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
			case t.Status == "rejected":
				actionBtns = fmt.Sprintf(
					`<form method="post" action="/templates/%s/submit" style="display:inline"><button class="btn btn-sm btn-primary" type="submit">Resubmit</button></form>`,
					t.ID)
			case t.Status == "pending" || t.Status == "draft":
				actionBtns = fmt.Sprintf(
					`<form method="post" action="/templates/%s/submit" style="display:inline"><button class="btn btn-sm btn-primary" type="submit">Submit to Meta</button></form>`,
					t.ID)
			}
			editBtn := fmt.Sprintf(
				`<button class="btn btn-sm btn-secondary" hx-get="/templates/%s" hx-target="#tmpl-editor" hx-swap="innerHTML">Edit</button>`,
				t.ID)

			_, err = fmt.Fprintf(w,
				`<div class="tmpl-card2" x-show="(status==='all'||status===%s)&&(cat==='all'||cat===%s)&&(!q||%s.toLowerCase().includes(q.toLowerCase()))">
<div class="tmpl-card2-hd"><span class="tmpl-card2-name">%s</span><span class="badge badge-cat cat-%s">%s</span>%s</div>
<div class="tmpl-card2-body">%s</div>%s%s
<div class="tmpl-card2-ft"><span class="tmpl-card2-meta">%s &middot; %s</span><div class="tmpl-card2-actions">%s%s</div></div>
</div>`,
				jsLit(t.Status), jsLit(t.Category), jsLit(searchScope),
				html.EscapeString(t.Name),
				t.Category, strings.ToUpper(t.Category),
				BadgeHTML(t.Status, t.Status),
				html.EscapeString(bodyText),
				rejHTML, btnChips,
				html.EscapeString(t.Name), html.EscapeString(t.Language),
				actionBtns, editBtn,
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
		body := ""
		if t != nil {
			for _, comp := range t.Components {
				if comp["type"] == "BODY" {
					if text, ok := comp["text"].(string); ok {
						body = text
					}
				}
			}
		}

		name := ""
		language := "en_US"
		category := "marketing"
		id := ""
		if t != nil {
			name = t.Name
			language = t.Language
			category = t.Category
			id = t.ID
		}

		_, err := fmt.Fprintf(w, `
<div class="tmpl-editor">
<h3>Edit template</h3>
<form method="post" action="/templates/%s"
  hx-put="/templates/%s"
  hx-target="#tmpl-main"
  hx-swap="innerHTML"
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
<label class="field"><span>Body text</span>
<textarea name="body" rows="5">%s</textarea></label>
<div class="form-btns">
<button class="btn btn-primary btn-sm" type="submit">Save changes</button>
<button class="btn btn-secondary btn-sm" type="button"
  onclick="document.getElementById('tmpl-editor').innerHTML=''">Cancel</button>
</div>
</form>
</div>`,
			id, id,
			html.EscapeString(name),
			sel(language, "en_US"), sel(language, "en_GB"),
			sel(category, "marketing"), sel(category, "utility"), sel(category, "authentication"),
			html.EscapeString(body),
		)
		return err
	})
}
