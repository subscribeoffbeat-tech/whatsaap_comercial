package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"whatsapptool/internal/db"
	mw "whatsapptool/internal/web/middleware"
	"whatsapptool/internal/web/templates"
	"whatsapptool/internal/whatsapp"
)

// TemplatesHandler handles all /templates routes.
type TemplatesHandler struct {
	pool     *pgxpool.Pool
	waClient *whatsapp.Client
}

func NewTemplatesHandler(pool *pgxpool.Pool, waClient *whatsapp.Client) *TemplatesHandler {
	return &TemplatesHandler{pool: pool, waClient: waClient}
}

// Mount registers all templates routes.
func (h *TemplatesHandler) Mount(r chi.Router) {
	r.Get("/", h.Page)
	r.Get("/gallery", h.Gallery)
	r.Get("/library", h.Library)
	r.Get("/new", h.NewForm)
	r.Post("/new", h.CreateFromForm)
	r.Post("/", h.Create)
	r.Get("/{id}", h.EditForm)
	r.Put("/{id}", h.Update)
	r.Delete("/{id}", h.Delete)
	r.Post("/{id}/submit", h.Submit)
}

// ── Page ─────────────────────────────────────────────────────────────────────

func (h *TemplatesHandler) Page(w http.ResponseWriter, r *http.Request) {
	agent := mw.AgentFromCtx(r.Context())
	flash := r.URL.Query().Get("flash")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.TemplatesPage(agent, flash).Render(r.Context(), w); err != nil {
		log.Printf("templates page render: %v", err)
	}
}

// ── New form (full page) ──────────────────────────────────────────────────────

func (h *TemplatesHandler) NewForm(w http.ResponseWriter, r *http.Request) {
	agent := mw.AgentFromCtx(r.Context())
	name := r.URL.Query().Get("name")
	category := r.URL.Query().Get("category")
	language := r.URL.Query().Get("language")
	body := r.URL.Query().Get("body")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.TemplateNewPage(agent, name, category, language, body, "").Render(r.Context(), w); err != nil {
		log.Printf("template new page render: %v", err)
	}
}

func (h *TemplatesHandler) CreateFromForm(w http.ResponseWriter, r *http.Request) {
	// Accept both multipart (file upload) and URL-encoded (no file) forms.
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		_ = r.ParseForm()
	}

	rawName := strings.TrimSpace(r.FormValue("name"))
	category := r.FormValue("category")
	language := r.FormValue("language")
	body := strings.TrimSpace(r.FormValue("body"))
	footer := strings.TrimSpace(r.FormValue("footer"))
	headerType := r.FormValue("header_type")
	headerText := strings.TrimSpace(r.FormValue("header_text"))
	action := r.FormValue("action")

	slug := tmplSlugify(rawName)

	agent := mw.AgentFromCtx(r.Context())

	renderErr := func(msg string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = templates.TemplateNewPage(agent, rawName, category, language, body, msg).Render(r.Context(), w)
	}

	if slug == "" {
		renderErr("Template name is required.")
		return
	}
	if body == "" {
		renderErr("Body text is required.")
		return
	}
	switch category {
	case "marketing", "utility", "authentication":
	default:
		category = "marketing"
	}
	if language == "" {
		language = "en"
	}

	var components []map[string]any
	switch headerType {
	case "text":
		if headerText != "" {
			components = append(components, map[string]any{"type": "HEADER", "format": "TEXT", "text": headerText})
		}
	case "image", "video", "document":
		format := strings.ToUpper(headerType)
		hdrComp := map[string]any{"type": "HEADER", "format": format}
		f, fh, ferr := r.FormFile("header_file")
		if ferr != nil {
			renderErr("Please upload a " + headerType + " for the header, or set the header to None or Text.")
			return
		}
		data, rerr := io.ReadAll(f)
		f.Close()
		if rerr != nil || len(data) == 0 {
			renderErr("Could not read the uploaded header file. Please try again.")
			return
		}
		// Template header media must go through Meta's Resumable Upload API, which
		// returns a header_handle (a Cloud API media ID is not accepted here).
		// Detect a real MIME type — browsers/drag-drop sometimes omit it, and
		// Meta rejects application/octet-stream.
		mimeType := detectHeaderMime(fh, data)
		handle, uerr := h.waClient.UploadResumable(r.Context(), fh.Filename, mimeType, data)
		if uerr != nil {
			log.Printf("template header resumable upload: %v", uerr)
			renderErr("Header upload to Meta failed: " + uerr.Error())
			return
		}
		hdrComp["example"] = map[string]any{"header_handle": []string{handle}}
		components = append(components, hdrComp)
	}

	bodyExample, missingEx := buildBodyExample(body, r.FormValue)
	if action == "submit" && len(missingEx) > 0 {
		renderErr(fmt.Sprintf("Provide an example value for variable %s. Meta requires a sample for every {{N}} placeholder.",
			joinVars(missingEx)))
		return
	}
	bodyComp := map[string]any{"type": "BODY", "text": body}
	if bodyExample != nil {
		bodyComp["example"] = bodyExample
	}
	components = append(components, bodyComp)
	if footer != "" {
		components = append(components, map[string]any{"type": "FOOTER", "text": footer})
	}

	var btns []map[string]any
	for i := 0; i < 3; i++ {
		btype := r.FormValue(fmt.Sprintf("btn_type_%d", i))
		blabel := strings.TrimSpace(r.FormValue(fmt.Sprintf("btn_label_%d", i)))
		if blabel == "" {
			continue
		}
		if btype == "" {
			btype = "QUICK_REPLY"
		}
		btn := map[string]any{"type": btype, "text": blabel}
		if btype == "URL" {
			u := strings.TrimSpace(r.FormValue(fmt.Sprintf("btn_url_%d", i)))
			if u == "" {
				renderErr(fmt.Sprintf("The \"%s\" button is a website button but has no URL. Enter the link (e.g. https://www.example.com) or remove the button.", blabel))
				return
			}
			if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
				u = "https://" + u
			}
			btn["url"] = u
		} else if btype == "PHONE_NUMBER" {
			p := strings.TrimSpace(r.FormValue(fmt.Sprintf("btn_phone_%d", i)))
			if p == "" {
				renderErr(fmt.Sprintf("The \"%s\" button is a call button but has no phone number. Enter the number or remove the button.", blabel))
				return
			}
			btn["phone_number"] = p
		}
		btns = append(btns, btn)
	}
	if len(btns) > 0 {
		components = append(components, map[string]any{"type": "BUTTONS", "buttons": btns})
	}

	agentID := ""
	if agent != nil {
		agentID = agent.ID
	}
	t := &db.Template{
		Name:       slug,
		Language:   language,
		Category:   category,
		Components: components,
		CreatedBy:  &agentID,
	}

	if err := db.CreateTemplate(r.Context(), h.pool, t); err != nil {
		log.Printf("create template from form: %v", err)
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			renderErr(fmt.Sprintf("A template named %q already exists in this language.", slug))
		} else {
			renderErr("Failed to save template. Please try again.")
		}
		return
	}

	if action == "submit" {
		if msg := validateComponentsForMeta(t.Components); msg != "" {
			renderErr(msg)
			return
		}
		req := whatsapp.SubmitTemplateRequest{
			Name:       t.Name,
			Language:   t.Language,
			Category:   strings.ToUpper(t.Category),
			Components: t.Components,
		}
		waID, err := h.waClient.SubmitTemplate(r.Context(), req)
		if err != nil {
			log.Printf("submit template to Meta: %v", err)
			http.Redirect(w, r, "/templates?flash=Template+saved+as+draft.+Meta+submission+failed.", http.StatusSeeOther)
			return
		}
		if err := db.SetTemplateWAID(r.Context(), h.pool, t.ID, waID); err != nil {
			log.Printf("set template wa_id: %v", err)
		}
		http.Redirect(w, r, "/templates?flash=Template+submitted+for+Meta+approval.", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/templates?flash=Template+saved+as+draft.", http.StatusSeeOther)
}

// buildBodyExample collects example values for each {{N}} variable in body from
// the form (fields named var_example_N), ordered by variable number. Meta
// requires a sample value for every variable or it rejects with INVALID_FORMAT.
// Returns the example object to attach to the BODY component (nil if the body
// has no variables) and the list of variable indices missing an example value.
func buildBodyExample(body string, form func(string) string) (map[string]any, []string) {
	vars := whatsapp.ExtractVariables(body)
	if len(vars) == 0 {
		return nil, nil
	}
	sort.Slice(vars, func(i, j int) bool {
		a, _ := strconv.Atoi(vars[i])
		b, _ := strconv.Atoi(vars[j])
		return a < b
	})
	var vals, missing []string
	for _, v := range vars {
		val := strings.TrimSpace(form("var_example_" + v))
		if val == "" {
			missing = append(missing, v)
		}
		vals = append(vals, val)
	}
	return map[string]any{"body_text": [][]string{vals}}, missing
}

// detectHeaderMime determines a Meta-acceptable MIME type for an uploaded
// header file. Browsers and drag-and-drop sometimes omit the part Content-Type
// (arriving as application/octet-stream), which Meta rejects — so fall back to
// the file extension, then content sniffing.
func detectHeaderMime(fh *multipart.FileHeader, data []byte) string {
	if ct := fh.Header.Get("Content-Type"); ct != "" && ct != "application/octet-stream" {
		return ct
	}
	switch strings.ToLower(filepath.Ext(fh.Filename)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".mp4":
		return "video/mp4"
	case ".3gp", ".3gpp":
		return "video/3gpp"
	case ".pdf":
		return "application/pdf"
	case ".doc":
		return "application/msword"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".ppt":
		return "application/vnd.ms-powerpoint"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".xls":
		return "application/vnd.ms-excel"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".txt":
		return "text/plain"
	}
	if ct := http.DetectContentType(data); ct != "" && ct != "application/octet-stream" {
		// http.DetectContentType may append "; charset=..."; strip it.
		if i := strings.IndexByte(ct, ';'); i >= 0 {
			ct = ct[:i]
		}
		return ct
	}
	return "application/octet-stream"
}

// validateComponentsForMeta catches the most common reasons Meta rejects a
// template at submit time, returning a clear message (empty = looks OK). This
// runs before the API call so users see plain guidance, not Meta's jargon.
func validateComponentsForMeta(components []map[string]any) string {
	for _, c := range components {
		if !strings.EqualFold(fmt.Sprint(c["type"]), "HEADER") {
			continue
		}
		format := strings.ToUpper(fmt.Sprint(c["format"]))
		switch format {
		case "IMAGE", "VIDEO", "DOCUMENT":
			if !headerHasHandle(c) {
				kind := strings.ToLower(format)
				return fmt.Sprintf("This template has a %s header but no sample %s was uploaded. Open Edit, switch the header to None or Text, or re-create the template and upload a header %s.", kind, kind, kind)
			}
		case "TEXT":
			text, _ := c["text"].(string)
			if len(whatsapp.ExtractVariables(text)) > 0 && !headerHasText(c) {
				return "The header text has a variable but no example value. Remove the variable from the header or add a header example."
			}
		}
	}
	return ""
}

// headerHasHandle reports whether a media HEADER carries an example media handle.
func headerHasHandle(c map[string]any) bool {
	ex, ok := c["example"].(map[string]any)
	if !ok {
		return false
	}
	switch v := ex["header_handle"].(type) {
	case []any:
		return len(v) > 0
	case []string:
		return len(v) > 0
	}
	return false
}

// existingHeaderHandle returns the uploaded media handle from a stored HEADER
// component, or "" if none — used to keep a header image when editing without
// re-uploading.
func existingHeaderHandle(components []map[string]any) string {
	for _, c := range components {
		if !strings.EqualFold(fmt.Sprint(c["type"]), "HEADER") {
			continue
		}
		ex, ok := c["example"].(map[string]any)
		if !ok {
			continue
		}
		switch v := ex["header_handle"].(type) {
		case []any:
			if len(v) > 0 {
				if s, ok := v[0].(string); ok {
					return s
				}
			}
		case []string:
			if len(v) > 0 {
				return v[0]
			}
		}
	}
	return ""
}

// headerHasText reports whether a TEXT HEADER carries an example header value.
func headerHasText(c map[string]any) bool {
	ex, ok := c["example"].(map[string]any)
	if !ok {
		return false
	}
	switch v := ex["header_text"].(type) {
	case []any:
		return len(v) > 0
	case []string:
		return len(v) > 0
	}
	return false
}

// joinVars formats variable indices for display, e.g. ["1","2"] -> "{{1}}, {{2}}".
func joinVars(vars []string) string {
	parts := make([]string, len(vars))
	for i, v := range vars {
		parts[i] = "{{" + v + "}}"
	}
	return strings.Join(parts, ", ")
}

func tmplSlugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	prevUnderscore := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevUnderscore = false
		} else if !prevUnderscore && b.Len() > 0 {
			b.WriteRune('_')
			prevUnderscore = true
		}
	}
	return strings.TrimRight(b.String(), "_")
}

// ── Gallery partial ───────────────────────────────────────────────────────────

func (h *TemplatesHandler) Gallery(w http.ResponseWriter, r *http.Request) {
	// Reconcile local statuses with Meta so approvals/rejections show even if a
	// webhook was missed. Best-effort with a short timeout — never block the page.
	h.syncStatusesFromMeta(r.Context())

	tmplList, err := db.ListTemplates(r.Context(), h.pool)
	if err != nil {
		log.Printf("list templates: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.TemplateGallery(tmplList).Render(r.Context(), w); err != nil {
		log.Printf("template gallery render: %v", err)
	}
}

// syncStatusesFromMeta pulls live template statuses from Meta and reconciles the
// local DB. Best-effort: a short timeout and any error is logged, never fatal.
func (h *TemplatesHandler) syncStatusesFromMeta(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	metaList, err := h.waClient.ListTemplates(ctx)
	if err != nil {
		log.Printf("template status sync: %v", err)
		return
	}
	for _, mt := range metaList {
		status := mapMetaStatus(mt.Status)
		if status == "" {
			continue
		}
		reason := mt.RejectedReason
		if strings.EqualFold(reason, "NONE") {
			reason = ""
		}
		if err := db.SyncTemplateStatusByName(ctx, h.pool, mt.Name, status, reason); err != nil {
			log.Printf("template status sync %s: %v", mt.Name, err)
		}
	}
}

// mapMetaStatus maps Meta's template status enum to our local status values.
// Returns "" for genuinely unknown events so callers can skip the update.
func mapMetaStatus(s string) string {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "APPROVED", "REINSTATED":
		return "approved"
	case "REJECTED", "DISABLED":
		return "rejected"
	case "PENDING", "IN_APPEAL", "PENDING_DELETION":
		return "pending"
	case "PAUSED", "FLAGGED":
		return "paused"
	}
	return ""
}

// ── Library partial ───────────────────────────────────────────────────────────

func (h *TemplatesHandler) Library(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.TemplateLibrary().Render(r.Context(), w); err != nil {
		log.Printf("template library render: %v", err)
	}
}

// ── Create ────────────────────────────────────────────────────────────────────

func (h *TemplatesHandler) Create(w http.ResponseWriter, r *http.Request) {
	t, fallbacks, err := decodeTemplateRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if msgs := validateTemplateVars(t, fallbacks); len(msgs) > 0 {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnprocessableEntity)
		fmt.Fprint(w, templates.FormBanner(strings.Join(msgs, "; ")))
		return
	}
	if err := db.CreateTemplate(r.Context(), h.pool, t); err != nil {
		log.Printf("create template: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("HX-Trigger", "templatesUpdated")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, templates.ToastFragment(templates.ToastSuccess, "Template saved.", "", ""))
}

// ── Edit form ─────────────────────────────────────────────────────────────────

func (h *TemplatesHandler) EditForm(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	t, err := db.GetTemplate(r.Context(), h.pool, id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.TemplateEditorForm(t).Render(r.Context(), w); err != nil {
		log.Printf("template editor render: %v", err)
	}
}

// ── Update ────────────────────────────────────────────────────────────────────

func (h *TemplatesHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	// Accept multipart (header file upload) or URL-encoded forms.
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		_ = r.ParseForm()
	}

	rawName := strings.TrimSpace(r.FormValue("name"))
	category := r.FormValue("category")
	language := r.FormValue("language")
	body := strings.TrimSpace(r.FormValue("body"))
	footer := strings.TrimSpace(r.FormValue("footer"))
	headerText := strings.TrimSpace(r.FormValue("header_text"))
	headerType := r.FormValue("header_type")

	slug := tmplSlugify(rawName)
	if slug == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, templates.FormBanner("Template name is required."))
		return
	}
	if body == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, templates.FormBanner("Body text is required."))
		return
	}
	switch category {
	case "marketing", "utility", "authentication":
	default:
		category = "marketing"
	}
	if language == "" {
		language = "en"
	}

	// Existing template: reuse an already-uploaded header handle and preserve
	// buttons (the editor doesn't edit buttons).
	existingTmpl, _ := db.GetTemplate(r.Context(), h.pool, id)

	bodyExample, missingEx := buildBodyExample(body, r.FormValue)
	if r.FormValue("action") == "resubmit" && len(missingEx) > 0 {
		editErr(w, fmt.Sprintf("Provide an example value for variable %s — Meta requires a sample for every {{N}} placeholder.",
			joinVars(missingEx)))
		return
	}

	// Backward-compat: older editor only sent header_text.
	if headerType == "" {
		if headerText != "" {
			headerType = "text"
		} else {
			headerType = "none"
		}
	}

	var components []map[string]any
	switch headerType {
	case "text":
		if headerText != "" {
			components = append(components, map[string]any{"type": "HEADER", "format": "TEXT", "text": headerText})
		}
	case "image", "video", "document":
		hdr := map[string]any{"type": "HEADER", "format": strings.ToUpper(headerType)}
		if f, fh, ferr := r.FormFile("header_file"); ferr == nil {
			data, rerr := io.ReadAll(f)
			f.Close()
			if rerr == nil && len(data) > 0 {
				handle, uerr := h.waClient.UploadResumable(r.Context(), fh.Filename, detectHeaderMime(fh, data), data)
				if uerr != nil {
					log.Printf("edit header upload: %v", uerr)
					editErr(w, "Header upload to Meta failed: "+uerr.Error())
					return
				}
				hdr["example"] = map[string]any{"header_handle": []string{handle}}
			}
		} else if existingTmpl != nil {
			// No new file — keep the existing uploaded handle if there is one.
			if handle := existingHeaderHandle(existingTmpl.Components); handle != "" {
				hdr["example"] = map[string]any{"header_handle": []string{handle}}
			}
		}
		components = append(components, hdr)
	}

	bodyComp := map[string]any{"type": "BODY", "text": body}
	if bodyExample != nil {
		bodyComp["example"] = bodyExample
	}
	components = append(components, bodyComp)
	if footer != "" {
		components = append(components, map[string]any{"type": "FOOTER", "text": footer})
	}
	// Preserve existing buttons (not edited in this form).
	if existingTmpl != nil {
		for _, c := range existingTmpl.Components {
			if strings.EqualFold(fmt.Sprint(c["type"]), "BUTTONS") {
				components = append(components, c)
			}
		}
	}

	t := &db.Template{
		ID:         id,
		Name:       slug,
		Language:   language,
		Category:   category,
		Components: components,
	}
	if err := db.UpdateTemplate(r.Context(), h.pool, t); err != nil {
		log.Printf("update template: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	// "Save & Resubmit to Meta" — push the edited content to Meta for review.
	if r.FormValue("action") == "resubmit" {
		if msg := validateComponentsForMeta(components); msg != "" {
			editErr(w, msg)
			return
		}
		existing, gerr := db.GetTemplate(r.Context(), h.pool, id)
		if gerr != nil {
			log.Printf("resubmit: reload template: %v", gerr)
			editErr(w, "Could not reload the template. Please try again.")
			return
		}
		metaCat := strings.ToUpper(category)
		if existing.WATemplateID != nil && *existing.WATemplateID != "" {
			// Template already exists in Meta — edit it (re-enters review).
			if err := h.waClient.EditTemplate(r.Context(), *existing.WATemplateID, metaCat, components); err != nil {
				log.Printf("resubmit (edit) template to Meta: %v", err)
				editErr(w, "Meta rejected the resubmission: "+err.Error())
				return
			}
			if err := db.SetTemplatePending(r.Context(), h.pool, id); err != nil {
				log.Printf("resubmit: set pending: %v", err)
			}
		} else {
			// Never submitted before — create it.
			waID, err := h.waClient.SubmitTemplate(r.Context(), whatsapp.SubmitTemplateRequest{
				Name:       slug,
				Language:   language,
				Category:   metaCat,
				Components: components,
			})
			if err != nil {
				log.Printf("resubmit (create) template to Meta: %v", err)
				editErr(w, "Meta rejected the submission: "+err.Error())
				return
			}
			if err := db.SetTemplateWAID(r.Context(), h.pool, id, waID); err != nil {
				log.Printf("resubmit: set wa_id: %v", err)
			}
		}
		w.Header().Set("HX-Trigger", "templatesUpdated")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, templates.ToastFragment(templates.ToastSuccess, "Resubmitted to Meta — awaiting review.", "", ""))
		return
	}

	w.Header().Set("HX-Trigger", "templatesUpdated")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, templates.ToastFragment(templates.ToastSuccess, "Template updated.", "", ""))
}

// editErr returns an error fragment as a 422 so the editor form's
// hx-on::response-error handler shows it in #tmpl-edit-errors without
// swapping away the form.
func editErr(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnprocessableEntity)
	fmt.Fprintf(w, `<div class="callout callout--danger" style="margin-bottom:8px"><strong>Could not resubmit:</strong> %s</div>`, html.EscapeString(msg))
}

// ── Delete ────────────────────────────────────────────────────────────────────

func (h *TemplatesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := db.DeleteTemplate(r.Context(), h.pool, id); err != nil {
		log.Printf("delete template: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("HX-Trigger", "templatesUpdated")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, templates.ToastFragment(templates.ToastSuccess, "Template deleted.", "", ""))
}

// ── Submit to Meta ─────────────────────────────────────────────────────────────

func (h *TemplatesHandler) Submit(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	t, err := db.GetTemplate(r.Context(), h.pool, id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Catch common problems before hitting Meta, so the user gets clear guidance.
	if msg := validateComponentsForMeta(t.Components); msg != "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<div class="callout callout--danger" style="margin-top:4px"><strong>Can't submit yet:</strong> %s</div>`, html.EscapeString(msg))
		return
	}

	req := whatsapp.SubmitTemplateRequest{
		Name:       t.Name,
		Language:   t.Language,
		Category:   strings.ToUpper(t.Category),
		Components: t.Components,
	}
	waID, err := h.waClient.SubmitTemplate(r.Context(), req)
	if err != nil {
		log.Printf("submit template to Meta: %v", err)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<div class="callout callout--danger" style="margin-top:4px"><strong>Meta rejected:</strong> %s</div>`, err.Error())
		return
	}
	if err := db.SetTemplateWAID(r.Context(), h.pool, id, waID); err != nil {
		log.Printf("set template wa_id: %v", err)
	}

	w.Header().Set("HX-Trigger", "templatesUpdated")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<div style="font-size:12px;color:var(--text-muted);padding:6px 0 2px">⏳ Submitted to Meta — awaiting review (usually 2–5 min)</div>`)
}

// ── helpers ───────────────────────────────────────────────────────────────────

// templateRequest is the JSON body for create/update.
type templateRequest struct {
	Name       string            `json:"name"`
	Language   string            `json:"language"`
	Category   string            `json:"category"`   // marketing|utility|authentication
	Components []map[string]any  `json:"components"` // Meta components array
	// Fallbacks maps {{N}} index → default value used when contact field is empty.
	// Required for every variable in the BODY component. Not sent to Meta.
	Fallbacks  map[string]string `json:"fallbacks"`
}

// decodeTemplateRequest parses and validates a create/update body.
// Returns the db.Template ready for storage and the fallbacks map.
func decodeTemplateRequest(r *http.Request) (*db.Template, map[string]string, error) {
	var req templateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if req.Name == "" {
		return nil, nil, fmt.Errorf("name is required")
	}
	if req.Language == "" {
		req.Language = "en"
	}
	switch req.Category {
	case "marketing", "utility", "authentication":
	default:
		return nil, nil, fmt.Errorf("category must be marketing, utility, or authentication")
	}
	return &db.Template{
		Name:       req.Name,
		Language:   req.Language,
		Category:   req.Category,
		Components: req.Components,
	}, req.Fallbacks, nil
}

// validateTemplateVars checks that every {{N}} variable in the BODY component
// has a corresponding entry in the fallbacks map. Returns error messages (empty
// slice = valid).
func validateTemplateVars(t *db.Template, fallbacks map[string]string) []string {
	var errs []string
	for _, comp := range t.Components {
		if comp["type"] == "BODY" {
			text, _ := comp["text"].(string)
			if missing := whatsapp.ValidateVariables(text, fallbacks); len(missing) > 0 {
				errs = append(errs, fmt.Sprintf("body variables %v have no fallback default", missing))
			}
		}
	}
	return errs
}
