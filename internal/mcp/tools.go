package mcp

import (
	"context"
	"fmt"
	"strings"

	"kantext/internal/models"
	"kantext/internal/services"
)

// ToolHandler handles MCP tool calls
type ToolHandler struct {
	store  *services.TaskStore
	runner *services.TestRunner
}

// NewToolHandler creates a new tool handler
func NewToolHandler(store *services.TaskStore, runner *services.TestRunner) *ToolHandler {
	return &ToolHandler{
		store:  store,
		runner: runner,
	}
}

// GetTools returns the list of available tools
func (h *ToolHandler) GetTools() []Tool {
	return []Tool{
		{
			Name:        "list_tasks",
			Description: "List all tasks on the Kantext board. Returns tasks organized by column (todo, in_progress, done) with their priority and test status.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]Property{},
			},
		},
		{
			Name:        "get_task",
			Description: "Get details of a specific task by ID, including its acceptance criteria, priority, test status, and last test output.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"task_id": {
						Type:        "string",
						Description: "The unique ID of the task to retrieve",
					},
				},
				Required: []string{"task_id"},
			},
		},
		{
			Name:        "create_task",
			Description: "Create a new TDD task. By default, a Go test file is auto-generated. Optionally specify an existing test file and function to associate with the task.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"title": {
						Type:        "string",
						Description: "Short title describing the feature or task (e.g., 'User Login', 'Add Shopping Cart')",
					},
					"acceptance_criteria": {
						Type:        "string",
						Description: "Clear criteria that define when this task is complete. What should the test verify?",
					},
					"priority": {
						Type:        "string",
						Description: "Task priority: 'high', 'medium', or 'low'. Defaults to 'medium' if not specified.",
					},
					"test_file": {
						Type:        "string",
						Description: "Optional: Name of an existing test file (e.g., 'auth_test.go'). If not specified, a new test file will be generated.",
					},
					"test_func": {
						Type:        "string",
						Description: "Optional: Name of an existing test function (e.g., 'TestLogin'). Required if test_file is specified.",
					},
				},
				Required: []string{"title"},
			},
		},
		{
			Name:        "run_test",
			Description: "Run the Go test associated with a task. If the test passes, the task is automatically moved to the 'done' column. Returns the test output and pass/fail status.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"task_id": {
						Type:        "string",
						Description: "The unique ID of the task whose test should be run",
					},
				},
				Required: []string{"task_id"},
			},
		},
		{
			Name:        "move_task",
			Description: "Move a task to a different column (todo, in_progress, or done). Use this to manually organize tasks.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"task_id": {
						Type:        "string",
						Description: "The unique ID of the task to move",
					},
					"column": {
						Type:        "string",
						Description: "Target column: 'todo', 'in_progress', or 'done'",
					},
				},
				Required: []string{"task_id", "column"},
			},
		},
		{
			Name:        "delete_task",
			Description: "Delete a task from the Kantext board. This action cannot be undone.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"task_id": {
						Type:        "string",
						Description: "The unique ID of the task to delete",
					},
				},
				Required: []string{"task_id"},
			},
		},
	}
}

// CallTool executes a tool by name
func (h *ToolHandler) CallTool(name string, args map[string]interface{}) ToolResult {
	// Reload tasks from file before each operation to ensure fresh data
	if err := h.store.Load(); err != nil {
		return ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Failed to reload tasks: %v", err)}},
			IsError: true,
		}
	}

	switch name {
	case "list_tasks":
		return h.listTasks()
	case "get_task":
		return h.getTask(args)
	case "create_task":
		return h.createTask(args)
	case "run_test":
		return h.runTest(args)
	case "move_task":
		return h.moveTask(args)
	case "delete_task":
		return h.deleteTask(args)
	default:
		return ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Unknown tool: %s", name)}},
			IsError: true,
		}
	}
}

func (h *ToolHandler) listTasks() ToolResult {
	tasks := h.store.GetAll()

	// Organize by column
	columns := map[string][]*models.Task{
		"todo":        {},
		"in_progress": {},
		"done":        {},
	}

	for _, task := range tasks {
		col := string(task.Column)
		columns[col] = append(columns[col], task)
	}

	var sb strings.Builder
	sb.WriteString("# Kantext Tasks\n\n")

	sb.WriteString("## Todo\n")
	for _, t := range columns["todo"] {
		sb.WriteString(formatTask(t))
	}
	if len(columns["todo"]) == 0 {
		sb.WriteString("(no tasks)\n")
	}

	sb.WriteString("\n## In Progress\n")
	for _, t := range columns["in_progress"] {
		sb.WriteString(formatTask(t))
	}
	if len(columns["in_progress"]) == 0 {
		sb.WriteString("(no tasks)\n")
	}

	sb.WriteString("\n## Done\n")
	for _, t := range columns["done"] {
		sb.WriteString(formatTask(t))
	}
	if len(columns["done"]) == 0 {
		sb.WriteString("(no tasks)\n")
	}

	return ToolResult{
		Content: []ContentBlock{{Type: "text", Text: sb.String()}},
	}
}

func formatTask(t *models.Task) string {
	priorityEmoji := ""
	switch t.Priority {
	case models.PriorityHigh:
		priorityEmoji = "[HIGH]"
	case models.PriorityMedium:
		priorityEmoji = "[MEDIUM]"
	case models.PriorityLow:
		priorityEmoji = "[LOW]"
	}

	// Build output based on whether task has a test
	if t.HasTest() {
		status := string(t.TestStatus)
		if t.TestStatus == models.TestStatusPassed {
			status = "PASSED"
		} else if t.TestStatus == models.TestStatusFailed {
			status = "FAILED"
		}

		return fmt.Sprintf("- [%s] %s %s\n  ID: %s\n  Priority: %s\n  Test: %s:%s\n  Status: %s\n  Acceptance Criteria: %s\n",
			statusCheckbox(t.TestStatus),
			priorityEmoji,
			t.Title,
			t.ID,
			t.Priority,
			t.TestFile,
			t.TestFunc,
			status,
			t.AcceptanceCriteria,
		)
	}

	// Task without test - simpler format
	return fmt.Sprintf("- [ ] %s %s\n  ID: %s\n  Priority: %s\n  Acceptance Criteria: %s\n",
		priorityEmoji,
		t.Title,
		t.ID,
		t.Priority,
		t.AcceptanceCriteria,
	)
}

func statusCheckbox(status models.TestStatus) string {
	if status == models.TestStatusPassed {
		return "x"
	}
	return " "
}

func (h *ToolHandler) getTask(args map[string]interface{}) ToolResult {
	taskID, ok := args["task_id"].(string)
	if !ok || taskID == "" {
		return ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "task_id is required"}},
			IsError: true,
		}
	}

	task, err := h.store.Get(taskID)
	if err != nil {
		return ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Task not found: %s", taskID)}},
			IsError: true,
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Task: %s\n\n", task.Title))
	sb.WriteString(fmt.Sprintf("**ID:** %s\n", task.ID))
	sb.WriteString(fmt.Sprintf("**Priority:** %s\n", task.Priority))
	sb.WriteString(fmt.Sprintf("**Column:** %s\n", task.Column))

	// Only show test info if task has a test
	if task.HasTest() {
		sb.WriteString(fmt.Sprintf("**Test:** %s:%s\n", task.TestFile, task.TestFunc))
		sb.WriteString(fmt.Sprintf("**Status:** %s\n", task.TestStatus))
	}

	sb.WriteString(fmt.Sprintf("**Acceptance Criteria:** %s\n", task.AcceptanceCriteria))

	if task.HasTest() && task.LastOutput != "" {
		sb.WriteString(fmt.Sprintf("\n## Last Test Output\n```\n%s\n```\n", task.LastOutput))
	}

	return ToolResult{
		Content: []ContentBlock{{Type: "text", Text: sb.String()}},
	}
}

func (h *ToolHandler) createTask(args map[string]interface{}) ToolResult {
	title, _ := args["title"].(string)
	acceptanceCriteria, _ := args["acceptance_criteria"].(string)
	priorityStr, _ := args["priority"].(string)
	testFile, _ := args["test_file"].(string)
	testFunc, _ := args["test_func"].(string)

	if title == "" {
		return ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "title is required"}},
			IsError: true,
		}
	}

	// Validate and set priority
	var priority models.Priority
	switch priorityStr {
	case "high":
		priority = models.PriorityHigh
	case "low":
		priority = models.PriorityLow
	case "medium", "":
		priority = models.PriorityMedium
	default:
		return ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "priority must be 'high', 'medium', or 'low'"}},
			IsError: true,
		}
	}

	req := models.CreateTaskRequest{
		Title:              title,
		AcceptanceCriteria: acceptanceCriteria,
		Priority:           priority,
		TestFile:           testFile,
		TestFunc:           testFunc,
	}

	task, err := h.store.Create(req)
	if err != nil {
		return ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Failed to create task: %v", err)}},
			IsError: true,
		}
	}

	// Different message based on whether test was auto-generated or specified
	var message string
	if testFile != "" && testFunc != "" {
		message = fmt.Sprintf(`Task created successfully!

**ID:** %s
**Title:** %s
**Priority:** %s
**Test File:** %s
**Test Function:** %s

The task is linked to an existing test.
Run the test with run_test to check if it passes.`,
			task.ID, task.Title, task.Priority, task.TestFile, task.TestFunc)
	} else {
		message = fmt.Sprintf(`Task created successfully!

**ID:** %s
**Title:** %s
**Priority:** %s
**Test File:** %s
**Test Function:** %s

A failing test has been generated at tests/%s.
The task has been added to the 'todo' column.

Next steps:
1. Implement the feature to make the test pass
2. Run the test with run_test to verify completion
3. The task will auto-move to 'done' when the test passes`,
			task.ID, task.Title, task.Priority, task.TestFile, task.TestFunc, task.TestFile)
	}

	return ToolResult{
		Content: []ContentBlock{{
			Type: "text",
			Text: message,
		}},
	}
}

func (h *ToolHandler) runTest(args map[string]interface{}) ToolResult {
	taskID, ok := args["task_id"].(string)
	if !ok || taskID == "" {
		return ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "task_id is required"}},
			IsError: true,
		}
	}

	task, err := h.store.Get(taskID)
	if err != nil {
		return ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Task not found: %s", taskID)}},
			IsError: true,
		}
	}

	// Check if task has a test associated
	if !task.HasTest() {
		return ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Task '%s' does not have a test associated with it. Add test metadata to the task in TASKS.md first.", task.Title)}},
			IsError: true,
		}
	}

	// Mark as running
	h.store.SetTestRunning(taskID)

	// Run the test
	ctx := context.Background()
	result := h.runner.Run(ctx, task.TestFile, task.TestFunc)

	// Update the task
	updatedTask, err := h.store.UpdateTestResult(taskID, result)
	if err != nil {
		return ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Failed to update task: %v", err)}},
			IsError: true,
		}
	}

	var sb strings.Builder
	if result.Passed {
		sb.WriteString("# Test PASSED!\n\n")
		sb.WriteString(fmt.Sprintf("The task '%s' has been automatically moved to the 'done' column.\n\n", task.Title))
	} else {
		sb.WriteString("# Test FAILED\n\n")
		sb.WriteString(fmt.Sprintf("The task '%s' remains in the '%s' column.\n\n", task.Title, updatedTask.Column))
	}

	sb.WriteString(fmt.Sprintf("**Test:** %s:%s\n", task.TestFile, task.TestFunc))
	sb.WriteString(fmt.Sprintf("**Duration:** %dms\n\n", result.RunTime))
	sb.WriteString("## Output\n```\n")
	sb.WriteString(result.Output)
	sb.WriteString("\n```")

	if result.Error != "" {
		sb.WriteString(fmt.Sprintf("\n\n**Error:** %s", result.Error))
	}

	return ToolResult{
		Content: []ContentBlock{{Type: "text", Text: sb.String()}},
	}
}

func (h *ToolHandler) moveTask(args map[string]interface{}) ToolResult {
	taskID, ok := args["task_id"].(string)
	if !ok || taskID == "" {
		return ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "task_id is required"}},
			IsError: true,
		}
	}

	columnStr, ok := args["column"].(string)
	if !ok || columnStr == "" {
		return ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "column is required (todo, in_progress, or done)"}},
			IsError: true,
		}
	}

	var column models.Column
	switch columnStr {
	case "todo":
		column = models.ColumnTodo
	case "in_progress":
		column = models.ColumnInProgress
	case "done":
		column = models.ColumnDone
	default:
		return ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "Invalid column. Must be 'todo', 'in_progress', or 'done'"}},
			IsError: true,
		}
	}

	// Get current task to check test status
	currentTask, err := h.store.Get(taskID)
	if err != nil {
		return ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Task not found: %s", taskID)}},
			IsError: true,
		}
	}

	// Prevent moving to "done" unless the test has passed (only for tasks with tests)
	if column == models.ColumnDone && currentTask.HasTest() && currentTask.TestStatus != models.TestStatusPassed {
		return ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Cannot move task to 'done': test has not passed (current status: %s). Run the test first with run_test.", currentTask.TestStatus)}},
			IsError: true,
		}
	}

	task, err := h.store.Update(taskID, models.UpdateTaskRequest{Column: &column})
	if err != nil {
		return ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Failed to move task: %v", err)}},
			IsError: true,
		}
	}

	return ToolResult{
		Content: []ContentBlock{{
			Type: "text",
			Text: fmt.Sprintf("Task '%s' moved to '%s' column.", task.Title, column),
		}},
	}
}

func (h *ToolHandler) deleteTask(args map[string]interface{}) ToolResult {
	taskID, ok := args["task_id"].(string)
	if !ok || taskID == "" {
		return ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "task_id is required"}},
			IsError: true,
		}
	}

	task, err := h.store.Get(taskID)
	if err != nil {
		return ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Task not found: %s", taskID)}},
			IsError: true,
		}
	}

	title := task.Title

	if err := h.store.Delete(taskID); err != nil {
		return ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Failed to delete task: %v", err)}},
			IsError: true,
		}
	}

	return ToolResult{
		Content: []ContentBlock{{
			Type: "text",
			Text: fmt.Sprintf("Task '%s' has been deleted.", title),
		}},
	}
}
