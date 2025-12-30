package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// TestRunnerConfig holds configuration for the test runner
type TestRunnerConfig struct {
	// Command is the command template to run tests
	// Placeholders: {testFunc} = test function name, {testPath} = test file directory
	// Default: "go test -v -count=1 -run ^{testFunc}$ {testPath}"
	Command string `json:"command,omitempty"`
	// PassString is the string to look for in output to determine if test passed
	// Default: "PASS"
	PassString string `json:"pass_string,omitempty"`
	// FailString is the string to look for in output to determine if test failed
	// Default: "FAIL"
	FailString string `json:"fail_string,omitempty"`
	// NoTestsString is the string that indicates no tests were found
	// Default: "no tests to run"
	NoTestsString string `json:"no_tests_string,omitempty"`
}

// Config holds the application configuration
type Config struct {
	// WorkingDirectory is the base directory for the tasks file and tests/
	WorkingDirectory string `json:"working_directory"`
	// TasksFileName is the name of the tasks file (default: TASKS.md)
	TasksFileName string `json:"tasks_file,omitempty"`
	// TestRunner holds test runner configuration
	TestRunner TestRunnerConfig `json:"test_runner,omitempty"`
}

// Load reads configuration from a JSON file
func Load(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Expand ~ to home directory if present
	if len(cfg.WorkingDirectory) > 0 && cfg.WorkingDirectory[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		cfg.WorkingDirectory = filepath.Join(home, cfg.WorkingDirectory[1:])
	}

	// Convert to absolute path if relative
	if !filepath.IsAbs(cfg.WorkingDirectory) {
		absPath, err := filepath.Abs(cfg.WorkingDirectory)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve working directory: %w", err)
		}
		cfg.WorkingDirectory = absPath
	}

	return &cfg, nil
}

// DefaultTasksFileName is the default name for the tasks file
const DefaultTasksFileName = "TASKS.md"

// Default test runner configuration values
const (
	DefaultTestCommand    = "go test -v -count=1 -run ^{testFunc}$ {testPath}"
	DefaultPassString     = "PASS"
	DefaultFailString     = "FAIL"
	DefaultNoTestsString  = "no tests to run"
)

// GetCommand returns the test command, or the default if not set
func (c *TestRunnerConfig) GetCommand() string {
	if c.Command == "" {
		return DefaultTestCommand
	}
	return c.Command
}

// GetPassString returns the pass string, or the default if not set
func (c *TestRunnerConfig) GetPassString() string {
	if c.PassString == "" {
		return DefaultPassString
	}
	return c.PassString
}

// GetFailString returns the fail string, or the default if not set
func (c *TestRunnerConfig) GetFailString() string {
	if c.FailString == "" {
		return DefaultFailString
	}
	return c.FailString
}

// GetNoTestsString returns the no tests string, or the default if not set
func (c *TestRunnerConfig) GetNoTestsString() string {
	if c.NoTestsString == "" {
		return DefaultNoTestsString
	}
	return c.NoTestsString
}

// TasksFile returns the path to the tasks file
func (c *Config) TasksFile() string {
	fileName := c.TasksFileName
	if fileName == "" {
		fileName = DefaultTasksFileName
	}
	return filepath.Join(c.WorkingDirectory, fileName)
}
