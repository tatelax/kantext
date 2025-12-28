package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config holds the application configuration
type Config struct {
	// WorkingDirectory is the base directory for the tasks file and tests/
	WorkingDirectory string `json:"working_directory"`
	// TasksFileName is the name of the tasks file (default: TASKS.md)
	TasksFileName string `json:"tasks_file,omitempty"`
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

// TasksFile returns the path to the tasks file
func (c *Config) TasksFile() string {
	fileName := c.TasksFileName
	if fileName == "" {
		fileName = DefaultTasksFileName
	}
	return filepath.Join(c.WorkingDirectory, fileName)
}

// TestsDir returns the path to the tests directory
func (c *Config) TestsDir() string {
	return filepath.Join(c.WorkingDirectory, "tests")
}
