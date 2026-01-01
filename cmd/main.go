package main

import (
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
	"kantext/internal/services"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	// Parse command line flags
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
	apiHandler := handlers.NewAPIHandler(taskStore, testRunner)
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
