package services

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"kantext/internal/models"
)

// TestRunner executes Go tests
type TestRunner struct {
	workDir string
}

// NewTestRunner creates a new TestRunner
func NewTestRunner(workDir string) *TestRunner {
	return &TestRunner{
		workDir: workDir,
	}
}

// Run executes a specific test and returns the result
// testFile should be a path relative to the working directory (e.g., "internal/auth/auth_test.go")
func (r *TestRunner) Run(ctx context.Context, testFile, testFunc string) models.TestResult {
	start := time.Now()

	// Extract the directory from the test file path
	// e.g., "internal/auth/auth_test.go" -> "internal/auth"
	testDir := filepath.Dir(testFile)
	if testDir == "." {
		testDir = ""
	}

	// Build the go test command
	// Run specific test function: go test -v -count=1 -run ^TestFuncName$ ./path/to/dir/
	// -count=1 disables test caching to ensure tests always run fresh
	testPath := "./"
	if testDir != "" {
		testPath = "./" + testDir + "/"
	}
	cmd := exec.CommandContext(ctx, "go", "test", "-v", "-count=1", "-run", "^"+testFunc+"$", testPath)

	// Set the working directory if specified
	if r.workDir != "" {
		cmd.Dir = r.workDir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	elapsed := time.Since(start).Milliseconds()

	output := stdout.String()
	if stderr.Len() > 0 {
		output += "\n" + stderr.String()
	}

	result := models.TestResult{
		Output:  output,
		RunTime: elapsed,
	}

	if err != nil {
		// Check if it's a test failure or an execution error
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Exit code 1 typically means test failed
			if exitErr.ExitCode() == 1 {
				result.Passed = false
				// Check if output contains FAIL
				if strings.Contains(output, "FAIL") {
					result.Error = "Test failed"
				} else {
					result.Error = err.Error()
				}
			} else {
				result.Passed = false
				result.Error = err.Error()
			}
		} else {
			result.Passed = false
			result.Error = err.Error()
		}
	} else {
		// Check output for PASS, but treat "no tests to run" as a failure
		if strings.Contains(output, "no tests to run") {
			result.Passed = false
			result.Error = "No matching test found - test file or function may not exist"
		} else {
			result.Passed = strings.Contains(output, "PASS")
		}
	}

	return result
}

// RunAsync runs a test asynchronously and calls the callback with the result
func (r *TestRunner) RunAsync(testFile, testFunc string, callback func(models.TestResult)) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		result := r.Run(ctx, testFile, testFunc)
		callback(result)
	}()
}
