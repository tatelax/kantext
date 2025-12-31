package services

import (
	"log"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// FileWatcher monitors a file for changes and notifies the hub
type FileWatcher struct {
	filePath     string
	hub          *WSHub
	watcher      *fsnotify.Watcher
	debounce     time.Duration
	onFileChange func() // Callback when file changes (before notifying clients)
}

// NewFileWatcher creates a new file watcher
func NewFileWatcher(filePath string, hub *WSHub) (*FileWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &FileWatcher{
		filePath: filePath,
		hub:      hub,
		watcher:  watcher,
		debounce: 1 * time.Second, // Wait before processing to handle git operations that briefly remove files
	}, nil
}

// SetOnFileChange sets a callback to be called when the file changes
func (fw *FileWatcher) SetOnFileChange(callback func()) {
	fw.onFileChange = callback
}

// Start begins watching the file for changes
func (fw *FileWatcher) Start() error {
	// Get the directory containing the file (to handle file replacements)
	dir := filepath.Dir(fw.filePath)
	filename := filepath.Base(fw.filePath)

	// Watch the directory, not the file directly
	// This handles cases where the file is replaced (atomic writes)
	err := fw.watcher.Add(dir)
	if err != nil {
		return err
	}

	log.Printf("File watcher started for: %s", fw.filePath)

	go fw.watch(filename)
	return nil
}

// Stop stops the file watcher
func (fw *FileWatcher) Stop() error {
	return fw.watcher.Close()
}

func (fw *FileWatcher) watch(targetFilename string) {
	var debounceTimer *time.Timer

	for {
		select {
		case event, ok := <-fw.watcher.Events:
			if !ok {
				return
			}

			// Only care about our target file
			eventFilename := filepath.Base(event.Name)
			if eventFilename != targetFilename {
				continue
			}

			// Only care about write and create events
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}

			// Always debounce: reset timer on each event
			// This prevents acting on transient states (e.g., git checkout briefly removing the file)
			if debounceTimer != nil {
				debounceTimer.Stop()
			}

			eventName := event.Name // Capture for closure
			debounceTimer = time.AfterFunc(fw.debounce, func() {
				log.Printf("File changed (after debounce): %s", eventName)
				fw.handleFileChange()
			})

		case err, ok := <-fw.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("File watcher error: %v", err)
		}
	}
}

// handleFileChange calls the callback (to reload data) then notifies clients
func (fw *FileWatcher) handleFileChange() {
	// First, call the callback to reload the TaskStore
	if fw.onFileChange != nil {
		fw.onFileChange()
	}
	// Then notify connected clients to refresh
	fw.hub.NotifyTasksUpdated()
}
