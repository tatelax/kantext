package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"
	"path/filepath"

	"kantext/internal/mcp"
	"kantext/internal/services"
)

func main() {
	// Disable logging to stderr as it interferes with MCP protocol
	log.SetOutput(os.Stderr)

	// Parse command line flags
	workDirFlag := flag.String("workdir", "", "Working directory containing TASKS.md (required)")
	flag.Parse()

	if *workDirFlag == "" {
		log.Fatal("workdir flag is required: -workdir /path/to/project")
	}

	// Expand ~ to home directory if present
	workDir := *workDirFlag
	if len(workDir) > 0 && workDir[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("Failed to get home directory: %v", err)
		}
		workDir = filepath.Join(home, workDir[1:])
	}

	// Convert to absolute path if relative
	if !filepath.IsAbs(workDir) {
		absPath, err := filepath.Abs(workDir)
		if err != nil {
			log.Fatalf("Failed to resolve working directory: %v", err)
		}
		workDir = absPath
	}

	tasksFile := filepath.Join(workDir, "TASKS.md")

	// Check if tasks file exists, create if not
	if _, err := os.Stat(tasksFile); os.IsNotExist(err) {
		// Create initial tasks file with YAML front matter
		initialContent := `---
---
# Kantext Tasks

## Inbox

## In Progress

## Done
`
		if err := os.WriteFile(tasksFile, []byte(initialContent), 0644); err != nil {
			log.Fatalf("Failed to create tasks file: %v", err)
		}
	}

	// Initialize services
	taskStore := services.NewTaskStore(workDir)
	testRunner := services.NewTestRunnerWithStore(taskStore)

	// Initialize tool handler
	toolHandler := mcp.NewToolHandler(taskStore, testRunner)

	// Create MCP server
	server := mcp.NewServer()

	// Register handlers
	server.RegisterHandler("initialize", func(params json.RawMessage) (interface{}, error) {
		return mcp.InitializeResult{
			ProtocolVersion: "2024-11-05",
			Capabilities: mcp.ServerCapabilities{
				Tools: &mcp.ToolsCapability{},
			},
			ServerInfo: mcp.ServerInfo{
				Name:    "kantext",
				Version: "1.0.0",
			},
		}, nil
	})

	server.RegisterHandler("notifications/initialized", func(params json.RawMessage) (interface{}, error) {
		// This is a notification, no response needed
		return nil, nil
	})

	server.RegisterHandler("tools/list", func(params json.RawMessage) (interface{}, error) {
		return mcp.ListToolsResult{
			Tools: toolHandler.GetTools(),
		}, nil
	})

	server.RegisterHandler("tools/call", func(params json.RawMessage) (interface{}, error) {
		var callParams mcp.CallToolParams
		if err := json.Unmarshal(params, &callParams); err != nil {
			return nil, err
		}

		result := toolHandler.CallTool(callParams.Name, callParams.Arguments)
		return result, nil
	})

	// Run the server
	if err := server.Run(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
