package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"kantext/internal/handlers"
	"kantext/internal/mcp"
	"kantext/internal/services"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	// Check if running in MCP mode (first argument is "mcp")
	if len(os.Args) > 1 && os.Args[0] != "-" && os.Args[1] == "mcp" {
		runMCPServer()
		return
	}

	// Parse command line flags for web server mode
	workDirFlag := flag.String("workdir", "", "Working directory containing TASKS.md (default: current directory)")
	port := flag.String("port", "8081", "Port to run the server on")
	flag.Parse()

	// Determine working directory
	var workDir string
	var err error
	if *workDirFlag != "" {
		workDir = *workDirFlag
		// Expand ~ to home directory if present
		if len(workDir) > 0 && workDir[0] == '~' {
			home, err := os.UserHomeDir()
			if err != nil {
				log.Fatalf("Failed to get home directory: %v", err)
			}
			workDir = filepath.Join(home, workDir[1:])
		}
		// Convert to absolute path if relative
		if !filepath.IsAbs(workDir) {
			workDir, err = filepath.Abs(workDir)
			if err != nil {
				log.Fatalf("Failed to resolve working directory: %v", err)
			}
		}
	} else {
		// Fall back to current working directory
		workDir, err = os.Getwd()
		if err != nil {
			log.Fatalf("Failed to get working directory: %v", err)
		}
		log.Printf("Warning: No -workdir provided, using current directory: %s", workDir)
	}

	tasksFile := filepath.Join(workDir, "TASKS.md")

	// Initialize WebSocket hub (must be before other services)
	wsHub := services.NewWSHub()
	go wsHub.Run()

	// Initialize services
	taskStore := services.NewTaskStore(workDir)
	testRunner := services.NewTestRunnerWithStore(taskStore)
	claudeRunner := services.NewClaudeRunner(wsHub, workDir)

	// Initialize file watcher for real-time updates
	fileWatcher, err := services.NewFileWatcher(tasksFile, wsHub)
	if err != nil {
		log.Fatalf("Failed to initialize file watcher: %v", err)
	}
	// When file changes, reload TaskStore before notifying clients
	fileWatcher.SetOnFileChange(func() {
		log.Println("Reloading TaskStore from file...")
		if err := taskStore.Load(); err != nil {
			log.Printf("Failed to reload tasks: %v", err)
		}
	})
	if err := fileWatcher.Start(); err != nil {
		log.Fatalf("Failed to start file watcher: %v", err)
	}

	// Initialize handlers
	apiHandler := handlers.NewAPIHandler(taskStore, testRunner, claudeRunner)
	wsHandler := handlers.NewWSHandler(wsHub)
	pageHandler, err := handlers.NewPageHandler(taskStore)
	if err != nil {
		log.Fatalf("Failed to initialize page handler: %v", err)
	}

	// Setup router
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)

	// Static files
	fileServer := http.FileServer(http.Dir("web/static"))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	// Page routes
	r.Get("/", pageHandler.ServeBoard)

	// WebSocket endpoint
	r.Get("/ws", wsHandler.ServeWS)

	// API routes
	r.Route("/api", func(r chi.Router) {
		// Config routes
		r.Get("/config", apiHandler.GetConfig)
		r.Put("/config", apiHandler.UpdateConfig)

		// Task routes
		r.Get("/tasks", apiHandler.ListTasks)
		r.Post("/tasks", apiHandler.CreateTask)
		r.Get("/tasks/{id}", apiHandler.GetTask)
		r.Put("/tasks/{id}", apiHandler.UpdateTask)
		r.Delete("/tasks/{id}", apiHandler.DeleteTask)
		r.Post("/tasks/{id}/run", apiHandler.RunTest)
		r.Get("/tasks/{id}/status", apiHandler.GetTaskStatus)
		r.Put("/tasks/{id}/reorder", apiHandler.ReorderTask)

		// Column routes
		r.Get("/columns", apiHandler.ListColumns)
		r.Post("/columns", apiHandler.CreateColumn)
		r.Put("/columns/{slug}", apiHandler.UpdateColumn)
		r.Delete("/columns/{slug}", apiHandler.DeleteColumn)
		r.Put("/columns/reorder", apiHandler.ReorderColumns)

		// AI Queue routes
		r.Get("/ai-queue", apiHandler.GetAIQueue)
		r.Post("/ai-queue", apiHandler.AddToAIQueue)
		r.Delete("/ai-queue/{taskId}", apiHandler.RemoveFromAIQueue)
		r.Put("/ai-queue/reorder", apiHandler.ReorderAIQueue)
		r.Post("/ai-queue/start", apiHandler.StartAITask)
		r.Post("/ai-queue/stop", apiHandler.StopAITask)
		r.Get("/ai-session", apiHandler.GetAISession)
		r.Post("/ai-session/message", apiHandler.SendAIMessage)
	})

	// Create server
	server := &http.Server{
		Addr:         ":" + *port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 5 * time.Minute, // Long timeout for test execution
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		log.Println("Shutting down server...")
		claudeRunner.Stop() // Stop Claude subprocess if running
		fileWatcher.Stop()
		server.Close()
	}()

	// Start server
	fmt.Printf(`
╔════════════════════════════════════════════════════════════════╗
║                       Kantext Web Server                       ║
║    Behavior-Driven Development meets Visual Task Management    ║
╠════════════════════════════════════════════════════════════════╣
║  Server running at: http://localhost:%s
║  WebSocket endpoint: ws://localhost:%s/ws
║  Working directory: %s
║  Tasks file: %s
║  Real-time updates: ENABLED
╚════════════════════════════════════════════════════════════════╝
`, *port, *port, workDir, tasksFile)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}

// runMCPServer runs the MCP server for Claude integration
func runMCPServer() {
	// Parse MCP-specific flags (skip "mcp" argument)
	mcpFlags := flag.NewFlagSet("mcp", flag.ExitOnError)
	workDirFlag := mcpFlags.String("workdir", "", "Working directory containing TASKS.md (required)")
	mcpFlags.Parse(os.Args[2:])

	if *workDirFlag == "" {
		log.Fatal("workdir flag is required: kantext mcp -workdir /path/to/project")
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
	mcpServer := mcp.NewServer()

	// Register handlers
	mcpServer.RegisterHandler("initialize", func(params json.RawMessage) (interface{}, error) {
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

	mcpServer.RegisterHandler("notifications/initialized", func(params json.RawMessage) (interface{}, error) {
		return nil, nil
	})

	mcpServer.RegisterHandler("tools/list", func(params json.RawMessage) (interface{}, error) {
		return mcp.ListToolsResult{
			Tools: toolHandler.GetTools(),
		}, nil
	})

	mcpServer.RegisterHandler("tools/call", func(params json.RawMessage) (interface{}, error) {
		var callParams mcp.CallToolParams
		if err := json.Unmarshal(params, &callParams); err != nil {
			return nil, err
		}
		result := toolHandler.CallTool(callParams.Name, callParams.Arguments)
		return result, nil
	})

	// Run the MCP server
	if err := mcpServer.Run(); err != nil {
		log.Fatalf("MCP Server error: %v", err)
	}
}
