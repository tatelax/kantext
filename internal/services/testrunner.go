package services

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"kantext/internal/config"
	"kantext/internal/models"
)

// TestRunner executes tests using configurable commands
type TestRunner struct {
	workDir string
	config  config.TestRunnerConfig
}

// NewTestRunner creates a new TestRunner with default configuration
func NewTestRunner(workDir string) *TestRunner {
	return &TestRunner{
		workDir: workDir,
		config:  config.TestRunnerConfig{},
	}
}

// NewTestRunnerWithConfig creates a new TestRunner with custom configuration
func NewTestRunnerWithConfig(workDir string, cfg config.TestRunnerConfig) *TestRunner {
	return &TestRunner{
		workDir: workDir,
		config:  cfg,
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

	// Build the test path
	testPath := "./"
	if testDir != "" {
		testPath = "./" + testDir + "/"
	}

	// Build the command from config template
	// Replace placeholders: {testFunc} and {testPath}
	cmdStr := r.config.GetCommand()
	cmdStr = strings.ReplaceAll(cmdStr, "{testFunc}", testFunc)
	cmdStr = strings.ReplaceAll(cmdStr, "{testPath}", testPath)

	// Split command into parts for exec
	// Use shell to handle the command properly
	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)

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

	// Get configurable strings
	passString := r.config.GetPassString()
	failString := r.config.GetFailString()
	noTestsString := r.config.GetNoTestsString()

	if err != nil {
		// Check if it's a test failure or an execution error
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Exit code 1 typically means test failed
			if exitErr.ExitCode() == 1 {
				result.Passed = false
				// Check if output contains the fail string
				if strings.Contains(output, failString) {
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
		// Check output for pass string, but treat "no tests to run" as a failure
		if strings.Contains(output, noTestsString) {
			result.Passed = false
			result.Error = "No matching test found - test file or function may not exist"
		} else {
			result.Passed = strings.Contains(output, passString)
		}
	}

	return result
}

// RunAll executes all tests in the given array and returns aggregated results
// All tests must pass for AllPassed to be true
func (r *TestRunner) RunAll(ctx context.Context, tests []models.TestSpec) models.TestResults {
	start := time.Now()

	results := models.TestResults{
		AllPassed: true,
		Results:   make([]models.TestResult, 0, len(tests)),
	}

	for _, test := range tests {
		result := r.Run(ctx, test.File, test.Func)
		results.Results = append(results.Results, result)
		if !result.Passed {
			results.AllPassed = false
		}
	}

	results.TotalTime = time.Since(start).Milliseconds()
	return results
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
