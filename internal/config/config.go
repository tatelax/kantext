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
	// OriginalWorkingDirectory preserves the original value from config for saving
	OriginalWorkingDirectory string `json:"-"`
	// TasksFileName is the name of the tasks file (default: TASKS.md)
	TasksFileName string `json:"tasks_file,omitempty"`
	// StaleThresholdDays is the number of days after which a task is considered stale
	// Default: 7 (one week)
	StaleThresholdDays int `json:"stale_threshold_days,omitempty"`
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

	// Store original working directory before expansion
	cfg.OriginalWorkingDirectory = cfg.WorkingDirectory

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

// DefaultStaleThresholdDays is the default number of days for a task to be considered stale
const DefaultStaleThresholdDays = 7

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

// GetStaleThresholdDays returns the stale threshold in days, or the default if not set
func (c *Config) GetStaleThresholdDays() int {
	if c.StaleThresholdDays <= 0 {
		return DefaultStaleThresholdDays
	}
	return c.StaleThresholdDays
}

// Save writes the configuration to a JSON file
func (c *Config) Save(configPath string) error {
	// Create a copy for saving with the original working directory
	saveConfig := struct {
		WorkingDirectory   string           `json:"working_directory"`
		TasksFileName      string           `json:"tasks_file,omitempty"`
		StaleThresholdDays int              `json:"stale_threshold_days,omitempty"`
		TestRunner         TestRunnerConfig `json:"test_runner,omitempty"`
	}{
		WorkingDirectory:   c.OriginalWorkingDirectory,
		TasksFileName:      c.TasksFileName,
		StaleThresholdDays: c.StaleThresholdDays,
		TestRunner:         c.TestRunner,
	}

	data, err := json.MarshalIndent(saveConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
