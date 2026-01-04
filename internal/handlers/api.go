package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"kantext/internal/models"
	"kantext/internal/services"

	"github.com/go-chi/chi/v5"
)

// APIHandler handles REST API requests
type APIHandler struct {
	store        *services.TaskStore
	runner       *services.TestRunner
	claudeRunner *services.ClaudeRunner
}

// NewAPIHandler creates a new APIHandler
func NewAPIHandler(store *services.TaskStore, runner *services.TestRunner, claudeRunner *services.ClaudeRunner) *APIHandler {
	return &APIHandler{
		store:        store,
		runner:       runner,
		claudeRunner: claudeRunner,
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
	settings := h.store.GetSettings()

	configData := map[string]interface{}{
		"stale_threshold_days": settings.GetStaleThresholdDays(),
		"working_directory":    h.store.GetWorkingDir(),
		"test_runner": map[string]string{
			"command":         settings.GetTestCommand(),
			"pass_string":     settings.GetPassString(),
			"fail_string":     settings.GetFailString(),
			"no_tests_string": settings.GetNoTestsString(),
		},
	}

	respondJSON(w, http.StatusOK, configData)
}

// UpdateConfigRequest defines the structure for config update requests
type UpdateConfigRequest struct {
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

	// Get current settings and update
	settings := h.store.GetSettings()

	if req.StaleThresholdDays != nil {
		settings.StaleThresholdDays = *req.StaleThresholdDays
	}
	if req.TestRunner != nil {
		if req.TestRunner.Command != nil {
			settings.TestRunner.Command = *req.TestRunner.Command
		}
		if req.TestRunner.PassString != nil {
			settings.TestRunner.PassString = *req.TestRunner.PassString
		}
		if req.TestRunner.FailString != nil {
			settings.TestRunner.FailString = *req.TestRunner.FailString
		}
		if req.TestRunner.NoTestsString != nil {
			settings.TestRunner.NoTestsString = *req.TestRunner.NoTestsString
		}
	}

	// Save to TASKS.md via TaskStore
	if err := h.store.UpdateSettings(settings); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to save settings: "+err.Error())
		return
	}

	// Return updated config
	h.GetConfig(w, r)
}

// ===== AI Queue Handlers =====

// GetAIQueue returns the current AI queue state
func (h *APIHandler) GetAIQueue(w http.ResponseWriter, r *http.Request) {
	state := h.store.GetAIQueue()
	respondJSON(w, http.StatusOK, state)
}

// AddToAIQueue adds a task to the AI queue
func (h *APIHandler) AddToAIQueue(w http.ResponseWriter, r *http.Request) {
	var req models.AddToQueueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.TaskID == "" {
		respondError(w, http.StatusBadRequest, "task_id is required")
		return
	}

	if err := h.store.AddToQueue(req.TaskID, req.Position); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	state := h.store.GetAIQueue()
	respondJSON(w, http.StatusOK, state)
}

// RemoveFromAIQueue removes a task from the AI queue
func (h *APIHandler) RemoveFromAIQueue(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")

	if err := h.store.RemoveFromQueue(taskID); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	state := h.store.GetAIQueue()
	respondJSON(w, http.StatusOK, state)
}

// ReorderAIQueue sets the new order of tasks in the queue
func (h *APIHandler) ReorderAIQueue(w http.ResponseWriter, r *http.Request) {
	var req models.ReorderQueueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if len(req.TaskIDs) == 0 {
		respondError(w, http.StatusBadRequest, "task_ids is required")
		return
	}

	if err := h.store.ReorderQueue(req.TaskIDs); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	state := h.store.GetAIQueue()
	respondJSON(w, http.StatusOK, state)
}

// StartAITask starts working on the next task in the queue
func (h *APIHandler) StartAITask(w http.ResponseWriter, r *http.Request) {
	taskID, err := h.store.StartNextTask()
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Get task details for prompt
	task, err := h.store.Get(taskID)
	if err != nil {
		h.store.StopCurrentTask()
		respondError(w, http.StatusInternalServerError, "Failed to get task: "+err.Error())
		return
	}

	// Build prompt for Claude
	prompt := buildClaudePrompt(task)

	// Start Claude subprocess
	ctx := context.Background()
	if err := h.claudeRunner.Start(ctx, taskID, prompt); err != nil {
		h.store.StopCurrentTask()
		respondError(w, http.StatusInternalServerError, "Failed to start Claude: "+err.Error())
		return
	}

	response := map[string]interface{}{
		"task_id": taskID,
		"queue":   h.store.GetAIQueue(),
		"status":  "started",
	}
	respondJSON(w, http.StatusOK, response)
}

// buildClaudePrompt creates the initial prompt for Claude based on task details
func buildClaudePrompt(task *models.Task) string {
	var sb strings.Builder

	sb.WriteString("You are working on a task from the Kantext task board.\n\n")
	sb.WriteString("## Task Details\n")
	sb.WriteString(fmt.Sprintf("**Title:** %s\n", task.Title))
	sb.WriteString(fmt.Sprintf("**ID:** %s\n", task.ID))
	sb.WriteString(fmt.Sprintf("**Priority:** %s\n", task.Priority))

	if task.AcceptanceCriteria != "" {
		sb.WriteString(fmt.Sprintf("\n**Acceptance Criteria:**\n%s\n", task.AcceptanceCriteria))
	}

	if len(task.Tags) > 0 {
		sb.WriteString(fmt.Sprintf("\n**Tags:** %s\n", strings.Join(task.Tags, ", ")))
	}

	if len(task.Tests) > 0 {
		sb.WriteString("\n**Associated Tests:**\n")
		for _, test := range task.Tests {
			sb.WriteString(fmt.Sprintf("- %s:%s\n", test.File, test.Func))
		}
	}

	sb.WriteString("\n## Instructions\n")
	sb.WriteString("1. Implement the task according to the acceptance criteria\n")
	sb.WriteString("2. Use the Kantext MCP tools to update task status when complete\n")
	sb.WriteString("3. Move the task to 'in_review' column when finished\n")

	return sb.String()
}

// StopAITask stops working on the current task
func (h *APIHandler) StopAITask(w http.ResponseWriter, r *http.Request) {
	// Stop Claude subprocess first
	if err := h.claudeRunner.Stop(); err != nil {
		// Log but don't fail - the process might have already exited
		fmt.Printf("Warning: error stopping Claude: %v\n", err)
	}

	if err := h.store.StopCurrentTask(); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	state := h.store.GetAIQueue()
	respondJSON(w, http.StatusOK, state)
}

// GetAISession returns the current AI conversation session
func (h *APIHandler) GetAISession(w http.ResponseWriter, r *http.Request) {
	session := h.store.GetAISession()
	if session == nil {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"task_id":  "",
			"messages": []models.ChatMessage{},
		})
		return
	}
	respondJSON(w, http.StatusOK, session)
}

// SendAIMessage sends a message to the AI subprocess
func (h *APIHandler) SendAIMessage(w http.ResponseWriter, r *http.Request) {
	var req models.SendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Message == "" {
		respondError(w, http.StatusBadRequest, "message is required")
		return
	}

	// Check if Claude is running
	if !h.claudeRunner.IsRunning() {
		respondError(w, http.StatusBadRequest, "No active Claude session")
		return
	}

	// Add user message to session for history
	userMsg, err := h.store.AddChatMessage("user", req.Message)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Send to Claude's stdin
	if err := h.claudeRunner.SendInput(req.Message); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to send message: "+err.Error())
		return
	}

	response := map[string]interface{}{
		"user_message": userMsg,
		"status":       "sent",
	}
	respondJSON(w, http.StatusOK, response)
}
