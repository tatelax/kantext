package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kantext/internal/models"
)

// setupTestRunnerEnv creates a temporary directory with a TASKS.md file for testing
func setupTestRunnerEnv(t *testing.T, tasksContent string) (*TaskStore, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "testrunner-test-*")
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

// TestTestRunner_Run_PassingTest tests running a command that outputs PASS
func TestTestRunner_Run_PassingTest(t *testing.T) {
	// Create a TASKS.md with custom test runner that uses echo
	content := `---
test_runner:
  command: echo "PASS"
  pass_string: "PASS"
  fail_string: "FAIL"
---
# Kantext Tasks

## Inbox
`
	store, cleanup := setupTestRunnerEnv(t, content)
	defer cleanup()

	runner := NewTestRunnerWithStore(store)
	ctx := context.Background()

	result := runner.Run(ctx, "dummy_test.go", "TestDummy")

	if !result.Passed {
		t.Errorf("Expected test to pass, but it failed: %v", result.Error)
	}
	if !strings.Contains(result.Output, "PASS") {
		t.Errorf("Expected output to contain 'PASS', got: %s", result.Output)
	}
	if result.RunTime < 0 {
		t.Errorf("Expected non-negative runtime, got: %d", result.RunTime)
	}
}

// TestTestRunner_Run_FailingTest tests running a command that outputs FAIL
func TestTestRunner_Run_FailingTest(t *testing.T) {
	content := `---
test_runner:
  command: sh -c 'echo "FAIL" && exit 1'
  pass_string: "PASS"
  fail_string: "FAIL"
---
# Kantext Tasks

## Inbox
`
	store, cleanup := setupTestRunnerEnv(t, content)
	defer cleanup()

	runner := NewTestRunnerWithStore(store)
	ctx := context.Background()

	result := runner.Run(ctx, "dummy_test.go", "TestDummy")

	if result.Passed {
		t.Error("Expected test to fail, but it passed")
	}
	if !strings.Contains(result.Output, "FAIL") {
		t.Errorf("Expected output to contain 'FAIL', got: %s", result.Output)
	}
}

// TestTestRunner_Run_NoTestsFound tests handling of "no tests to run" output
func TestTestRunner_Run_NoTestsFound(t *testing.T) {
	content := `---
test_runner:
  command: "echo no tests to run"
  pass_string: PASS
  fail_string: FAIL
  no_tests_string: "no tests to run"
---
# Kantext Tasks

## Inbox
`
	store, cleanup := setupTestRunnerEnv(t, content)
	defer cleanup()

	runner := NewTestRunnerWithStore(store)
	ctx := context.Background()

	result := runner.Run(ctx, "dummy_test.go", "TestDummy")

	if result.Passed {
		t.Error("Expected test to fail when no tests found, but it passed")
	}
	if !strings.Contains(result.Error, "No matching test found") {
		t.Errorf("Expected error about no matching test, got: %s", result.Error)
	}
}

// TestTestRunner_Run_CommandNotFound tests handling of non-existent commands
func TestTestRunner_Run_CommandNotFound(t *testing.T) {
	content := `---
test_runner:
  command: nonexistent_command_xyz123
---
# Kantext Tasks

## Inbox
`
	store, cleanup := setupTestRunnerEnv(t, content)
	defer cleanup()

	runner := NewTestRunnerWithStore(store)
	ctx := context.Background()

	result := runner.Run(ctx, "dummy_test.go", "TestDummy")

	if result.Passed {
		t.Error("Expected test to fail when command not found, but it passed")
	}
	if result.Error == "" {
		t.Error("Expected error message when command not found")
	}
}

// TestTestRunner_Run_Timeout tests handling of timeout
func TestTestRunner_Run_Timeout(t *testing.T) {
	content := `---
test_runner:
  command: sleep 10
---
# Kantext Tasks

## Inbox
`
	store, cleanup := setupTestRunnerEnv(t, content)
	defer cleanup()

	runner := NewTestRunnerWithStore(store)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result := runner.Run(ctx, "dummy_test.go", "TestDummy")

	if result.Passed {
		t.Error("Expected test to fail on timeout, but it passed")
	}
	// Check that we got an error (could be context deadline exceeded or killed)
	if result.Error == "" {
		t.Error("Expected error message on timeout")
	}
}

// TestTestRunner_Run_CommandTemplateSubstitution tests {testFunc} and {testPath} replacement
func TestTestRunner_Run_CommandTemplateSubstitution(t *testing.T) {
	content := `---
test_runner:
  command: echo "Running {testFunc} in {testPath}"
  pass_string: "PASS"
---
# Kantext Tasks

## Inbox
`
	store, cleanup := setupTestRunnerEnv(t, content)
	defer cleanup()

	runner := NewTestRunnerWithStore(store)
	ctx := context.Background()

	result := runner.Run(ctx, "internal/auth/auth_test.go", "TestLogin")

	// Check that placeholders were substituted
	if !strings.Contains(result.Output, "TestLogin") {
		t.Errorf("Expected output to contain 'TestLogin', got: %s", result.Output)
	}
	if !strings.Contains(result.Output, "./internal/auth/") {
		t.Errorf("Expected output to contain './internal/auth/', got: %s", result.Output)
	}
}

// TestTestRunner_Run_DefaultSettings tests using default settings when none specified
func TestTestRunner_Run_DefaultSettings(t *testing.T) {
	// Minimal TASKS.md with no test_runner settings
	content := `---
stale_threshold_days: 7
---
# Kantext Tasks

## Inbox
`
	store, cleanup := setupTestRunnerEnv(t, content)
	defer cleanup()

	// Verify default settings are used
	settings := store.GetSettings()
	if settings.GetTestCommand() != DefaultTestCommand {
		t.Errorf("Expected default test command %q, got %q", DefaultTestCommand, settings.GetTestCommand())
	}
	if settings.GetPassString() != DefaultPassString {
		t.Errorf("Expected default pass string %q, got %q", DefaultPassString, settings.GetPassString())
	}
	if settings.GetFailString() != DefaultFailString {
		t.Errorf("Expected default fail string %q, got %q", DefaultFailString, settings.GetFailString())
	}
	if settings.GetNoTestsString() != DefaultNoTestsString {
		t.Errorf("Expected default no tests string %q, got %q", DefaultNoTestsString, settings.GetNoTestsString())
	}
}

// TestTestRunner_RunAll_AllPass tests running multiple tests that all pass
func TestTestRunner_RunAll_AllPass(t *testing.T) {
	content := `---
test_runner:
  command: echo "PASS"
  pass_string: "PASS"
---
# Kantext Tasks

## Inbox
`
	store, cleanup := setupTestRunnerEnv(t, content)
	defer cleanup()

	runner := NewTestRunnerWithStore(store)
	ctx := context.Background()

	tests := []models.TestSpec{
		{File: "test1.go", Func: "Test1"},
		{File: "test2.go", Func: "Test2"},
		{File: "test3.go", Func: "Test3"},
	}

	results := runner.RunAll(ctx, tests)

	if !results.AllPassed {
		t.Error("Expected all tests to pass")
	}
	if len(results.Results) != 3 {
		t.Errorf("Expected 3 results, got %d", len(results.Results))
	}
	for i, r := range results.Results {
		if !r.Passed {
			t.Errorf("Test %d failed unexpectedly: %v", i, r.Error)
		}
	}
	if results.TotalTime < 0 {
		t.Errorf("Expected non-negative total time, got: %d", results.TotalTime)
	}
}

// TestTestRunner_RunAll_SomeFail tests running multiple tests where some fail
func TestTestRunner_RunAll_SomeFail(t *testing.T) {
	// Create a TASKS.md with a command that passes or fails based on test name
	content := `---
test_runner:
  command: sh -c 'if [ "{testFunc}" = "TestFail" ]; then echo FAIL && exit 1; else echo PASS; fi'
  pass_string: "PASS"
  fail_string: "FAIL"
---
# Kantext Tasks

## Inbox
`
	store, cleanup := setupTestRunnerEnv(t, content)
	defer cleanup()

	runner := NewTestRunnerWithStore(store)
	ctx := context.Background()

	tests := []models.TestSpec{
		{File: "test1.go", Func: "TestPass"},
		{File: "test2.go", Func: "TestFail"},
		{File: "test3.go", Func: "TestPass2"},
	}

	results := runner.RunAll(ctx, tests)

	if results.AllPassed {
		t.Error("Expected AllPassed to be false when some tests fail")
	}
	if len(results.Results) != 3 {
		t.Errorf("Expected 3 results, got %d", len(results.Results))
	}

	// First test should pass
	if !results.Results[0].Passed {
		t.Error("Expected first test to pass")
	}
	// Second test should fail
	if results.Results[1].Passed {
		t.Error("Expected second test to fail")
	}
	// Third test should pass
	if !results.Results[2].Passed {
		t.Error("Expected third test to pass")
	}
}

// TestTestRunner_RunAll_Empty tests running with empty test slice
func TestTestRunner_RunAll_Empty(t *testing.T) {
	content := `---
test_runner:
  command: echo "PASS"
---
# Kantext Tasks

## Inbox
`
	store, cleanup := setupTestRunnerEnv(t, content)
	defer cleanup()

	runner := NewTestRunnerWithStore(store)
	ctx := context.Background()

	results := runner.RunAll(ctx, []models.TestSpec{})

	if !results.AllPassed {
		t.Error("Expected AllPassed to be true for empty test slice")
	}
	if len(results.Results) != 0 {
		t.Errorf("Expected 0 results, got %d", len(results.Results))
	}
}

// TestTestRunner_Run_TestPathExtraction tests correct path extraction from test file
func TestTestRunner_Run_TestPathExtraction(t *testing.T) {
	tests := []struct {
		name         string
		testFile     string
		expectedPath string
	}{
		{"root file", "main_test.go", "./"},
		{"single dir", "pkg/pkg_test.go", "./pkg/"},
		{"nested dirs", "internal/auth/auth_test.go", "./internal/auth/"},
		{"deep nesting", "a/b/c/d/e_test.go", "./a/b/c/d/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := `---
test_runner:
  command: echo "path={testPath}"
  pass_string: "PASS"
---
# Kantext Tasks

## Inbox
`
			store, cleanup := setupTestRunnerEnv(t, content)
			defer cleanup()

			runner := NewTestRunnerWithStore(store)
			ctx := context.Background()

			result := runner.Run(ctx, tt.testFile, "TestFunc")

			if !strings.Contains(result.Output, "path="+tt.expectedPath) {
				t.Errorf("Expected path %q in output, got: %s", tt.expectedPath, result.Output)
			}
		})
	}
}

// TestTestRunner_Run_CustomPassFailStrings tests custom pass/fail detection strings
func TestTestRunner_Run_CustomPassFailStrings(t *testing.T) {
	content := `---
test_runner:
  command: echo "SUCCESS"
  pass_string: "SUCCESS"
  fail_string: "FAILURE"
---
# Kantext Tasks

## Inbox
`
	store, cleanup := setupTestRunnerEnv(t, content)
	defer cleanup()

	runner := NewTestRunnerWithStore(store)
	ctx := context.Background()

	result := runner.Run(ctx, "test.go", "TestFunc")

	if !result.Passed {
		t.Errorf("Expected test to pass with custom 'SUCCESS' string, got error: %v", result.Error)
	}
}

// TestTestRunner_Run_StderrCapture tests that stderr is captured in output
func TestTestRunner_Run_StderrCapture(t *testing.T) {
	content := `---
test_runner:
  command: sh -c 'echo "stdout message" && echo "stderr message" >&2 && echo "PASS"'
  pass_string: "PASS"
---
# Kantext Tasks

## Inbox
`
	store, cleanup := setupTestRunnerEnv(t, content)
	defer cleanup()

	runner := NewTestRunnerWithStore(store)
	ctx := context.Background()

	result := runner.Run(ctx, "test.go", "TestFunc")

	if !strings.Contains(result.Output, "stdout message") {
		t.Error("Expected stdout to be captured")
	}
	if !strings.Contains(result.Output, "stderr message") {
		t.Error("Expected stderr to be captured")
	}
}
