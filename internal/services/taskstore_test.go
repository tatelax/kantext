package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kantext/internal/models"
)

// setupTaskStoreEnv creates a temporary directory with a TASKS.md file for testing
func setupTaskStoreEnv(t *testing.T, tasksContent string) (*TaskStore, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "taskstore-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	tasksPath := filepath.Join(tmpDir, "TASKS.md")
	if err := os.WriteFile(tasksPath, []byte(tasksContent), 0644); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to write TASKS.md: %v", err)
	}

	store := NewTaskStore(tmpDir)
	if err := store.Load(); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to load store: %v", err)
	}

	cleanup := func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}

	return store, cleanup
}

// ============================================================================
// Create Tests
// ============================================================================

func TestTaskStore_Create_Basic(t *testing.T) {
	content := `---
stale_threshold_days: 7
---
# Kantext Tasks

## Inbox
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	req := models.CreateTaskRequest{
		Title: "Test Task",
	}

	task, err := store.Create(req)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if task.Title != "Test Task" {
		t.Errorf("Expected title 'Test Task', got %q", task.Title)
	}
	if task.ID == "" {
		t.Error("Expected non-empty ID")
	}
	if !strings.HasPrefix(task.ID, "task-") {
		t.Errorf("Expected ID to start with 'task-', got %q", task.ID)
	}
	if task.Priority != models.PriorityMedium {
		t.Errorf("Expected default priority 'medium', got %q", task.Priority)
	}
	if task.Column != models.ColumnInbox {
		t.Errorf("Expected column 'inbox', got %q", task.Column)
	}
	if task.TestStatus != models.TestStatusPending {
		t.Errorf("Expected test status 'pending', got %q", task.TestStatus)
	}
}

func TestTaskStore_Create_WithPriority(t *testing.T) {
	content := `---
stale_threshold_days: 7
---
# Kantext Tasks

## Inbox
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	req := models.CreateTaskRequest{
		Title:    "High Priority Task",
		Priority: models.PriorityHigh,
	}

	task, err := store.Create(req)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if task.Priority != models.PriorityHigh {
		t.Errorf("Expected priority 'high', got %q", task.Priority)
	}
}

func TestTaskStore_Create_WithTags(t *testing.T) {
	content := `---
stale_threshold_days: 7
---
# Kantext Tasks

## Inbox
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	req := models.CreateTaskRequest{
		Title: "Tagged Task",
		Tags:  []string{"frontend", "urgent"},
	}

	task, err := store.Create(req)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if len(task.Tags) != 2 {
		t.Errorf("Expected 2 tags, got %d", len(task.Tags))
	}
	if task.Tags[0] != "frontend" || task.Tags[1] != "urgent" {
		t.Errorf("Expected tags [frontend, urgent], got %v", task.Tags)
	}
}

func TestTaskStore_Create_WithAcceptanceCriteria(t *testing.T) {
	content := `---
stale_threshold_days: 7
---
# Kantext Tasks

## Inbox
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	req := models.CreateTaskRequest{
		Title:              "Task with criteria",
		AcceptanceCriteria: "Must do X, Y, and Z",
	}

	task, err := store.Create(req)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if task.AcceptanceCriteria != "Must do X, Y, and Z" {
		t.Errorf("Expected acceptance criteria to be set, got %q", task.AcceptanceCriteria)
	}
}

func TestTaskStore_Create_GeneratesUniqueID(t *testing.T) {
	content := `---
stale_threshold_days: 7
---
# Kantext Tasks

## Inbox
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	ids := make(map[string]bool)
	for i := 0; i < 10; i++ {
		task, err := store.Create(models.CreateTaskRequest{
			Title: "Task",
		})
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}
		if ids[task.ID] {
			t.Errorf("Duplicate ID generated: %s", task.ID)
		}
		ids[task.ID] = true
	}
}

// ============================================================================
// Get Tests
// ============================================================================

func TestTaskStore_Get_Exists(t *testing.T) {
	content := `---
stale_threshold_days: 7
---
# Kantext Tasks

## Inbox

- [ ] Existing Task
  - id: task-abc12345
  - priority: high
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	task, err := store.Get("task-abc12345")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if task.Title != "Existing Task" {
		t.Errorf("Expected title 'Existing Task', got %q", task.Title)
	}
	if task.Priority != models.PriorityHigh {
		t.Errorf("Expected priority 'high', got %q", task.Priority)
	}
}

func TestTaskStore_Get_NotFound(t *testing.T) {
	content := `---
stale_threshold_days: 7
---
# Kantext Tasks

## Inbox
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	_, err := store.Get("nonexistent-id")
	if err == nil {
		t.Error("Expected error for non-existent task")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected 'not found' error, got: %v", err)
	}
}

// ============================================================================
// GetAll Tests
// ============================================================================

func TestTaskStore_GetAll_Empty(t *testing.T) {
	content := `---
stale_threshold_days: 7
---
# Kantext Tasks

## Inbox
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	tasks := store.GetAll()
	if len(tasks) != 0 {
		t.Errorf("Expected 0 tasks, got %d", len(tasks))
	}
}

func TestTaskStore_GetAll_MultipleTasks(t *testing.T) {
	content := `---
stale_threshold_days: 7
---
# Kantext Tasks

## Inbox

- [ ] Task 1
  - id: task-00000001
  - priority: high

- [ ] Task 2
  - id: task-00000002
  - priority: low

## In Progress

- [ ] Task 3
  - id: task-00000003
  - priority: medium
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	tasks := store.GetAll()
	if len(tasks) != 3 {
		t.Errorf("Expected 3 tasks, got %d", len(tasks))
	}
}

// ============================================================================
// Update Tests
// ============================================================================

func TestTaskStore_Update_Title(t *testing.T) {
	content := `---
stale_threshold_days: 7
---
# Kantext Tasks

## Inbox

- [ ] Original Title
  - id: task-update01
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	newTitle := "Updated Title"
	task, err := store.Update("task-update01", models.UpdateTaskRequest{
		Title: &newTitle,
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if task.Title != "Updated Title" {
		t.Errorf("Expected title 'Updated Title', got %q", task.Title)
	}
}

func TestTaskStore_Update_Priority(t *testing.T) {
	content := `---
stale_threshold_days: 7
---
# Kantext Tasks

## Inbox

- [ ] Task
  - id: task-update02
  - priority: low
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	newPriority := models.PriorityHigh
	task, err := store.Update("task-update02", models.UpdateTaskRequest{
		Priority: &newPriority,
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if task.Priority != models.PriorityHigh {
		t.Errorf("Expected priority 'high', got %q", task.Priority)
	}
}

func TestTaskStore_Update_Column(t *testing.T) {
	content := `---
stale_threshold_days: 7
---
# Kantext Tasks

## Inbox

- [ ] Task
  - id: task-update03
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	newColumn := models.ColumnInProgress
	task, err := store.Update("task-update03", models.UpdateTaskRequest{
		Column: &newColumn,
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if task.Column != models.ColumnInProgress {
		t.Errorf("Expected column 'in_progress', got %q", task.Column)
	}
}

func TestTaskStore_Update_Tests(t *testing.T) {
	content := `---
stale_threshold_days: 7
---
# Kantext Tasks

## Inbox

- [ ] Task
  - id: task-update04
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	tests := []models.TestSpec{
		{File: "internal/auth/auth_test.go", Func: "TestLogin"},
		{File: "internal/auth/auth_test.go", Func: "TestLogout"},
	}
	task, err := store.Update("task-update04", models.UpdateTaskRequest{
		Tests: tests,
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if len(task.Tests) != 2 {
		t.Errorf("Expected 2 tests, got %d", len(task.Tests))
	}
}

func TestTaskStore_Update_NotFound(t *testing.T) {
	content := `---
stale_threshold_days: 7
---
# Kantext Tasks

## Inbox
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	newTitle := "New Title"
	_, err := store.Update("nonexistent", models.UpdateTaskRequest{
		Title: &newTitle,
	})
	if err == nil {
		t.Error("Expected error for non-existent task")
	}
}

// ============================================================================
// Delete Tests
// ============================================================================

func TestTaskStore_Delete_Exists(t *testing.T) {
	content := `---
stale_threshold_days: 7
---
# Kantext Tasks

## Inbox

- [ ] Task to delete
  - id: task-delete01
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	err := store.Delete("task-delete01")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify task is gone
	_, err = store.Get("task-delete01")
	if err == nil {
		t.Error("Expected task to be deleted")
	}
}

func TestTaskStore_Delete_NotFound(t *testing.T) {
	content := `---
stale_threshold_days: 7
---
# Kantext Tasks

## Inbox
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	err := store.Delete("nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent task")
	}
}

// ============================================================================
// Load/Parse Tests
// ============================================================================

func TestTaskStore_Load_EmptyFile(t *testing.T) {
	content := ``
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	tasks := store.GetAll()
	if len(tasks) != 0 {
		t.Errorf("Expected 0 tasks from empty file, got %d", len(tasks))
	}
}

func TestTaskStore_Load_WithYAMLFrontMatter(t *testing.T) {
	content := `---
stale_threshold_days: 14
test_runner:
  command: pytest -v {testPath}::{testFunc}
  pass_string: passed
  fail_string: failed
---
# Kantext Tasks

## Inbox
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	settings := store.GetSettings()
	if settings.GetStaleThresholdDays() != 14 {
		t.Errorf("Expected stale_threshold_days 14, got %d", settings.GetStaleThresholdDays())
	}
	if settings.GetTestCommand() != "pytest -v {testPath}::{testFunc}" {
		t.Errorf("Expected custom test command, got %q", settings.GetTestCommand())
	}
	if settings.GetPassString() != "passed" {
		t.Errorf("Expected pass_string 'passed', got %q", settings.GetPassString())
	}
	if settings.GetFailString() != "failed" {
		t.Errorf("Expected fail_string 'failed', got %q", settings.GetFailString())
	}
}

func TestTaskStore_Load_WithTestSpecs(t *testing.T) {
	content := `---
stale_threshold_days: 7
---
# Kantext Tasks

## Inbox

- [ ] Task with tests
  - id: task-tests001
  - priority: high
  - test: internal/auth/auth_test.go:TestLogin
  - test: internal/auth/auth_test.go:TestLogout
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	task, err := store.Get("task-tests001")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if len(task.Tests) != 2 {
		t.Errorf("Expected 2 tests, got %d", len(task.Tests))
	}
	if task.Tests[0].File != "internal/auth/auth_test.go" {
		t.Errorf("Expected test file 'internal/auth/auth_test.go', got %q", task.Tests[0].File)
	}
	if task.Tests[0].Func != "TestLogin" {
		t.Errorf("Expected test func 'TestLogin', got %q", task.Tests[0].Func)
	}
}

func TestTaskStore_Load_WithTags(t *testing.T) {
	content := `---
stale_threshold_days: 7
---
# Kantext Tasks

## Inbox

- [ ] Task with tags
  - id: task-tags0001
  - tags: frontend, urgent, bug
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	task, err := store.Get("task-tags0001")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if len(task.Tags) != 3 {
		t.Errorf("Expected 3 tags, got %d", len(task.Tags))
	}
}

func TestTaskStore_Load_WithAcceptanceCriteria(t *testing.T) {
	content := `---
stale_threshold_days: 7
---
# Kantext Tasks

## Inbox

- [ ] Task with criteria
  - id: task-crit0001
  - criteria: Must implement X and Y
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	task, err := store.Get("task-crit0001")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if task.AcceptanceCriteria != "Must implement X and Y" {
		t.Errorf("Expected acceptance criteria, got %q", task.AcceptanceCriteria)
	}
}

func TestTaskStore_Load_MultipleColumns(t *testing.T) {
	content := `---
stale_threshold_days: 7
---
# Kantext Tasks

## Inbox

- [ ] Task 1
  - id: task-col00001

## In Progress

- [ ] Task 2
  - id: task-col00002

## Done

- [x] Task 3
  - id: task-col00003
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	task1, _ := store.Get("task-col00001")
	task2, _ := store.Get("task-col00002")
	task3, _ := store.Get("task-col00003")

	if task1.Column != models.ColumnInbox {
		t.Errorf("Expected task1 in inbox, got %q", task1.Column)
	}
	if task2.Column != models.ColumnInProgress {
		t.Errorf("Expected task2 in in_progress, got %q", task2.Column)
	}
	if task3.Column != models.ColumnDone {
		t.Errorf("Expected task3 in done, got %q", task3.Column)
	}
}

// ============================================================================
// Column Tests
// ============================================================================

func TestTaskStore_GetColumns(t *testing.T) {
	content := `---
stale_threshold_days: 7
---
# Kantext Tasks

## Inbox

## In Progress

## Done
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	columns := store.GetColumns()
	if len(columns) < 3 {
		t.Errorf("Expected at least 3 columns, got %d", len(columns))
	}

	// Check default columns exist
	slugs := make(map[string]bool)
	for _, col := range columns {
		slugs[col.Slug] = true
	}

	if !slugs["inbox"] {
		t.Error("Expected inbox column")
	}
	if !slugs["in_progress"] {
		t.Error("Expected in_progress column")
	}
	if !slugs["done"] {
		t.Error("Expected done column")
	}
}

func TestTaskStore_CreateColumn(t *testing.T) {
	content := `---
stale_threshold_days: 7
---
# Kantext Tasks

## Inbox

## In Progress

## Done
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	col, err := store.CreateColumn("In Review")
	if err != nil {
		t.Fatalf("CreateColumn failed: %v", err)
	}

	if col.Name != "In Review" {
		t.Errorf("Expected name 'In Review', got %q", col.Name)
	}
	if col.Slug != "in_review" {
		t.Errorf("Expected slug 'in_review', got %q", col.Slug)
	}
}

func TestTaskStore_DeleteColumn_Custom(t *testing.T) {
	content := `---
stale_threshold_days: 7
---
# Kantext Tasks

## Inbox

## In Review

## In Progress

## Done
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	err := store.DeleteColumn("in_review")
	if err != nil {
		t.Fatalf("DeleteColumn failed: %v", err)
	}

	// Verify column is gone
	columns := store.GetColumns()
	for _, col := range columns {
		if col.Slug == "in_review" {
			t.Error("Expected in_review column to be deleted")
		}
	}
}

func TestTaskStore_DeleteColumn_DefaultProtected(t *testing.T) {
	content := `---
stale_threshold_days: 7
---
# Kantext Tasks

## Inbox

## In Progress

## Done
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	// Should not be able to delete default columns
	err := store.DeleteColumn("inbox")
	if err == nil {
		t.Error("Expected error when deleting default column 'inbox'")
	}

	err = store.DeleteColumn("in_progress")
	if err == nil {
		t.Error("Expected error when deleting default column 'in_progress'")
	}

	err = store.DeleteColumn("done")
	if err == nil {
		t.Error("Expected error when deleting default column 'done'")
	}
}

// ============================================================================
// Settings Tests
// ============================================================================

func TestTaskStore_Settings_Defaults(t *testing.T) {
	content := `# Kantext Tasks

## Inbox
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	settings := store.GetSettings()

	if settings.GetStaleThresholdDays() != DefaultStaleThresholdDays {
		t.Errorf("Expected default stale threshold %d, got %d",
			DefaultStaleThresholdDays, settings.GetStaleThresholdDays())
	}
	if settings.GetTestCommand() != DefaultTestCommand {
		t.Errorf("Expected default test command, got %q", settings.GetTestCommand())
	}
	if settings.GetPassString() != DefaultPassString {
		t.Errorf("Expected default pass string, got %q", settings.GetPassString())
	}
	if settings.GetFailString() != DefaultFailString {
		t.Errorf("Expected default fail string, got %q", settings.GetFailString())
	}
	if settings.GetNoTestsString() != DefaultNoTestsString {
		t.Errorf("Expected default no tests string, got %q", settings.GetNoTestsString())
	}
}

func TestTaskStore_WorkingDir(t *testing.T) {
	content := `# Kantext Tasks

## Inbox
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	workDir := store.GetWorkingDir()
	if workDir == "" {
		t.Error("Expected non-empty working directory")
	}
	if _, err := os.Stat(workDir); os.IsNotExist(err) {
		t.Error("Expected working directory to exist")
	}
}

// ============================================================================
// Persistence Tests
// ============================================================================

func TestTaskStore_Save_RoundTrip(t *testing.T) {
	content := `---
stale_threshold_days: 7
---
# Kantext Tasks

## Inbox
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	// Create a task
	task, err := store.Create(models.CreateTaskRequest{
		Title:              "Persisted Task",
		Priority:           models.PriorityHigh,
		AcceptanceCriteria: "Must work correctly",
		Tags:               []string{"test", "important"},
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Wait a bit for async save
	time.Sleep(100 * time.Millisecond)

	// Create a new store from the same file
	store2 := NewTaskStore(store.GetWorkingDir())
	if err := store2.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	defer store2.Close()

	// Verify task was persisted
	reloaded, err := store2.Get(task.ID)
	if err != nil {
		t.Fatalf("Get after reload failed: %v", err)
	}

	if reloaded.Title != "Persisted Task" {
		t.Errorf("Expected title 'Persisted Task', got %q", reloaded.Title)
	}
	if reloaded.Priority != models.PriorityHigh {
		t.Errorf("Expected priority 'high', got %q", reloaded.Priority)
	}
	if reloaded.AcceptanceCriteria != "Must work correctly" {
		t.Errorf("Expected acceptance criteria, got %q", reloaded.AcceptanceCriteria)
	}
	if len(reloaded.Tags) != 2 {
		t.Errorf("Expected 2 tags, got %d", len(reloaded.Tags))
	}
}

func TestTaskStore_Delete_Persists(t *testing.T) {
	content := `---
stale_threshold_days: 7
---
# Kantext Tasks

## Inbox

- [ ] Task to delete
  - id: task-persist1
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	// Delete the task
	err := store.Delete("task-persist1")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Wait for async save
	time.Sleep(100 * time.Millisecond)

	// Reload and verify deletion persisted
	store2 := NewTaskStore(store.GetWorkingDir())
	if err := store2.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	defer store2.Close()

	_, err = store2.Get("task-persist1")
	if err == nil {
		t.Error("Expected task to be deleted after reload")
	}
}

// ============================================================================
// Test Status Tests
// ============================================================================

func TestTaskStore_TestStatus_Parsing(t *testing.T) {
	content := `---
stale_threshold_days: 7
---
# Kantext Tasks

## Inbox

- [ ] Pending task
  - id: task-status01

- [-] Failed task
  - id: task-status02
  - test_status: failed

- [x] Passed task
  - id: task-status03
  - test_status: passed
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	pending, _ := store.Get("task-status01")
	failed, _ := store.Get("task-status02")
	passed, _ := store.Get("task-status03")

	// Note: The checkbox char determines display, test_status metadata determines actual status
	if passed.TestStatus != models.TestStatusPassed {
		t.Errorf("Expected passed test status, got %q", passed.TestStatus)
	}
	if failed.TestStatus != models.TestStatusFailed {
		t.Errorf("Expected failed test status, got %q", failed.TestStatus)
	}
	if pending.TestStatus != models.TestStatusPending {
		t.Errorf("Expected pending test status, got %q", pending.TestStatus)
	}
}

// ============================================================================
// RequiresTest Tests
// ============================================================================

func TestTaskStore_RequiresTest_Parsing(t *testing.T) {
	content := `---
stale_threshold_days: 7
---
# Kantext Tasks

## Inbox

- [ ] Task requiring test
  - id: task-reqtest1
  - requires_test: true

- [ ] Task not requiring test
  - id: task-reqtest2
  - requires_test: false

- [ ] Task with default
  - id: task-reqtest3
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	task1, _ := store.Get("task-reqtest1")
	task2, _ := store.Get("task-reqtest2")
	task3, _ := store.Get("task-reqtest3")

	if !task1.RequiresTest {
		t.Error("Expected task1 to require test")
	}
	if task2.RequiresTest {
		t.Error("Expected task2 to not require test")
	}
	if task3.RequiresTest {
		t.Error("Expected task3 to default to not requiring test")
	}
}

func TestTaskStore_Create_WithRequiresTest(t *testing.T) {
	content := `---
stale_threshold_days: 7
---
# Kantext Tasks

## Inbox
`
	store, cleanup := setupTaskStoreEnv(t, content)
	defer cleanup()

	requiresTest := true
	task, err := store.Create(models.CreateTaskRequest{
		Title:        "TDD Task",
		RequiresTest: &requiresTest,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if !task.RequiresTest {
		t.Error("Expected task to require test")
	}
}
