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
	testsDir string
	workDir  string
}

// NewTestRunner creates a new TestRunner
func NewTestRunner(testsDir string, workDir string) *TestRunner {
	return &TestRunner{
		testsDir: testsDir,
		workDir:  workDir,
	}
}

// Run executes a specific test and returns the result
func (r *TestRunner) Run(ctx context.Context, testFile, testFunc string) models.TestResult {
	start := time.Now()

	// Build the go test command
	// Run specific test function: go test -v -count=1 -run ^TestFuncName$ ./tests/
	// -count=1 disables test caching to ensure tests always run fresh
	cmd := exec.CommandContext(ctx, "go", "test", "-v", "-count=1", "-run", "^"+testFunc+"$", "./"+r.testsDir+"/")

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

// GetTestsDir returns the absolute path to the tests directory
func (r *TestRunner) GetTestsDir() string {
	abs, err := filepath.Abs(r.testsDir)
	if err != nil {
		return r.testsDir
	}
	return abs
}
