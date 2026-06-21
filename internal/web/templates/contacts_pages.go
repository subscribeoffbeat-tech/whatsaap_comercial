package templates

import (
	"context"
	"fmt"
	"html"
	"io"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"whatsapptool/internal/db"
	mw "whatsapptool/internal/web/middleware"
)

// ImportPreviewRow holds one row of the CSV import preview.
type ImportPreviewRow struct {
	Phone string
	Name  string
	Email string
	Error string
	Valid bool
}

func ContactsPage(agent *mw.AgentClaims, tags []db.Tag, industries []string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/contacts", "Contacts", "")); err != nil {
			return err
		}

		// Build industry options for the add-contact modal
		industryOpts := `<option value="">&#8212; select industry &#8212;</option>`
		for _, ind := range industries {
			industryOpts += fmt.Sprintf(`<option value="%s">%s</option>`, html.EscapeString(ind), html.EscapeString(ind))
		}

		// Build industry filter options
		industryFilterOpts := `<option value="">All industries</option>`
		for _, ind := range industries {
			industryFilterOpts += fmt.Sprintf(`<option value="%s">%s</option>`, html.EscapeString(ind), html.EscapeString(ind))
		}

		// Pre-build the new-contact modal using ModalShellRaw so the canonical
		// aria-modal/aria-labelledby/backdrop-close wrapper lives in one place (G-AP).
		// for/id pairing added to every input/select (G-AQ).
		ncInner := fmt.Sprintf(
			`<div class="modal-gradient-hd" style="background:var(--avatar-gradient);padding:28px 24px;text-align:center;position:relative;border-radius:12px 12px 0 0">`+
				`<button type="button" onclick="document.getElementById('new-contact-modal').close()"`+
				` style="position:absolute;top:12px;right:16px;background:var(--bg-fill-overlay-w-secondary);border:none;color:var(--text-on-fill);width:28px;height:28px;border-radius:50%%;cursor:pointer;font-size:16px">&#215;</button>`+
				`<div style="width:60px;height:60px;border-radius:50%%;background:var(--bg-fill-overlay-w-secondary);display:flex;align-items:center;justify-content:center;margin:0 auto 12px;font-size:24px;color:var(--text-on-fill)">?</div>`+
				`<div id="new-contact-modal-title" style="color:var(--text-on-fill);font-size:18px;font-weight:700">Add Contact</div>`+
				`<div style="color:var(--text-overlay-w-secondary);font-size:13px;margin-top:4px">Fill in the details to create a new contact</div>`+
				`</div>`+
				`<div style="padding:20px 24px">`+
				`<form hx-post="/contacts" hx-target="body" hx-swap="none"`+
				` hx-disabled-elt="find button[type='submit']"`+
				` hx-on::after-request="if(event.detail.successful){document.getElementById('new-contact-modal').close();htmx.trigger(document.body,'contactsUpdated')}"`+
				` hx-on::response-error="document.getElementById('nc-form-errors').innerHTML=event.detail.xhr.responseText">`+
				`<div id="nc-form-errors"></div>`+
				`<div class="form-group">`+
				`<label class="form-label" for="nc-phone">Phone <span style="color:var(--danger)">*</span></label>`+
				`<div style="display:flex;gap:8px">`+
				`<select name="country_code" class="form-input" style="width:110px" aria-label="Country code">`+
				`<option value="+91">+91 IN</option><option value="+1">+1 US</option>`+
				`<option value="+44">+44 GB</option><option value="+971">+971 AE</option><option value="+65">+65 SG</option>`+
				`</select>`+
				`<input id="nc-phone" class="form-input" type="tel" name="phone_number" required placeholder="9876543210" style="flex:1">`+
				`</div></div>`+
				`<div class="form-group">`+
				`<label class="form-label" for="nc-name">Full name</label>`+
				`<input id="nc-name" class="form-input" type="text" name="name" placeholder="Anita Desai">`+
				`</div>`+
				`<div class="form-group">`+
				`<label class="form-label" for="nc-email">Email</label>`+
				`<input id="nc-email" class="form-input" type="email" name="email" placeholder="anita@example.com">`+
				`</div>`+
				`<div class="nc-row-2">`+
				`<div class="form-group">`+
				`<label class="form-label" for="nc-company">Company</label>`+
				`<input id="nc-company" class="form-input" type="text" name="company" placeholder="Acme Pvt Ltd">`+
				`</div>`+
				`<div class="form-group">`+
				`<label class="form-label" for="nc-role">Role</label>`+
				`<input id="nc-role" class="form-input" type="text" name="role" placeholder="Sales Manager">`+
				`</div></div>`+
				`<div class="form-group">`+
				`<label class="form-label" for="nc-industry">Industry</label>`+
				`<select id="nc-industry" class="form-input" name="industry">%s</select>`+
				`</div>`+
				`<label style="display:flex;align-items:flex-start;gap:10px;padding:12px;background:var(--success-light,#f0fdf4);border-radius:var(--radius,6px);margin-bottom:16px;cursor:pointer">`+
				`<input type="checkbox" name="opt_in" value="true" style="margin-top:3px">`+
				`<div>`+
				`<div style="font-weight:600;font-size:14px">Opted-in to marketing</div>`+
				`<div style="font-size:12px;color:var(--text-secondary)">Contact has given consent to receive WhatsApp messages</div>`+
				`</div></label>`+
				`<div style="display:flex;gap:8px;justify-content:flex-end">`+
				`<button class="btn btn-secondary btn-sm" type="button" onclick="document.getElementById('new-contact-modal').close()">Cancel</button>`+
				`<button class="btn btn-primary btn-sm" type="submit">Add contact</button>`+
				`</div></form></div>`,
			industryOpts,
		)
		ncDialogHTML := ModalShellRaw("new-contact-modal", "nc-dialog", "new-contact-modal-title", ncInner)

		_, err := fmt.Fprintf(w, `
<div class="page-wrap contacts-wrap">
<div class="page-hd">
  <div>
    <div class="screen-title">Contacts</div>
    <div class="screen-subtitle">Manage your contact list and segments</div>
  </div>
  <div style="display:flex;gap:0.5rem;align-items:center">
    <div class="view-toggle" id="view-toggle">
      <button class="view-btn active" id="btn-grid" onclick="setView('grid')" aria-label="Grid view">
        <svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor"><rect x="1" y="1" width="6" height="6" rx="1"/><rect x="9" y="1" width="6" height="6" rx="1"/><rect x="1" y="9" width="6" height="6" rx="1"/><rect x="9" y="9" width="6" height="6" rx="1"/></svg>
      </button>
      <button class="view-btn" id="btn-list" onclick="setView('list')" aria-label="List view">
        <svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor"><rect x="1" y="2" width="14" height="2" rx="1"/><rect x="1" y="7" width="14" height="2" rx="1"/><rect x="1" y="12" width="14" height="2" rx="1"/></svg>
      </button>
    </div>
    <button class="btn btn-secondary btn-sm" onclick="openModal('import-modal',this)">Import CSV</button>
    <button class="btn btn-primary btn-sm" onclick="openModal('new-contact-modal',this)">+ Add contact</button>
  </div>
</div>

<div class="contacts-filters">
  <div class="ct-opt-tabs" id="opt-tabs" role="tablist" x-data="{optFilter:''}">
    <button class="ct-opt-tab" role="tab" type="button" :class="{'active':optFilter===''}" :aria-selected="optFilter===''" @click="optFilter='';setOptFilter('',null)">All</button>
    <button class="ct-opt-tab" role="tab" type="button" :class="{'active':optFilter==='true'}" :aria-selected="optFilter==='true'" @click="optFilter='true';setOptFilter('true',null)">Opted in</button>
    <button class="ct-opt-tab" role="tab" type="button" :class="{'active':optFilter==='false'}" :aria-selected="optFilter==='false'" @click="optFilter='false';setOptFilter('false',null)">Opted out</button>
  </div>
  <select id="industry-filter-input" name="industry_filter" class="ct-filter-select" aria-label="Filter by industry"
    hx-get="/contacts/table" hx-trigger="change" hx-target="#contacts-table" hx-swap="innerHTML"
    hx-include="#opt-filter-input,[name=search]">
    %s
  </select>
  <input type="search" placeholder="Search name or phone..."
    hx-get="/contacts/table" hx-trigger="input changed delay:300ms" hx-target="#contacts-table" hx-swap="innerHTML"
    hx-include="#opt-filter-input,#industry-filter-input" hx-indicator="#ct-search-ind"
    name="search" class="form-input search-input" style="flex:1;min-width:180px;max-width:340px">
  <span id="ct-search-ind" class="htmx-indicator" aria-hidden="true"><span class="htmx-ind-spin"></span></span>
</div>
<input type="hidden" id="opt-filter-input" name="opted_in_filter" value="">

<div id="contacts-table"
  role="tabpanel" tabindex="0"
  hx-get="/contacts/table"
  hx-trigger="load, contactsUpdated from:body"
  hx-swap="innerHTML"
  aria-live="polite" aria-atomic="false">` + SkeletonRows(6) + `</div>

<div id="ct-panel" class="ct-panel" role="complementary" aria-label="Contact details">
  <div id="ct-panel-body"></div>
</div>
<div id="ct-panel-overlay" onclick="closeContactPanel()"
  style="display:none;position:fixed;inset:0;z-index:98;background:rgba(0,0,0,0.08)"></div>

` + ModalShell("import-modal", "Import contacts from CSV",
			`<form hx-post="/contacts/import/upload" hx-target="#import-steps" hx-swap="innerHTML" enctype="multipart/form-data" hx-disabled-elt="find button[type='submit']">
<label class="field"><span>CSV file</span><input type="file" name="csv_file" accept=".csv" required></label>
<div class="modal-btns">
<button class="btn btn-primary btn-sm" type="submit">Upload &amp; map columns</button>
<button class="btn btn-secondary btn-sm" type="button" onclick="document.getElementById('import-modal').close()">Cancel</button>
</div>
</form>
<div id="import-steps" aria-live="polite" aria-atomic="false"></div>`) + `

%s

</div>

<script>
function setOptFilter(val, evt) {
  document.getElementById('opt-filter-input').value = val;
  var indEl = document.getElementById('industry-filter-input');
  htmx.ajax('GET', '/contacts/table', {target:'#contacts-table', swap:'innerHTML',
    values: {
      opted_in_filter: val,
      search: (document.querySelector('[name=search]') || {}).value || '',
      industry_filter: indEl ? indEl.value : ''
    }});
}
function setView(v) {
  localStorage.setItem('ct_view', v);
  document.getElementById('btn-grid').classList.toggle('active', v === 'grid');
  document.getElementById('btn-list').classList.toggle('active', v === 'list');
  htmx.ajax('GET', '/contacts/table?view=' + v, {target:'#contacts-table', swap:'innerHTML'});
}
function openContactPanel() {
  document.getElementById('ct-panel').classList.add('open');
  document.getElementById('ct-panel-overlay').style.display = 'block';
}
function closeContactPanel() {
  document.getElementById('ct-panel').classList.remove('open');
  document.getElementById('ct-panel-overlay').style.display = 'none';
}
</script>`, industryFilterOpts, ncDialogHTML)
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, ShellClose())
		return err
	})
}


func contactAvatarInitials(name, phone string) string {
	name = strings.TrimSpace(name)
	if name != "" {
		r := []rune(name)
		initials := string(r[0])
		// Try second word initial
		parts := strings.Fields(name)
		if len(parts) > 1 {
			initials += string([]rune(parts[1])[0])
		}
		return strings.ToUpper(initials)
	}
	phone = strings.TrimSpace(phone)
	return "?"
}

func avatarGradient(id string) string {
	if id == "" {
		return "av-g-1"
	}
	return fmt.Sprintf("av-g-%d", (int(id[0])%6)+1)
}

func ContactTable(contacts []db.Contact, total int, offset, limit int, searchActive bool) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if len(contacts) == 0 {
			icon := EmptyIconContacts
			title := "No contacts yet"
			body := "Import a CSV or add contacts manually to get started."
			if searchActive {
				icon = EmptyIconSearch
				title = "No contacts found"
				body = "Try a different search term or clear your filters."
			}
			_, err := io.WriteString(w, EmptyStateHTML(icon, title, body, nil))
			return err
		}

		end := offset + len(contacts)
		_, err := fmt.Fprintf(w, `<div class="contacts-table-wrap">
<div class="table-count">%d&#8211;%d of %d</div>
<table class="contacts-tbl">
<thead><tr>
  <th>PERSON</th><th>COMPANY</th><th>INDUSTRY</th><th>STATUS</th><th></th>
</tr></thead>
<tbody>`, offset+1, end, total)
		if err != nil {
			return err
		}

		for _, c := range contacts {
			company := "&#8212;"
			if v, ok := c.CustomFields["company"]; ok {
				if s, ok := v.(string); ok && s != "" {
					company = html.EscapeString(s)
				}
			}
			industryHTML := "&#8212;"
			if c.Industry != "" {
				industryHTML = fmt.Sprintf(`<span class="ind-chip">%s</span>`, html.EscapeString(c.Industry))
			}

			statusBadge := `<span class="ct-badge ct-badge-off">Not opted in</span>`
			if c.OptedIn {
				statusBadge = `<span class="ct-badge ct-badge-on">Opted in</span>`
			}

			initials := contactAvatarInitials(c.Name, c.WAPhone)
			grad := avatarGradient(c.ID)
			displayName := c.Name
			if displayName == "" {
				displayName = c.WAPhone
			}

			if _, err := fmt.Fprintf(w,
				`<tr class="ct-row"`+
				` hx-get="/contacts/%s"`+
				` hx-target="#ct-panel-body"`+
				` hx-swap="innerHTML"`+
				` hx-on::after-request="if(event.detail.successful)openContactPanel()">`+
				`<td><div class="td-person">`+
				`<div class="ct-av %s">%s</div>`+
				`<div><div class="ct-name">%s</div><div class="ct-phone">%s</div></div>`+
				`</div></td>`+
				`<td class="td-company">%s</td>`+
				`<td>%s</td>`+
				`<td>%s</td>`+
				`<td onclick="event.stopPropagation()">`+
				`<div class="td-actions">`+
				`<button class="ct-tbl-act ct-tbl-act-del"`+
				` hx-delete="/contacts/%s"`+
				` hx-confirm="Permanently delete this contact?"`+
				` hx-target="#contacts-table" hx-swap="innerHTML"`+
				` hx-include="#opt-filter-input,#industry-filter-input,[name=search]"`+
				` aria-label="Delete contact"><svg width="12" height="12" viewBox="0 0 24 24" fill="none"><path d="M8 1.5V2.5H3C2.44772 2.5 2 2.94772 2 3.5V4.5C2 5.05228 2.44772 5.5 3 5.5H21C21.5523 5.5 22 5.05228 22 4.5V3.5C22 2.94772 21.5523 2.5 21 2.5H16V1.5C16 0.947715 15.5523 0.5 15 0.5H9C8.44772 0.5 8 0.947715 8 1.5Z" fill="currentColor"/><path d="M3.9231 7.5H20.0767L19.1344 20.2216C19.0183 21.7882 17.7135 23 16.1426 23H7.85724C6.28636 23 4.98148 21.7882 4.86544 20.2216L3.9231 7.5Z" fill="currentColor"/></svg></button>`+
				`</div></td></tr>`,
				c.ID,
				html.EscapeString(grad), html.EscapeString(initials),
				html.EscapeString(displayName),
				html.EscapeString(c.WAPhone),
				company,
				industryHTML,
				statusBadge,
				c.ID,
			); err != nil {
				return err
			}
		}

		if _, err := io.WriteString(w, `</tbody></table>`); err != nil {
			return err
		}

		// Pagination
		paginationHTML := `<div class="ct-pagination">`
		if offset > 0 {
			prev := offset - limit
			if prev < 0 {
				prev = 0
			}
			paginationHTML += fmt.Sprintf(`<button class="btn btn-secondary btn-sm" hx-get="/contacts/table?offset=%d" hx-target="#contacts-table" hx-swap="innerHTML" hx-indicator="#ct-search-ind">&#8592; Previous</button>`, prev)
		}
		if end < total {
			paginationHTML += fmt.Sprintf(`<button class="btn btn-secondary btn-sm" hx-get="/contacts/table?offset=%d" hx-target="#contacts-table" hx-swap="innerHTML" hx-indicator="#ct-search-ind">Next &#8594;</button>`, offset+limit)
		}
		paginationHTML += `</div>`
		if _, err := io.WriteString(w, paginationHTML+"</div>"); err != nil {
			return err
		}
		return nil
	})
}

func ContactDetail(c *db.Contact, tags []db.Tag, notes []db.ContactNote, allTags []db.Tag) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		optedBadge := `<span class="ct-badge ct-badge-off">Not opted in</span>`
		if c.OptedIn {
			optedBadge = `<span class="ct-badge ct-badge-on">Opted in</span>`
		}
		initials := contactAvatarInitials(c.Name, c.WAPhone)
		grad := avatarGradient(c.ID)
		displayName := c.Name
		if displayName == "" {
			displayName = c.WAPhone
		}
		company := ""
		if v, ok := c.CustomFields["company"]; ok {
			if s, ok := v.(string); ok {
				company = s
			}
		}

		// Subtitle: show phone only when a real name exists (avoid repeating phone twice)
		subtitleHTML := ""
		if c.Name != "" {
			subtitleHTML = `<div class="ct-panel-role">` + html.EscapeString(c.WAPhone) + `</div>`
		}

		// Industry chip in header (quick visual context)
		industryChipHTML := ""
		if c.Industry != "" {
			industryChipHTML = `<div class="ct-panel-ind-chip">` + html.EscapeString(c.Industry) + `</div>`
		}

		// SVG icons for detail rows
		icoPhone := `<svg class="ct-detail-icon" width="14" height="14" viewBox="0 0 24 24" fill="none"><path d="M2.00589 4.54166C1.905 3.11236 3.11531 2 4.54522 2H7.60606C8.34006 2 9.00207 2.44226 9.28438 3.1212L10.5643 6.19946C10.8761 6.94932 10.6548 7.81544 10.0218 8.32292L9.22394 8.96254C8.86788 9.24798 8.74683 9.74018 8.95794 10.1448C10.0429 12.2241 11.6464 13.9888 13.5964 15.2667C14.008 15.5364 14.5517 15.4291 14.8588 15.0445L15.6902 14.003C16.1966 13.3687 17.0609 13.147 17.8092 13.4594L20.8811 14.742C21.5587 15.0249 22 15.6883 22 16.4238V19.5C22 20.9329 20.8489 22.0955 19.4226 21.9941C10.3021 21.3452 2.65247 13.7017 2.00589 4.54166Z" fill="currentColor"/></svg>`
		icoMail := `<svg class="ct-detail-icon" width="14" height="14" viewBox="0 0 24 24" fill="none"><path d="M1.60175 4.20114C2.14997 3.47258 3.02158 3 4 3H20C20.9784 3 21.85 3.47258 22.3982 4.20113L12 11.7635L1.60175 4.20114Z" fill="currentColor"/><path d="M1 6.2365V18C1 19.6523 2.34772 21 4 21H20C21.6523 21 23 19.6523 23 18V6.23649L13.1763 13.381C12.475 13.891 11.525 13.891 10.8237 13.381L1 6.2365Z" fill="currentColor"/></svg>`
		icoBiz := `<svg class="ct-detail-icon" width="14" height="14" viewBox="0 0 24 24" fill="none"><path d="M11.3861 1.21065C11.7472 0.929784 12.2528 0.929784 12.6139 1.21065L21.6139 8.21065C21.8575 8.4001 22 8.69141 22 9V20.5C22 21.3284 21.3284 22 20.5 22H15V14C15 13.4477 14.5523 13 14 13H10C9.44772 13 9 13.4477 9 14V22H3.5C2.67157 22 2 21.3284 2 20.5V9C2 8.69141 2.14247 8.4001 2.38606 8.21065L11.3861 1.21065Z" fill="currentColor"/></svg>`
		icoTag := `<svg class="ct-detail-icon" width="14" height="14" viewBox="0 0 24 24" fill="none"><path fill-rule="evenodd" clip-rule="evenodd" d="M1.94971 4.44987C1.94969 3.06915 3.06898 1.94985 4.4497 1.94984L12.0209 1.94983C12.8165 1.94983 13.5796 2.2659 14.1422 2.82851L22.0417 10.728C23.6038 12.2901 23.6038 14.8228 22.0417 16.3849L16.3848 22.0417C14.8227 23.6038 12.2901 23.6038 10.728 22.0417L2.82846 14.1422C2.26586 13.5796 1.94979 12.8166 1.94978 12.0209L1.94971 4.44987ZM8.5 10C9.32843 10 10 9.32843 10 8.5C10 7.67157 9.32843 7 8.5 7C7.67157 7 7 7.67157 7 8.5C7 9.32843 7.67157 10 8.5 10Z" fill="currentColor"/></svg>`
		icoShield := `<svg class="ct-detail-icon" width="14" height="14" viewBox="0 0 24 24" fill="none"><path d="M12.6966 1.28263C12.6972 1.28313 12.6989 1.28483L12.6978 1.28374L12.7105 1.29508C12.7281 1.31064 12.763 1.34044 12.8171 1.38252C12.9252 1.46665 13.1107 1.60023 13.3895 1.76751C13.9469 2.10195 14.8797 2.57246 16.3162 3.05132C17.7858 3.54118 18.7451 3.77921 19.3211 3.89442C19.6087 3.95194 19.7987 3.97843 19.9073 3.99049C19.9615 3.99651 19.995 3.9989 20.0099 3.99977L20.0166 4.00014C20.5613 4.00902 21 4.45327 21 5V12C21 15.4464 18.7183 18.2003 16.6585 20.0026C15.6076 20.9221 14.5615 21.6408 13.78 22.1292C13.3882 22.3741 13.0603 22.5627 12.8281 22.6913C12.6303 22.8008 12.4417 22.8738 12.2373 22.935C12.0825 22.9814 11.9171 22.983 11.7622 22.9369C11.5576 22.8761 11.3695 22.8007 11.1719 22.6913C10.9397 22.5627 10.6118 22.3741 10.22 22.1292C9.43854 21.6408 8.39238 20.9221 7.3415 20.0026C5.28175 18.2003 3 15.4464 3 12V5C3 4.45328 3.43875 4.00903 3.98335 4.00014L3.99011 3.99977C4.00499 3.9989 4.0385 3.99651 4.09269 3.99049C4.20126 3.97843 4.39127 3.95194 4.67888 3.89442C5.25494 3.77921 6.21419 3.54118 7.68377 3.05132C9.12034 2.57246 10.0531 2.10195 10.6105 1.76751C10.8893 1.60023 11.0748 1.46665 11.1829 1.38252C11.237 1.34044 11.2719 1.31064 11.2896 1.29508L11.3022 1.28374C11.6903 0.905419 12.3097 0.905419 12.6978 1.28374L12.6966 1.28263Z" fill="currentColor"/></svg>`
		icoCal := `<svg class="ct-detail-icon" width="14" height="14" viewBox="0 0 24 24" fill="none"><path d="M8 0.5C8.82843 0.5 9.5 1.17157 9.5 2V3H14.5V2C14.5 1.17157 15.1716 0.5 16 0.5C16.8284 0.5 17.5 1.17157 17.5 2V3H19C20.6569 3 22 4.34315 22 6V8H2V6C2 4.34315 3.34315 3 5 3H6.5V2C6.5 1.17157 7.17157 0.5 8 0.5Z" fill="currentColor"/><path d="M2 20V10H22V20C22 21.6569 20.6569 23 19 23H5C3.34315 23 2 21.6569 2 20Z" fill="currentColor"/></svg>`

		if _, err := fmt.Fprintf(w,
			`<div class="ct-panel-hd %s">`+
				`<button class="ct-close-btn" onclick="closeContactPanel()" aria-label="Close panel">&#215;</button>`+
				`<div class="ct-panel-av">%s</div>`+
				`<div class="ct-panel-name">%s</div>`+
				`%s%s`+
				`<div class="ct-panel-actions">`+
				`<a href="/inbox" class="ct-panel-act-btn ct-panel-act-primary"><svg width="13" height="13" viewBox="0 0 24 24" fill="none" style="flex-shrink:0"><path d="M2.2928 21.292L2.28337 21.3026C1.97175 21.6227 1.91001 22.1115 2.1337 22.4995C2.35966 22.8914 2.82058 23.0828 3.25769 22.9662L9.05302 21.4208C10.1339 21.7963 11.2942 22 12.5 22C18.299 22 23 17.299 23 11.5C23 5.70101 18.299 1 12.5 1C6.70103 1 2.00002 5.70101 2.00002 11.5C2.00002 13.6029 2.61921 15.5638 3.6852 17.2072C3.65453 17.5251 3.60229 17.8896 3.51944 18.3039C3.28993 19.4515 2.95112 20.2289 2.68837 20.7019C2.55663 20.939 2.44292 21.1015 2.36973 21.1972C2.3331 21.2451 2.30653 21.2764 2.2928 21.292Z" fill="currentColor"/></svg> Message</a>`+
				`</div></div>`,
			html.EscapeString(grad),
			html.EscapeString(initials),
			html.EscapeString(displayName),
			subtitleHTML,
			industryChipHTML,
		); err != nil {
			return err
		}

		// Build optional detail rows
		emailRow := ""
		if c.Email != nil && *c.Email != "" {
			emailRow = `<div class="ct-detail-row">` + icoMail + `<span class="ct-detail-key">Email</span><span class="ct-detail-val">` + html.EscapeString(*c.Email) + `</span></div>`
		}
		companyRow := ""
		if company != "" {
			companyRow = `<div class="ct-detail-row">` + icoBiz + `<span class="ct-detail-key">Company</span><span class="ct-detail-val">` + html.EscapeString(company) + `</span></div>`
		}
		industryRow := ""
		if c.Industry != "" {
			industryRow = `<div class="ct-detail-row">` + icoTag + `<span class="ct-detail-key">Industry</span><span class="ct-detail-val">` + html.EscapeString(c.Industry) + `</span></div>`
		}

		if _, err := fmt.Fprintf(w,
			`<div class="ct-panel-details">`+
				`<div class="ct-detail-label">CONTACT INFO</div>`+
				`<div class="ct-detail-row">`+icoPhone+`<span class="ct-detail-key">Phone</span><span class="ct-detail-val">%s</span></div>`+
				`%s%s%s`+
				`<div class="ct-detail-row">`+icoShield+`<span class="ct-detail-key">Status</span><span class="ct-detail-val">%s</span></div>`+
				`<div class="ct-detail-row">`+icoCal+`<span class="ct-detail-key">Added</span><span class="ct-detail-val">%s</span></div>`+
				`</div>`,
			html.EscapeString(c.WAPhone),
			emailRow,
			companyRow,
			industryRow,
			optedBadge,
			c.CreatedAt.Format("02 Jan 2006"),
		); err != nil {
			return err
		}

		if _, err := fmt.Fprintf(w,
			`<div class="ct-panel-section">`+
				`<span class="ct-section-label">Tags</span>`+
				`<div id="contact-tags-%s" aria-live="polite">`, c.ID); err != nil {
			return err
		}
		if err := ContactTagList(tags, allTags, c.ID).Render(ctx, w); err != nil {
			return err
		}
		if _, err := io.WriteString(w, `</div></div>`); err != nil {
			return err
		}

		if _, err := fmt.Fprintf(w,
			`<div class="ct-panel-section">`+
				`<span class="ct-section-label">Notes</span>`+
				`<div id="contact-notes-%s" aria-live="polite">`, c.ID); err != nil {
			return err
		}
		if err := ContactNoteList(notes).Render(ctx, w); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w,
			`</div>`+
				`<form hx-post="/contacts/%s/notes" hx-target="#contact-notes-%s" hx-swap="innerHTML"`+
				` hx-on::after-request="this.reset()" style="margin-top:8px">`+
				`<div class="note-input-row">`+
				`<textarea name="body" rows="2" placeholder="Add a note..." required></textarea>`+
				`<button class="btn btn-sm btn-primary" type="submit">Add</button>`+
				`</div></form></div>`,
			c.ID, c.ID,
		); err != nil {
			return err
		}

		_, err := fmt.Fprintf(w,
			`<div class="ct-panel-section" style="border-top:1px solid var(--danger-light)">`+
				`<button class="btn btn-sm btn-danger" style="width:100%%"`+
				` hx-delete="/contacts/%s"`+
				` hx-confirm="Permanently delete this contact?"`+
				` hx-target="#contacts-table" hx-swap="innerHTML"`+
				` hx-include="#opt-filter-input,#industry-filter-input,[name=search]"`+
				` onclick="closeContactPanel()">Delete contact</button></div>`,
			c.ID,
		)
		return err
	})
}

func ContactTagList(tags []db.Tag, allTags []db.Tag, contactID string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		tagSet := make(map[int64]bool, len(tags))
		for _, t := range tags {
			tagSet[t.ID] = true
		}
		if _, err := io.WriteString(w, `<div class="ct-tags-wrap">`); err != nil {
			return err
		}
		for _, t := range tags {
			if _, err := fmt.Fprintf(w,
				`<span class="ct-tag-chip" style="background:%s">%s`+
					`<button class="ct-tag-remove" type="button" aria-label="Remove tag"`+
					` hx-delete="/contacts/%s/tags/%d"`+
					` hx-target="#contact-tags-%s"`+
					` hx-swap="innerHTML">&#215;</button></span>`,
				html.EscapeString(t.Color), html.EscapeString(t.Name),
				contactID, t.ID, contactID,
			); err != nil {
				return err
			}
		}
		hasAvailable := false
		for _, t := range allTags {
			if !tagSet[t.ID] {
				hasAvailable = true
				break
			}
		}
		if hasAvailable {
			if _, err := io.WriteString(w,
				`<details class="ct-tag-adder">`+
					`<summary class="ct-tag-add-btn" style="list-style:none;cursor:pointer">+ Add tag</summary>`+
					`<div class="ct-tag-dropdown">`); err != nil {
				return err
			}
			for _, t := range allTags {
				if !tagSet[t.ID] {
					if _, err := fmt.Fprintf(w,
						`<form hx-post="/contacts/%s/tags" hx-target="#contact-tags-%s" hx-swap="innerHTML" style="display:inline">`+
							`<button class="ct-tag-opt" type="submit" name="tag_id" value="%d"`+
							` style="background:%s">%s</button></form>`,
						contactID, contactID, t.ID,
						html.EscapeString(t.Color), html.EscapeString(t.Name),
					); err != nil {
						return err
					}
				}
			}
			if _, err := io.WriteString(w, `</div></details>`); err != nil {
				return err
			}
		}
		_, err := io.WriteString(w, `</div>`)
		return err
	})
}

func ContactNoteList(notes []db.ContactNote) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if len(notes) == 0 {
			_, err := io.WriteString(w, `<p class="empty-hint">No notes yet.</p>`)
			return err
		}
		for _, n := range notes {
			if _, err := fmt.Fprintf(w,
				`<div class="note-item"><div class="note-body">%s</div><div class="note-meta">%s</div></div>`,
				html.EscapeString(n.Body), n.CreatedAt.Format("02 Jan 2006 15:04"),
			); err != nil {
				return err
			}
		}
		return nil
	})
}

func TagManagerList(tags []db.Tag) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, `<ul class="tag-list">`); err != nil {
			return err
		}
		for _, t := range tags {
			if _, err := fmt.Fprintf(w,
				`<li><span class="chip" style="background:%s">%s</span>
<button class="btn btn-sm btn-danger"
  hx-delete="/contacts/tags/%d"
  hx-target="closest li"
  hx-swap="outerHTML"
  hx-confirm="Delete tag %s?">Delete</button></li>`,
				html.EscapeString(t.Color), html.EscapeString(t.Name), t.ID, html.EscapeString(t.Name),
			); err != nil {
				return err
			}
		}
		_, err := fmt.Fprintf(w, `</ul>
<form hx-post="/contacts/tags" hx-target="#tag-manager-list" hx-swap="innerHTML">
<label class="field"><span>Tag name</span><input type="text" name="name" required></label>
<label class="field"><span>Colour</span><input type="color" name="color" value="#0E7A40"></label>
<button class="btn btn-sm btn-primary" type="submit">Create tag</button>
</form>`)
		return err
	})
}

func SegmentList(segs []db.Segment) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if len(segs) == 0 {
			_, err := io.WriteString(w, `<p class="empty-hint">No saved segments.</p>`)
			return err
		}
		_, err := io.WriteString(w, `<ul class="seg-list">`)
		if err != nil {
			return err
		}
		for _, s := range segs {
			if _, err := fmt.Fprintf(w,
				`<li><strong>%s</strong>
<button class="btn btn-sm btn-danger"
  hx-delete="/contacts/segments/%d"
  hx-target="closest li"
  hx-swap="outerHTML"
  hx-confirm="Delete segment?">Delete</button></li>`,
				html.EscapeString(s.Name), s.ID,
			); err != nil {
				return err
			}
		}
		_, err = io.WriteString(w, `</ul>`)
		return err
	})
}

func ImportMapColumns(headers []string, csvData string, rowCount int) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		opts := `<option value="">&#8212; skip &#8212;</option>`
		for i, h := range headers {
			opts += fmt.Sprintf(`<option value="%d">%s</option>`, i, html.EscapeString(h))
		}

		_, err := fmt.Fprintf(w, `
<div class="import-map">
<p>%d data rows detected. Map the columns:</p>
<form hx-post="/contacts/import/preview" hx-target="#import-steps" hx-swap="innerHTML">
<input type="hidden" name="csv_data" value="%s">
<label class="field"><span>Phone column <span class="req">*</span></span>
<select name="phone_col" required>%s</select></label>
<label class="field"><span>Name column</span>
<select name="name_col">%s</select></label>
<label class="field"><span>Email column</span>
<select name="email_col">%s</select></label>
<label class="field"><span>Tags (comma-separated)</span>
<input type="text" name="tags" placeholder="newsletter, promo"></label>
<label class="field cb">
<input type="checkbox" name="mark_opted_in">
<span>Mark all as opted-in</span></label>
<div class="form-btns">
<button class="btn btn-primary btn-sm" type="submit">Preview import</button>
</div>
</form>
</div>`, rowCount, html.EscapeString(csvData), opts, opts, opts)
		return err
	})
}

func ImportPreview(rows []ImportPreviewRow, total int, csvData string, phoneCol, nameCol, emailCol int, tags string, optedIn bool) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, err := fmt.Fprintf(w, `
<div class="import-preview">
<p>Preview (first %d of %d rows):</p>
<table class="tbl">
<thead><tr><th>Phone</th><th>Name</th><th>Email</th><th>Status</th></tr></thead>
<tbody>`, len(rows), total)
		if err != nil {
			return err
		}
		for _, r := range rows {
			status := BadgeHTML("approved", "Valid")
			if !r.Valid {
				status = BadgeHTML("rejected", r.Error)
			}
			if _, err := fmt.Fprintf(w, `<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>`,
				html.EscapeString(r.Phone), html.EscapeString(r.Name),
				html.EscapeString(r.Email), status,
			); err != nil {
				return err
			}
		}
		optedInVal := "false"
		if optedIn {
			optedInVal = "true"
		}
		_, err = fmt.Fprintf(w, `</tbody></table>
<form method="post" action="/contacts/import/confirm">
<input type="hidden" name="csv_data" value="%s">
<input type="hidden" name="phone_col" value="%s">
<input type="hidden" name="name_col" value="%s">
<input type="hidden" name="email_col" value="%s">
<input type="hidden" name="tags" value="%s">
<input type="hidden" name="mark_opted_in" value="%s">
<div class="form-btns">
<button class="btn btn-primary btn-sm" type="submit">Confirm import (%d total rows)</button>
</div>
</form>
</div>`,
			html.EscapeString(csvData),
			strconv.Itoa(phoneCol), strconv.Itoa(nameCol), strconv.Itoa(emailCol),
			html.EscapeString(tags), optedInVal, total,
		)
		return err
	})
}

func ImportResult(inserted, skipped, invalidCount int) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, err := fmt.Fprintf(w, `
<div class="import-result">
<p class="success">Import complete.</p>
<dl>
<dt>Inserted</dt><dd>%d</dd>
<dt>Skipped (duplicate phone)</dt><dd>%d</dd>
<dt>Invalid</dt><dd>%d</dd>
</dl>
<a class="btn btn-primary btn-sm" href="/contacts">View contacts</a>
</div>`, inserted, skipped, invalidCount)
		return err
	})
}

