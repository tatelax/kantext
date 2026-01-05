package models

import (
	"strings"
	"time"
)

// Column represents the Kanban column
type Column string

const (
	ColumnInbox      Column = "inbox"
	ColumnInProgress Column = "in_progress"
	ColumnDone       Column = "done"
)

// ColumnDefinition represents a column with its display name and order
type ColumnDefinition struct {
	Slug  string `json:"slug"`
	Name  string `json:"name"`
	Order int    `json:"order"`
}

// DefaultColumns defines the columns that must always exist
var DefaultColumns = []ColumnDefinition{
	{Slug: string(ColumnInbox), Name: "Inbox", Order: 0},
	{Slug: string(ColumnInProgress), Name: "In Progress", Order: 1},
	{Slug: string(ColumnDone), Name: "Done", Order: 2},
}

// IsDefaultColumn returns true if the given slug is a protected default column
func IsDefaultColumn(slug string) bool {
	return slug == string(ColumnInbox) || slug == string(ColumnInProgress) || slug == string(ColumnDone)
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

// IsKnownPriority returns true if the priority is a known standard value
func IsKnownPriority(p Priority) bool {
	return p == PriorityHigh || p == PriorityMedium || p == PriorityLow
}

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
	Tags               []string   `json:"tags"`                // Array of tags for categorization
	RequiresTest       bool       `json:"requires_test"`       // Whether task completion requires a passing test
	Tests              []TestSpec `json:"tests"`               // Array of test specifications
	TestStatus         TestStatus `json:"test_status"`
	TestsPassed        int        `json:"tests_passed"`        // Number of tests that passed in last run
	TestsTotal         int        `json:"tests_total"`         // Total number of tests in last run
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
	Tags               []string `json:"tags,omitempty"`          // Optional: array of tags for categorization
	RequiresTest       *bool    `json:"requires_test,omitempty"` // Optional: whether task requires a passing test (default: false)
	Author             string   `json:"author,omitempty"`        // Optional: who is creating this task
}

// UpdateTaskRequest is the request body for updating a task
type UpdateTaskRequest struct {
	Title              *string    `json:"title,omitempty"`
	AcceptanceCriteria *string    `json:"acceptance_criteria,omitempty"`
	Priority           *Priority  `json:"priority,omitempty"`
	Column             *Column    `json:"column,omitempty"`
	Tags               []string   `json:"tags,omitempty"`         // Optional: array of tags for categorization
	RequiresTest       *bool      `json:"requires_test,omitempty"` // Optional: whether task requires a passing test
	Tests              []TestSpec `json:"tests,omitempty"`         // Optional: array of test specifications
	Author             string     `json:"author,omitempty"`        // Optional: who is updating this task
}

// TestSpec represents a single test file and function pair
type TestSpec struct {
	File string `json:"file"` // Path to test file relative to working dir (e.g., "internal/auth/auth_test.go")
	Func string `json:"func"` // Test function name (e.g., "TestLogin")
}

// TestResult represents the result of running a test
type TestResult struct {
	Passed  bool   `json:"passed"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
	RunTime int64  `json:"run_time_ms"`
}

// TestResults represents the aggregated result of running multiple tests
type TestResults struct {
	AllPassed bool         `json:"all_passed"`
	Results   []TestResult `json:"results"`
	TotalTime int64        `json:"total_time_ms"`
}

// HasTest returns true if the task has at least one test associated with it
func (t *Task) HasTest() bool {
	return len(t.Tests) > 0
}

// CheckboxChar returns the markdown checkbox character for this task's test status.
func (t *Task) CheckboxChar() string {
	switch t.TestStatus {
	case TestStatusPassed:
		return "x"
	case TestStatusFailed:
		return "-"
	default:
		return " "
	}
}

// ParsePriority converts a string to Priority, returning defaultPriority if invalid.
func ParsePriority(s string, defaultPriority Priority) Priority {
	switch strings.ToLower(s) {
	case "high":
		return PriorityHigh
	case "medium":
		return PriorityMedium
	case "low":
		return PriorityLow
	default:
		return defaultPriority
	}
}

// AIQueueState holds the current AI task queue state
type AIQueueState struct {
	ActiveTaskID string   `json:"active_task_id"` // ID of task currently being worked on (empty = none)
	TaskIDs      []string `json:"task_ids"`       // Ordered list of task IDs in queue
}

// ChatMessage represents a message in the LLM conversation
type ChatMessage struct {
	Role      string    `json:"role"`      // "user", "assistant", "system"
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// AISession holds the current AI conversation state
type AISession struct {
	TaskID   string        `json:"task_id"`
	Messages []ChatMessage `json:"messages"`
	Started  time.Time     `json:"started"`
}

// AddToQueueRequest is the request body for adding a task to the AI queue
type AddToQueueRequest struct {
	TaskID   string `json:"task_id"`
	Position int    `json:"position"` // -1 for end of queue
}

// ReorderQueueRequest is the request body for reordering the AI queue
type ReorderQueueRequest struct {
	TaskIDs []string `json:"task_ids"`
}

// SendMessageRequest is the request body for sending a chat message
type SendMessageRequest struct {
	Message string `json:"message"`
	Mode    string `json:"mode,omitempty"` // "plan" or empty (default: accept edits)
}
