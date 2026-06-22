package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

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
		// Upload the file to Meta now so we can include the example handle.
		if f, fh, ferr := r.FormFile("header_file"); ferr == nil {
			defer f.Close()
			mimeType := fh.Header.Get("Content-Type")
			if mimeType == "" {
				mimeType = "application/octet-stream"
			}
			if mediaID, uerr := h.waClient.UploadMediaStream(r.Context(), f, fh.Filename, mimeType); uerr != nil {
				log.Printf("template header media upload: %v", uerr)
				// Continue without example — save what we can
			} else {
				hdrComp["example"] = map[string]any{"header_handle": []string{mediaID}}
			}
		}
		components = append(components, hdrComp)
	}

	components = append(components, map[string]any{"type": "BODY", "text": body})
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
			if u := strings.TrimSpace(r.FormValue(fmt.Sprintf("btn_url_%d", i))); u != "" {
				btn["url"] = u
			}
		} else if btype == "PHONE_NUMBER" {
			if p := strings.TrimSpace(r.FormValue(fmt.Sprintf("btn_phone_%d", i))); p != "" {
				btn["phone_number"] = p
			}
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
	t, fallbacks, err := decodeTemplateRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	t.ID = id
	if msgs := validateTemplateVars(t, fallbacks); len(msgs) > 0 {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnprocessableEntity)
		fmt.Fprint(w, templates.FormBanner(strings.Join(msgs, "; ")))
		return
	}
	if err := db.UpdateTemplate(r.Context(), h.pool, t); err != nil {
		log.Printf("update template: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("HX-Trigger", "templatesUpdated")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, templates.ToastFragment(templates.ToastSuccess, "Template updated.", "", ""))
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

	req := whatsapp.SubmitTemplateRequest{
		Name:       t.Name,
		Language:   t.Language,
		Category:   strings.ToUpper(t.Category),
		Components: t.Components,
	}
	waID, err := h.waClient.SubmitTemplate(r.Context(), req)
	if err != nil {
		log.Printf("submit template to Meta: %v", err)
		http.Error(w, fmt.Sprintf("Meta API error: %v", err), http.StatusBadGateway)
		return
	}
	if err := db.SetTemplateWAID(r.Context(), h.pool, id, waID); err != nil {
		log.Printf("set template wa_id: %v", err)
	}

	w.Header().Set("HX-Trigger", "templatesUpdated")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"wa_template_id": waID, "status": "pending"})
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
