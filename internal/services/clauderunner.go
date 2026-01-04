package services

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// AI streaming message types
const (
	MsgTypeAIOutput       = "ai_output"
	MsgTypeAIStarted      = "ai_started"
	MsgTypeAIStopped      = "ai_stopped"
	MsgTypeAIQueueUpdated = "ai_queue_updated"
)

// AIOutputMessage represents a streaming output message from Claude
type AIOutputMessage struct {
	TaskID    string    `json:"task_id"`
	Content   string    `json:"content"`
	Type      string    `json:"type"` // "text" or "json"
	Timestamp time.Time `json:"timestamp"`
}

// AIStatusMessage represents a status change message
type AIStatusMessage struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"` // "started", "completed", "error"
	Error  string `json:"error,omitempty"`
}

// ClaudeRunner manages the Claude CLI subprocess lifecycle
type ClaudeRunner struct {
	mu          sync.RWMutex
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	stdout      io.ReadCloser
	stderr      io.ReadCloser
	cancel      context.CancelFunc
	isRunning   bool
	currentTask string
	wsHub       *WSHub
	workDir     string
}

// NewClaudeRunner creates a new ClaudeRunner
func NewClaudeRunner(wsHub *WSHub, workDir string) *ClaudeRunner {
	return &ClaudeRunner{
		wsHub:   wsHub,
		workDir: workDir,
	}
}

// Start spawns the Claude CLI subprocess for a given task
func (r *ClaudeRunner) Start(ctx context.Context, taskID string, prompt string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.isRunning {
		return fmt.Errorf("Claude is already running on task %s", r.currentTask)
	}

	// Create cancellable context
	ctx, r.cancel = context.WithCancel(ctx)

	// Build MCP config JSON for kantext
	// Use the current executable path to ensure the MCP server can be found
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	mcpConfig := fmt.Sprintf(`{"mcpServers":{"kantext":{"command":"%s","args":["mcp","-workdir","%s"]}}}`, execPath, r.workDir)

	// Build command with streaming JSON output
	// Use script to create a PTY so Claude outputs immediately (Node.js apps need a TTY)
	// Note: --verbose is required when using --print with --output-format stream-json
	claudeCmd := fmt.Sprintf("claude --dangerously-skip-permissions --output-format stream-json --verbose --mcp-config '%s' --print -p '%s'",
		mcpConfig, strings.ReplaceAll(prompt, "'", "'\\''"))
	r.cmd = exec.CommandContext(ctx, "script", "-q", "-c", claudeCmd, "/dev/null")
	r.cmd.Dir = r.workDir

	log.Printf("[ClaudeRunner] Starting Claude with command: claude --dangerously-skip-permissions --output-format stream-json --mcp-config '%s' --print -p '%s'",
		truncateForLog(mcpConfig, 100), truncateForLog(prompt, 100))
	log.Printf("[ClaudeRunner] Working directory: %s", r.workDir)

	// Setup pipes
	r.stdin, err = r.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	r.stdout, err = r.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	r.stderr, err = r.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start process
	if err := r.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start Claude: %w", err)
	}

	r.isRunning = true
	r.currentTask = taskID

	log.Printf("[ClaudeRunner] Claude started for task %s (PID: %d)", taskID, r.cmd.Process.Pid)
	log.Printf("[ClaudeRunner] Connected WebSocket clients: %d", r.wsHub.ClientCount())

	// Broadcast started event
	log.Printf("[ClaudeRunner] Broadcasting ai_started event for task %s", taskID)
	r.wsHub.Broadcast(WSMessage{
		Type: MsgTypeAIStarted,
		Data: AIStatusMessage{
			TaskID: taskID,
			Status: "started",
		},
	})

	// Start output streaming goroutines
	go r.streamOutput(taskID)
	go r.streamErrors(taskID)
	go r.waitForExit(taskID)

	return nil
}

// streamOutput reads stdout line-by-line and broadcasts to WebSocket clients
func (r *ClaudeRunner) streamOutput(taskID string) {
	log.Printf("[ClaudeRunner] Starting stdout stream for task %s", taskID)

	if r.stdout == nil {
		log.Printf("[ClaudeRunner] ERROR: stdout pipe is nil!")
		return
	}

	// Use bufio.Reader instead of Scanner for better pipe handling
	reader := bufio.NewReader(r.stdout)
	log.Printf("[ClaudeRunner] Waiting for Claude stdout output...")

	lineCount := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				log.Printf("[ClaudeRunner] Error reading Claude stdout: %v", err)
			}
			// Process any remaining partial line before breaking
			if len(line) > 0 {
				line = strings.TrimRight(line, "\n\r")
				lineCount++
				log.Printf("[ClaudeRunner] stdout line %d (final): %s", lineCount, truncateForLog(line, 200))
				r.broadcastLine(taskID, line, lineCount)
			}
			break
		}

		line = strings.TrimRight(line, "\n\r")
		if len(line) == 0 {
			continue // Skip empty lines
		}

		lineCount++
		log.Printf("[ClaudeRunner] stdout line %d: %s", lineCount, truncateForLog(line, 200))
		r.broadcastLine(taskID, line, lineCount)
	}

	log.Printf("[ClaudeRunner] stdout stream ended for task %s (read %d lines)", taskID, lineCount)
}

// broadcastLine sends a single line of output to WebSocket clients
func (r *ClaudeRunner) broadcastLine(taskID string, line string, lineCount int) {
	// Determine if this is JSON (Claude's stream-json format) or plain text
	msgType := "text"
	if len(line) > 0 && line[0] == '{' {
		msgType = "json"
	}

	log.Printf("[ClaudeRunner] Broadcasting ai_output line %d (type=%s) to %d clients", lineCount, msgType, r.wsHub.ClientCount())
	r.wsHub.Broadcast(WSMessage{
		Type: MsgTypeAIOutput,
		Data: AIOutputMessage{
			TaskID:    taskID,
			Content:   line,
			Type:      msgType,
			Timestamp: time.Now(),
		},
	})
}

// truncateForLog truncates a string for logging purposes
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// streamErrors reads stderr and broadcasts as error output
func (r *ClaudeRunner) streamErrors(taskID string) {
	log.Printf("[ClaudeRunner] Starting stderr stream for task %s", taskID)
	scanner := bufio.NewScanner(r.stderr)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	lineCount := 0
	for scanner.Scan() {
		line := scanner.Text()
		lineCount++
		log.Printf("[ClaudeRunner] stderr line %d: %s", lineCount, truncateForLog(line, 200))

		r.wsHub.Broadcast(WSMessage{
			Type: MsgTypeAIOutput,
			Data: AIOutputMessage{
				TaskID:    taskID,
				Content:   "[stderr] " + line,
				Type:      "error",
				Timestamp: time.Now(),
			},
		})
	}

	log.Printf("[ClaudeRunner] stderr stream ended for task %s (read %d lines)", taskID, lineCount)
	if err := scanner.Err(); err != nil {
		log.Printf("[ClaudeRunner] Error reading Claude stderr: %v", err)
	}
}

// waitForExit waits for the process to exit and broadcasts completion
func (r *ClaudeRunner) waitForExit(taskID string) {
	log.Printf("[ClaudeRunner] Waiting for Claude process to exit for task %s...", taskID)
	err := r.cmd.Wait()

	// Log exit details
	if r.cmd.ProcessState != nil {
		log.Printf("[ClaudeRunner] Claude exited with code %d for task %s", r.cmd.ProcessState.ExitCode(), taskID)
	}

	r.mu.Lock()
	r.isRunning = false
	currentTask := r.currentTask
	r.currentTask = ""
	r.mu.Unlock()

	status := "completed"
	errorMsg := ""
	if err != nil {
		status = "error"
		errorMsg = err.Error()
		log.Printf("[ClaudeRunner] Claude exited with error for task %s: %v", taskID, err)
	} else {
		log.Printf("[ClaudeRunner] Claude completed successfully for task %s", taskID)
	}

	r.wsHub.Broadcast(WSMessage{
		Type: MsgTypeAIStopped,
		Data: AIStatusMessage{
			TaskID: currentTask,
			Status: status,
			Error:  errorMsg,
		},
	})
}

// Stop terminates the Claude subprocess gracefully
func (r *ClaudeRunner) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.isRunning {
		return nil
	}

	log.Printf("Stopping Claude for task %s", r.currentTask)

	// Cancel context first
	if r.cancel != nil {
		r.cancel()
	}

	// Close stdin to signal EOF
	if r.stdin != nil {
		r.stdin.Close()
	}

	// Send SIGTERM for graceful shutdown
	if r.cmd != nil && r.cmd.Process != nil {
		if err := r.cmd.Process.Signal(syscall.SIGTERM); err != nil {
			log.Printf("Failed to send SIGTERM: %v", err)
		}

		// Wait briefly for graceful exit
		done := make(chan error, 1)
		go func() {
			done <- r.cmd.Wait()
		}()

		select {
		case <-done:
			// Process exited gracefully
		case <-time.After(5 * time.Second):
			// Force kill if still running
			log.Printf("Claude did not exit gracefully, sending SIGKILL")
			r.cmd.Process.Kill()
		}
	}

	r.isRunning = false
	r.currentTask = ""

	return nil
}

// SendInput writes input to the Claude subprocess stdin
func (r *ClaudeRunner) SendInput(input string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.isRunning {
		return fmt.Errorf("Claude is not running")
	}

	if r.stdin == nil {
		return fmt.Errorf("stdin pipe not available")
	}

	_, err := fmt.Fprintln(r.stdin, input)
	if err != nil {
		return fmt.Errorf("failed to write to stdin: %w", err)
	}

	return nil
}

// IsRunning returns whether Claude is currently running
func (r *ClaudeRunner) IsRunning() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.isRunning
}

// GetCurrentTask returns the ID of the task currently being worked on
func (r *ClaudeRunner) GetCurrentTask() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.currentTask
}
