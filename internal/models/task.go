package models

import (
	"strings"
	"time"
)

// Column represents the Kanban column
type Column string

const (
	ColumnTodo       Column = "todo"
	ColumnInProgress Column = "in_progress"
	ColumnDone       Column = "done"
)

// ColumnDefinition represents a column with its display name and order
type ColumnDefinition struct {
	Slug  string `json:"slug"`
	Name  string `json:"name"`
	Order int    `json:"order"`
}

// NameToSlug converts a column name to a slug
func NameToSlug(name string) string {
	slug := strings.ToLower(strings.TrimSpace(name))
	slug = strings.ReplaceAll(slug, " ", "_")
	return slug
}

// Priority represents task priority
type Priority string

const (
	PriorityHigh   Priority = "high"
	PriorityMedium Priority = "medium"
	PriorityLow    Priority = "low"
)

// TestStatus represents the status of a test run
type TestStatus string

const (
	TestStatusPending TestStatus = "pending"
	TestStatusRunning TestStatus = "running"
	TestStatusPassed  TestStatus = "passed"
	TestStatusFailed  TestStatus = "failed"
)

// Task represents a TDD task with an associated test
type Task struct {
	ID                 string     `json:"id"`
	Title              string     `json:"title"`
	AcceptanceCriteria string     `json:"acceptance_criteria"`
	Priority           Priority   `json:"priority"`
	Column             Column     `json:"column"`
	RequiresTest       bool       `json:"requires_test"`                // Whether task completion requires a passing test
	TestFile           string     `json:"test_file"`                    // Path to test file relative to working dir (e.g., "internal/auth/auth_test.go")
	TestFunc           string     `json:"test_func"`                    // Test function name (e.g., "TestLogin")
	TestStatus         TestStatus `json:"test_status"`
	LastRun            *time.Time `json:"last_run,omitempty"`
	LastOutput         string     `json:"last_output,omitempty"`
	Order              int        `json:"-"` // Internal order tracking, not exposed to JSON
	CreatedAt          time.Time  `json:"created_at"`
	CreatedBy          string     `json:"created_by"`
	UpdatedAt          time.Time  `json:"updated_at"`
	UpdatedBy          string     `json:"updated_by"`
}

// CreateTaskRequest is the request body for creating a task
type CreateTaskRequest struct {
	Title              string   `json:"title"`
	AcceptanceCriteria string   `json:"acceptance_criteria"`
	Priority           Priority `json:"priority"`
	RequiresTest       *bool    `json:"requires_test,omitempty"` // Optional: whether task requires a passing test (default: false)
	Author             string   `json:"author,omitempty"`        // Optional: who is creating this task
}

// UpdateTaskRequest is the request body for updating a task
type UpdateTaskRequest struct {
	Title              *string   `json:"title,omitempty"`
	AcceptanceCriteria *string   `json:"acceptance_criteria,omitempty"`
	Priority           *Priority `json:"priority,omitempty"`
	Column             *Column   `json:"column,omitempty"`
	RequiresTest       *bool     `json:"requires_test,omitempty"` // Optional: whether task requires a passing test
	TestFile           *string   `json:"test_file,omitempty"`     // Optional: path to test file relative to working dir
	TestFunc           *string   `json:"test_func,omitempty"`     // Optional: test function name
	Author             string    `json:"author,omitempty"`        // Optional: who is updating this task
}

// TestResult represents the result of running a test
type TestResult struct {
	Passed  bool   `json:"passed"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
	RunTime int64  `json:"run_time_ms"`
}

// HasTest returns true if the task has a test associated with it
func (t *Task) HasTest() bool {
	return t.TestFile != "" && t.TestFunc != ""
}
