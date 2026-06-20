package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"whatsapptool/internal/db"
	mw "whatsapptool/internal/web/middleware"
	"whatsapptool/internal/web/templates"
)

// AutomationHandler manages /automation routes.
type AutomationHandler struct {
	pool *pgxpool.Pool
}

func NewAutomationHandler(pool *pgxpool.Pool) *AutomationHandler {
	return &AutomationHandler{pool: pool}
}

func (h *AutomationHandler) Mount(r chi.Router) {
	r.Get("/", h.List)
	r.Get("/new", h.NewForm)
	r.Post("/", h.Create)
	r.Get("/{id}/edit", h.EditForm)
	r.Post("/{id}", h.Update)
	r.Post("/{id}/toggle", h.Toggle)
	r.Delete("/{id}", h.Delete)
}

func (h *AutomationHandler) List(w http.ResponseWriter, r *http.Request) {
	rules, err := db.ListRules(r.Context(), h.pool)
	if err != nil {
		http.Error(w, "load rules: "+err.Error(), http.StatusInternalServerError)
		return
	}
	tmpls, _ := db.ListTemplates(r.Context(), h.pool)
	agent := mw.AgentFromCtx(r.Context())
	flash := r.URL.Query().Get("flash")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.AutomationPage(agent, rules, tmpls, flash).Render(r.Context(), w)
}

func (h *AutomationHandler) NewForm(w http.ResponseWriter, r *http.Request) {
	tmpls, _ := db.ListTemplates(r.Context(), h.pool)
	agent := mw.AgentFromCtx(r.Context())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.AutomationForm(agent, nil, tmpls, "").Render(r.Context(), w)
}

func (h *AutomationHandler) EditForm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	rule, err := db.GetRule(r.Context(), h.pool, id)
	if err != nil {
		http.Error(w, "rule not found", http.StatusNotFound)
		return
	}
	if rule.IsSystem() {
		http.Error(w, "system rules cannot be edited", http.StatusForbidden)
		return
	}
	tmpls, _ := db.ListTemplates(r.Context(), h.pool)
	agent := mw.AgentFromCtx(r.Context())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.AutomationForm(agent, rule, tmpls, "").Render(r.Context(), w)
}

func (h *AutomationHandler) Create(w http.ResponseWriter, r *http.Request) {
	rule, err := parseRuleForm(r)
	if err != nil {
		tmpls, _ := db.ListTemplates(r.Context(), h.pool)
		agent := mw.AgentFromCtx(r.Context())
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnprocessableEntity)
		templates.AutomationForm(agent, nil, tmpls, err.Error()).Render(r.Context(), w)
		return
	}
	if err := db.CreateRule(r.Context(), h.pool, rule); err != nil {
		log.Printf("create rule: %v", err)
		http.Error(w, "create failed", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/automation?flash=Rule+created.", http.StatusSeeOther)
}

func (h *AutomationHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	rule, err := parseRuleForm(r)
	if err != nil {
		dbRule, _ := db.GetRule(r.Context(), h.pool, id)
		tmpls, _ := db.ListTemplates(r.Context(), h.pool)
		agent := mw.AgentFromCtx(r.Context())
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnprocessableEntity)
		templates.AutomationForm(agent, dbRule, tmpls, err.Error()).Render(r.Context(), w)
		return
	}
	rule.ID = id
	if err := db.UpdateRule(r.Context(), h.pool, rule); err != nil {
		log.Printf("update rule %d: %v", id, err)
		http.Error(w, "update failed", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/automation?flash=Rule+saved.", http.StatusSeeOther)
}

func (h *AutomationHandler) Toggle(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	// Load the current rule to flip its state.
	rule, err := db.GetRule(r.Context(), h.pool, id)
	if err != nil {
		http.Error(w, "rule not found", http.StatusNotFound)
		return
	}
	if err := db.ToggleRule(r.Context(), h.pool, id, !rule.Active); err != nil {
		log.Printf("toggle rule %d: %v", id, err)
		http.Error(w, "toggle failed", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/automation?flash=Rule+updated.", http.StatusSeeOther)
}

func (h *AutomationHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := db.DeleteRule(r.Context(), h.pool, id); err != nil {
		log.Printf("delete rule %d: %v", id, err)
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, templates.ToastFragment(templates.ToastSuccess, "Rule deleted.", "", ""))
}

// parseRuleForm reads an automation rule from the request form.
func parseRuleForm(r *http.Request) (*db.AutomationRule, error) {
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	rule := &db.AutomationRule{
		Name:         r.FormValue("name"),
		TriggerType:  r.FormValue("trigger_type"),
		KeywordMatch: r.FormValue("keyword_match"),
		Active:       r.FormValue("active") == "on" || r.FormValue("active") == "true",
	}
	if rule.KeywordMatch == "" {
		rule.KeywordMatch = "exact"
	}
	if p, err := strconv.Atoi(r.FormValue("priority")); err == nil {
		rule.Priority = p
	}
	if kw := r.FormValue("keyword"); kw != "" {
		rule.Keyword = &kw
	}
	if rt := r.FormValue("response_text"); rt != "" {
		rule.ResponseText = &rt
	}
	if tid := r.FormValue("template_id"); tid != "" {
		rule.TemplateID = &tid
	}
	return rule, nil
}
