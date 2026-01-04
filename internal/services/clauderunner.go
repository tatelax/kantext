package services

import (
	"bufio"
	"context"
	"encoding/json"
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
	onComplete  func() // Callback when task completes (for queue cleanup)
}

// NewClaudeRunner creates a new ClaudeRunner
func NewClaudeRunner(wsHub *WSHub, workDir string) *ClaudeRunner {
	return &ClaudeRunner{
		wsHub:   wsHub,
		workDir: workDir,
	}
}

// SetOnComplete sets the callback function to be called when a task completes
func (r *ClaudeRunner) SetOnComplete(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onComplete = fn
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

	// Build command with bidirectional streaming JSON
	// --print is required for --input-format stream-json per Claude CLI help
	// --input-format stream-json enables Claude to read from stdin for multi-turn conversations
	// --output-format stream-json enables streaming output
	// Note: Initial prompt is sent via stdin (not -p flag) to enable multi-turn conversations
	// We don't use script/PTY wrapper to avoid rendering Claude's interactive terminal UI
	r.cmd = exec.CommandContext(ctx, "claude",
		"--dangerously-skip-permissions",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--print",
		"--verbose",
		"--mcp-config", mcpConfig,
	)
	r.cmd.Dir = r.workDir

	log.Printf("[ClaudeRunner] Starting Claude with --print --input-format stream-json --output-format stream-json")
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

	// Send initial prompt via stdin as JSON (required for --input-format stream-json)
	initialPrompt := map[string]interface{}{
		"type": "user",
		"message": map[string]interface{}{
			"role": "user",
			"content": []map[string]interface{}{
				{"type": "text", "text": prompt},
			},
		},
	}
	jsonBytes, err := json.Marshal(initialPrompt)
	if err != nil {
		log.Printf("[ClaudeRunner] Warning: failed to marshal initial prompt: %v", err)
	} else {
		if _, err := fmt.Fprintln(r.stdin, string(jsonBytes)); err != nil {
			log.Printf("[ClaudeRunner] Warning: failed to send initial prompt: %v", err)
		} else {
			log.Printf("[ClaudeRunner] Sent initial prompt via stdin")
		}
	}

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

	// Call completion callback to clean up queue
	r.mu.RLock()
	onComplete := r.onComplete
	r.mu.RUnlock()
	if onComplete != nil {
		onComplete()
	}
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

// SendInput writes input to the Claude subprocess stdin as JSON
// With --input-format stream-json, all input must be properly formatted
func (r *ClaudeRunner) SendInput(input string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.isRunning {
		return fmt.Errorf("Claude is not running")
	}

	if r.stdin == nil {
		return fmt.Errorf("stdin pipe not available")
	}

	// Format as stream-json user message
	message := map[string]interface{}{
		"type": "user",
		"message": map[string]interface{}{
			"role": "user",
			"content": []map[string]interface{}{
				{"type": "text", "text": input},
			},
		},
	}
	jsonBytes, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal input: %w", err)
	}

	log.Printf("[ClaudeRunner] Sending user input: %s", truncateForLog(input, 100))
	_, err = fmt.Fprintln(r.stdin, string(jsonBytes))
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
