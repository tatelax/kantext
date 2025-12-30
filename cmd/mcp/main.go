package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"

	"kantext/internal/config"
	"kantext/internal/mcp"
	"kantext/internal/services"
)

func main() {
	// Disable logging to stderr as it interferes with MCP protocol
	log.SetOutput(os.Stderr)

	// Parse command line flags
	configPath := flag.String("config", "", "Path to config.json file")
	flag.Parse()

	if *configPath == "" {
		log.Fatal("config flag is required: -config /path/to/config.json")
	}

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Configuration from config file
	workDir := cfg.WorkingDirectory
	tasksFile := cfg.TasksFile()
	testRunnerConfig := cfg.TestRunner

	// Check if tasks file exists, create if not
	if _, err := os.Stat(tasksFile); os.IsNotExist(err) {
		// Create initial tasks file
		initialContent := `# Kantext Tasks

## Inbox

## In Progress

## Done
`
		if err := os.WriteFile(tasksFile, []byte(initialContent), 0644); err != nil {
			log.Fatalf("Failed to create tasks file: %v", err)
		}
	}

	// Initialize services
	taskStore := services.NewTaskStore(tasksFile)
	testRunner := services.NewTestRunnerWithConfig(workDir, testRunnerConfig)

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
