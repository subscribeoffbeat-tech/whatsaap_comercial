package handlers

import (
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"whatsapptool/internal/db"
	mw "whatsapptool/internal/web/middleware"
	"whatsapptool/internal/web/templates"
)

// ContactsHandler handles all /contacts routes.
type ContactsHandler struct {
	pool *pgxpool.Pool
}

func NewContactsHandler(pool *pgxpool.Pool) *ContactsHandler {
	return &ContactsHandler{pool: pool}
}

// Mount registers all contacts routes.
func (h *ContactsHandler) Mount(r chi.Router) {
	r.Get("/", h.Page)
	r.Get("/table", h.Table)
	r.Get("/export.csv", h.ExportCSV)
	r.Post("/", h.Create)

	// New contact page
	r.Get("/new", h.NewContactPage)
	r.Post("/new", h.CreateNewContact)

	// CSV import wizard
	r.Get("/import", h.ImportPage)
	r.Get("/import/sample", h.ImportSample)
	r.Post("/import/upload", h.ImportUpload)
	r.Post("/import/preview", h.ImportPreview)
	r.Post("/import/confirm", h.ImportConfirm)

	// Tags management
	r.Get("/tags", h.ListTagsPartial)
	r.Post("/tags", h.CreateTag)
	r.Put("/tags/{id}", h.UpdateTag)
	r.Delete("/tags/{id}", h.DeleteTag)

	// Segments
	r.Get("/segments", h.ListSegments)
	r.Post("/segments", h.CreateSegment)
	r.Delete("/segments/{id}", h.DeleteSegment)

	// Per-contact routes (must come after named sub-paths above)
	r.Get("/{id}", h.Detail)
	r.Get("/{id}/view", h.View)
	r.Get("/{id}/edit", h.EditForm)
	r.Get("/{id}/edit-page", h.EditPage)
	r.Post("/{id}/update", h.UpdatePage)
	r.Post("/{id}/consent", h.Consent)
	r.Put("/{id}", h.Update)
	r.Delete("/{id}", h.Delete)
	r.Post("/{id}/tags", h.AddTag)
	r.Delete("/{id}/tags/{tagID}", h.RemoveTag)
	r.Post("/{id}/notes", h.AddNote)
}

// ── Page ─────────────────────────────────────────────────────────────────────

var contactIndustries = []string{
	"Real Estate", "Manufacturing", "Chemicals", "Construction",
	"Building Materials", "Healthcare & Pharma", "Food & FMCG",
	"Beauty & Personal Care", "Technology", "Renewable Energy",
	"Retail & Branding", "Consumer Brands",
}

func (h *ContactsHandler) Page(w http.ResponseWriter, r *http.Request) {
	tags, _ := db.ListTags(r.Context(), h.pool)
	agent := mw.AgentFromCtx(r.Context())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ContactsPage(agent, tags, contactIndustries).Render(r.Context(), w); err != nil {
		log.Printf("contacts page render: %v", err)
	}
}

// ── New contact page ──────────────────────────────────────────────────────────

func (h *ContactsHandler) NewContactPage(w http.ResponseWriter, r *http.Request) {
	agent := mw.AgentFromCtx(r.Context())
	tags, _ := db.ListTags(r.Context(), h.pool)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.NewContactPage(agent, tags, "").Render(r.Context(), w); err != nil {
		log.Printf("new contact page render: %v", err)
	}
}

func (h *ContactsHandler) CreateNewContact(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	agent := mw.AgentFromCtx(r.Context())

	rawPhone := strings.TrimSpace(r.FormValue("phone"))
	phone, err := db.NormalizePhone(rawPhone)
	if err != nil {
		tags, _ := db.ListTags(r.Context(), h.pool)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnprocessableEntity)
		templates.NewContactPage(agent, tags, "Invalid phone: "+err.Error()).Render(r.Context(), w)
		return
	}

	customFields := map[string]any{}
	if city := strings.TrimSpace(r.FormValue("city")); city != "" {
		customFields["city"] = city
	}

	src := "manual"
	optIn := r.FormValue("opt_in") == "on" || r.FormValue("opt_in") == "true"
	now := time.Now()

	c := &db.Contact{
		WAPhone:      phone,
		Name:         strings.TrimSpace(r.FormValue("name")),
		OptedIn:      optIn,
		OptInSource:  &src,
		CustomFields: customFields,
	}
	if optIn {
		c.OptInAt = &now
	}
	if email := strings.TrimSpace(r.FormValue("email")); email != "" {
		c.Email = &email
	}

	if err := db.CreateContact(r.Context(), h.pool, c); err != nil {
		log.Printf("create new contact: %v", err)
		tags, _ := db.ListTags(r.Context(), h.pool)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnprocessableEntity)
		msg := "Failed to save contact."
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			msg = "A contact with this phone number already exists."
		}
		templates.NewContactPage(agent, tags, msg).Render(r.Context(), w)
		return
	}

	// Apply selected tags
	if tagNames := strings.TrimSpace(r.FormValue("tags")); tagNames != "" {
		allTags, _ := db.ListTags(r.Context(), h.pool)
		tagMap := map[string]int64{}
		for _, t := range allTags {
			tagMap[strings.ToLower(t.Name)] = t.ID
		}
		for _, name := range strings.Split(tagNames, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			tagID, ok := tagMap[strings.ToLower(name)]
			if !ok {
				if t, err := db.CreateTag(r.Context(), h.pool, name, ""); err == nil {
					tagID = t.ID
					ok = true
				}
			}
			if ok {
				_ = db.BulkAddTag(r.Context(), h.pool, []string{c.ID}, tagID)
			}
		}
	}

	// Add initial note if provided
	if note := strings.TrimSpace(r.FormValue("notes")); note != "" {
		var agentID *string
		if agent != nil {
			agentID = &agent.ID
		}
		_ = db.CreateNote(r.Context(), h.pool, &db.ContactNote{
			ContactID: c.ID,
			AgentID:   agentID,
			Body:      note,
		})
	}

	http.Redirect(w, r, "/contacts", http.StatusSeeOther)
}

// ── Table partial ─────────────────────────────────────────────────────────────

func (h *ContactsHandler) Table(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	offset, _ := strconv.Atoi(q.Get("offset"))
	f := db.ListContactsFilter{Search: q.Get("search"), Offset: offset, Limit: 50}
	// Multi-tag filter (comma-separated IDs) — show contacts with ANY selected tag.
	if tagsStr := q.Get("tags"); tagsStr != "" {
		for _, p := range strings.Split(tagsStr, ",") {
			if id, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64); err == nil {
				f.TagIDs = append(f.TagIDs, id)
			}
		}
	} else if tagStr := q.Get("tag"); tagStr != "" {
		if id, err := strconv.ParseInt(tagStr, 10, 64); err == nil {
			f.TagID = &id
		}
	}
	switch q.Get("opted_in_filter") {
	case "true":
		t := true
		f.OptedIn = &t
	case "false":
		fa := false
		f.OptedIn = &fa
	}
	f.Industry = q.Get("industry_filter")

	contacts, total, err := db.ListContacts(r.Context(), h.pool, f)
	if err != nil {
		log.Printf("list contacts: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	searchActive := q.Get("search") != "" || q.Get("tag") != "" || q.Get("tags") != "" || q.Get("opted_in_filter") != "" || q.Get("industry_filter") != ""
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ContactTable(contacts, total, offset, 50, searchActive).Render(r.Context(), w); err != nil {
		log.Printf("contact table render: %v", err)
	}
}

// ── Create ────────────────────────────────────────────────────────────────────

func (h *ContactsHandler) Create(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	countryCode := strings.TrimSpace(r.FormValue("country_code"))
	phoneNumber := strings.TrimSpace(r.FormValue("phone_number"))
	// Support both new split format and legacy single "phone" field
	rawPhone := r.FormValue("phone")
	if rawPhone == "" {
		rawPhone = countryCode + phoneNumber
	}

	phone, err := db.NormalizePhone(rawPhone)
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, templates.FieldError(err.Error()))
		return
	}

	src := "manual"
	customFields := map[string]any{}
	if company := strings.TrimSpace(r.FormValue("company")); company != "" {
		customFields["company"] = company
	}
	if role := strings.TrimSpace(r.FormValue("role")); role != "" {
		customFields["role"] = role
	}

	optIn := r.FormValue("opt_in") == "true" || r.FormValue("opt_in") == "on"

	c := &db.Contact{
		WAPhone:      phone,
		Name:         strings.TrimSpace(r.FormValue("name")),
		Industry:     strings.TrimSpace(r.FormValue("industry")),
		OptedIn:      optIn,
		OptInSource:  &src,
		CustomFields: customFields,
	}
	if email := strings.TrimSpace(r.FormValue("email")); email != "" {
		c.Email = &email
	}

	if err := db.CreateContact(r.Context(), h.pool, c); err != nil {
		log.Printf("create contact: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("HX-Trigger", "contactsUpdated")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, templates.ToastFragment(templates.ToastSuccess, "Contact added.", "", ""))
}

// ── Detail panel ─────────────────────────────────────────────────────────────

func (h *ContactsHandler) Detail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	c, err := db.GetContact(r.Context(), h.pool, id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	tags, _ := db.GetContactTags(r.Context(), h.pool, id)
	notes, _ := db.ListNotes(r.Context(), h.pool, id)
	allTags, _ := db.ListTags(r.Context(), h.pool)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ContactDetail(c, tags, notes, allTags).Render(r.Context(), w); err != nil {
		log.Printf("contact detail render: %v", err)
	}
}

// ── Update ────────────────────────────────────────────────────────────────────

// View renders the full contact profile page with a real activity timeline,
// campaign history and notes.
func (h *ContactsHandler) View(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	c, err := db.GetContact(r.Context(), h.pool, id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	tags, _ := db.GetContactTags(r.Context(), h.pool, id)
	allTags, _ := db.ListTags(r.Context(), h.pool)
	notes, _ := db.ListNotes(r.Context(), h.pool, id)
	msgs, _ := db.ListContactMessages(r.Context(), h.pool, id, 100)
	camps, _ := db.ListContactCampaigns(r.Context(), h.pool, id)

	// Assemble a unified activity timeline from real data.
	var activity []templates.ActivityItem
	for _, m := range msgs {
		if m.IsCampaign {
			continue // campaign sends are listed via the campaign rows below
		}
		kind, title := "inbound", "Received message"
		if m.Direction == "outbound" {
			kind, title = "outbound", "Sent message"
		}
		activity = append(activity, templates.ActivityItem{
			Kind: kind, Title: title, Body: m.Body, Status: m.Status, At: m.CreatedAt,
		})
	}
	for _, cp := range camps {
		activity = append(activity, templates.ActivityItem{
			Kind: "campaign", Title: "Sent campaign: " + cp.CampaignName, Status: cp.Status, At: cp.SentAt,
		})
	}
	for _, n := range notes {
		activity = append(activity, templates.ActivityItem{
			Kind: "note", Title: "Note", Body: n.Body, At: n.CreatedAt,
		})
	}
	activity = append(activity, templates.ActivityItem{
		Kind: "created", Title: "Contact created", At: c.CreatedAt,
	})
	sort.Slice(activity, func(i, j int) bool { return activity[i].At.After(activity[j].At) })

	agent := mw.AgentFromCtx(r.Context())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ContactViewPage(agent, c, tags, allTags, activity, camps, notes).Render(r.Context(), w); err != nil {
		log.Printf("contact view render: %v", err)
	}
}

// contactTagNames returns the contact's current tag names.
func (h *ContactsHandler) contactTagNames(r *http.Request, id string) []string {
	tags, _ := db.GetContactTags(r.Context(), h.pool, id)
	names := make([]string, 0, len(tags))
	for _, t := range tags {
		names = append(names, t.Name)
	}
	return names
}

// EditPage renders the full-page contact editor (matching the New Contact form).
func (h *ContactsHandler) EditPage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	c, err := db.GetContact(r.Context(), h.pool, id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	allTags, _ := db.ListTags(r.Context(), h.pool)
	agent := mw.AgentFromCtx(r.Context())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.EditContactPage(agent, c, allTags, h.contactTagNames(r, id), "").Render(r.Context(), w); err != nil {
		log.Printf("contact edit page render: %v", err)
	}
}

// UpdatePage handles the full-page editor submit: updates fields, syncs tags,
// optionally adds a note, then redirects back to the profile.
func (h *ContactsHandler) UpdatePage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	c, err := db.GetContact(ctx, h.pool, id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	_ = r.ParseForm()
	agent := mw.AgentFromCtx(ctx)
	renderErr := func(msg string) {
		allTags, _ := db.ListTags(ctx, h.pool)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = templates.EditContactPage(agent, c, allTags, h.contactTagNames(r, id), msg).Render(ctx, w)
	}

	phone, perr := db.NormalizePhone(strings.TrimSpace(r.FormValue("phone")))
	if perr != nil {
		renderErr("Invalid phone: " + perr.Error())
		return
	}

	c.Name = strings.TrimSpace(r.FormValue("name"))
	c.WAPhone = phone
	c.OptedIn = r.FormValue("opt_in") == "on" || r.FormValue("opt_in") == "true"
	if email := strings.TrimSpace(r.FormValue("email")); email != "" {
		c.Email = &email
	} else {
		c.Email = nil
	}
	if c.CustomFields == nil {
		c.CustomFields = map[string]any{}
	}
	if city := strings.TrimSpace(r.FormValue("city")); city != "" {
		c.CustomFields["city"] = city
	} else {
		delete(c.CustomFields, "city")
	}

	if err := db.UpdateContact(ctx, h.pool, c); err != nil {
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			renderErr("That phone number already belongs to another contact.")
			return
		}
		log.Printf("update contact (page): %v", err)
		renderErr("Could not save changes. Please try again.")
		return
	}

	h.syncContactTags(ctx, id, r.FormValue("tags"))

	// Optionally add a note.
	if note := strings.TrimSpace(r.FormValue("notes")); note != "" {
		var agentID *string
		if agent != nil {
			agentID = &agent.ID
		}
		_ = db.CreateNote(ctx, h.pool, &db.ContactNote{ContactID: id, AgentID: agentID, Body: note})
	}

	http.Redirect(w, r, "/contacts/"+id+"/view", http.StatusSeeOther)
}

// syncContactTags makes the contact's tags exactly match the comma-separated
// selection (adds new tags, creating any that don't exist; removes deselected).
func (h *ContactsHandler) syncContactTags(ctx context.Context, contactID, csv string) {
	allTags, _ := db.ListTags(ctx, h.pool)
	nameToID := map[string]int64{}
	for _, t := range allTags {
		nameToID[strings.ToLower(t.Name)] = t.ID
	}
	current, _ := db.GetContactTags(ctx, h.pool, contactID)
	currentByName := map[string]int64{}
	for _, t := range current {
		currentByName[strings.ToLower(t.Name)] = t.ID
	}

	selected := map[string]bool{}
	for _, n := range strings.Split(csv, ",") {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		key := strings.ToLower(n)
		selected[key] = true
		if _, has := currentByName[key]; has {
			continue // already on the contact
		}
		id, ok := nameToID[key]
		if !ok {
			if t, err := db.CreateTag(ctx, h.pool, n, ""); err == nil {
				id, ok = t.ID, true
			}
		}
		if ok {
			_ = db.BulkAddTag(ctx, h.pool, []string{contactID}, id)
		}
	}
	// Remove tags no longer selected.
	for name, id := range currentByName {
		if !selected[name] {
			_ = db.RemoveContactTag(ctx, h.pool, contactID, id)
		}
	}
}

// Consent toggles a contact's marketing opt-in from the profile page switch.
func (h *ContactsHandler) Consent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_ = r.ParseForm()
	optedIn := r.FormValue("opted_in") == "on" || r.FormValue("opted_in") == "true"
	if err := db.SetContactConsent(r.Context(), h.pool, id, optedIn); err != nil {
		log.Printf("set consent %s: %v", id, err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("HX-Trigger", "contactsUpdated")
	w.WriteHeader(http.StatusNoContent)
}

// EditForm renders the inline edit form inside the contact panel.
func (h *ContactsHandler) EditForm(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	c, err := db.GetContact(r.Context(), h.pool, id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ContactEditForm(c, "").Render(r.Context(), w); err != nil {
		log.Printf("contact edit form render: %v", err)
	}
}

func (h *ContactsHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	c, err := db.GetContact(r.Context(), h.pool, id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// JSON path (programmatic API) — keep as-is, returns 204.
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var body struct {
			Name         string         `json:"name"`
			Email        string         `json:"email"`
			OptedIn      bool           `json:"opted_in"`
			CustomFields map[string]any `json:"custom_fields"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		c.Name = body.Name
		c.OptedIn = body.OptedIn
		if body.CustomFields != nil {
			c.CustomFields = body.CustomFields
		}
		if body.Email != "" {
			c.Email = &body.Email
		} else {
			c.Email = nil
		}
		if err := db.UpdateContact(r.Context(), h.pool, c); err != nil {
			log.Printf("update contact: %v", err)
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("HX-Trigger", "contactsUpdated")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Form path (from the panel edit form) — returns the refreshed panel.
	_ = r.ParseForm()
	phone, perr := db.NormalizePhone(r.FormValue("phone"))
	editErr := func(msg string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = templates.ContactEditForm(c, msg).Render(r.Context(), w)
	}
	if perr != nil {
		editErr("Invalid phone: " + perr.Error())
		return
	}

	c.Name = strings.TrimSpace(r.FormValue("name"))
	c.WAPhone = phone
	c.Industry = strings.TrimSpace(r.FormValue("industry"))
	c.OptedIn = r.FormValue("opted_in") == "on" || r.FormValue("opted_in") == "true"
	if email := strings.TrimSpace(r.FormValue("email")); email != "" {
		c.Email = &email
	} else {
		c.Email = nil
	}

	if err := db.UpdateContact(r.Context(), h.pool, c); err != nil {
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			editErr("That phone number already belongs to another contact.")
			return
		}
		log.Printf("update contact: %v", err)
		editErr("Could not save changes. Please try again.")
		return
	}

	// Return the refreshed read-only panel and tell the table to reload.
	updated, _ := db.GetContact(r.Context(), h.pool, id)
	tags, _ := db.GetContactTags(r.Context(), h.pool, id)
	notes, _ := db.ListNotes(r.Context(), h.pool, id)
	allTags, _ := db.ListTags(r.Context(), h.pool)
	w.Header().Set("HX-Trigger", "contactsUpdated")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ContactDetail(updated, tags, notes, allTags).Render(r.Context(), w); err != nil {
		log.Printf("contact detail re-render: %v", err)
	}
}


// ── Delete (admin only — DPDP / delete-on-request) ────────────────────────────

func (h *ContactsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	actor := mw.AgentFromCtx(r.Context())
	if actor == nil || actor.Role != "admin" {
		http.Error(w, "admin only", http.StatusForbidden)
		return
	}
	id := chi.URLParam(r, "id")
	if err := db.DeleteContact(r.Context(), h.pool, id); err != nil {
		log.Printf("delete contact: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	db.Log(r.Context(), h.pool, actor.ID, "contact_deleted", "contact", id, nil)
	w.Header().Set("HX-Trigger", "contactsUpdated")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, templates.ToastFragment(templates.ToastSuccess, "Contact deleted.", "", ""))
}

// ── Tag operations on a contact ───────────────────────────────────────────────

func (h *ContactsHandler) AddTag(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	tagID, err := strconv.ParseInt(r.FormValue("tag_id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid tag_id", http.StatusBadRequest)
		return
	}
	if err := db.AddContactTag(r.Context(), h.pool, id, tagID); err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	tags, _ := db.GetContactTags(r.Context(), h.pool, id)
	allTags, _ := db.ListTags(r.Context(), h.pool)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("HX-Trigger", "contactsUpdated")
	templates.ContactTagList(tags, allTags, id).Render(r.Context(), w)
}

func (h *ContactsHandler) RemoveTag(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	tagID, err := strconv.ParseInt(chi.URLParam(r, "tagID"), 10, 64)
	if err != nil {
		http.Error(w, "invalid tagID", http.StatusBadRequest)
		return
	}
	if err := db.RemoveContactTag(r.Context(), h.pool, id, tagID); err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	tags, _ := db.GetContactTags(r.Context(), h.pool, id)
	allTags, _ := db.ListTags(r.Context(), h.pool)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("HX-Trigger", "contactsUpdated")
	templates.ContactTagList(tags, allTags, id).Render(r.Context(), w)
}

// ── Notes ─────────────────────────────────────────────────────────────────────

func (h *ContactsHandler) AddNote(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	body := strings.TrimSpace(r.FormValue("body"))
	if body == "" {
		http.Error(w, "body required", http.StatusBadRequest)
		return
	}
	note := &db.ContactNote{ContactID: id, Body: body}
	if agent := mw.AgentFromCtx(r.Context()); agent != nil {
		note.AgentID = &agent.ID
	}
	if err := db.CreateNote(r.Context(), h.pool, note); err != nil {
		log.Printf("create note: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	notes, _ := db.ListNotes(r.Context(), h.pool, id)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.ContactNoteList(notes).Render(r.Context(), w)
}

// ── Tag CRUD ─────────────────────────────────────────────────────────────────

func (h *ContactsHandler) ListTagsPartial(w http.ResponseWriter, r *http.Request) {
	tags, _ := db.ListTags(r.Context(), h.pool)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.TagManagerList(tags).Render(r.Context(), w)
}

func (h *ContactsHandler) CreateTag(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	if _, err := db.CreateTag(r.Context(), h.pool, name, r.FormValue("color")); err != nil {
		log.Printf("create tag: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	tags, _ := db.ListTags(r.Context(), h.pool)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.TagManagerList(tags).Render(r.Context(), w)
}

func (h *ContactsHandler) UpdateTag(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err := db.UpdateTag(r.Context(), h.pool, id, r.FormValue("name"), r.FormValue("color")); err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	tags, _ := db.ListTags(r.Context(), h.pool)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.TagManagerList(tags).Render(r.Context(), w)
}

func (h *ContactsHandler) DeleteTag(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err := db.DeleteTag(r.Context(), h.pool, id); err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	tags, _ := db.ListTags(r.Context(), h.pool)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.TagManagerList(tags).Render(r.Context(), w)
}

// ── Segments ─────────────────────────────────────────────────────────────────

func (h *ContactsHandler) ListSegments(w http.ResponseWriter, r *http.Request) {
	segs, _ := db.ListSegments(r.Context(), h.pool)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.SegmentList(segs).Render(r.Context(), w)
}

func (h *ContactsHandler) CreateSegment(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string           `json:"name"`
		Filter db.SegmentFilter `json:"filter"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	seg := &db.Segment{Name: body.Name, Filter: body.Filter}
	if err := db.CreateSegment(r.Context(), h.pool, seg); err != nil {
		log.Printf("create segment: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	segs, _ := db.ListSegments(r.Context(), h.pool)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.SegmentList(segs).Render(r.Context(), w)
}

func (h *ContactsHandler) DeleteSegment(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err := db.DeleteSegment(r.Context(), h.pool, id); err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	segs, _ := db.ListSegments(r.Context(), h.pool)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.SegmentList(segs).Render(r.Context(), w)
}

// ── CSV import wizard ─────────────────────────────────────────────────────────

func (h *ContactsHandler) ImportPage(w http.ResponseWriter, r *http.Request) {
	agent := mw.AgentFromCtx(r.Context())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ImportPage(agent).Render(r.Context(), w); err != nil {
		log.Printf("import page render: %v", err)
	}
}

func (h *ContactsHandler) ImportSample(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="contacts_sample.csv"`)
	fmt.Fprint(w, "name,phone,email,city,tags,consent\nPriya Sharma,+919876543210,priya@mail.com,Mumbai,\"Technology, Consumer Brands\",yes\nRaj Patel,+918765432109,,Delhi,Healthcare & Pharma,yes\n")
}

const maxUploadBytes = 5 << 20 // 5 MB

// importErr writes an HTML error fragment (200) so HTMX swaps it into #import-content.
func importErr(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<div class="callout callout--danger" style="margin-bottom:16px">%s</div>`+
		`<p><a href="/contacts/import" class="btn btn-secondary btn-md">Try again</a></p>`, msg)
}

// ImportUpload parses the uploaded CSV and returns the column-mapping UI.
func (h *ContactsHandler) ImportUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		importErr(w, "File too large — maximum size is 5 MB.")
		return
	}
	f, _, err := r.FormFile("csv_file")
	if err != nil {
		importErr(w, "No file received. Please select a CSV file and try again.")
		return
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.TrimLeadingSpace = true
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		importErr(w, "Could not parse CSV: "+err.Error())
		return
	}
	if len(records) < 2 {
		importErr(w, "CSV must have a header row and at least one data row.")
		return
	}

	rawJSON, _ := json.Marshal(records)
	csvData := base64.StdEncoding.EncodeToString(rawJSON)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.ImportMapColumns(records[0], csvData, len(records)-1).Render(r.Context(), w)
}

// importMapping holds the user's column-mapping choices parsed from a form.
type importMapping struct {
	PhoneCol    int
	NameCol     int // -1 = not mapped
	EmailCol    int // -1 = not mapped
	CityCol     int // -1 = not mapped
	TagsCol     int // -1 = not mapped (per-row tags column)
	Tags        []string
	MarkOptedIn bool
	CSVData     string
	Records     [][]string
}

func parseImportMapping(r *http.Request) (*importMapping, error) {
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	phoneColStr := r.FormValue("phone_col")
	if phoneColStr == "" {
		return nil, errors.New("phone column is required")
	}
	phoneCol, err := strconv.Atoi(phoneColStr)
	if err != nil || phoneCol < 0 {
		return nil, errors.New("invalid phone column index")
	}

	csvData := r.FormValue("csv_data")
	rawBytes, err := base64.StdEncoding.DecodeString(csvData)
	if err != nil || len(rawBytes) == 0 {
		return nil, errors.New("missing or invalid csv_data")
	}
	var records [][]string
	if err := json.Unmarshal(rawBytes, &records); err != nil || len(records) < 2 {
		return nil, errors.New("corrupted csv_data")
	}

	parseOptCol := func(key string) int {
		v := r.FormValue(key)
		if v == "" {
			return -1
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return -1
		}
		return n
	}

	var tags []string
	if t := strings.TrimSpace(r.FormValue("tags")); t != "" {
		for _, tag := range strings.Split(t, ",") {
			if tag = strings.TrimSpace(tag); tag != "" {
				tags = append(tags, tag)
			}
		}
	}

	return &importMapping{
		PhoneCol:    phoneCol,
		NameCol:     parseOptCol("name_col"),
		EmailCol:    parseOptCol("email_col"),
		CityCol:     parseOptCol("city_col"),
		TagsCol:     parseOptCol("tags_col"),
		Tags:        tags,
		MarkOptedIn: r.FormValue("mark_opted_in") == "on" || r.FormValue("mark_opted_in") == "true",
		CSVData:     csvData,
		Records:     records,
	}, nil
}

// splitTagCell splits a CSV tags cell on commas/semicolons, trimming each tag.
func splitTagCell(cell string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(cell, func(r rune) bool { return r == ',' || r == ';' }) {
		if t := strings.TrimSpace(part); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// ImportPreview shows up to 5 sample rows with the chosen mapping applied.
func (h *ContactsHandler) ImportPreview(w http.ResponseWriter, r *http.Request) {
	m, err := parseImportMapping(r)
	if err != nil {
		importErr(w, "Mapping error: "+err.Error())
		return
	}

	dataRows := m.Records[1:]
	if len(dataRows) > 5 {
		dataRows = dataRows[:5]
	}
	var rows []templates.ImportPreviewRow
	for _, rec := range dataRows {
		pr := templates.ImportPreviewRow{}
		if m.PhoneCol < len(rec) {
			pr.Phone = rec[m.PhoneCol]
		}
		if m.NameCol >= 0 && m.NameCol < len(rec) {
			pr.Name = rec[m.NameCol]
		}
		if m.EmailCol >= 0 && m.EmailCol < len(rec) {
			pr.Email = rec[m.EmailCol]
		}
		if p, err := db.NormalizePhone(pr.Phone); err != nil {
			pr.Error = err.Error()
		} else {
			pr.Phone = p
			pr.Valid = true
		}
		rows = append(rows, pr)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.ImportPreview(rows, len(m.Records)-1, m.CSVData,
		m.PhoneCol, m.NameCol, m.EmailCol, m.CityCol, m.TagsCol,
		strings.Join(m.Tags, ","), m.MarkOptedIn).Render(r.Context(), w)
}

// ImportConfirm performs the actual bulk insert.
func (h *ContactsHandler) ImportConfirm(w http.ResponseWriter, r *http.Request) {
	m, err := parseImportMapping(r)
	if err != nil {
		importErr(w, "Import error: "+err.Error())
		return
	}

	src := "csv_import"
	var contacts []db.Contact
	var phones []string
	var invalidCount int
	tagsByPhone := map[string][]string{} // per-row tags from the mapped Tags column

	for _, rec := range m.Records[1:] {
		rawPhone := ""
		if m.PhoneCol < len(rec) {
			rawPhone = rec[m.PhoneCol]
		}
		phone, err := db.NormalizePhone(rawPhone)
		if err != nil {
			invalidCount++
			continue
		}
		c := db.Contact{
			WAPhone:      phone,
			OptedIn:      m.MarkOptedIn,
			OptInSource:  &src,
			CustomFields: map[string]any{},
		}
		if m.NameCol >= 0 && m.NameCol < len(rec) {
			c.Name = rec[m.NameCol]
		}
		if m.EmailCol >= 0 && m.EmailCol < len(rec) {
			if email := rec[m.EmailCol]; email != "" {
				c.Email = &email
			}
		}
		if m.CityCol >= 0 && m.CityCol < len(rec) {
			if city := strings.TrimSpace(rec[m.CityCol]); city != "" {
				c.CustomFields["city"] = city
			}
		}
		if m.TagsCol >= 0 && m.TagsCol < len(rec) {
			if rt := splitTagCell(rec[m.TagsCol]); len(rt) > 0 {
				tagsByPhone[phone] = rt
			}
		}
		contacts = append(contacts, c)
		phones = append(phones, phone)
	}

	inserted, skipped, err := db.BulkInsertContacts(r.Context(), h.pool, contacts)
	if err != nil {
		log.Printf("bulk insert contacts: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	// Apply tags: batch tags to every imported contact, plus any per-row tags from
	// the mapped Tags column. Tag names not seen before are created automatically.
	if (len(m.Tags) > 0 || len(tagsByPhone) > 0) && len(phones) > 0 {
		idByPhone, _ := db.GetContactIDMapByPhones(r.Context(), h.pool, phones)
		if len(idByPhone) > 0 {
			allTags, _ := db.ListTags(r.Context(), h.pool)
			tagIDByName := map[string]int64{}
			for _, t := range allTags {
				tagIDByName[strings.ToLower(t.Name)] = t.ID
			}
			ensureTag := func(name string) (int64, bool) {
				key := strings.ToLower(strings.TrimSpace(name))
				if key == "" {
					return 0, false
				}
				if id, ok := tagIDByName[key]; ok {
					return id, true
				}
				t, err := db.CreateTag(r.Context(), h.pool, strings.TrimSpace(name), "")
				if err != nil {
					return 0, false
				}
				tagIDByName[key] = t.ID
				return t.ID, true
			}

			// tagID → set of contact IDs to receive it.
			apply := map[int64]map[string]bool{}
			addAssign := func(tagID int64, contactID string) {
				if apply[tagID] == nil {
					apply[tagID] = map[string]bool{}
				}
				apply[tagID][contactID] = true
			}

			for _, name := range m.Tags { // batch → every imported contact
				if id, ok := ensureTag(name); ok {
					for _, cid := range idByPhone {
						addAssign(id, cid)
					}
				}
			}
			for phone, names := range tagsByPhone { // per-row → that contact
				cid, ok := idByPhone[phone]
				if !ok {
					continue
				}
				for _, name := range names {
					if id, ok := ensureTag(name); ok {
						addAssign(id, cid)
					}
				}
			}

			for tagID, cidSet := range apply {
				cids := make([]string, 0, len(cidSet))
				for cid := range cidSet {
					cids = append(cids, cid)
				}
				_ = db.BulkAddTag(r.Context(), h.pool, cids, tagID)
			}
		}
	}

	log.Printf("csv import: inserted=%d skipped=%d invalid=%d tags_col=%d per_row_tagged=%d batch_tags=%d",
		inserted, skipped, invalidCount, m.TagsCol, len(tagsByPhone), len(m.Tags))

	w.Header().Set("HX-Trigger", "contactsUpdated")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.ImportResult(inserted, skipped, invalidCount).Render(r.Context(), w)
}

// ── Export CSV (admin only — DPDP data portability) ───────────────────────────

func (h *ContactsHandler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	actor := mw.AgentFromCtx(r.Context())
	if actor == nil || actor.Role != "admin" {
		http.Error(w, "admin only", http.StatusForbidden)
		return
	}

	contacts, _, err := db.ListContacts(r.Context(), h.pool, db.ListContactsFilter{Limit: 10000})
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="contacts_export.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "wa_phone", "name", "email", "opted_in", "opt_in_source", "opt_in_at", "opt_out_at", "created_at"})
	for _, c := range contacts {
		email := ""
		if c.Email != nil {
			email = *c.Email
		}
		src := ""
		if c.OptInSource != nil {
			src = *c.OptInSource
		}
		optInAt := ""
		if c.OptInAt != nil {
			optInAt = c.OptInAt.Format("2006-01-02T15:04:05Z")
		}
		optOutAt := ""
		if c.OptOutAt != nil {
			optOutAt = c.OptOutAt.Format("2006-01-02T15:04:05Z")
		}
		_ = cw.Write([]string{
			c.ID, c.WAPhone, c.Name, email,
			strconv.FormatBool(c.OptedIn), src, optInAt, optOutAt,
			c.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	cw.Flush()
}
