package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"kantext/internal/models"
	"kantext/internal/services"
)

// Default timeout for test execution via MCP
const mcpTestTimeout = 5 * time.Minute

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
			Description: "List all tasks on the Kantext board. Returns tasks organized by column (inbox, in_progress, done) with their priority and test status.",
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
			Description: "Create a new task. Optionally specify if a passing test is required for completion.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"title": {
						Type:        "string",
						Description: "Short title describing the feature or task (e.g., 'User Login', 'Add Shopping Cart')",
					},
					"acceptance_criteria": {
						Type:        "string",
						Description: "Clear criteria that define when this task is complete.",
					},
					"priority": {
						Type:        "string",
						Description: "Task priority: 'high', 'medium', or 'low'. Defaults to 'medium' if not specified.",
					},
					"tags": {
						Type:        "array",
						Description: "Array of tags for categorization (e.g., ['frontend', 'bug', 'urgent'])",
						Items: &PropertyItems{
							Type: "string",
						},
					},
					"requires_test": {
						Type:        "boolean",
						Description: "Whether a passing test is required to complete this task. Defaults to false.",
					},
				},
				Required: []string{"title"},
			},
		},
		{
			Name:        "update_task",
			Description: "Update an existing task's properties including test configuration. Use this to set the tests array after task creation.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"task_id": {
						Type:        "string",
						Description: "The unique ID of the task to update",
					},
					"title": {
						Type:        "string",
						Description: "New title for the task",
					},
					"acceptance_criteria": {
						Type:        "string",
						Description: "New acceptance criteria for the task",
					},
					"priority": {
						Type:        "string",
						Description: "Task priority: 'high', 'medium', or 'low'",
					},
					"tags": {
						Type:        "array",
						Description: "Array of tags for categorization (e.g., ['frontend', 'bug', 'urgent'])",
						Items: &PropertyItems{
							Type: "string",
						},
					},
					"requires_test": {
						Type:        "boolean",
						Description: "Whether a passing test is required to complete this task",
					},
					"tests": {
						Type:        "array",
						Description: "Array of test specifications. Each test has 'file' (path relative to working directory) and 'func' (test function name)",
						Items: &PropertyItems{
							Type: "object",
							Properties: map[string]Property{
								"file": {
									Type:        "string",
									Description: "Path to test file relative to working directory (e.g., 'internal/auth/auth_test.go')",
								},
								"func": {
									Type:        "string",
									Description: "Test function name (e.g., 'TestLogin')",
								},
							},
							Required: []string{"file", "func"},
						},
					},
				},
				Required: []string{"task_id"},
			},
		},
		{
			Name:        "run_test",
			Description: "Run all Go tests associated with a task. All tests must pass for the task to be automatically moved to the 'done' column. Returns the test output and pass/fail status for each test.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"task_id": {
						Type:        "string",
						Description: "The unique ID of the task whose tests should be run",
					},
				},
				Required: []string{"task_id"},
			},
		},
		{
			Name:        "move_task",
			Description: "Move a task to a different column. Use this to manually organize tasks.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"task_id": {
						Type:        "string",
						Description: "The unique ID of the task to move",
					},
					"column": {
						Type:        "string",
						Description: "Target column slug (e.g., 'inbox', 'in_progress', 'in_review', 'done')",
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
	case "update_task":
		return h.updateTask(args)
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

	// Get columns from store (already sorted by Order)
	columnDefs := h.store.GetColumns()

	// Organize tasks by column
	tasksByColumn := make(map[string][]*models.Task)
	for _, col := range columnDefs {
		tasksByColumn[col.Slug] = []*models.Task{}
	}
	for _, task := range tasks {
		col := string(task.Column)
		tasksByColumn[col] = append(tasksByColumn[col], task)
	}

	var sb strings.Builder
	sb.WriteString("# Kantext Tasks\n\n")

	// Output tasks for each column in order
	for i, col := range columnDefs {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(fmt.Sprintf("## %s\n", col.Name))
		colTasks := tasksByColumn[col.Slug]
		for _, t := range colTasks {
			sb.WriteString(formatTask(t))
		}
		if len(colTasks) == 0 {
			sb.WriteString("(no tasks)\n")
		}
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

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("- [%s] %s %s\n", statusCheckbox(t.TestStatus), priorityEmoji, t.Title))
	sb.WriteString(fmt.Sprintf("  ID: %s\n", t.ID))
	sb.WriteString(fmt.Sprintf("  Priority: %s\n", t.Priority))
	if len(t.Tags) > 0 {
		sb.WriteString(fmt.Sprintf("  Tags: %s\n", strings.Join(t.Tags, ", ")))
	}
	sb.WriteString(fmt.Sprintf("  Requires Test: %t\n", t.RequiresTest))

	if t.HasTest() {
		status := string(t.TestStatus)
		if t.TestStatus == models.TestStatusPassed {
			status = "PASSED"
		} else if t.TestStatus == models.TestStatusFailed {
			status = "FAILED"
		}
		// Display all tests
		if len(t.Tests) == 1 {
			sb.WriteString(fmt.Sprintf("  Test: %s:%s\n", t.Tests[0].File, t.Tests[0].Func))
		} else {
			sb.WriteString("  Tests:\n")
			for _, test := range t.Tests {
				sb.WriteString(fmt.Sprintf("    - %s:%s\n", test.File, test.Func))
			}
		}
		sb.WriteString(fmt.Sprintf("  Status: %s\n", status))
	}

	if t.AcceptanceCriteria != "" {
		sb.WriteString(fmt.Sprintf("  Acceptance Criteria: %s\n", t.AcceptanceCriteria))
	}

	return sb.String()
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
	if len(task.Tags) > 0 {
		sb.WriteString(fmt.Sprintf("**Tags:** %s\n", strings.Join(task.Tags, ", ")))
	}
	sb.WriteString(fmt.Sprintf("**Requires Test:** %t\n", task.RequiresTest))

	// Only show test info if task has tests
	if task.HasTest() {
		if len(task.Tests) == 1 {
			sb.WriteString(fmt.Sprintf("**Test:** %s:%s\n", task.Tests[0].File, task.Tests[0].Func))
		} else {
			sb.WriteString("**Tests:**\n")
			for _, test := range task.Tests {
				sb.WriteString(fmt.Sprintf("  - %s:%s\n", test.File, test.Func))
			}
		}
		sb.WriteString(fmt.Sprintf("**Status:** %s\n", task.TestStatus))
	}

	if task.AcceptanceCriteria != "" {
		sb.WriteString(fmt.Sprintf("**Acceptance Criteria:** %s\n", task.AcceptanceCriteria))
	}

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
	requiresTest, hasRequiresTest := args["requires_test"].(bool)

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

	// Parse tags array
	var tags []string
	if tagsRaw, ok := args["tags"].([]interface{}); ok {
		for _, tagRaw := range tagsRaw {
			if tag, ok := tagRaw.(string); ok && tag != "" {
				tags = append(tags, tag)
			}
		}
	}

	// Set requires_test if provided
	var requiresTestPtr *bool
	if hasRequiresTest {
		requiresTestPtr = &requiresTest
	}

	req := models.CreateTaskRequest{
		Title:              title,
		AcceptanceCriteria: acceptanceCriteria,
		Priority:           priority,
		Tags:               tags,
		RequiresTest:       requiresTestPtr,
	}

	task, err := h.store.Create(req)
	if err != nil {
		return ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Failed to create task: %v", err)}},
			IsError: true,
		}
	}

	var message string
	if task.RequiresTest {
		message = fmt.Sprintf(`Task created successfully!

**ID:** %s
**Title:** %s
**Priority:** %s
**Requires Test:** Yes

The task requires a passing test to be marked as complete.
Use update_task to configure the test file and function, then run_test to verify.`,
			task.ID, task.Title, task.Priority)
	} else {
		message = fmt.Sprintf(`Task created successfully!

**ID:** %s
**Title:** %s
**Priority:** %s
**Requires Test:** No

The task has been added to the 'inbox' column.
It can be moved to 'done' at any time without requiring a test.`,
			task.ID, task.Title, task.Priority)
	}

	return ToolResult{
		Content: []ContentBlock{{
			Type: "text",
			Text: message,
		}},
	}
}

func (h *ToolHandler) updateTask(args map[string]interface{}) ToolResult {
	taskID, ok := args["task_id"].(string)
	if !ok || taskID == "" {
		return ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "task_id is required"}},
			IsError: true,
		}
	}

	// Build update request with only provided fields
	req := models.UpdateTaskRequest{}

	if title, ok := args["title"].(string); ok && title != "" {
		req.Title = &title
	}
	if criteria, ok := args["acceptance_criteria"].(string); ok {
		req.AcceptanceCriteria = &criteria
	}
	if priorityStr, ok := args["priority"].(string); ok && priorityStr != "" {
		var priority models.Priority
		switch priorityStr {
		case "high":
			priority = models.PriorityHigh
		case "low":
			priority = models.PriorityLow
		case "medium":
			priority = models.PriorityMedium
		default:
			return ToolResult{
				Content: []ContentBlock{{Type: "text", Text: "priority must be 'high', 'medium', or 'low'"}},
				IsError: true,
			}
		}
		req.Priority = &priority
	}
	if requiresTest, ok := args["requires_test"].(bool); ok {
		req.RequiresTest = &requiresTest
	}
	// Parse tags array
	if tagsRaw, ok := args["tags"].([]interface{}); ok {
		var tags []string
		for _, tagRaw := range tagsRaw {
			if tag, ok := tagRaw.(string); ok && tag != "" {
				tags = append(tags, tag)
			}
		}
		req.Tags = tags
	}
	// Parse tests array
	if testsRaw, ok := args["tests"].([]interface{}); ok {
		tests := make([]models.TestSpec, 0, len(testsRaw))
		for _, testRaw := range testsRaw {
			if testMap, ok := testRaw.(map[string]interface{}); ok {
				file, _ := testMap["file"].(string)
				fn, _ := testMap["func"].(string)
				if file != "" && fn != "" {
					tests = append(tests, models.TestSpec{File: file, Func: fn})
				}
			}
		}
		if len(tests) > 0 {
			req.Tests = tests
		}
	}

	task, err := h.store.Update(taskID, req)
	if err != nil {
		return ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Failed to update task: %v", err)}},
			IsError: true,
		}
	}

	var sb strings.Builder
	sb.WriteString("Task updated successfully!\n\n")
	sb.WriteString(fmt.Sprintf("**ID:** %s\n", task.ID))
	sb.WriteString(fmt.Sprintf("**Title:** %s\n", task.Title))
	sb.WriteString(fmt.Sprintf("**Priority:** %s\n", task.Priority))
	sb.WriteString(fmt.Sprintf("**Requires Test:** %t\n", task.RequiresTest))
	if task.HasTest() {
		if len(task.Tests) == 1 {
			sb.WriteString(fmt.Sprintf("**Test:** %s:%s\n", task.Tests[0].File, task.Tests[0].Func))
		} else {
			sb.WriteString("**Tests:**\n")
			for _, test := range task.Tests {
				sb.WriteString(fmt.Sprintf("  - %s:%s\n", test.File, test.Func))
			}
		}
	}

	return ToolResult{
		Content: []ContentBlock{{Type: "text", Text: sb.String()}},
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

	// Check if task has tests associated
	if !task.HasTest() {
		return ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Task '%s' does not have any tests associated with it. Use update_task to add tests first.", task.Title)}},
			IsError: true,
		}
	}

	// Mark as running
	h.store.SetTestRunning(taskID)

	// Run all tests with timeout
	ctx, cancel := context.WithTimeout(context.Background(), mcpTestTimeout)
	defer cancel()
	results := h.runner.RunAll(ctx, task.Tests)

	// Update the task with aggregated results
	updatedTask, err := h.store.UpdateTestResults(taskID, results)
	if err != nil {
		return ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Failed to update task: %v", err)}},
			IsError: true,
		}
	}

	var sb strings.Builder
	if results.AllPassed {
		sb.WriteString("# All Tests PASSED!\n\n")
		sb.WriteString(fmt.Sprintf("The task '%s' has been automatically moved to the 'done' column.\n\n", task.Title))
	} else {
		sb.WriteString("# Tests FAILED\n\n")
		sb.WriteString(fmt.Sprintf("The task '%s' remains in the '%s' column.\n\n", task.Title, updatedTask.Column))
	}

	sb.WriteString(fmt.Sprintf("**Total Duration:** %dms\n\n", results.TotalTime))

	// Show results for each test
	sb.WriteString("## Test Results\n\n")
	for i, result := range results.Results {
		var testName string
		if i < len(task.Tests) {
			testName = fmt.Sprintf("%s:%s", task.Tests[i].File, task.Tests[i].Func)
		} else {
			testName = fmt.Sprintf("Test %d", i+1)
		}

		status := "PASSED"
		if !result.Passed {
			status = "FAILED"
		}
		sb.WriteString(fmt.Sprintf("### %s - %s (%dms)\n", testName, status, result.RunTime))
		sb.WriteString("```\n")
		sb.WriteString(result.Output)
		sb.WriteString("\n```\n")
		if result.Error != "" {
			sb.WriteString(fmt.Sprintf("**Error:** %s\n", result.Error))
		}
		sb.WriteString("\n")
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
			Content: []ContentBlock{{Type: "text", Text: "column is required"}},
			IsError: true,
		}
	}

	// Validate column exists in the store
	columns := h.store.GetColumns()
	validColumn := false
	var validColumnNames []string
	for _, col := range columns {
		validColumnNames = append(validColumnNames, col.Slug)
		if col.Slug == columnStr {
			validColumn = true
		}
	}
	if !validColumn {
		return ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Invalid column '%s'. Valid columns: %s", columnStr, strings.Join(validColumnNames, ", "))}},
			IsError: true,
		}
	}
	column := models.Column(columnStr)

	// Get current task to check test status
	currentTask, err := h.store.Get(taskID)
	if err != nil {
		return ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Task not found: %s", taskID)}},
			IsError: true,
		}
	}

	// Prevent moving to "done" if task requires a test
	if column == models.ColumnDone && currentTask.RequiresTest {
		if !currentTask.HasTest() {
			return ToolResult{
				Content: []ContentBlock{{Type: "text", Text: "Cannot move task to 'done': task requires a test but no test is configured. Use update_task to set test_file and test_func first."}},
				IsError: true,
			}
		}
		if currentTask.TestStatus != models.TestStatusPassed {
			return ToolResult{
				Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Cannot move task to 'done': test has not passed (current status: %s). Run the test first with run_test.", currentTask.TestStatus)}},
				IsError: true,
			}
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
