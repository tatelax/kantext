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
		debounce: 100 * time.Millisecond, // Debounce rapid file changes
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
	var lastNotify time.Time

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

			// Debounce: if we recently notified, delay the next notification
			now := time.Now()
			if now.Sub(lastNotify) < fw.debounce {
				// Reset or create debounce timer
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.AfterFunc(fw.debounce, func() {
					log.Printf("File changed (debounced): %s", event.Name)
					fw.handleFileChange()
					lastNotify = time.Now()
				})
				continue
			}

			log.Printf("File changed: %s (%s)", event.Name, event.Op)
			fw.handleFileChange()
			lastNotify = now

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
