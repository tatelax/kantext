package models

import (
	"testing"
)

// TestNameToSlug tests the NameToSlug function
func TestNameToSlug(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple lowercase", "inbox", "inbox"},
		{"uppercase", "INBOX", "inbox"},
		{"mixed case", "In Progress", "in_progress"},
		{"with spaces", "My Custom Column", "my_custom_column"},
		{"leading/trailing spaces", "  inbox  ", "inbox"},
		{"multiple spaces", "my   column", "my___column"},
		{"already slug", "in_progress", "in_progress"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NameToSlug(tt.input)
			if result != tt.expected {
				t.Errorf("NameToSlug(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestIsDefaultColumn tests the IsDefaultColumn function
func TestIsDefaultColumn(t *testing.T) {
	tests := []struct {
		slug     string
		expected bool
	}{
		{"inbox", true},
		{"in_progress", true},
		{"done", true},
		{"custom", false},
		{"INBOX", false}, // case sensitive
		{"", false},
		{"in_review", false},
	}

	for _, tt := range tests {
		t.Run(tt.slug, func(t *testing.T) {
			result := IsDefaultColumn(tt.slug)
			if result != tt.expected {
				t.Errorf("IsDefaultColumn(%q) = %v, want %v", tt.slug, result, tt.expected)
			}
		})
	}
}

// TestIsKnownPriority tests the IsKnownPriority function
func TestIsKnownPriority(t *testing.T) {
	tests := []struct {
		priority Priority
		expected bool
	}{
		{PriorityHigh, true},
		{PriorityMedium, true},
		{PriorityLow, true},
		{Priority("unknown"), false},
		{Priority(""), false},
		{Priority("HIGH"), false}, // case sensitive
	}

	for _, tt := range tests {
		t.Run(string(tt.priority), func(t *testing.T) {
			result := IsKnownPriority(tt.priority)
			if result != tt.expected {
				t.Errorf("IsKnownPriority(%q) = %v, want %v", tt.priority, result, tt.expected)
			}
		})
	}
}

// TestParsePriority tests the ParsePriority function
func TestParsePriority(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		defaultPriority Priority
		expected        Priority
	}{
		{"high lowercase", "high", PriorityMedium, PriorityHigh},
		{"high uppercase", "HIGH", PriorityMedium, PriorityHigh},
		{"high mixed", "High", PriorityMedium, PriorityHigh},
		{"medium lowercase", "medium", PriorityHigh, PriorityMedium},
		{"medium uppercase", "MEDIUM", PriorityHigh, PriorityMedium},
		{"low lowercase", "low", PriorityMedium, PriorityLow},
		{"low uppercase", "LOW", PriorityMedium, PriorityLow},
		{"invalid returns default", "invalid", PriorityMedium, PriorityMedium},
		{"empty returns default", "", PriorityHigh, PriorityHigh},
		{"whitespace returns default", "  ", PriorityLow, PriorityLow},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParsePriority(tt.input, tt.defaultPriority)
			if result != tt.expected {
				t.Errorf("ParsePriority(%q, %q) = %q, want %q", tt.input, tt.defaultPriority, result, tt.expected)
			}
		})
	}
}

// TestTaskHasTest tests the Task.HasTest method
func TestTaskHasTest(t *testing.T) {
	tests := []struct {
		name     string
		task     Task
		expected bool
	}{
		{
			name:     "no tests",
			task:     Task{Tests: nil},
			expected: false,
		},
		{
			name:     "empty tests slice",
			task:     Task{Tests: []TestSpec{}},
			expected: false,
		},
		{
			name: "one test",
			task: Task{Tests: []TestSpec{
				{File: "test.go", Func: "TestFoo"},
			}},
			expected: true,
		},
		{
			name: "multiple tests",
			task: Task{Tests: []TestSpec{
				{File: "test.go", Func: "TestFoo"},
				{File: "test.go", Func: "TestBar"},
			}},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.task.HasTest()
			if result != tt.expected {
				t.Errorf("Task.HasTest() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestTaskCheckboxChar tests the Task.CheckboxChar method
func TestTaskCheckboxChar(t *testing.T) {
	tests := []struct {
		name       string
		testStatus TestStatus
		expected   string
	}{
		{"passed", TestStatusPassed, "x"},
		{"failed", TestStatusFailed, "-"},
		{"pending", TestStatusPending, " "},
		{"running", TestStatusRunning, " "},
		{"empty", TestStatus(""), " "},
		{"unknown", TestStatus("unknown"), " "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := Task{TestStatus: tt.testStatus}
			result := task.CheckboxChar()
			if result != tt.expected {
				t.Errorf("Task.CheckboxChar() with status %q = %q, want %q", tt.testStatus, result, tt.expected)
			}
		})
	}
}

// TestColumnConstants verifies column constant values
func TestColumnConstants(t *testing.T) {
	if ColumnInbox != "inbox" {
		t.Errorf("ColumnInbox = %q, want %q", ColumnInbox, "inbox")
	}
	if ColumnInProgress != "in_progress" {
		t.Errorf("ColumnInProgress = %q, want %q", ColumnInProgress, "in_progress")
	}
	if ColumnDone != "done" {
		t.Errorf("ColumnDone = %q, want %q", ColumnDone, "done")
	}
}

// TestPriorityConstants verifies priority constant values
func TestPriorityConstants(t *testing.T) {
	if PriorityHigh != "high" {
		t.Errorf("PriorityHigh = %q, want %q", PriorityHigh, "high")
	}
	if PriorityMedium != "medium" {
		t.Errorf("PriorityMedium = %q, want %q", PriorityMedium, "medium")
	}
	if PriorityLow != "low" {
		t.Errorf("PriorityLow = %q, want %q", PriorityLow, "low")
	}
}

// TestTestStatusConstants verifies test status constant values
func TestTestStatusConstants(t *testing.T) {
	if TestStatusPending != "pending" {
		t.Errorf("TestStatusPending = %q, want %q", TestStatusPending, "pending")
	}
	if TestStatusRunning != "running" {
		t.Errorf("TestStatusRunning = %q, want %q", TestStatusRunning, "running")
	}
	if TestStatusPassed != "passed" {
		t.Errorf("TestStatusPassed = %q, want %q", TestStatusPassed, "passed")
	}
	if TestStatusFailed != "failed" {
		t.Errorf("TestStatusFailed = %q, want %q", TestStatusFailed, "failed")
	}
}

// TestDefaultColumns verifies the default columns configuration
func TestDefaultColumns(t *testing.T) {
	if len(DefaultColumns) != 3 {
		t.Errorf("DefaultColumns length = %d, want 3", len(DefaultColumns))
	}

	expectedColumns := []struct {
		slug  string
		name  string
		order int
	}{
		{"inbox", "Inbox", 0},
		{"in_progress", "In Progress", 1},
		{"done", "Done", 2},
	}

	for i, expected := range expectedColumns {
		if DefaultColumns[i].Slug != expected.slug {
			t.Errorf("DefaultColumns[%d].Slug = %q, want %q", i, DefaultColumns[i].Slug, expected.slug)
		}
		if DefaultColumns[i].Name != expected.name {
			t.Errorf("DefaultColumns[%d].Name = %q, want %q", i, DefaultColumns[i].Name, expected.name)
		}
		if DefaultColumns[i].Order != expected.order {
			t.Errorf("DefaultColumns[%d].Order = %d, want %d", i, DefaultColumns[i].Order, expected.order)
		}
	}
}
