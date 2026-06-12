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
	r.Post("/", h.Create)
	r.Get("/{id}", h.EditForm)
	r.Put("/{id}", h.Update)
	r.Delete("/{id}", h.Delete)
	r.Post("/{id}/submit", h.Submit)
}

// ── Page ─────────────────────────────────────────────────────────────────────

func (h *TemplatesHandler) Page(w http.ResponseWriter, r *http.Request) {
	agent := mw.AgentFromCtx(r.Context())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.TemplatesPage(agent).Render(r.Context(), w); err != nil {
		log.Printf("templates page render: %v", err)
	}
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

// ── Create ────────────────────────────────────────────────────────────────────

func (h *TemplatesHandler) Create(w http.ResponseWriter, r *http.Request) {
	t, fallbacks, err := decodeTemplateRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if msgs := validateTemplateVars(t, fallbacks); len(msgs) > 0 {
		http.Error(w, strings.Join(msgs, "; "), http.StatusUnprocessableEntity)
		return
	}
	if err := db.CreateTemplate(r.Context(), h.pool, t); err != nil {
		log.Printf("create template: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("HX-Trigger", "templatesUpdated")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"id": t.ID})
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
		http.Error(w, strings.Join(msgs, "; "), http.StatusUnprocessableEntity)
		return
	}
	if err := db.UpdateTemplate(r.Context(), h.pool, t); err != nil {
		log.Printf("update template: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("HX-Trigger", "templatesUpdated")
	w.WriteHeader(http.StatusNoContent)
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
	w.WriteHeader(http.StatusOK)
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
