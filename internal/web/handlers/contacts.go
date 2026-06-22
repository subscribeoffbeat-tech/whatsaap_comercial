package handlers

import (
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
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
	if tagStr := q.Get("tag"); tagStr != "" {
		if id, err := strconv.ParseInt(tagStr, 10, 64); err == nil {
			f.TagID = &id
		}
	}
	switch q.Get("opted_in_filter") {
	case "true":
		t := true; f.OptedIn = &t
	case "false":
		fa := false; f.OptedIn = &fa
	}
	f.Industry = q.Get("industry_filter")

	contacts, total, err := db.ListContacts(r.Context(), h.pool, f)
	if err != nil {
		log.Printf("list contacts: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	searchActive := q.Get("search") != "" || q.Get("tag") != "" || q.Get("opted_in_filter") != "" || q.Get("industry_filter") != ""
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

func (h *ContactsHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	c, err := db.GetContact(r.Context(), h.pool, id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
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
	fmt.Fprint(w, "name,phone,email,city,tags,consent\nPriya Sharma,+919876543210,priya@mail.com,Mumbai,VIP,yes\nRaj Patel,+918765432109,,Delhi,Lead,yes\n")
}

const maxUploadBytes = 5 << 20 // 5 MB

// ImportUpload parses the uploaded CSV and returns the column-mapping UI.
func (h *ContactsHandler) ImportUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		http.Error(w, "file too large (max 5 MB)", http.StatusBadRequest)
		return
	}
	f, _, err := r.FormFile("csv_file")
	if err != nil {
		http.Error(w, "csv_file required", http.StatusBadRequest)
		return
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.TrimLeadingSpace = true
	records, err := reader.ReadAll()
	if err != nil || len(records) < 2 {
		http.Error(w, "CSV must have a header row and at least one data row", http.StatusBadRequest)
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
		Tags:        tags,
		MarkOptedIn: r.FormValue("mark_opted_in") == "on" || r.FormValue("mark_opted_in") == "true",
		CSVData:     csvData,
		Records:     records,
	}, nil
}

// ImportPreview shows up to 5 sample rows with the chosen mapping applied.
func (h *ContactsHandler) ImportPreview(w http.ResponseWriter, r *http.Request) {
	m, err := parseImportMapping(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		m.PhoneCol, m.NameCol, m.EmailCol,
		strings.Join(m.Tags, ","), m.MarkOptedIn).Render(r.Context(), w)
}

// ImportConfirm performs the actual bulk insert.
func (h *ContactsHandler) ImportConfirm(w http.ResponseWriter, r *http.Request) {
	m, err := parseImportMapping(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	src := "csv_import"
	var contacts []db.Contact
	var phones []string
	var invalidCount int

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
		contacts = append(contacts, c)
		phones = append(phones, phone)
	}

	inserted, skipped, err := db.BulkInsertContacts(r.Context(), h.pool, contacts)
	if err != nil {
		log.Printf("bulk insert contacts: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	// Apply tags to the imported contacts (by phone lookup).
	if len(m.Tags) > 0 && len(phones) > 0 {
		contactIDs, err := db.GetContactIDsByPhones(r.Context(), h.pool, phones)
		if err == nil && len(contactIDs) > 0 {
			allTags, _ := db.ListTags(r.Context(), h.pool)
			tagMap := map[string]int64{}
			for _, t := range allTags {
				tagMap[strings.ToLower(t.Name)] = t.ID
			}
			for _, tagName := range m.Tags {
				tagID, ok := tagMap[strings.ToLower(tagName)]
				if !ok {
					if t, err := db.CreateTag(r.Context(), h.pool, tagName, ""); err == nil {
						tagID = t.ID
						ok = true
					}
				}
				if ok {
					_ = db.BulkAddTag(r.Context(), h.pool, contactIDs, tagID)
				}
			}
		}
	}

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
