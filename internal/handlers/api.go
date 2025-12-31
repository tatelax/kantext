package handlers

import (
	"encoding/json"
	"net/http"
	"sync"

	"kantext/internal/config"
	"kantext/internal/models"
	"kantext/internal/services"

	"github.com/go-chi/chi/v5"
)

// APIHandler handles REST API requests
type APIHandler struct {
	mu                 sync.RWMutex
	store              *services.TaskStore
	runner             *services.TestRunner
	staleThresholdDays int
	configPath         string
	config             *config.Config
}

// NewAPIHandler creates a new APIHandler
func NewAPIHandler(store *services.TaskStore, runner *services.TestRunner) *APIHandler {
	return &APIHandler{
		store:              store,
		runner:             runner,
		staleThresholdDays: 7, // default
	}
}

// SetStaleThresholdDays sets the stale threshold for tasks
func (h *APIHandler) SetStaleThresholdDays(days int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.staleThresholdDays = days
}

// SetConfigPath sets the path to the config file
func (h *APIHandler) SetConfigPath(path string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.configPath = path
}

// SetConfig sets the config reference
func (h *APIHandler) SetConfig(cfg *config.Config) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.config = cfg
}

// getConfig returns the current config (thread-safe read)
func (h *APIHandler) getConfig() *config.Config {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.config
}

// getConfigPath returns the current config path (thread-safe read)
func (h *APIHandler) getConfigPath() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.configPath
}

// getStaleThresholdDays returns the stale threshold (thread-safe read)
func (h *APIHandler) getStaleThresholdDays() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.staleThresholdDays
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

// RunTest executes all tests associated with a task
func (h *APIHandler) RunTest(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	task, err := h.store.Get(id)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	// Check if task has tests associated
	if !task.HasTest() {
		respondError(w, http.StatusBadRequest, "Task does not have any tests associated with it")
		return
	}

	// Mark as running
	h.store.SetTestRunning(id)

	// Run all tests synchronously
	results := h.runner.RunAll(r.Context(), task.Tests)

	// Update the task with the aggregated results
	updatedTask, err := h.store.UpdateTestResults(id, results)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Return both the task and results
	response := map[string]interface{}{
		"task":    updatedTask,
		"results": results,
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

// ReorderTask moves a task to a specific position within a column
func (h *APIHandler) ReorderTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		Column   string `json:"column"`
		Position int    `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Column == "" {
		respondError(w, http.StatusBadRequest, "Column is required")
		return
	}

	task, err := h.store.Reorder(id, models.Column(req.Column), req.Position)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, task)
}

// GetConfig returns client-side configuration settings
func (h *APIHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	configData := map[string]interface{}{
		"stale_threshold_days": h.staleThresholdDays,
	}

	// Include full config if available
	if h.config != nil {
		configData["working_directory"] = h.config.WorkingDirectory
		configData["tasks_file"] = h.config.TasksFileName
		if configData["tasks_file"] == "" {
			configData["tasks_file"] = config.DefaultTasksFileName
		}
		configData["test_runner"] = map[string]string{
			"command":         h.config.TestRunner.GetCommand(),
			"pass_string":     h.config.TestRunner.GetPassString(),
			"fail_string":     h.config.TestRunner.GetFailString(),
			"no_tests_string": h.config.TestRunner.GetNoTestsString(),
		}
	}

	respondJSON(w, http.StatusOK, configData)
}

// UpdateConfigRequest defines the structure for config update requests
type UpdateConfigRequest struct {
	TasksFile          *string                  `json:"tasks_file,omitempty"`
	StaleThresholdDays *int                     `json:"stale_threshold_days,omitempty"`
	TestRunner         *TestRunnerUpdateRequest `json:"test_runner,omitempty"`
}

// TestRunnerUpdateRequest defines test runner config updates
type TestRunnerUpdateRequest struct {
	Command       *string `json:"command,omitempty"`
	PassString    *string `json:"pass_string,omitempty"`
	FailString    *string `json:"fail_string,omitempty"`
	NoTestsString *string `json:"no_tests_string,omitempty"`
}

// UpdateConfig updates the application configuration
func (h *APIHandler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	var req UpdateConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate stale_threshold_days
	if req.StaleThresholdDays != nil && *req.StaleThresholdDays < 1 {
		respondError(w, http.StatusBadRequest, "stale_threshold_days must be at least 1")
		return
	}

	h.mu.Lock()
	if h.configPath == "" || h.config == nil {
		h.mu.Unlock()
		respondError(w, http.StatusBadRequest, "Configuration not available")
		return
	}

	// Update config in memory
	if req.TasksFile != nil {
		h.config.TasksFileName = *req.TasksFile
	}
	if req.StaleThresholdDays != nil {
		h.config.StaleThresholdDays = *req.StaleThresholdDays
		h.staleThresholdDays = *req.StaleThresholdDays
	}
	if req.TestRunner != nil {
		if req.TestRunner.Command != nil {
			h.config.TestRunner.Command = *req.TestRunner.Command
		}
		if req.TestRunner.PassString != nil {
			h.config.TestRunner.PassString = *req.TestRunner.PassString
		}
		if req.TestRunner.FailString != nil {
			h.config.TestRunner.FailString = *req.TestRunner.FailString
		}
		if req.TestRunner.NoTestsString != nil {
			h.config.TestRunner.NoTestsString = *req.TestRunner.NoTestsString
		}
	}

	// Save to file
	if err := h.config.Save(h.configPath); err != nil {
		h.mu.Unlock()
		respondError(w, http.StatusInternalServerError, "Failed to save config: "+err.Error())
		return
	}
	h.mu.Unlock()

	// Return updated config
	h.GetConfig(w, r)
}
