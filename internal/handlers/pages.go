package handlers

import (
	"html/template"
	"net/http"
	"path/filepath"

	"kantext/internal/services"
)

// PageHandler handles HTML page rendering
type PageHandler struct {
	store     *services.TaskStore
	templates *template.Template
}

// NewPageHandler creates a new PageHandler
func NewPageHandler(store *services.TaskStore) (*PageHandler, error) {
	// Parse templates
	tmpl, err := template.ParseGlob(filepath.Join("web", "templates", "*.html"))
	if err != nil {
		return nil, err
	}

	return &PageHandler{
		store:     store,
		templates: tmpl,
	}, nil
}

// BoardData is the data passed to the board template
type BoardData struct {
	Title string
	Tasks interface{}
}

// ServeBoard renders the Kanban board
func (h *PageHandler) ServeBoard(w http.ResponseWriter, r *http.Request) {
	tasks := h.store.GetAll()

	data := BoardData{
		Title: "Kantext",
		Tasks: tasks,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.templates.ExecuteTemplate(w, "base.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
