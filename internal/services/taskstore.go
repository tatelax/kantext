package services

import (
	"bufio"
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"kantext/internal/models"

	"gopkg.in/yaml.v3"
)

// Default settings values
const (
	DefaultStaleThresholdDays = 7
	DefaultTestCommand        = "go test -v -count=1 -run ^{testFunc}$ {testPath}"
	DefaultPassString         = "PASS"
	DefaultFailString         = "FAIL"
	DefaultNoTestsString      = "no tests to run"
)

// Short ID configuration
const (
	shortIDLength  = 8
	shortIDCharset = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

// generateShortID creates a short, unique ID using cryptographically secure random bytes.
// The ID is "task-" followed by 8 alphanumeric characters (62^8 = 218 trillion combinations).
// This maintains backwards compatibility with existing UUID IDs since IDs are just strings.
func generateShortID() string {
	result := make([]byte, shortIDLength)
	charsetLen := big.NewInt(int64(len(shortIDCharset)))

	for i := 0; i < shortIDLength; i++ {
		num, err := rand.Int(rand.Reader, charsetLen)
		if err != nil {
			// Fallback to a simple timestamp-based ID if crypto/rand fails
			return fmt.Sprintf("task-%d", time.Now().UnixNano())
		}
		result[i] = shortIDCharset[num.Int64()]
	}

	return "task-" + string(result)
}

// TestRunnerSettings holds test runner configuration from YAML front matter
type TestRunnerSettings struct {
	Command       string `yaml:"command,omitempty"`
	PassString    string `yaml:"pass_string,omitempty"`
	FailString    string `yaml:"fail_string,omitempty"`
	NoTestsString string `yaml:"no_tests_string,omitempty"`
}

// Settings holds all configurable settings stored in YAML front matter
type Settings struct {
	StaleThresholdDays int                `yaml:"stale_threshold_days,omitempty"`
	TestRunner         TestRunnerSettings `yaml:"test_runner,omitempty"`
}

// GetStaleThresholdDays returns the stale threshold, or default if not set
func (s *Settings) GetStaleThresholdDays() int {
	if s.StaleThresholdDays <= 0 {
		return DefaultStaleThresholdDays
	}
	return s.StaleThresholdDays
}

// GetTestCommand returns the test command, or default if not set
func (s *Settings) GetTestCommand() string {
	if s.TestRunner.Command == "" {
		return DefaultTestCommand
	}
	return s.TestRunner.Command
}

// GetPassString returns the pass string, or default if not set
func (s *Settings) GetPassString() string {
	if s.TestRunner.PassString == "" {
		return DefaultPassString
	}
	return s.TestRunner.PassString
}

// GetFailString returns the fail string, or default if not set
func (s *Settings) GetFailString() string {
	if s.TestRunner.FailString == "" {
		return DefaultFailString
	}
	return s.TestRunner.FailString
}

// GetNoTestsString returns the no tests string, or default if not set
func (s *Settings) GetNoTestsString() string {
	if s.TestRunner.NoTestsString == "" {
		return DefaultNoTestsString
	}
	return s.TestRunner.NoTestsString
}

// Pre-compiled regex patterns for parsing task files
var (
	columnRegex             = regexp.MustCompile(`^## (.+)$`)
	taskTitleRegex          = regexp.MustCompile(`^- \[([ x-])\] (.+)$`)
	metadataRegex           = regexp.MustCompile(`^  - ([^:]+): (.*)$`)
	legacyTaskWithTestRegex = regexp.MustCompile(`^- \[([ x-])\] \[(high|medium|low)\] (.+?) \| ([^:]+):([^ ]+) \| (.+?)(?:\s*<!-- id:([a-f0-9-]+) -->)?$`)
	legacyTaskNoTestRegex   = regexp.MustCompile(`^- \[([ x-])\] \[(high|medium|low)\] (.+?) \| (.+?)(?:\s*<!-- id:([a-f0-9-]+) -->)?$`)
	legacyOldTaskRegex      = regexp.MustCompile(`^- \[([ x-])\] (.+?) \| ([^:]+):([^ ]+) \| (.+?)(?:\s*<!-- id:([a-f0-9-]+) -->)?$`)
)

// TaskStore manages reading and writing tasks to a markdown file
type TaskStore struct {
	filePath        string
	workingDir      string // Working directory for test execution
	mu              sync.RWMutex
	tasks           map[string]*models.Task
	columns         []models.ColumnDefinition
	settings        Settings                   // Settings from YAML front matter
	taskLineNumbers map[string]int             // Maps task ID to line number for git blame

	// Async save infrastructure
	saveChan  chan struct{}  // Channel to trigger background saves
	saveErr   error          // Last save error (for monitoring)
	saveErrMu sync.RWMutex   // Protects saveErr
}

// NewTaskStore creates a new TaskStore with the specified working directory
func NewTaskStore(workingDir string) *TaskStore {
	filePath := filepath.Join(workingDir, "TASKS.md")
	store := &TaskStore{
		filePath:        filePath,
		workingDir:      workingDir,
		tasks:           make(map[string]*models.Task),
		columns:         []models.ColumnDefinition{},
		settings:        Settings{},
		taskLineNumbers: make(map[string]int),
		saveChan:        make(chan struct{}, 1), // Buffered channel of 1 for coalescing saves
	}
	store.Load()

	// Start background saver goroutine
	go store.backgroundSaver()

	return store
}

// GetSettings returns the current settings (thread-safe)
func (s *TaskStore) GetSettings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

// GetWorkingDir returns the working directory
func (s *TaskStore) GetWorkingDir() string {
	return s.workingDir
}

// UpdateSettings updates the settings and saves to file
func (s *TaskStore) UpdateSettings(settings Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings = settings
	return s.saveLocked()
}

// applyMetadata parses a metadata key-value pair and applies it to the task.
func applyMetadata(task *models.Task, key, value string) {
	switch key {
	case "id":
		task.ID = value
	case "priority":
		task.Priority = models.Priority(value)
	case "tags":
		// Parse tags as comma-separated values
		if value != "" {
			tags := strings.Split(value, ",")
			for i, tag := range tags {
				tags[i] = strings.TrimSpace(tag)
			}
			task.Tags = tags
		}
	case "requires_test":
		task.RequiresTest = value == "true"
	case "test":
		// Parse test: path/to/file:TestFunc and append to Tests array
		parts := strings.SplitN(value, ":", 2)
		if len(parts) == 2 {
			task.Tests = append(task.Tests, models.TestSpec{
				File: parts[0],
				Func: parts[1],
			})
		}
	case "tests_passed":
		fmt.Sscanf(value, "%d", &task.TestsPassed)
	case "tests_total":
		fmt.Sscanf(value, "%d", &task.TestsTotal)
	case "criteria":
		task.AcceptanceCriteria = value
	case "created_at":
		if t, err := time.Parse("2006-01-02T15:04:05Z", value); err == nil {
			task.CreatedAt = t
		}
	case "created_by":
		task.CreatedBy = value
	case "updated_at":
		if t, err := time.Parse("2006-01-02T15:04:05Z", value); err == nil {
			task.UpdatedAt = t
		}
	case "updated_by":
		task.UpdatedBy = value
	}
}

// parseCheckboxStatus converts a checkbox character to TestStatus.
func parseCheckboxStatus(checkbox string) models.TestStatus {
	switch checkbox {
	case "x":
		return models.TestStatusPassed
	case "-":
		return models.TestStatusFailed
	default:
		return models.TestStatusPending
	}
}

// getSortedColumns returns a copy of columns sorted by order.
// Must be called with at least a read lock held.
func (s *TaskStore) getSortedColumns() []models.ColumnDefinition {
	sorted := make([]models.ColumnDefinition, len(s.columns))
	copy(sorted, s.columns)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Order < sorted[j].Order
	})
	return sorted
}

// getFirstColumn returns the first column by order, or nil if no columns exist.
// Must be called with at least a read lock held.
func (s *TaskStore) getFirstColumn() *models.ColumnDefinition {
	if len(s.columns) == 0 {
		return nil
	}
	sorted := s.getSortedColumns()
	return &sorted[0]
}

// getLastColumn returns the last column by order, or nil if no columns exist.
// Must be called with at least a read lock held.
func (s *TaskStore) getLastColumn() *models.ColumnDefinition {
	if len(s.columns) == 0 {
		return nil
	}
	sorted := s.getSortedColumns()
	return &sorted[len(sorted)-1]
}

// getMaxColumnOrder returns the highest column order value.
// Must be called with at least a read lock held.
func (s *TaskStore) getMaxColumnOrder() int {
	maxOrder := -1
	for _, col := range s.columns {
		if col.Order > maxOrder {
			maxOrder = col.Order
		}
	}
	return maxOrder
}

// Load reads tasks from the markdown file
func (s *TaskStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	file, err := os.Open(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return s.createInitialFile()
		}
		return err
	}
	defer file.Close()

	s.tasks = make(map[string]*models.Task)
	s.columns = []models.ColumnDefinition{}
	s.settings = Settings{} // Reset settings
	scanner := bufio.NewScanner(file)
	var currentColumn models.Column
	columnOrder := 0
	taskOrder := 0
	var currentTask *models.Task
	var currentTaskLine int // 1-indexed line number where current task starts
	var lines []string

	// Reset line numbers map for git blame lookup
	s.taskLineNumbers = make(map[string]int)

	// Read all lines first
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	// Parse YAML front matter if present
	startLine := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		// Find closing ---
		endLine := -1
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				endLine = i
				break
			}
		}
		if endLine > 0 {
			// Extract YAML content
			yamlLines := lines[1:endLine]
			yamlContent := strings.Join(yamlLines, "\n")

			// Parse YAML into settings
			if err := yaml.Unmarshal([]byte(yamlContent), &s.settings); err != nil {
				log.Printf("Warning: failed to parse YAML front matter: %v", err)
				// Continue with default settings
			}

			// Start parsing markdown after front matter
			startLine = endLine + 1
		}
	}

	// Helper to finalize current task
	finalizeTask := func() {
		if currentTask != nil {
			if currentTask.ID == "" {
				currentTask.ID = generateShortID()
			}
			currentTask.Order = taskOrder
			taskOrder++
			s.tasks[currentTask.ID] = currentTask
			// Store the line number for git blame lookup
			if currentTaskLine > 0 {
				s.taskLineNumbers[currentTask.ID] = currentTaskLine
			}
			currentTask = nil
			currentTaskLine = 0
		}
	}

	for i := startLine; i < len(lines); i++ {
		line := lines[i]
		trimmedLine := strings.TrimSpace(line)

		// Check for column headers (## Section Name)
		if matches := columnRegex.FindStringSubmatch(trimmedLine); matches != nil {
			finalizeTask()
			columnName := strings.TrimSpace(matches[1])
			slug := models.NameToSlug(columnName)
			currentColumn = models.Column(slug)

			s.columns = append(s.columns, models.ColumnDefinition{
				Slug:  slug,
				Name:  columnName,
				Order: columnOrder,
			})
			columnOrder++
			continue
		}

		// Check for metadata line (indented with 2 spaces)
		if strings.HasPrefix(line, "  - ") && currentTask != nil {
			if matches := metadataRegex.FindStringSubmatch(line); matches != nil {
				applyMetadata(currentTask, strings.TrimSpace(matches[1]), strings.TrimSpace(matches[2]))
				continue
			}
		}

		// Check for new nested format task title
		if matches := taskTitleRegex.FindStringSubmatch(trimmedLine); matches != nil {
			// Check if this looks like a legacy format (contains | separator)
			if strings.Contains(matches[2], " | ") {
				// Try legacy formats first
				if legacyMatches := legacyTaskWithTestRegex.FindStringSubmatch(trimmedLine); legacyMatches != nil {
					finalizeTask()
					currentTask = s.parseLegacyTaskWithTest(legacyMatches, currentColumn)
					currentTaskLine = i + 1
					continue
				}
				if legacyMatches := legacyTaskNoTestRegex.FindStringSubmatch(trimmedLine); legacyMatches != nil {
					finalizeTask()
					currentTask = s.parseLegacyTaskNoTest(legacyMatches, currentColumn)
					currentTaskLine = i + 1
					continue
				}
				if legacyMatches := legacyOldTaskRegex.FindStringSubmatch(trimmedLine); legacyMatches != nil {
					finalizeTask()
					currentTask = s.parseLegacyOldTask(legacyMatches, currentColumn)
					currentTaskLine = i + 1
					continue
				}
			}

			// New nested format
			finalizeTask()
			currentTask = &models.Task{
				Title:      strings.TrimSpace(matches[2]),
				Column:     currentColumn,
				Priority:   models.PriorityMedium,
				TestStatus: parseCheckboxStatus(matches[1]),
			}
			currentTaskLine = i + 1 // 1-indexed for git blame
			continue
		}
	}

	// Finalize last task
	finalizeTask()

	// Enrich tasks with git blame author information
	s.refreshGitBlame()

	// Ensure default columns exist (regardless of what was parsed)
	columnsChanged := s.ensureDefaultColumnsLocked()

	// Normalize tasks - fill in missing required fields
	tasksChanged := s.normalizeTasksLocked()

	// Check if settings need to be initialized with defaults
	settingsNeedInit := s.settingsNeedInitializationLocked()

	// If changes were made, save the file
	if columnsChanged || tasksChanged || settingsNeedInit {
		if err := s.saveLocked(); err != nil {
			return fmt.Errorf("failed to save after normalization: %w", err)
		}
	}

	return nil
}

// settingsNeedInitializationLocked checks if settings are missing values that need defaults.
// Returns true if settings need to be written to the file. Must be called with the lock held.
func (s *TaskStore) settingsNeedInitializationLocked() bool {
	// Check if any setting is missing (would use default)
	if s.settings.StaleThresholdDays == 0 {
		return true
	}
	if s.settings.TestRunner.Command == "" {
		return true
	}
	if s.settings.TestRunner.PassString == "" {
		return true
	}
	if s.settings.TestRunner.FailString == "" {
		return true
	}
	if s.settings.TestRunner.NoTestsString == "" {
		return true
	}
	return false
}

// normalizeTasksLocked checks all tasks for missing required fields and fills them in.
// Returns true if any changes were made. Must be called with the lock held.
func (s *TaskStore) normalizeTasksLocked() bool {
	changed := false
	now := time.Now().UTC()

	for _, task := range s.tasks {
		// Check and fill missing ID
		if task.ID == "" {
			task.ID = generateShortID()
			changed = true
		}

		// Check and fill missing priority
		if task.Priority == "" {
			task.Priority = models.PriorityMedium
			changed = true
		}

		// Check and fill missing created_at
		if task.CreatedAt.IsZero() {
			task.CreatedAt = now
			changed = true
		}

		// Check and fill missing updated_at
		if task.UpdatedAt.IsZero() {
			task.UpdatedAt = now
			changed = true
		}
	}

	return changed
}

// ensureDefaultColumnsLocked ensures all default columns exist.
// Adds missing defaults while preserving existing columns and their order.
// Returns true if changes were made. Must be called with lock held.
func (s *TaskStore) ensureDefaultColumnsLocked() bool {
	changed := false

	// Build map of existing column slugs
	existingColumns := make(map[string]bool)
	for _, col := range s.columns {
		existingColumns[col.Slug] = true
	}

	// Find max order to append after existing columns
	maxOrder := s.getMaxColumnOrder()

	// Add any missing default columns
	for _, defaultCol := range models.DefaultColumns {
		if !existingColumns[defaultCol.Slug] {
			maxOrder++
			s.columns = append(s.columns, models.ColumnDefinition{
				Slug:  defaultCol.Slug,
				Name:  defaultCol.Name,
				Order: maxOrder,
			})
			changed = true
		}
	}

	return changed
}

// parseLegacyTaskWithTest parses the old format with test reference
func (s *TaskStore) parseLegacyTaskWithTest(matches []string, column models.Column) *models.Task {
	id := matches[7]
	if id == "" {
		id = generateShortID()
	}

	task := &models.Task{
		ID:       id,
		Priority: models.Priority(matches[2]),
		Title:    strings.TrimSpace(matches[3]),
		Tests: []models.TestSpec{{
			File: strings.TrimSpace(matches[4]),
			Func: strings.TrimSpace(matches[5]),
		}},
		AcceptanceCriteria: strings.TrimSpace(matches[6]),
		Column:             column,
		TestStatus:         parseCheckboxStatus(matches[1]),
	}

	return task
}

// parseLegacyTaskNoTest parses the old format without test reference
func (s *TaskStore) parseLegacyTaskNoTest(matches []string, column models.Column) *models.Task {
	id := matches[5]
	if id == "" {
		id = generateShortID()
	}

	task := &models.Task{
		ID:                 id,
		Priority:           models.Priority(matches[2]),
		Title:              strings.TrimSpace(matches[3]),
		AcceptanceCriteria: strings.TrimSpace(matches[4]),
		Column:             column,
		TestStatus:         models.TestStatusPending,
	}

	return task
}

// parseLegacyOldTask parses the oldest format (no priority brackets)
func (s *TaskStore) parseLegacyOldTask(matches []string, column models.Column) *models.Task {
	id := matches[6]
	if id == "" {
		id = generateShortID()
	}

	task := &models.Task{
		ID:    id,
		Title: strings.TrimSpace(matches[2]),
		Tests: []models.TestSpec{{
			File: strings.TrimSpace(matches[3]),
			Func: strings.TrimSpace(matches[4]),
		}},
		AcceptanceCriteria: strings.TrimSpace(matches[5]),
		Priority:           models.PriorityMedium,
		Column:             column,
		TestStatus:         parseCheckboxStatus(matches[1]),
	}

	return task
}

// Save writes all tasks to the markdown file (acquires lock)
func (s *TaskStore) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.saveLocked()
}

// saveLocked triggers an async save. Returns immediately without waiting for I/O.
// Caller must hold at least a read lock.
func (s *TaskStore) saveLocked() error {
	// Non-blocking send - if channel is full, a save is already pending
	select {
	case s.saveChan <- struct{}{}:
	default:
		// Save already pending, skip
	}
	return nil
}

// backgroundSaver runs in a goroutine and handles file writes asynchronously.
// This prevents blocking API responses during disk I/O.
func (s *TaskStore) backgroundSaver() {
	for range s.saveChan {
		s.mu.RLock()
		err := s.saveToFile()
		s.mu.RUnlock()

		if err != nil {
			s.saveErrMu.Lock()
			s.saveErr = err
			s.saveErrMu.Unlock()
			log.Printf("Error saving tasks: %v", err)
		}
	}
}

// Close stops the background saver and performs a final synchronous save.
func (s *TaskStore) Close() error {
	close(s.saveChan)
	// Do one final save to ensure all changes are persisted
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.saveToFile()
}

// saveToFile writes all tasks to the markdown file synchronously.
// Caller must hold at least a read lock.
func (s *TaskStore) saveToFile() error {
	file, err := os.Create(s.filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Ensure we have default settings to write
	settingsToWrite := s.settings
	if settingsToWrite.StaleThresholdDays == 0 {
		settingsToWrite.StaleThresholdDays = DefaultStaleThresholdDays
	}
	if settingsToWrite.TestRunner.Command == "" {
		settingsToWrite.TestRunner.Command = DefaultTestCommand
	}
	if settingsToWrite.TestRunner.PassString == "" {
		settingsToWrite.TestRunner.PassString = DefaultPassString
	}
	if settingsToWrite.TestRunner.FailString == "" {
		settingsToWrite.TestRunner.FailString = DefaultFailString
	}
	if settingsToWrite.TestRunner.NoTestsString == "" {
		settingsToWrite.TestRunner.NoTestsString = DefaultNoTestsString
	}

	// Write YAML front matter
	fmt.Fprintln(file, "---")
	yamlBytes, err := yaml.Marshal(&settingsToWrite)
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}
	// Trim trailing newline from YAML output since we add our own
	yamlStr := strings.TrimSuffix(string(yamlBytes), "\n")
	fmt.Fprintln(file, yamlStr)
	fmt.Fprintln(file, "---")

	// Write header
	fmt.Fprintln(file, "# Kantext Tasks")
	fmt.Fprintln(file, "")

	// Write each column section
	for _, col := range s.getSortedColumns() {
		tasks := s.getTasksByColumn(models.Column(col.Slug))

		fmt.Fprintf(file, "## %s\n", col.Name)
		for _, task := range tasks {
			s.writeTask(file, task)
		}
		fmt.Fprintln(file, "")
	}

	return nil
}

func (s *TaskStore) getTasksByColumn(column models.Column) []*models.Task {
	var tasks []*models.Task
	for _, task := range s.tasks {
		if task.Column == column {
			tasks = append(tasks, task)
		}
	}
	// Sort by Order to preserve file order
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].Order < tasks[j].Order
	})
	return tasks
}

func (s *TaskStore) writeTask(file *os.File, task *models.Task) {
	// Write task title line
	fmt.Fprintf(file, "- [%s] %s\n", task.CheckboxChar(), task.Title)

	// Write metadata as nested bullet points
	fmt.Fprintf(file, "  - id: %s\n", task.ID)
	fmt.Fprintf(file, "  - priority: %s\n", task.Priority)

	// Write tags as comma-separated values
	if len(task.Tags) > 0 {
		fmt.Fprintf(file, "  - tags: %s\n", strings.Join(task.Tags, ", "))
	}

	fmt.Fprintf(file, "  - requires_test: %t\n", task.RequiresTest)

	// Write all tests
	for _, test := range task.Tests {
		fmt.Fprintf(file, "  - test: %s:%s\n", test.File, test.Func)
	}

	// Write test results if available
	if task.TestsTotal > 0 {
		fmt.Fprintf(file, "  - tests_passed: %d\n", task.TestsPassed)
		fmt.Fprintf(file, "  - tests_total: %d\n", task.TestsTotal)
	}

	if task.AcceptanceCriteria != "" {
		fmt.Fprintf(file, "  - criteria: %s\n", task.AcceptanceCriteria)
	}

	// Write timestamp metadata
	if !task.CreatedAt.IsZero() {
		fmt.Fprintf(file, "  - created_at: %s\n", task.CreatedAt.Format("2006-01-02T15:04:05Z"))
	}
	if task.CreatedBy != "" {
		fmt.Fprintf(file, "  - created_by: %s\n", task.CreatedBy)
	}
	if !task.UpdatedAt.IsZero() {
		fmt.Fprintf(file, "  - updated_at: %s\n", task.UpdatedAt.Format("2006-01-02T15:04:05Z"))
	}
	if task.UpdatedBy != "" {
		fmt.Fprintf(file, "  - updated_by: %s\n", task.UpdatedBy)
	}
}

func (s *TaskStore) createInitialFile() error {
	s.columns = make([]models.ColumnDefinition, len(models.DefaultColumns))
	copy(s.columns, models.DefaultColumns)
	s.settings = Settings{} // Initialize with defaults (will be filled on save)

	// Use saveLocked to write the file with proper default settings
	return s.saveLocked()
}

// GetColumns returns all column definitions in order
func (s *TaskStore) GetColumns() []models.ColumnDefinition {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getSortedColumns()
}

// CreateColumn adds a new column
func (s *TaskStore) CreateColumn(name string) (*models.ColumnDefinition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	slug := models.NameToSlug(name)

	// Check if column already exists
	for _, col := range s.columns {
		if col.Slug == slug {
			return nil, fmt.Errorf("column already exists: %s", name)
		}
	}

	newCol := models.ColumnDefinition{
		Slug:  slug,
		Name:  name,
		Order: s.getMaxColumnOrder() + 1,
	}
	s.columns = append(s.columns, newCol)

	// Save to file
	if err := s.saveLocked(); err != nil {
		// Rollback
		s.columns = s.columns[:len(s.columns)-1]
		return nil, err
	}

	return &newCol, nil
}

// UpdateColumn renames a column
func (s *TaskStore) UpdateColumn(slug string, newName string) (*models.ColumnDefinition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, col := range s.columns {
		if col.Slug == slug {
			newSlug := models.NameToSlug(newName)

			// Check if new slug conflicts with existing
			if newSlug != slug {
				for _, other := range s.columns {
					if other.Slug == newSlug {
						return nil, fmt.Errorf("column already exists: %s", newName)
					}
				}
			}

			// Update column
			s.columns[i].Name = newName
			s.columns[i].Slug = newSlug

			// Update all tasks in this column
			for _, task := range s.tasks {
				if task.Column == models.Column(slug) {
					task.Column = models.Column(newSlug)
				}
			}

			// Save to file
			if err := s.saveLocked(); err != nil {
				return nil, err
			}

			return &s.columns[i], nil
		}
	}

	return nil, fmt.Errorf("column not found: %s", slug)
}

// DeleteColumn removes a column (only if empty)
func (s *TaskStore) DeleteColumn(slug string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if this is a protected default column
	if models.IsDefaultColumn(slug) {
		return fmt.Errorf("cannot delete default column '%s'", slug)
	}

	// Check if column has tasks
	for _, task := range s.tasks {
		if task.Column == models.Column(slug) {
			return fmt.Errorf("cannot delete column with tasks")
		}
	}

	// Find and remove column
	idx := -1
	for i, col := range s.columns {
		if col.Slug == slug {
			idx = i
			break
		}
	}

	if idx == -1 {
		return fmt.Errorf("column not found: %s", slug)
	}

	// Require at least one column
	if len(s.columns) <= 1 {
		return fmt.Errorf("cannot delete the last column")
	}

	// Remove column
	s.columns = append(s.columns[:idx], s.columns[idx+1:]...)

	// Save to file
	return s.saveLocked()
}

// ReorderColumns sets the order of columns
func (s *TaskStore) ReorderColumns(slugs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate that all slugs exist
	slugSet := make(map[string]bool)
	for _, slug := range slugs {
		slugSet[slug] = true
	}

	for _, col := range s.columns {
		if !slugSet[col.Slug] {
			return fmt.Errorf("missing column in reorder: %s", col.Slug)
		}
	}

	if len(slugs) != len(s.columns) {
		return fmt.Errorf("reorder list must contain all columns")
	}

	// Update order
	for i, slug := range slugs {
		for j := range s.columns {
			if s.columns[j].Slug == slug {
				s.columns[j].Order = i
				break
			}
		}
	}

	// Save to file
	return s.saveLocked()
}

// GetAll returns all tasks in file order
func (s *TaskStore) GetAll() []*models.Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Refresh git blame data to pick up any new commits
	s.refreshGitBlame()

	tasks := make([]*models.Task, 0, len(s.tasks))
	for _, task := range s.tasks {
		tasks = append(tasks, task)
	}

	// Sort by Order to preserve file order
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].Order < tasks[j].Order
	})

	return tasks
}

// Get returns a task by ID
func (s *TaskStore) Get(id string) (*models.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	task, ok := s.tasks[id]
	if !ok {
		return nil, fmt.Errorf("task not found: %s", id)
	}
	return task, nil
}

// Create adds a new task
func (s *TaskStore) Create(req models.CreateTaskRequest) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Set default priority if not specified
	priority := req.Priority
	if priority == "" {
		priority = models.PriorityMedium
	}

	// Determine requires_test (default: false)
	requiresTest := req.RequiresTest != nil && *req.RequiresTest

	// Default to first column if exists
	column := models.Column("inbox")
	if firstCol := s.getFirstColumn(); firstCol != nil {
		column = models.Column(firstCol.Slug)
	}

	now := time.Now().UTC()
	task := &models.Task{
		ID:                 generateShortID(),
		Title:              req.Title,
		AcceptanceCriteria: req.AcceptanceCriteria,
		Priority:           priority,
		Tags:               req.Tags,
		RequiresTest:       requiresTest,
		Column:             column,
		TestStatus:         models.TestStatusPending,
		CreatedAt:          now,
		CreatedBy:          req.Author,
		UpdatedAt:          now,
		UpdatedBy:          req.Author,
	}

	s.tasks[task.ID] = task

	// Save to file
	if err := s.saveLocked(); err != nil {
		delete(s.tasks, task.ID)
		return nil, err
	}

	return task, nil
}

// Update modifies an existing task
func (s *TaskStore) Update(id string, req models.UpdateTaskRequest) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[id]
	if !ok {
		return nil, fmt.Errorf("task not found: %s", id)
	}

	if req.Title != nil {
		task.Title = *req.Title
	}
	if req.AcceptanceCriteria != nil {
		task.AcceptanceCriteria = *req.AcceptanceCriteria
	}
	if req.Priority != nil {
		task.Priority = *req.Priority
	}
	if req.Column != nil {
		task.Column = *req.Column
	}
	if req.Tags != nil {
		task.Tags = req.Tags
	}
	if req.RequiresTest != nil {
		task.RequiresTest = *req.RequiresTest
	}
	if req.Tests != nil {
		task.Tests = req.Tests
	}

	// Update timestamp metadata
	task.UpdatedAt = time.Now().UTC()
	if req.Author != "" {
		task.UpdatedBy = req.Author
	}

	// Save to file
	if err := s.saveLocked(); err != nil {
		return nil, err
	}

	return task, nil
}

// Delete removes a task
func (s *TaskStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.tasks[id]; !ok {
		return fmt.Errorf("task not found: %s", id)
	}

	delete(s.tasks, id)

	// Save to file
	return s.saveLocked()
}

// UpdateTestResult updates a task's test status and output (for single test)
func (s *TaskStore) UpdateTestResult(id string, result models.TestResult) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[id]
	if !ok {
		return nil, fmt.Errorf("task not found: %s", id)
	}

	if result.Passed {
		task.TestStatus = models.TestStatusPassed
		// Auto-move to last column on pass
		if lastCol := s.getLastColumn(); lastCol != nil {
			task.Column = models.Column(lastCol.Slug)
		}
	} else {
		task.TestStatus = models.TestStatusFailed
	}

	task.LastOutput = result.Output

	// Save to file
	if err := s.saveLocked(); err != nil {
		return nil, err
	}

	return task, nil
}

// UpdateTestResults updates a task's test status based on aggregated results (for multiple tests)
func (s *TaskStore) UpdateTestResults(id string, results models.TestResults) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[id]
	if !ok {
		return nil, fmt.Errorf("task not found: %s", id)
	}

	// Calculate tests passed/total
	passed := 0
	for _, r := range results.Results {
		if r.Passed {
			passed++
		}
	}
	task.TestsPassed = passed
	task.TestsTotal = len(results.Results)

	if results.AllPassed {
		task.TestStatus = models.TestStatusPassed
		// Auto-move to last column on pass
		if lastCol := s.getLastColumn(); lastCol != nil {
			task.Column = models.Column(lastCol.Slug)
		}
	} else {
		task.TestStatus = models.TestStatusFailed
	}

	// Combine all outputs
	var outputs []string
	for i, result := range results.Results {
		if len(task.Tests) > i {
			outputs = append(outputs, fmt.Sprintf("=== %s:%s ===\n%s", task.Tests[i].File, task.Tests[i].Func, result.Output))
		} else {
			outputs = append(outputs, result.Output)
		}
	}
	task.LastOutput = strings.Join(outputs, "\n\n")

	// Save to file
	if err := s.saveLocked(); err != nil {
		return nil, err
	}

	return task, nil
}

// SetTestRunning marks a task as currently running a test
func (s *TaskStore) SetTestRunning(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}

	task.TestStatus = models.TestStatusRunning
	return nil
}

// Reorder moves a task to a specific position within a column
func (s *TaskStore) Reorder(id string, column models.Column, position int) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[id]
	if !ok {
		return nil, fmt.Errorf("task not found: %s", id)
	}

	// Update the column and timestamp
	task.Column = column
	task.UpdatedAt = time.Now().UTC()

	// Get all tasks in the target column (excluding the task being moved)
	var columnTasks []*models.Task
	for _, t := range s.tasks {
		if t.Column == column && t.ID != id {
			columnTasks = append(columnTasks, t)
		}
	}

	// Sort by current order
	sort.Slice(columnTasks, func(i, j int) bool {
		return columnTasks[i].Order < columnTasks[j].Order
	})

	// Clamp position to valid range
	if position < 0 {
		position = 0
	}
	if position > len(columnTasks) {
		position = len(columnTasks)
	}

	// Insert task at the specified position and reassign orders
	// We need to give tasks in this column sequential orders
	// First, find the order range we need to use
	baseOrder := 0
	if len(columnTasks) > 0 {
		// Use the minimum order from existing tasks as base
		baseOrder = columnTasks[0].Order
	}

	// Reassign orders for tasks in this column
	for i, t := range columnTasks {
		if i < position {
			t.Order = baseOrder + i
		} else {
			t.Order = baseOrder + i + 1
		}
	}
	task.Order = baseOrder + position

	// Save to file
	if err := s.saveLocked(); err != nil {
		return nil, err
	}

	return task, nil
}

// refreshGitBlame updates task author information from git blame.
// It searches for each task's ID in the blame output to find the correct author,
// which handles cases where line numbers shift due to added/removed tasks.
func (s *TaskStore) refreshGitBlame() {
	// Get blame data with line content
	blameData := s.getGitBlameWithContent()
	if len(blameData) == 0 {
		return // Git blame not available
	}

	// Build a map of task ID -> author by searching for ID lines in blame output
	taskAuthors := make(map[string]string)
	for _, entry := range blameData {
		// Look for lines like "  - id: <uuid>"
		if strings.Contains(entry.Content, "  - id: ") {
			// Extract the ID from the line
			parts := strings.SplitN(entry.Content, "  - id: ", 2)
			if len(parts) == 2 {
				id := strings.TrimSpace(parts[1])
				taskAuthors[id] = entry.Author
			}
		}
	}

	// Update each task's author based on the blame data
	for taskID, author := range taskAuthors {
		if task, exists := s.tasks[taskID]; exists {
			task.UpdatedBy = author
			task.CreatedBy = author
		}
	}
}

// BlameEntry represents a single line from git blame output
type BlameEntry struct {
	LineNum int
	Author  string
	Content string
}

// getGitBlameWithContent runs git blame on HEAD and returns entries with author and content.
// Using HEAD ignores uncommitted changes, so we only see the last committed author.
// This allows us to search for specific content (like task IDs) regardless of line numbers.
func (s *TaskStore) getGitBlameWithContent() []BlameEntry {
	var entries []BlameEntry

	dir := filepath.Dir(s.filePath)
	// Use HEAD to only look at committed changes, not working directory
	cmd := exec.Command("git", "blame", "--porcelain", "HEAD", "--", s.filePath)
	cmd.Dir = dir

	output, err := cmd.Output()
	if err != nil {
		return entries
	}

	lines := strings.Split(string(output), "\n")
	var currentLine int
	var currentAuthor string

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		// SHA line starts a new blame entry
		if len(line) >= 40 && !strings.HasPrefix(line, "\t") && !strings.Contains(line[:40], " ") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				fmt.Sscanf(parts[2], "%d", &currentLine)
			}
		}

		// Author line
		if strings.HasPrefix(line, "author ") {
			currentAuthor = strings.TrimPrefix(line, "author ")
		}

		// Content line (starts with tab)
		if strings.HasPrefix(line, "\t") && currentLine > 0 {
			content := strings.TrimPrefix(line, "\t")
			entries = append(entries, BlameEntry{
				LineNum: currentLine,
				Author:  currentAuthor,
				Content: content,
			})
		}
	}

	return entries
}
