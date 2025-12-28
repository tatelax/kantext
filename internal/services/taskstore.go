package services

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"kantext/internal/models"

	"github.com/google/uuid"
)

// TaskStore manages reading and writing tasks to a markdown file
type TaskStore struct {
	filePath string
	mu       sync.RWMutex
	tasks    map[string]*models.Task
	columns  []models.ColumnDefinition
	testGen  *TestGenerator
}

// NewTaskStore creates a new TaskStore
func NewTaskStore(filePath string, testGen *TestGenerator) *TaskStore {
	store := &TaskStore{
		filePath: filePath,
		tasks:    make(map[string]*models.Task),
		columns:  []models.ColumnDefinition{},
		testGen:  testGen,
	}
	store.Load()
	return store
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
	scanner := bufio.NewScanner(file)
	var currentColumn models.Column
	columnOrder := 0

	// Regex patterns
	columnRegex := regexp.MustCompile(`^## (.+)$`)
	// New nested format: - [ ] Task title (checkbox can be space, x, or -)
	taskTitleRegex := regexp.MustCompile(`^- \[([ x-])\] (.+)$`)
	// Metadata line: - key: value (with 2-space indent)
	metadataRegex := regexp.MustCompile(`^  - ([^:]+): (.*)$`)
	// Legacy format for backward compatibility
	legacyTaskWithTestRegex := regexp.MustCompile(`^- \[([ x-])\] \[(high|medium|low)\] (.+?) \| ([^:]+):([^ ]+) \| (.+?)(?:\s*<!-- id:([a-f0-9-]+) -->)?$`)
	legacyTaskNoTestRegex := regexp.MustCompile(`^- \[([ x-])\] \[(high|medium|low)\] (.+?) \| (.+?)(?:\s*<!-- id:([a-f0-9-]+) -->)?$`)
	legacyOldTaskRegex := regexp.MustCompile(`^- \[([ x-])\] (.+?) \| ([^:]+):([^ ]+) \| (.+?)(?:\s*<!-- id:([a-f0-9-]+) -->)?$`)

	taskOrder := 0
	var currentTask *models.Task
	var lines []string

	// Read all lines first
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	// Helper to finalize current task
	finalizeTask := func() {
		if currentTask != nil {
			if currentTask.ID == "" {
				currentTask.ID = uuid.New().String()
			}
			currentTask.Order = taskOrder
			taskOrder++
			s.tasks[currentTask.ID] = currentTask
			currentTask = nil
		}
	}

	for i := 0; i < len(lines); i++ {
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
				key := strings.TrimSpace(matches[1])
				value := strings.TrimSpace(matches[2])

				switch key {
				case "id":
					currentTask.ID = value
				case "priority":
					currentTask.Priority = models.Priority(value)
				case "test":
					// Parse test: file:TestFunc
					parts := strings.SplitN(value, ":", 2)
					if len(parts) == 2 {
						currentTask.TestFile = parts[0]
						currentTask.TestFunc = parts[1]
					}
				case "criteria":
					currentTask.AcceptanceCriteria = value
				case "created_at":
					if t, err := time.Parse("2006-01-02T15:04:05Z", value); err == nil {
						currentTask.CreatedAt = t
					}
				case "created_by":
					currentTask.CreatedBy = value
				case "updated_at":
					if t, err := time.Parse("2006-01-02T15:04:05Z", value); err == nil {
						currentTask.UpdatedAt = t
					}
				case "updated_by":
					currentTask.UpdatedBy = value
				}
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
					continue
				}
				if legacyMatches := legacyTaskNoTestRegex.FindStringSubmatch(trimmedLine); legacyMatches != nil {
					finalizeTask()
					currentTask = s.parseLegacyTaskNoTest(legacyMatches, currentColumn)
					continue
				}
				if legacyMatches := legacyOldTaskRegex.FindStringSubmatch(trimmedLine); legacyMatches != nil {
					finalizeTask()
					currentTask = s.parseLegacyOldTask(legacyMatches, currentColumn)
					continue
				}
			}

			// New nested format
			finalizeTask()
			currentTask = &models.Task{
				Title:      strings.TrimSpace(matches[2]),
				Column:     currentColumn,
				Priority:   models.PriorityMedium, // Default
				TestStatus: models.TestStatusPending,
			}

			switch matches[1] {
			case "x":
				currentTask.TestStatus = models.TestStatusPassed
			case "-":
				currentTask.TestStatus = models.TestStatusFailed
			}
			continue
		}
	}

	// Finalize last task
	finalizeTask()

	// If no columns were found, create defaults
	if len(s.columns) == 0 {
		s.columns = []models.ColumnDefinition{
			{Slug: "todo", Name: "Todo", Order: 0},
			{Slug: "in_progress", Name: "In Progress", Order: 1},
			{Slug: "done", Name: "Done", Order: 2},
		}
	}

	// Normalize tasks - fill in missing required fields
	needsSave := s.normalizeTasksLocked()

	// If normalization made changes, save the file
	if needsSave {
		s.mu.Unlock()
		err := s.Save()
		s.mu.Lock()
		if err != nil {
			return fmt.Errorf("failed to save after normalization: %w", err)
		}
	}

	return nil
}

// normalizeTasksLocked checks all tasks for missing required fields and fills them in.
// Returns true if any changes were made. Must be called with the lock held.
func (s *TaskStore) normalizeTasksLocked() bool {
	changed := false
	now := time.Now().UTC()

	for _, task := range s.tasks {
		// Check and fill missing ID
		if task.ID == "" {
			task.ID = uuid.New().String()
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

// parseLegacyTaskWithTest parses the old format with test reference
func (s *TaskStore) parseLegacyTaskWithTest(matches []string, column models.Column) *models.Task {
	id := matches[7]
	if id == "" {
		id = uuid.New().String()
	}

	task := &models.Task{
		ID:                 id,
		Priority:           models.Priority(matches[2]),
		Title:              strings.TrimSpace(matches[3]),
		TestFile:           strings.TrimSpace(matches[4]),
		TestFunc:           strings.TrimSpace(matches[5]),
		AcceptanceCriteria: strings.TrimSpace(matches[6]),
		Column:             column,
		TestStatus:         models.TestStatusPending,
	}

	switch matches[1] {
	case "x":
		task.TestStatus = models.TestStatusPassed
	case "-":
		task.TestStatus = models.TestStatusFailed
	}

	return task
}

// parseLegacyTaskNoTest parses the old format without test reference
func (s *TaskStore) parseLegacyTaskNoTest(matches []string, column models.Column) *models.Task {
	id := matches[5]
	if id == "" {
		id = uuid.New().String()
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
		id = uuid.New().String()
	}

	task := &models.Task{
		ID:                 id,
		Title:              strings.TrimSpace(matches[2]),
		TestFile:           strings.TrimSpace(matches[3]),
		TestFunc:           strings.TrimSpace(matches[4]),
		AcceptanceCriteria: strings.TrimSpace(matches[5]),
		Priority:           models.PriorityMedium,
		Column:             column,
		TestStatus:         models.TestStatusPending,
	}

	switch matches[1] {
	case "x":
		task.TestStatus = models.TestStatusPassed
	case "-":
		task.TestStatus = models.TestStatusFailed
	}

	return task
}

// Save writes all tasks to the markdown file
func (s *TaskStore) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	file, err := os.Create(s.filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Write header
	fmt.Fprintln(file, "# Kantext Tasks")
	fmt.Fprintln(file, "")

	// Sort columns by order
	sortedColumns := make([]models.ColumnDefinition, len(s.columns))
	copy(sortedColumns, s.columns)
	sort.Slice(sortedColumns, func(i, j int) bool {
		return sortedColumns[i].Order < sortedColumns[j].Order
	})

	// Write each column section
	for _, col := range sortedColumns {
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
	checkbox := " "
	if task.TestStatus == models.TestStatusPassed {
		checkbox = "x"
	} else if task.TestStatus == models.TestStatusFailed {
		checkbox = "-"
	}

	// Write task title line
	fmt.Fprintf(file, "- [%s] %s\n", checkbox, task.Title)

	// Write metadata as nested bullet points
	fmt.Fprintf(file, "  - id: %s\n", task.ID)
	fmt.Fprintf(file, "  - priority: %s\n", task.Priority)

	if task.HasTest() {
		fmt.Fprintf(file, "  - test: %s:%s\n", task.TestFile, task.TestFunc)
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
	s.columns = []models.ColumnDefinition{
		{Slug: "todo", Name: "Todo", Order: 0},
		{Slug: "in_progress", Name: "In Progress", Order: 1},
		{Slug: "done", Name: "Done", Order: 2},
	}

	file, err := os.Create(s.filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	content := `# Kantext Tasks

## Todo

## In Progress

## Done
`
	_, err = file.WriteString(content)
	return err
}

// GetColumns returns all column definitions in order
func (s *TaskStore) GetColumns() []models.ColumnDefinition {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]models.ColumnDefinition, len(s.columns))
	copy(result, s.columns)
	sort.Slice(result, func(i, j int) bool {
		return result[i].Order < result[j].Order
	})
	return result
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

	// Find max order
	maxOrder := -1
	for _, col := range s.columns {
		if col.Order > maxOrder {
			maxOrder = col.Order
		}
	}

	newCol := models.ColumnDefinition{
		Slug:  slug,
		Name:  name,
		Order: maxOrder + 1,
	}
	s.columns = append(s.columns, newCol)

	// Save to file
	s.mu.Unlock()
	err := s.Save()
	s.mu.Lock()

	if err != nil {
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
			s.mu.Unlock()
			err := s.Save()
			s.mu.Lock()

			if err != nil {
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
	s.mu.Unlock()
	err := s.Save()
	s.mu.Lock()

	return err
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
	s.mu.Unlock()
	err := s.Save()
	s.mu.Lock()

	return err
}

// GetAll returns all tasks in file order
func (s *TaskStore) GetAll() []*models.Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

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

// Create adds a new task and optionally generates the test file
func (s *TaskStore) Create(req models.CreateTaskRequest) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Set default priority if not specified
	priority := req.Priority
	if priority == "" {
		priority = models.PriorityMedium
	}

	var testFile, testFunc string
	var err error

	// Determine if we should generate test files (default: true)
	shouldGenerateFile := req.GenerateTestFile == nil || *req.GenerateTestFile

	// Use provided test file/function or generate new ones
	if req.TestFile != "" && req.TestFunc != "" {
		// Use existing test
		testFile = req.TestFile
		testFunc = req.TestFunc
	} else if shouldGenerateFile {
		// Generate test file on disk
		testFile, testFunc, err = s.testGen.GenerateTestFile(req.Title, req.AcceptanceCriteria)
		if err != nil {
			return nil, fmt.Errorf("failed to generate test file: %w", err)
		}
	}
	// If GenerateTestFile is explicitly false and no test file/func provided,
	// the task is created without a test (testFile and testFunc remain empty)

	// Default to first column if exists
	column := models.Column("todo")
	if len(s.columns) > 0 {
		// Sort by order and get first
		sorted := make([]models.ColumnDefinition, len(s.columns))
		copy(sorted, s.columns)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Order < sorted[j].Order
		})
		column = models.Column(sorted[0].Slug)
	}

	now := time.Now().UTC()
	task := &models.Task{
		ID:                 uuid.New().String(),
		Title:              req.Title,
		AcceptanceCriteria: req.AcceptanceCriteria,
		Priority:           priority,
		TestFile:           testFile,
		TestFunc:           testFunc,
		Column:             column,
		TestStatus:         models.TestStatusPending,
		CreatedAt:          now,
		CreatedBy:          req.Author,
		UpdatedAt:          now,
		UpdatedBy:          req.Author,
	}

	s.tasks[task.ID] = task

	// Save to file
	s.mu.Unlock()
	err = s.Save()
	s.mu.Lock()

	if err != nil {
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

	// Update timestamp metadata
	task.UpdatedAt = time.Now().UTC()
	if req.Author != "" {
		task.UpdatedBy = req.Author
	}

	// Save to file
	s.mu.Unlock()
	err := s.Save()
	s.mu.Lock()

	if err != nil {
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
	s.mu.Unlock()
	err := s.Save()
	s.mu.Lock()

	return err
}

// UpdateTestResult updates a task's test status and output
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
		if len(s.columns) > 0 {
			sorted := make([]models.ColumnDefinition, len(s.columns))
			copy(sorted, s.columns)
			sort.Slice(sorted, func(i, j int) bool {
				return sorted[i].Order < sorted[j].Order
			})
			task.Column = models.Column(sorted[len(sorted)-1].Slug)
		}
	} else {
		task.TestStatus = models.TestStatusFailed
	}

	task.LastOutput = result.Output

	// Save to file
	s.mu.Unlock()
	err := s.Save()
	s.mu.Lock()

	if err != nil {
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

	// Update the column
	task.Column = column

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
	s.mu.Unlock()
	err := s.Save()
	s.mu.Lock()

	if err != nil {
		return nil, err
	}

	return task, nil
}
