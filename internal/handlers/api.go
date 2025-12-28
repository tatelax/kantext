package handlers

import (
	"encoding/json"
	"net/http"

	"kantext/internal/models"
	"kantext/internal/services"

	"github.com/go-chi/chi/v5"
)

// APIHandler handles REST API requests
type APIHandler struct {
	store  *services.TaskStore
	runner *services.TestRunner
}

// NewAPIHandler creates a new APIHandler
func NewAPIHandler(store *services.TaskStore, runner *services.TestRunner) *APIHandler {
	return &APIHandler{
		store:  store,
		runner: runner,
	}
}

// respondJSON writes a JSON response
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		json.NewEncoder(w).Encode(data)
	}
}

// respondError writes a JSON error response
func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{"error": message})
}

// ListTasks returns all tasks
func (h *APIHandler) ListTasks(w http.ResponseWriter, r *http.Request) {
	tasks := h.store.GetAll()
	respondJSON(w, http.StatusOK, tasks)
}

// GetTask returns a single task by ID
func (h *APIHandler) GetTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	task, err := h.store.Get(id)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, task)
}

// CreateTask creates a new task
func (h *APIHandler) CreateTask(w http.ResponseWriter, r *http.Request) {
	var req models.CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate required fields
	if req.Title == "" {
		respondError(w, http.StatusBadRequest, "Title is required")
		return
	}

	// Validate priority if provided
	if req.Priority != "" && req.Priority != models.PriorityHigh &&
		req.Priority != models.PriorityMedium && req.Priority != models.PriorityLow {
		respondError(w, http.StatusBadRequest, "Priority must be 'high', 'medium', or 'low'")
		return
	}

	task, err := h.store.Create(req)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, task)
}

// UpdateTask updates an existing task
func (h *APIHandler) UpdateTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req models.UpdateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	task, err := h.store.Update(id, req)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, task)
}

// DeleteTask deletes a task
func (h *APIHandler) DeleteTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.store.Delete(id); err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// RunTest executes the test associated with a task
func (h *APIHandler) RunTest(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	task, err := h.store.Get(id)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	// Check if task has a test associated
	if !task.HasTest() {
		respondError(w, http.StatusBadRequest, "Task does not have a test associated with it")
		return
	}

	// Mark as running
	h.store.SetTestRunning(id)

	// Run the test synchronously (for now)
	result := h.runner.Run(r.Context(), task.TestFile, task.TestFunc)

	// Update the task with the result
	updatedTask, err := h.store.UpdateTestResult(id, result)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Return both the task and result
	response := map[string]interface{}{
		"task":   updatedTask,
		"result": result,
	}

	respondJSON(w, http.StatusOK, response)
}

// GetTaskStatus returns the current status of a task (useful for polling)
func (h *APIHandler) GetTaskStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	task, err := h.store.Get(id)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	status := map[string]interface{}{
		"id":          task.ID,
		"test_status": task.TestStatus,
		"column":      task.Column,
		"last_output": task.LastOutput,
	}

	respondJSON(w, http.StatusOK, status)
}

// ListColumns returns all columns
func (h *APIHandler) ListColumns(w http.ResponseWriter, r *http.Request) {
	columns := h.store.GetColumns()
	respondJSON(w, http.StatusOK, columns)
}

// CreateColumn creates a new column
func (h *APIHandler) CreateColumn(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name == "" {
		respondError(w, http.StatusBadRequest, "Name is required")
		return
	}

	column, err := h.store.CreateColumn(req.Name)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, column)
}

// UpdateColumn renames a column
func (h *APIHandler) UpdateColumn(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name == "" {
		respondError(w, http.StatusBadRequest, "Name is required")
		return
	}

	column, err := h.store.UpdateColumn(slug, req.Name)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, column)
}

// DeleteColumn deletes an empty column
func (h *APIHandler) DeleteColumn(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	if err := h.store.DeleteColumn(slug); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ReorderColumns sets the order of columns
func (h *APIHandler) ReorderColumns(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Slugs []string `json:"slugs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if len(req.Slugs) == 0 {
		respondError(w, http.StatusBadRequest, "Slugs array is required")
		return
	}

	if err := h.store.ReorderColumns(req.Slugs); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	columns := h.store.GetColumns()
	respondJSON(w, http.StatusOK, columns)
}
