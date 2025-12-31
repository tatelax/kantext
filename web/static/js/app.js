// Kantext - Frontend Application

const API_BASE = '/api';

// Default columns that cannot be deleted
const DEFAULT_COLUMN_SLUGS = new Set(['inbox', 'in_progress', 'done']);

function isDefaultColumn(slug) {
    return DEFAULT_COLUMN_SLUGS.has(slug);
}

// State
let tasks = [];
let columns = [];
let draggedTask = null;
let draggedColumn = null;
let columnDragAllowed = false; // Track if drag started from header
let ws = null; // WebSocket connection
let wsReconnectTimer = null;
let wsReconnectDelay = 1000; // Start with 1 second
let notificationContainer = null; // Notification container
let staleThresholdDays = 7; // Default: 1 week

// Drag effect state
let dragGhost = null;
let lastMouseX = 0;
let lastMouseY = 0;
let mouseVelocityX = 0;
let dragAnimationFrame = null;

// Drop position state
let dropIndicator = null;
let currentDropTarget = null; // The task list we're hovering over
let currentDropIndex = -1; // Index where the card will be inserted

// DOM Elements
const addTaskBtn = document.getElementById('add-task-btn');
const taskModal = document.getElementById('task-modal');
const outputModal = document.getElementById('output-modal');
const taskForm = document.getElementById('task-form');
const testOutput = document.getElementById('test-output');
const outputMovePrompt = document.getElementById('output-move-prompt');
const outputFailedCount = document.getElementById('output-failed-count');
const outputMoveBtn = document.getElementById('output-move-btn');
const board = document.getElementById('board');
const requiresTestCheckbox = document.getElementById('requires_test');
const panelRequiresTestCheckbox = document.getElementById('panel-requires-test');

// Task Panel Elements (for editing existing tasks)
const taskPanel = document.getElementById('task-panel');
const taskPanelOverlay = document.getElementById('task-panel-overlay');
const panelTaskForm = document.getElementById('panel-task-form');
const panelCloseBtn = document.getElementById('panel-close-btn');
const panelCancelBtn = document.getElementById('panel-cancel-btn');
const panelDeleteBtn = document.getElementById('panel-delete-btn');
const panelSaveBtn = document.getElementById('panel-save-btn');
let currentPanelTask = null; // Track the task being edited in the panel
let panelOriginalValues = null; // Track original form values for change detection

// Load configuration from server
async function loadConfig() {
    try {
        const response = await fetch(`${API_BASE}/config`);
        if (response.ok) {
            const config = await response.json();
            if (config.stale_threshold_days) {
                staleThresholdDays = config.stale_threshold_days;
            }
            console.log('[Config] Loaded config, stale threshold:', staleThresholdDays, 'days');
        }
    } catch (error) {
        console.error('[Config] Failed to load config:', error);
    }
}

// Check if a task is stale (not updated within the threshold)
function isTaskStale(task) {
    if (!task.updated_at) return false;
    const updatedAt = new Date(task.updated_at);
    const now = new Date();
    const diffMs = now - updatedAt;
    const diffDays = diffMs / (1000 * 60 * 60 * 24);
    return diffDays > staleThresholdDays;
}

// ============================================
// gotestsum JSON Output Support
// ============================================

/**
 * Escape HTML special characters for safe rendering
 * @param {string} str - String to escape
 * @returns {string} - Escaped string
 */
function escapeHtml(str) {
    if (!str) return '';
    return str
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#039;');
}

/**
 * Detect if output contains go test JSON format (newline-delimited JSON)
 * Handles output that may have non-JSON header lines (e.g., "=== file:func ===")
 * @param {string} output - Raw test output
 * @returns {boolean} - True if output contains go test JSON events
 */
function isGotestsumJSON(output) {
    if (!output || typeof output !== 'string') return false;

    // Look for JSON lines with go test event signature
    // Skip non-JSON lines (headers, empty lines, etc.)
    const lines = output.trim().split('\n');
    let jsonLinesFound = 0;

    for (const line of lines) {
        const trimmed = line.trim();
        if (!trimmed) continue;

        try {
            const obj = JSON.parse(trimmed);
            // go test -json events have Time and Action fields
            if (obj.Time !== undefined && obj.Action !== undefined) {
                jsonLinesFound++;
                // Found at least 2 JSON events, this is likely go test JSON output
                if (jsonLinesFound >= 2) {
                    return true;
                }
            }
        } catch {
            // Not JSON, continue looking (could be a header line)
            continue;
        }
    }

    // If we found at least one JSON event, consider it JSON output
    return jsonLinesFound > 0;
}

/**
 * Parse go test JSON output into structured test data
 * Handles output with header lines (e.g., "=== file:func ===") from test runner aggregation
 * @param {string} output - Raw NDJSON output (possibly with header lines)
 * @returns {Object} - Parsed test results with summary and individual tests
 */
function parseGotestsumOutput(output) {
    const lines = output.trim().split('\n');
    const events = [];
    const parseErrors = [];

    // Regex to match Kantext test runner header lines like "=== file:func ==="
    const headerPattern = /^===\s+.+\s+===$/;

    // Parse each line as JSON
    for (let i = 0; i < lines.length; i++) {
        const line = lines[i].trim();
        if (!line) continue;

        // Skip header lines silently (they're expected from test runner aggregation)
        if (headerPattern.test(line)) continue;

        try {
            events.push(JSON.parse(line));
        } catch (e) {
            parseErrors.push({ line: i + 1, content: line, error: e.message });
        }
    }

    // Aggregate events by package and test
    const packages = new Map(); // package -> { tests: Map, output: [], status, elapsed }
    const testMap = new Map();  // "package:test" -> { output: [], status, elapsed }

    for (const event of events) {
        const pkg = event.Package || '';
        const test = event.Test || '';
        const key = test ? `${pkg}:${test}` : null;

        // Initialize package if needed
        if (pkg && !packages.has(pkg)) {
            packages.set(pkg, { tests: new Map(), output: [], status: 'running', elapsed: 0 });
        }

        // Initialize test if needed
        if (key && !testMap.has(key)) {
            testMap.set(key, { package: pkg, name: test, output: [], status: 'running', elapsed: 0 });
            if (packages.has(pkg)) {
                packages.get(pkg).tests.set(test, testMap.get(key));
            }
        }

        // Process event based on action
        switch (event.Action) {
            case 'output':
                if (key && testMap.has(key)) {
                    testMap.get(key).output.push(event.Output || '');
                } else if (pkg && packages.has(pkg)) {
                    packages.get(pkg).output.push(event.Output || '');
                }
                break;
            case 'pass':
            case 'fail':
            case 'skip':
                if (key && testMap.has(key)) {
                    testMap.get(key).status = event.Action;
                    testMap.get(key).elapsed = event.Elapsed || 0;
                } else if (pkg && packages.has(pkg) && !test) {
                    packages.get(pkg).status = event.Action;
                    packages.get(pkg).elapsed = event.Elapsed || 0;
                }
                break;
        }
    }

    // Calculate summary
    let passed = 0, failed = 0, skipped = 0;
    let totalTime = 0;

    for (const [, testData] of testMap) {
        if (testData.status === 'pass') passed++;
        else if (testData.status === 'fail') failed++;
        else if (testData.status === 'skip') skipped++;
        totalTime += testData.elapsed;
    }

    return {
        summary: { passed, failed, skipped, total: testMap.size, totalTime },
        packages: Array.from(packages.entries()).map(([name, data]) => ({
            name,
            status: data.status,
            elapsed: data.elapsed,
            output: data.output.join(''),
            tests: Array.from(data.tests.values())
        })),
        tests: Array.from(testMap.values()),
        parseErrors,
        raw: output
    };
}

/**
 * Render parsed gotestsum results as rich HTML
 * @param {Object} parsed - Parsed gotestsum output from parseGotestsumOutput()
 * @returns {HTMLElement} - DOM element containing the rendered results
 */
function renderGotestsumResults(parsed) {
    const container = document.createElement('div');
    container.className = 'test-results-container';

    // Summary header
    const summary = document.createElement('div');
    summary.className = 'test-results-summary';
    summary.innerHTML = `
        <div class="summary-stats">
            <span class="stat stat-passed">${parsed.summary.passed} passed</span>
            ${parsed.summary.failed > 0 ? `<span class="stat stat-failed">${parsed.summary.failed} failed</span>` : ''}
            ${parsed.summary.skipped > 0 ? `<span class="stat stat-skipped">${parsed.summary.skipped} skipped</span>` : ''}
            <span class="stat stat-total">${parsed.summary.total} total</span>
            <span class="stat stat-time">${parsed.summary.totalTime.toFixed(2)}s</span>
        </div>
    `;
    container.appendChild(summary);

    // Test list
    const testList = document.createElement('div');
    testList.className = 'test-results-list';

    for (const test of parsed.tests) {
        const testEl = document.createElement('details');
        testEl.className = `test-result test-${test.status}`;

        // Auto-expand failed tests
        if (test.status === 'fail') {
            testEl.setAttribute('open', '');
        }

        const statusIcon = test.status === 'pass' ? '&#10003;' :
                          test.status === 'fail' ? '&#10007;' :
                          '&#8722;'; // checkmark, x, or minus

        testEl.innerHTML = `
            <summary class="test-result-header">
                <span class="test-status-icon">${statusIcon}</span>
                <span class="test-name">${escapeHtml(test.name)}</span>
                <span class="test-package">${escapeHtml(test.package)}</span>
                <span class="test-elapsed">${test.elapsed.toFixed(3)}s</span>
            </summary>
            <div class="test-result-output">
                <pre>${escapeHtml(test.output.join(''))}</pre>
            </div>
        `;

        testList.appendChild(testEl);
    }

    container.appendChild(testList);

    // Raw output toggle
    const rawToggle = document.createElement('details');
    rawToggle.className = 'raw-output-toggle';
    rawToggle.innerHTML = `
        <summary class="raw-output-header">View Raw Output</summary>
        <pre class="raw-output-content">${escapeHtml(parsed.raw)}</pre>
    `;
    container.appendChild(rawToggle);

    // Parse errors (if any)
    if (parsed.parseErrors.length > 0) {
        const errorsEl = document.createElement('div');
        errorsEl.className = 'parse-errors';
        errorsEl.innerHTML = `
            <div class="parse-errors-header">Parse warnings (${parsed.parseErrors.length} lines)</div>
            <ul class="parse-errors-list">
                ${parsed.parseErrors.map(e => `<li>Line ${e.line}: ${escapeHtml(e.error)}</li>`).join('')}
            </ul>
        `;
        container.appendChild(errorsEl);
    }

    return container;
}

// Update the stale badge in the task panel (if open)
function updatePanelStaleBadge() {
    const staleBadge = document.getElementById('panel-stale-badge');
    if (!staleBadge || !currentPanelTask) return;

    // Get fresh task data from tasks array
    const task = tasks.find(t => t.id === currentPanelTask.id);
    if (!task) return;

    const stale = isTaskStale(task);
    if (stale) {
        const updatedAt = new Date(task.updated_at);
        const now = new Date();
        const diffMs = now - updatedAt;
        const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24));
        const lastUpdateDate = updatedAt.toLocaleDateString('en-US', {
            month: 'short',
            day: 'numeric',
            year: 'numeric'
        });
        staleBadge.title = `This task has not been updated in ${diffDays} days (last updated ${lastUpdateDate}). Tasks are marked stale after ${staleThresholdDays} days of inactivity.`;
        staleBadge.classList.remove('hidden');
    } else {
        staleBadge.classList.add('hidden');
    }
}

// Initialize
document.addEventListener('DOMContentLoaded', () => {
    console.log('[Init] Page loaded, initializing Kantext...');
    initNotificationSystem();
    initThemeToggle();
    initConfigDialog();
    initDeleteDropZone();
    initDropIndicator();
    initDialogBackdropClose();
    initTaskPanel();
    loadConfig().then(() => loadColumns()).then(() => loadTasks());
    setupEventListeners();
    console.log('[Init] Setting up WebSocket connection...');
    connectWebSocket();
});

// ============================================
// Drop Indicator
// ============================================

/**
 * Initialize the drop indicator element used for showing
 * where a dragged card will be inserted.
 */
function initDropIndicator() {
    dropIndicator = document.createElement('div');
    dropIndicator.className = 'drop-indicator';
}

/**
 * Shows the drop indicator at the specified position in a task list.
 * @param {HTMLElement} taskList - The task list container
 * @param {number} index - The index where the indicator should appear
 */
function showDropIndicator(taskList, index) {
    if (!dropIndicator || !taskList) return;

    // Get all cards in the list (excluding the dragged one and the indicator)
    const cards = Array.from(taskList.querySelectorAll('.task-card:not(.dragging)'));

    // Remove indicator from current position if it's elsewhere
    if (dropIndicator.parentElement && dropIndicator.parentElement !== taskList) {
        dropIndicator.classList.remove('visible');
        dropIndicator.remove();
    }

    // Insert indicator at the correct position
    if (index >= cards.length) {
        // Insert at the end
        taskList.appendChild(dropIndicator);
    } else {
        // Insert before the card at this index
        taskList.insertBefore(dropIndicator, cards[index]);
    }

    // Trigger animation
    requestAnimationFrame(() => {
        dropIndicator.classList.add('visible');
    });

    currentDropTarget = taskList;
    currentDropIndex = index;
}

/**
 * Hides and removes the drop indicator.
 */
function hideDropIndicator() {
    if (!dropIndicator) return;

    dropIndicator.classList.remove('visible');

    // Remove after animation completes
    setTimeout(() => {
        if (dropIndicator.parentElement) {
            dropIndicator.remove();
        }
    }, 150);

    currentDropTarget = null;
    currentDropIndex = -1;
}

/**
 * Calculates the drop index based on mouse Y position within a task list.
 * @param {HTMLElement} taskList - The task list container
 * @param {number} mouseY - The current mouse Y position
 * @returns {number} The index where the card should be inserted
 */
function calculateDropIndex(taskList, mouseY) {
    const cards = Array.from(taskList.querySelectorAll('.task-card:not(.dragging)'));

    if (cards.length === 0) {
        return 0;
    }

    for (let i = 0; i < cards.length; i++) {
        const card = cards[i];
        const rect = card.getBoundingClientRect();
        const cardMiddle = rect.top + rect.height / 2;

        if (mouseY < cardMiddle) {
            return i;
        }
    }

    // Mouse is below all cards
    return cards.length;
}

// ============================================
// Dialog Backdrop Close
// ============================================

/**
 * Initialize dialog backdrop click handling.
 * Only closes dialog if both mousedown and click started on the backdrop,
 * preventing accidental closes when dragging from inside to outside.
 */
function initDialogBackdropClose() {
    const dialogs = document.querySelectorAll('.dialog-base');

    dialogs.forEach(dialog => {
        let mouseDownOnBackdrop = false;

        dialog.addEventListener('mousedown', (e) => {
            // Check if mousedown is directly on the dialog backdrop (not its children)
            mouseDownOnBackdrop = (e.target === dialog);
        });

        dialog.addEventListener('click', (e) => {
            // Only close if both mousedown and click were on the backdrop
            if (mouseDownOnBackdrop && e.target === dialog) {
                // Special handling for custom-dialog which uses handleCustomDialogCancel
                if (dialog.id === 'custom-dialog') {
                    handleCustomDialogCancel();
                } else {
                    dialog.close();
                }
            }
            mouseDownOnBackdrop = false;
        });
    });
}

// ============================================
// Theme Toggle
// ============================================

function initThemeToggle() {
    const themeToggle = document.getElementById('theme-toggle');
    if (themeToggle) {
        themeToggle.addEventListener('click', toggleTheme);
    }
}

function toggleTheme() {
    const html = document.documentElement;
    const isDark = html.classList.contains('dark');

    if (isDark) {
        html.classList.remove('dark');
        localStorage.setItem('kantext-theme', 'light');
    } else {
        html.classList.add('dark');
        localStorage.setItem('kantext-theme', 'dark');
    }
}

// ============================================
// Config Dialog
// ============================================

let configModal = null;
let configForm = null;
let currentConfig = null;

/**
 * Initialize config dialog event listeners
 */
function initConfigDialog() {
    const configBtn = document.getElementById('config-btn');
    configModal = document.getElementById('config-modal');
    configForm = document.getElementById('config-form');

    if (configBtn) {
        configBtn.addEventListener('click', openConfigModal);
    }

    if (configForm) {
        configForm.addEventListener('submit', handleConfigSubmit);
    }

    // Initialize backdrop close for config modal
    if (configModal) {
        let mouseDownOnBackdrop = false;
        configModal.addEventListener('mousedown', (e) => {
            mouseDownOnBackdrop = (e.target === configModal);
        });
        configModal.addEventListener('click', (e) => {
            if (mouseDownOnBackdrop && e.target === configModal) {
                closeConfigModal();
            }
            mouseDownOnBackdrop = false;
        });
        configModal.addEventListener('keydown', (e) => {
            if (e.key === 'Escape') {
                e.preventDefault();
                closeConfigModal();
            }
        });
    }
}

/**
 * Open the config modal and load current settings
 */
async function openConfigModal() {
    if (!configModal) return;

    try {
        // Load current config from server
        const response = await fetch(`${API_BASE}/config`);
        if (!response.ok) throw new Error('Failed to load config');

        currentConfig = await response.json();
        populateConfigForm(currentConfig);
        configModal.showModal();
    } catch (error) {
        console.error('Failed to load config:', error);
        showNotification('Failed to load settings', 'error');
    }
}

/**
 * Close the config modal
 */
function closeConfigModal() {
    if (configModal) {
        configModal.close();
    }
}

/**
 * Populate the config form with current values
 */
function populateConfigForm(config) {
    document.getElementById('config-working-dir').value = config.working_directory || '';
    document.getElementById('config-tasks-file').value = config.tasks_file || 'TASKS.md';
    document.getElementById('config-stale-days').value = config.stale_threshold_days || 7;

    if (config.test_runner) {
        document.getElementById('config-test-command').value = config.test_runner.command || '';
        document.getElementById('config-pass-string').value = config.test_runner.pass_string || 'PASS';
        document.getElementById('config-fail-string').value = config.test_runner.fail_string || 'FAIL';
        document.getElementById('config-no-tests-string').value = config.test_runner.no_tests_string || 'no tests to run';
    }
}

/**
 * Handle config form submission
 */
async function handleConfigSubmit(e) {
    e.preventDefault();

    const tasksFile = document.getElementById('config-tasks-file').value.trim();
    const staleDays = parseInt(document.getElementById('config-stale-days').value);
    const testCommand = document.getElementById('config-test-command').value.trim();
    const passString = document.getElementById('config-pass-string').value.trim();
    const failString = document.getElementById('config-fail-string').value.trim();
    const noTestsString = document.getElementById('config-no-tests-string').value.trim();

    const formData = {
        tasks_file: tasksFile || undefined,
        stale_threshold_days: staleDays || undefined,
        test_runner: {
            command: testCommand || undefined,
            pass_string: passString || undefined,
            fail_string: failString || undefined,
            no_tests_string: noTestsString || undefined
        }
    };

    // Clean up empty test_runner object
    if (Object.values(formData.test_runner).every(v => v === undefined)) {
        delete formData.test_runner;
    }

    try {
        const response = await fetch(`${API_BASE}/config`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(formData)
        });

        if (!response.ok) {
            const err = await response.json();
            throw new Error(err.error || 'Failed to save config');
        }

        const updatedConfig = await response.json();
        currentConfig = updatedConfig;

        // Update local staleThresholdDays if changed
        if (updatedConfig.stale_threshold_days) {
            staleThresholdDays = updatedConfig.stale_threshold_days;
        }

        closeConfigModal();
        showNotification('Settings saved successfully', 'success');

        // Refresh tasks to apply new stale threshold
        loadTasks();
    } catch (error) {
        console.error('Failed to save config:', error);
        showNotification(error.message || 'Failed to save settings', 'error');
    }
}

// ============================================
// Delete Drop Zone
// ============================================

let deleteDropZone = null;

function initDeleteDropZone() {
    // Create the delete drop zone element
    deleteDropZone = document.createElement('div');
    deleteDropZone.className = 'delete-drop-zone';
    deleteDropZone.innerHTML = '<span>Drop Here to Delete</span>';

    // Add to header
    const header = document.querySelector('body > header');
    if (header) {
        header.appendChild(deleteDropZone);

        // Setup drop zone event listeners
        deleteDropZone.addEventListener('dragover', handleDeleteZoneDragOver);
        deleteDropZone.addEventListener('dragleave', handleDeleteZoneDragLeave);
        deleteDropZone.addEventListener('drop', handleDeleteZoneDrop);
    }
}

function showDeleteDropZone() {
    if (deleteDropZone) {
        deleteDropZone.classList.add('visible');
    }
}

function hideDeleteDropZone() {
    if (deleteDropZone) {
        deleteDropZone.classList.remove('visible');
        deleteDropZone.classList.remove('drag-over');
    }
}

function handleDeleteZoneDragOver(e) {
    if (!draggedTask) return;
    e.preventDefault();
    // Don't stopPropagation - we need the event to reach trackDragMovement on document
    e.dataTransfer.dropEffect = 'move';
    deleteDropZone.classList.add('drag-over');
}

function handleDeleteZoneDragLeave(e) {
    e.stopPropagation();
    deleteDropZone.classList.remove('drag-over');
}

async function handleDeleteZoneDrop(e) {
    if (!draggedTask) return;
    e.preventDefault();
    e.stopPropagation();

    const taskId = e.dataTransfer.getData('text/plain');
    if (!taskId) return;

    // Hide the drop zone immediately
    hideDeleteDropZone();

    // Find the task to get its title for the confirmation message
    const task = tasks.find(t => t.id === taskId);
    const taskTitle = task ? task.title : 'this task';

    // Show the confirm dialog
    const confirmed = await showConfirmDialog(`Are you sure you want to delete "${taskTitle}"?`, {
        title: 'Delete Task',
        confirmText: 'Delete',
        destructive: true
    });

    if (confirmed) {
        try {
            await deleteTask(taskId);
            await loadTasks();
            showNotification(`"${taskTitle}" was deleted successfully`, 'success');
        } catch (error) {
            console.error('Failed to delete task:', error);
            showNotification('Failed to delete task. Please try again.', 'error');
        }
    }
}

// ============================================
// Notification System
// ============================================

function initNotificationSystem() {
    notificationContainer = document.createElement('div');
    notificationContainer.className = 'notification-container';
    notificationContainer.id = 'notification-container';
    document.body.appendChild(notificationContainer);
}

/**
 * Show a notification
 * @param {string} message - The message to display
 * @param {string} type - Notification type: 'success', 'error', 'warning', 'info'
 * @param {number} duration - Duration in milliseconds (default: 5000)
 */
function showNotification(message, type = 'info', duration = 5000) {
    if (!notificationContainer) {
        initNotificationSystem();
    }

    const notification = document.createElement('div');
    notification.className = `notification ${type}`;

    // Add icon based on type
    const icons = {
        success: '✓',
        error: '✕',
        warning: '⚠',
        info: 'ℹ'
    };

    notification.innerHTML = `
        <span class="notification-icon">${icons[type] || icons.info}</span>
        <span class="notification-message">${escapeHtml(message)}</span>
    `;

    notificationContainer.appendChild(notification);

    // Function to dismiss the notification
    const dismiss = () => {
        if (notification.classList.contains('fade-out')) return; // Already dismissing
        clearTimeout(autoRemoveTimeout);
        notification.classList.add('fade-out');
        setTimeout(() => {
            if (notification.parentNode) {
                notification.remove();
            }
        }, 300); // Wait for fade-out animation
    };

    // Click to dismiss
    notification.addEventListener('click', dismiss);

    // Auto-remove after duration
    const autoRemoveTimeout = setTimeout(dismiss, duration);

    return notification;
}

/**
 * Copy text to clipboard
 * @param {string} text - The text to copy
 * @returns {Promise<boolean>} - Whether the copy was successful
 */
async function copyToClipboard(text) {
    try {
        await navigator.clipboard.writeText(text);
        return true;
    } catch (err) {
        // Fallback for older browsers
        const textArea = document.createElement('textarea');
        textArea.value = text;
        textArea.style.position = 'fixed';
        textArea.style.left = '-9999px';
        document.body.appendChild(textArea);
        textArea.select();
        try {
            document.execCommand('copy');
            document.body.removeChild(textArea);
            return true;
        } catch (e) {
            document.body.removeChild(textArea);
            return false;
        }
    }
}

// ============================================
// Custom Dialog System
// ============================================

let customDialogResolve = null;
let customDialogMode = 'confirm'; // 'confirm' or 'prompt'

const customDialog = document.getElementById('custom-dialog');
const customDialogTitle = document.getElementById('custom-dialog-title');
const customDialogMessage = document.getElementById('custom-dialog-message');
const customDialogInputWrapper = document.getElementById('custom-dialog-input-wrapper');
const customDialogInput = document.getElementById('custom-dialog-input');
const customDialogCancel = document.getElementById('custom-dialog-cancel');
const customDialogConfirm = document.getElementById('custom-dialog-confirm');

/**
 * Show a custom confirm dialog
 * @param {string} message - The message to display
 * @param {Object} options - Optional configuration
 * @param {string} options.title - Dialog title (default: 'Confirm')
 * @param {string} options.confirmText - Confirm button text (default: 'Confirm')
 * @param {string} options.cancelText - Cancel button text (default: 'Cancel')
 * @param {boolean} options.destructive - Use red destructive button style (default: false)
 * @returns {Promise<boolean>} - Resolves to true if confirmed, false if cancelled
 */
function showConfirmDialog(message, options = {}) {
    return new Promise((resolve) => {
        customDialogResolve = resolve;
        customDialogMode = 'confirm';

        customDialogTitle.textContent = options.title || 'Confirm';
        customDialogMessage.textContent = message;
        customDialogInputWrapper.classList.add('hidden');
        customDialogCancel.textContent = options.cancelText || 'Cancel';
        customDialogConfirm.textContent = options.confirmText || 'Confirm';

        // Apply destructive style if requested
        if (options.destructive) {
            customDialogConfirm.className = 'btn-destructive';
        } else {
            customDialogConfirm.className = 'btn-primary';
        }

        customDialog.showModal();
        customDialogConfirm.focus();
    });
}

/**
 * Show a custom prompt dialog
 * @param {string} message - The message to display
 * @param {Object} options - Optional configuration
 * @param {string} options.title - Dialog title (default: 'Input')
 * @param {string} options.placeholder - Input placeholder text
 * @param {string} options.defaultValue - Default input value
 * @param {string} options.confirmText - Confirm button text (default: 'OK')
 * @param {string} options.cancelText - Cancel button text (default: 'Cancel')
 * @returns {Promise<string|null>} - Resolves to the input value if confirmed, null if cancelled
 */
function showPromptDialog(message, options = {}) {
    return new Promise((resolve) => {
        customDialogResolve = resolve;
        customDialogMode = 'prompt';

        customDialogTitle.textContent = options.title || 'Input';
        customDialogMessage.textContent = message;
        customDialogInputWrapper.classList.remove('hidden');
        customDialogInput.value = options.defaultValue || '';
        customDialogInput.placeholder = options.placeholder || '';
        customDialogCancel.textContent = options.cancelText || 'Cancel';
        customDialogConfirm.textContent = options.confirmText || 'OK';
        customDialogConfirm.className = 'btn-primary';

        customDialog.showModal();
        customDialogInput.focus();
        customDialogInput.select();
    });
}

function handleCustomDialogCancel() {
    customDialog.close();
    if (customDialogResolve) {
        if (customDialogMode === 'confirm') {
            customDialogResolve(false);
        } else {
            customDialogResolve(null);
        }
        customDialogResolve = null;
    }
}

function handleCustomDialogConfirm() {
    customDialog.close();
    if (customDialogResolve) {
        if (customDialogMode === 'confirm') {
            customDialogResolve(true);
        } else {
            customDialogResolve(customDialogInput.value);
        }
        customDialogResolve = null;
    }
}

// Setup custom dialog event listeners
if (customDialogCancel) {
    customDialogCancel.addEventListener('click', handleCustomDialogCancel);
}
if (customDialogConfirm) {
    customDialogConfirm.addEventListener('click', handleCustomDialogConfirm);
}
if (customDialogInput) {
    customDialogInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') {
            e.preventDefault();
            handleCustomDialogConfirm();
        }
    });
}
if (customDialog) {
    customDialog.addEventListener('keydown', (e) => {
        if (e.key === 'Escape') {
            e.preventDefault();
            handleCustomDialogCancel();
        }
    });
}

// ============================================
// WebSocket Functions
// ============================================

function connectWebSocket() {
    // Determine WebSocket URL based on current location
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/ws`;

    console.log('[WS] Attempting connection to:', wsUrl);
    console.log('[WS] Current location:', window.location.href);
    console.log('[WS] Protocol:', window.location.protocol, '-> WS Protocol:', protocol);

    try {
        ws = new WebSocket(wsUrl);
    } catch (e) {
        console.error('[WS] Failed to create WebSocket:', e);
        return;
    }

    ws.onopen = () => {
        console.log('[WS] Connected successfully to:', wsUrl);
        wsReconnectDelay = 1000; // Reset reconnect delay on successful connection
        showConnectionStatus(true);
    };

    ws.onmessage = (event) => {
        console.log('[WS] Message received:', event.data);
        try {
            const msg = JSON.parse(event.data);
            handleWebSocketMessage(msg);
        } catch (error) {
            console.error('[WS] Failed to parse message:', error);
        }
    };

    ws.onclose = (event) => {
        console.log('[WS] Disconnected - Code:', event.code, 'Reason:', event.reason, 'Clean:', event.wasClean);
        showConnectionStatus(false);
        scheduleReconnect();
    };

    ws.onerror = (error) => {
        console.error('[WS] Error:', error);
        console.error('[WS] ReadyState:', ws.readyState);
    };
}

function handleWebSocketMessage(msg) {
    console.log('WebSocket message:', msg);

    switch (msg.type) {
        case 'tasks_updated':
            // Fetch latest data and apply differential updates
            // loadColumns must complete first (in case columns changed),
            // then loadTasks renders tasks into the columns
            // Both use differential rendering to avoid flashing
            loadColumns().then(() => loadTasks());
            break;
        case 'task_moved':
            // Handle specific task move event if server sends it
            if (msg.task_id && msg.column) {
                handleRemoteTaskMove(msg.task_id, msg.column);
            }
            break;
        default:
            console.log('Unknown message type:', msg.type);
    }
}

/**
 * Handles a task move event from WebSocket (another client moved a task).
 * Only updates if the local state differs from the remote state.
 */
function handleRemoteTaskMove(taskId, newColumn) {
    const task = tasks.find(t => t.id === taskId);
    if (!task) {
        // Task not in our local state, fetch all tasks
        loadTasks();
        return;
    }

    // If our local state already matches, no update needed
    // (This happens when we initiated the move ourselves)
    if (task.column === newColumn) {
        return;
    }

    // Update local state
    task.column = newColumn;

    // Move the card in DOM
    const card = document.querySelector(`.task-card[data-id="${taskId}"]`);
    const targetList = document.querySelector(`.task-list[data-column="${newColumn}"]`);

    if (card && targetList) {
        targetList.appendChild(card);
    }

    // Update counts
    updateTaskCounts();
}

function scheduleReconnect() {
    if (wsReconnectTimer) {
        clearTimeout(wsReconnectTimer);
    }

    console.log(`Scheduling WebSocket reconnect in ${wsReconnectDelay}ms`);
    wsReconnectTimer = setTimeout(() => {
        connectWebSocket();
        // Exponential backoff with max of 30 seconds
        wsReconnectDelay = Math.min(wsReconnectDelay * 2, 30000);
    }, wsReconnectDelay);
}

// Track connection status to avoid duplicate notifications
let wasDisconnected = false;

function showConnectionStatus(connected) {
    if (connected) {
        // Only show reconnected notification if we were previously disconnected
        if (wasDisconnected) {
            showNotification('Connection restored', 'success');
            wasDisconnected = false;
        }
    } else {
        // Show disconnection warning
        if (!wasDisconnected) {
            showNotification('Connection lost. Attempting to reconnect...', 'warning');
            wasDisconnected = true;
        }
    }
}

// Event Listeners
function setupEventListeners() {
    // Add task button
    if (addTaskBtn) {
        addTaskBtn.addEventListener('click', () => openTaskModal());
    }

    // Global keyboard shortcut: Spacebar to open New Task dialog
    document.addEventListener('keydown', (e) => {
        // Only trigger on spacebar
        if (e.key !== ' ') return;

        // Don't trigger if user is typing in an input, textarea, or contenteditable
        const activeEl = document.activeElement;
        const isTyping = activeEl && (
            activeEl.tagName === 'INPUT' ||
            activeEl.tagName === 'TEXTAREA' ||
            activeEl.isContentEditable
        );
        if (isTyping) return;

        // Don't trigger if any dialog is open
        const anyDialogOpen = document.querySelector('dialog[open]') ||
                              taskPanel?.classList.contains('open');
        if (anyDialogOpen) return;

        // Prevent default spacebar scrolling behavior
        e.preventDefault();

        // Open the new task modal
        openNewTaskModal();
    });

    // Form submission
    if (taskForm) {
        taskForm.addEventListener('submit', handleFormSubmit);
    }

    // Copy task ID button
    const copyTaskIdBtn = document.getElementById('copy-task-id-btn');
    if (copyTaskIdBtn) {
        copyTaskIdBtn.addEventListener('click', handleCopyTaskId);
    }

    // Output modal Accept button (move task from Done to In Progress)
    if (outputMoveBtn) {
        outputMoveBtn.addEventListener('click', handleOutputMoveToInProgress);
    }
}

/**
 * Handle copying task ID to clipboard
 */
async function handleCopyTaskId() {
    const modalTaskIdEl = document.getElementById('modal-task-id');
    const copyBtn = document.getElementById('copy-task-id-btn');

    if (!modalTaskIdEl || !modalTaskIdEl.textContent) return;

    const success = await copyToClipboard(modalTaskIdEl.textContent);

    if (success) {
        // Show success state briefly
        copyBtn.classList.add('copied');
        showNotification('Task ID copied to clipboard!', 'success');

        setTimeout(() => {
            copyBtn.classList.remove('copied');
        }, 2000);
    } else {
        showNotification('Failed to copy task ID', 'error');
    }
}

/**
 * Handle Accept button click in output modal - moves task from Done to In Progress
 */
async function handleOutputMoveToInProgress() {
    if (!outputModalTask) return;

    const taskId = outputModalTask.id;

    try {
        // Move task to in_progress column
        await updateTask(taskId, { column: 'in_progress' });

        // Close the modal
        outputModal.close();

        // Reload tasks to reflect the change
        await loadTasks();

        showNotification('Task moved to In Progress', 'success');
    } catch (error) {
        console.error('Failed to move task:', error);
        showNotification('Failed to move task. Please try again.', 'error');
    }
}

// ============================================
// Column API Functions
// ============================================

async function loadColumns() {
    try {
        const response = await fetch(`${API_BASE}/columns`);
        const newColumns = await response.json();

        // Check if columns have actually changed
        if (!columnsEqual(columns, newColumns)) {
            columns = newColumns;
            renderColumns();
            // After recreating columns, we must re-render tasks
            // since renderColumns() clears the board
            renderTasks();
        }
    } catch (error) {
        console.error('Failed to load columns:', error);
    }
}

/**
 * Compares two column arrays to determine if they are equivalent.
 */
function columnsEqual(oldColumns, newColumns) {
    if (oldColumns.length !== newColumns.length) return false;

    for (let i = 0; i < oldColumns.length; i++) {
        if (oldColumns[i].slug !== newColumns[i].slug ||
            oldColumns[i].name !== newColumns[i].name) {
            return false;
        }
    }

    return true;
}

async function createColumn(name) {
    const response = await fetch(`${API_BASE}/columns`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name })
    });
    if (!response.ok) {
        const err = await response.json();
        throw new Error(err.error || 'Failed to create column');
    }
    return response.json();
}

async function deleteColumn(slug) {
    const response = await fetch(`${API_BASE}/columns/${slug}`, {
        method: 'DELETE'
    });
    if (!response.ok) {
        const err = await response.json();
        throw new Error(err.error || 'Failed to delete column');
    }
}

async function updateColumn(slug, name) {
    const response = await fetch(`${API_BASE}/columns/${slug}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name })
    });
    if (!response.ok) {
        const err = await response.json();
        throw new Error(err.error || 'Failed to update column');
    }
    return response.json();
}

async function reorderColumns(slugs) {
    const response = await fetch(`${API_BASE}/columns/reorder`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ slugs })
    });
    if (!response.ok) {
        const err = await response.json();
        throw new Error(err.error || 'Failed to reorder columns');
    }
    return response.json();
}

// ============================================
// Task API Functions
// ============================================

async function loadTasks() {
    try {
        const response = await fetch(`${API_BASE}/tasks`);
        const newTasks = await response.json();

        // Check if tasks have actually changed to avoid unnecessary DOM operations
        if (!tasksEqual(tasks, newTasks)) {
            tasks = newTasks;
            renderTasks();
        }

        // Always update panel stale badge (stale status can change with time)
        updatePanelStaleBadge();
    } catch (error) {
        console.error('Failed to load tasks:', error);
    }
}

/**
 * Compares two task arrays to determine if they are equivalent.
 * Returns true if the tasks are the same (no render needed).
 */
function tasksEqual(oldTasks, newTasks) {
    if (oldTasks.length !== newTasks.length) return false;

    // Create a map for quick lookup
    const oldMap = new Map(oldTasks.map(t => [t.id, t]));

    for (const newTask of newTasks) {
        const oldTask = oldMap.get(newTask.id);
        if (!oldTask) return false;

        // Compare relevant fields (including metadata displayed on cards)
        if (oldTask.title !== newTask.title ||
            oldTask.column !== newTask.column ||
            oldTask.priority !== newTask.priority ||
            oldTask.test_status !== newTask.test_status ||
            oldTask.tests_passed !== newTask.tests_passed ||
            oldTask.tests_total !== newTask.tests_total ||
            oldTask.requires_test !== newTask.requires_test ||
            oldTask.acceptance_criteria !== newTask.acceptance_criteria ||
            oldTask.updated_at !== newTask.updated_at ||
            oldTask.updated_by !== newTask.updated_by ||
            oldTask.created_by !== newTask.created_by ||
            !testsArrayEqual(oldTask.tests, newTask.tests)) {
            return false;
        }
    }

    return true;
}

async function createTask(data) {
    const response = await fetch(`${API_BASE}/tasks`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data)
    });
    if (!response.ok) throw new Error('Failed to create task');
    return response.json();
}

async function updateTask(id, data) {
    const response = await fetch(`${API_BASE}/tasks/${id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data)
    });
    if (!response.ok) throw new Error('Failed to update task');
    return response.json();
}

async function deleteTask(id) {
    const response = await fetch(`${API_BASE}/tasks/${id}`, {
        method: 'DELETE'
    });
    if (!response.ok) throw new Error('Failed to delete task');
}

async function runTest(id) {
    const response = await fetch(`${API_BASE}/tasks/${id}/run`, {
        method: 'POST'
    });
    if (!response.ok) throw new Error('Failed to run test');
    return response.json();
}

// ============================================
// Column Render Functions
// ============================================

function renderColumns() {
    if (!board) return;
    board.innerHTML = '';

    columns.forEach(col => {
        const columnEl = createColumnElement(col);
        board.appendChild(columnEl);
    });

    // Add the "New Column" button at the end
    const addColumnBtn = document.createElement('button');
    addColumnBtn.id = 'add-column-btn';
    addColumnBtn.className = 'add-column-btn-inline';
    addColumnBtn.title = 'Add Column';
    addColumnBtn.innerHTML = `
        <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <line x1="12" y1="5" x2="12" y2="19"></line>
            <line x1="5" y1="12" x2="19" y2="12"></line>
        </svg>
        <span>New Column</span>
    `;
    addColumnBtn.addEventListener('click', handleAddColumn);
    board.appendChild(addColumnBtn);

    // Setup drag and drop for task lists
    document.querySelectorAll('.task-list').forEach(list => {
        list.addEventListener('dragover', handleDragOver);
        list.addEventListener('dragleave', handleDragLeave);
        list.addEventListener('drop', handleDrop);
    });
}

function createColumnElement(col) {
    const column = document.createElement('div');
    column.className = 'column flex-1 max-w-md min-w-[300px] flex flex-col h-full max-h-full';
    column.dataset.column = col.slug;
    column.draggable = true;

    column.innerHTML = `
        <header>
            <div class="column-header-content">
                <span class="column-drag-handle">
                    <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <circle cx="9" cy="5" r="1"></circle>
                        <circle cx="9" cy="12" r="1"></circle>
                        <circle cx="9" cy="19" r="1"></circle>
                        <circle cx="15" cy="5" r="1"></circle>
                        <circle cx="15" cy="12" r="1"></circle>
                        <circle cx="15" cy="19" r="1"></circle>
                    </svg>
                </span>
                <h2>${escapeHtml(col.name)}</h2>
            </div>
            <div class="column-actions">
                ${!isDefaultColumn(col.slug) ? `
                <button class="edit-column-btn" title="Rename Column">
                    <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <path d="M17 3a2.828 2.828 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5L17 3z"></path>
                    </svg>
                </button>
                <button class="delete-column-btn" title="Delete Column">
                    <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <polyline points="3 6 5 6 21 6"></polyline>
                        <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
                    </svg>
                </button>
                ` : ''}
            </div>
            <span class="task-count" data-column="${col.slug}">0</span>
        </header>
        <section class="task-list flex-1 overflow-y-auto" data-column="${col.slug}">
        </section>
    `;

    // Track mousedown to know if drag started from header
    column.addEventListener('mousedown', (e) => {
        const header = e.target.closest('header');
        const isActionButton = e.target.closest('.column-actions');
        columnDragAllowed = header && !isActionButton;
    });

    // Column drag events
    column.addEventListener('dragstart', handleColumnDragStart);
    column.addEventListener('dragend', handleColumnDragEnd);
    column.addEventListener('dragover', handleColumnDragOver);
    column.addEventListener('dragleave', handleColumnDragLeave);
    column.addEventListener('drop', handleColumnDrop);

    // Edit column button (only exists for non-default columns)
    const editBtn = column.querySelector('.edit-column-btn');
    if (editBtn) {
        editBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            handleEditColumn(col.slug);
        });
    }

    // Delete column button (only exists for non-default columns)
    const deleteBtn = column.querySelector('.delete-column-btn');
    if (deleteBtn) {
        deleteBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            handleDeleteColumn(col.slug);
        });
    }

    return column;
}

// ============================================
// Task Render Functions
// ============================================

/**
 * Renders tasks using differential DOM updates.
 * Only creates, removes, or updates cards that have actually changed,
 * avoiding the flash/flicker caused by destroying and recreating all cards.
 */
function renderTasks() {
    // Build a map of existing cards by task ID
    const existingCards = new Map();
    document.querySelectorAll('.task-card').forEach(card => {
        existingCards.set(card.dataset.id, card);
    });

    // Track which task IDs are still present
    const currentTaskIds = new Set(tasks.map(t => t.id));

    // Remove cards for tasks that no longer exist
    existingCards.forEach((card, taskId) => {
        if (!currentTaskIds.has(taskId)) {
            card.remove();
        }
    });

    // Count tasks per column
    const counts = {};
    columns.forEach(col => counts[col.slug] = 0);

    // Group tasks by column, preserving order from TASKS.md
    const tasksByColumn = new Map();
    columns.forEach(col => tasksByColumn.set(col.slug, []));
    tasks.forEach(task => {
        if (tasksByColumn.has(task.column)) {
            tasksByColumn.get(task.column).push(task);
        }
    });

    // Process each column
    tasksByColumn.forEach((columnTasks, columnSlug) => {
        const list = document.querySelector(`.task-list[data-column="${columnSlug}"]`);
        if (!list) return;

        counts[columnSlug] = columnTasks.length;

        // Process tasks in order
        columnTasks.forEach((task, index) => {
            let card = existingCards.get(task.id);

            if (card) {
                // Card exists - update it in place if data changed
                updateTaskCard(card, task);

                // Move to correct column if needed
                if (card.parentElement !== list) {
                    list.appendChild(card);
                }
            } else {
                // Create new card
                card = createTaskCard(task);
                list.appendChild(card);
            }

            // Ensure correct order within column
            const currentIndex = Array.from(list.children).indexOf(card);
            if (currentIndex !== index) {
                const referenceNode = list.children[index];
                if (referenceNode) {
                    list.insertBefore(card, referenceNode);
                } else {
                    list.appendChild(card);
                }
            }
        });
    });

    // Update counts
    Object.entries(counts).forEach(([column, count]) => {
        const countEl = document.querySelector(`.task-count[data-column="${column}"]`);
        if (countEl) countEl.textContent = count;
    });
}

/**
 * Updates an existing task card's content in place without recreating it.
 * Only updates elements that have actually changed.
 */
function updateTaskCard(card, task) {
    const hasTest = taskHasTest(task);
    const requiresTest = task.requires_test || false;
    const priorityClass = `priority-${task.priority || 'medium'}`;
    const taskAuthor = task.updated_by || task.created_by || '';
    const uncommittedClass = isUncommitted(taskAuthor) ? ' uncommitted' : '';

    // Update priority class if changed
    // Apply no-test class only if task doesn't require a test
    const expectedClasses = `task-card ${priorityClass}` + (!requiresTest ? ' no-test' : '') + uncommittedClass;
    if (card.className !== expectedClasses) {
        card.className = expectedClasses;
    }

    // Update title if changed
    const titleEl = card.querySelector('.task-title-clickable');
    if (titleEl && titleEl.textContent !== task.title) {
        titleEl.textContent = task.title;
    }

    // Update strikethrough for done column
    if (titleEl) {
        titleEl.classList.toggle('task-done', task.column === 'done');
    }

    // Handle criteria icon
    const hasCriteria = task.acceptance_criteria && task.acceptance_criteria.trim() !== '';
    const existingCriteriaIcon = card.querySelector('.task-criteria-icon');
    const taskHeader = card.querySelector('.task-header');

    if (hasCriteria && !existingCriteriaIcon && taskHeader) {
        // Add criteria icon
        const iconHtml = `<span class="task-criteria-icon" title="Has acceptance criteria">
            <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <rect width="8" height="4" x="8" y="2" rx="1" ry="1"></rect>
                <path d="M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2"></path>
                <path d="M12 11h4"></path>
                <path d="M12 16h4"></path>
                <path d="M8 11h.01"></path>
                <path d="M8 16h.01"></path>
            </svg>
        </span>`;
        taskHeader.insertAdjacentHTML('afterbegin', iconHtml);
    } else if (!hasCriteria && existingCriteriaIcon) {
        // Remove criteria icon
        existingCriteriaIcon.remove();
    }

    // Handle test meta section - rebuild with new structure
    const existingMeta = card.querySelector('.task-meta');
    const dateText = formatCardDate(task.updated_at || task.created_at);
    const author = task.updated_by || task.created_by || '';
    const authorName = getFirstName(author); // Returns "Uncommitted" if no author

    // Icon SVGs for metadata
    const userIcon = `<svg class="meta-icon" xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>`;
    const calendarIcon = `<svg class="meta-icon" xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect width="18" height="18" x="3" y="4" rx="2" ry="2"/><line x1="16" x2="16" y1="2" y2="6"/><line x1="8" x2="8" y1="2" y2="6"/><line x1="3" x2="21" y1="10" y2="10"/></svg>`;
    const beakerIcon = `<svg class="meta-icon" xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4.5 3h15"/><path d="M6 3v16a2 2 0 0 0 2 2h8a2 2 0 0 0 2-2V3"/><path d="M6 14h12"/></svg>`;

    const fullAuthor = author || 'Uncommitted';
    const authorHtml = `<span class="task-meta-item" title="Author: ${escapeHtml(fullAuthor)}">${userIcon}<span class="task-author">${escapeHtml(authorName)}</span></span>`;
    const fullDate = task.updated_at ? new Date(task.updated_at).toLocaleString() : (task.created_at ? new Date(task.created_at).toLocaleString() : '');
    const dateHtml = dateText ? `<span class="task-meta-item" title="Updated: ${escapeHtml(fullDate)}">${calendarIcon}<span class="task-date">${escapeHtml(dateText)}</span></span>` : '';

    // Helper to build the new meta HTML
    const buildMetaHtml = () => {
        if (hasTest) {
            const testDisplay = getTestDisplayText(task);
            const progressHtml = createTestProgressHTML(task);
            // Show progress indicator if tests have run, otherwise show test count with icon
            const testInfoHtml = progressHtml
                ? progressHtml
                : `<span class="task-meta-item" title="Tests configured">${beakerIcon}<span class="task-test-count">${escapeHtml(testDisplay)}</span></span>`;
            return `<div class="task-meta">
                ${authorHtml}
                ${dateHtml}
                ${testInfoHtml}
            </div>`;
        } else if (requiresTest) {
            return `<div class="task-meta">
                ${authorHtml}
                ${dateHtml}
                <span class="task-meta-item" title="No tests configured">${beakerIcon}<span class="task-test-count no-test">No test</span></span>
            </div>`;
        } else {
            // No test required - show author and date
            return `<div class="task-meta">
                ${authorHtml}
                ${dateHtml}
            </div>`;
        }
    };

    // Update or create meta section
    const newMetaHtml = buildMetaHtml();
    if (existingMeta) {
        // Check if we need to update by comparing content
        const tempDiv = document.createElement('div');
        tempDiv.innerHTML = newMetaHtml;
        const newMetaContent = tempDiv.firstElementChild?.innerHTML || '';
        if (existingMeta.innerHTML !== newMetaContent) {
            if (newMetaHtml) {
                existingMeta.outerHTML = newMetaHtml;
            } else {
                existingMeta.remove();
            }
        }
    } else if (newMetaHtml) {
        card.insertAdjacentHTML('beforeend', newMetaHtml);
    }

    // Handle play button based on test state
    if (hasTest) {
        // Ensure play button exists
        const actionsContainer = card.querySelector('.task-actions');
        if (!actionsContainer) {
            const header = card.querySelector('.task-header');
            if (header) {
                const actionsHtml = `<div class="task-actions">
                    <button class="play-btn" title="Run Test">&#9658;</button>
                </div>`;
                header.insertAdjacentHTML('beforeend', actionsHtml);
                // Re-attach event listener
                const playBtn = header.querySelector('.play-btn');
                if (playBtn) {
                    playBtn.addEventListener('click', (e) => {
                        e.stopPropagation();
                        handleRunTest(task.id, playBtn);
                    });
                }
            }
        }
    } else {
        // Remove play button since no test is configured
        const actionsContainer = card.querySelector('.task-actions');
        if (actionsContainer) {
            actionsContainer.remove();
        }
    }
}

/**
 * Check if a task has a test associated with it
 * Supports the new tests array format
 */
function taskHasTest(task) {
    return task.tests && task.tests.length > 0 &&
           task.tests.some(t => t.file && t.file.trim() !== '' && t.func && t.func.trim() !== '');
}

/**
 * Get display text for tests count (e.g., "1 test" or "3 tests")
 */
function getTestDisplayText(task) {
    if (!taskHasTest(task)) return '';
    const count = task.tests.length;
    return count === 1 ? '1 test' : `${count} tests`;
}

/**
 * Create radial progress indicator HTML for test results
 * Shows a small circular progress ring with passed/total text
 * Color: green if all pass, red if any fail
 */
function createTestProgressHTML(task) {
    const hasRun = task.test_status && task.test_status !== 'pending';
    if (!hasRun) return '';

    const passed = task.tests_passed || 0;
    const total = task.tests_total || task.tests?.length || 0;
    if (total === 0) return '';

    const status = task.test_status; // passed, failed, running
    const progress = total > 0 ? passed / total : 0;

    // SVG circle calculations
    const radius = 5.5;
    const circumference = 2 * Math.PI * radius;
    const offset = circumference * (1 - progress);

    // Build tooltip text
    const statusText = status === 'passed' ? 'All tests passing' :
                       status === 'failed' ? `${total - passed} test${total - passed !== 1 ? 's' : ''} failing` :
                       'Tests running...';
    const tooltip = `${passed} of ${total} tests passing - ${statusText}`;

    return `<span class="test-progress ${status}" title="${tooltip}">
        <svg class="test-progress-ring" viewBox="0 0 16 16">
            <circle class="test-progress-track" cx="8" cy="8" r="${radius}"/>
            <circle class="test-progress-bar" cx="8" cy="8" r="${radius}"
                stroke-dasharray="${circumference}"
                stroke-dashoffset="${offset}"/>
        </svg>
        <span class="test-progress-text">${passed}/${total}</span>
    </span>`;
}

/**
 * Extract first name from a full name (e.g., "Tate McCormick" -> "Tate")
 * Returns "Uncommitted" for uncommitted tasks (empty author or "Not Committed Yet")
 */
function getFirstName(fullName) {
    // isUncommitted handles both empty and "Not Committed Yet"
    if (isUncommitted(fullName)) return 'Uncommitted';
    return fullName.split(' ')[0];
}

/**
 * Check if the author indicates uncommitted changes
 * Empty author means task hasn't been committed yet
 */
function isUncommitted(author) {
    return !author || author === 'Not Committed Yet';
}

/**
 * Compare two test arrays for equality
 */
function testsArrayEqual(a, b) {
    if (!a && !b) return true;
    if (!a || !b) return false;
    if (a.length !== b.length) return false;
    return a.every((item, i) => item.file === b[i].file && item.func === b[i].func);
}

/**
 * Creates HTML for a test entry row
 */
function createTestEntryHTML(test = {file: '', func: ''}, index) {
    return `
        <div class="test-entry" data-index="${index}">
            <input type="text" name="tests[${index}][file]" value="${escapeHtml(test.file || '')}"
                   class="input-field" placeholder="path/to/test.go">
            <input type="text" name="tests[${index}][func]" value="${escapeHtml(test.func || '')}"
                   class="input-field" placeholder="TestFunctionName">
            <button type="button" class="remove-test-btn" title="Remove test">&times;</button>
        </div>
    `;
}

/**
 * Updates the visibility of the tests helper text based on test count
 */
function updateTestsHelperVisibility() {
    const container = document.getElementById('panel-tests-container');
    const helper = document.getElementById('panel-tests-helper');
    if (!helper) return;

    const testCount = container?.querySelectorAll('.test-entry').length || 0;
    if (testCount > 0) {
        helper.classList.remove('hidden');
    } else {
        helper.classList.add('hidden');
    }
}

/**
 * Attaches event listeners to test entry elements
 */
function attachTestEntryListeners() {
    document.querySelectorAll('.test-entry .remove-test-btn').forEach(btn => {
        btn.onclick = (e) => {
            const container = document.getElementById('panel-tests-container');
            e.target.closest('.test-entry').remove();
            // Ensure container is truly empty (no whitespace) for CSS :empty selector
            if (container && container.querySelectorAll('.test-entry').length === 0) {
                container.innerHTML = '';
            }
            updateTestsHelperVisibility();
            updatePanelSaveButton();
        };
    });
    document.querySelectorAll('.test-entry input').forEach(input => {
        input.removeEventListener('input', updatePanelSaveButton);
        input.addEventListener('input', updatePanelSaveButton);
    });
}

function createTaskCard(task) {
    const card = document.createElement('div');
    const hasTest = taskHasTest(task);
    const requiresTest = task.requires_test || false;
    const priorityClass = `priority-${task.priority || 'medium'}`;
    const taskAuthor = task.updated_by || task.created_by || '';
    const uncommittedClass = isUncommitted(taskAuthor) ? ' uncommitted' : '';
    // Apply no-test class only if task doesn't require a test
    card.className = `task-card ${priorityClass}` + (!requiresTest ? ' no-test' : '') + uncommittedClass;
    card.draggable = true;
    card.dataset.id = task.id;

    // Build actions HTML - only include play button if task has a test configured
    const actionsHtml = hasTest
        ? `<div class="task-actions">
               <button class="play-btn" title="Run Test">&#9658;</button>
           </div>`
        : '';

    // Build meta HTML - show author, date, and optionally test info with icons
    let metaHtml = '';
    const dateText = formatCardDate(task.updated_at || task.created_at);
    const author = task.updated_by || task.created_by || '';
    const authorName = getFirstName(author); // Returns "Uncommitted" if no author

    // Icon SVGs for metadata
    const userIcon = `<svg class="meta-icon" xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>`;
    const calendarIcon = `<svg class="meta-icon" xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect width="18" height="18" x="3" y="4" rx="2" ry="2"/><line x1="16" x2="16" y1="2" y2="6"/><line x1="8" x2="8" y1="2" y2="6"/><line x1="3" x2="21" y1="10" y2="10"/></svg>`;
    const beakerIcon = `<svg class="meta-icon" xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4.5 3h15"/><path d="M6 3v16a2 2 0 0 0 2 2h8a2 2 0 0 0 2-2V3"/><path d="M6 14h12"/></svg>`;

    const fullAuthor = author || 'Uncommitted';
    const authorHtml = `<span class="task-meta-item" title="Author: ${escapeHtml(fullAuthor)}">${userIcon}<span class="task-author">${escapeHtml(authorName)}</span></span>`;
    const fullDate = task.updated_at ? new Date(task.updated_at).toLocaleString() : (task.created_at ? new Date(task.created_at).toLocaleString() : '');
    const dateHtml = dateText ? `<span class="task-meta-item" title="Updated: ${escapeHtml(fullDate)}">${calendarIcon}<span class="task-date">${escapeHtml(dateText)}</span></span>` : '';

    if (hasTest) {
        // Task has test configured - show author, date, and test progress indicator
        const testDisplay = getTestDisplayText(task);
        const progressHtml = createTestProgressHTML(task);
        // Show progress indicator if tests have run, otherwise show test count with icon
        const testInfoHtml = progressHtml
            ? progressHtml
            : `<span class="task-meta-item" title="Tests configured">${beakerIcon}<span class="task-test-count">${escapeHtml(testDisplay)}</span></span>`;
        metaHtml = `<div class="task-meta">
               ${authorHtml}
               ${dateHtml}
               ${testInfoHtml}
           </div>`;
    } else if (requiresTest) {
        // Task requires test but none configured - show author, date and "No test" indicator
        metaHtml = `<div class="task-meta">
               ${authorHtml}
               ${dateHtml}
               <span class="task-meta-item" title="No tests configured">${beakerIcon}<span class="task-test-count no-test">No test</span></span>
           </div>`;
    } else {
        // No test required - show author and date
        metaHtml = `<div class="task-meta">
               ${authorHtml}
               ${dateHtml}
           </div>`;
    }

    // Build stale icon - show red exclamation mark if task is stale
    const stale = isTaskStale(task);
    const staleIconHtml = stale
        ? `<span class="task-stale-icon" title="Task is stale (not updated in ${staleThresholdDays}+ days)">
               <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="currentColor" stroke="none">
                   <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm1 15h-2v-2h2v2zm0-4h-2V7h2v6z"/>
               </svg>
           </span>`
        : '';

    // Build criteria icon - show clipboard if task has acceptance criteria
    const hasCriteria = task.acceptance_criteria && task.acceptance_criteria.trim() !== '';
    const criteriaIconHtml = hasCriteria
        ? `<span class="task-criteria-icon" title="Has acceptance criteria">
               <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                   <rect width="8" height="4" x="8" y="2" rx="1" ry="1"></rect>
                   <path d="M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2"></path>
                   <path d="M12 11h4"></path>
                   <path d="M12 16h4"></path>
                   <path d="M8 11h.01"></path>
                   <path d="M8 16h.01"></path>
               </svg>
           </span>`
        : '';

    card.innerHTML = `
        <div class="task-header">
            ${staleIconHtml}${criteriaIconHtml}<span class="task-title task-title-clickable${task.column === 'done' ? ' task-done' : ''}" title="Click to copy task ID">${escapeHtml(task.title)}</span>
            ${actionsHtml}
        </div>
        ${metaHtml}
    `;

    // Event listeners
    const titleEl = card.querySelector('.task-title-clickable');

    // Only add play button listener if task has a test
    if (hasTest) {
        const playBtn = card.querySelector('.play-btn');
        playBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            handleRunTest(task.id, playBtn);
        });
    }

    // Title click to copy task ID
    titleEl.addEventListener('click', async (e) => {
        e.stopPropagation();
        const success = await copyToClipboard(task.id);
        if (success) {
            showNotification('Task ID copied to clipboard!', 'success');
        } else {
            showNotification('Failed to copy task ID', 'error');
        }
    });

    // Drag events
    card.addEventListener('dragstart', handleDragStart);
    card.addEventListener('dragend', handleDragEnd);

    // Click on card (not title) to open task panel
    card.addEventListener('click', (e) => {
        // Only open panel if click wasn't on title or action buttons
        if (!e.target.classList.contains('task-title-clickable') &&
            !e.target.closest('.task-actions')) {
            openTaskPanel(task);
        }
    });

    return card;
}

function formatStatus(status) {
    const labels = {
        pending: 'Pending',
        running: 'Running...',
        passed: 'Passed',
        failed: 'Failed'
    };
    return labels[status] || status;
}

function escapeHtml(text) {
    if (!text) return '';
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

// ============================================
// Column Handlers
// ============================================

async function handleAddColumn() {
    const name = await showPromptDialog('Enter a name for the new column:', {
        title: 'New Column',
        placeholder: 'Column name',
        confirmText: 'Create'
    });

    if (!name || !name.trim()) return;

    try {
        await createColumn(name.trim());
        await loadColumns();
        renderTasks();
    } catch (error) {
        console.error('Failed to create column:', error);
        showNotification(error.message || 'Failed to create column', 'error');
    }
}

async function handleEditColumn(slug) {
    const col = columns.find(c => c.slug === slug);
    if (!col) return;

    const newName = await showPromptDialog('Enter new column name:', {
        title: 'Rename Column',
        placeholder: 'Column name',
        confirmText: 'Rename',
        defaultValue: col.name
    });

    if (!newName || !newName.trim() || newName.trim() === col.name) return;

    try {
        await updateColumn(slug, newName.trim());
        await loadColumns();
        renderTasks();
        showNotification(`Column renamed to "${newName.trim()}"`, 'success');
    } catch (error) {
        console.error('Failed to rename column:', error);
        showNotification(error.message || 'Failed to rename column', 'error');
    }
}

async function handleDeleteColumn(slug) {
    const col = columns.find(c => c.slug === slug);
    if (!col) return;

    const confirmed = await showConfirmDialog(`Delete column "${col.name}"? Only empty columns can be deleted.`, {
        title: 'Delete Column',
        confirmText: 'Delete',
        destructive: true
    });

    if (!confirmed) {
        return;
    }

    try {
        await deleteColumn(slug);
        await loadColumns();
        renderTasks();
    } catch (error) {
        console.error('Failed to delete column:', error);
        showNotification(error.message || 'Failed to delete column', 'error');
    }
}

// Column drag and drop
function handleColumnDragStart(e) {
    // Ignore bubbled events from child elements (like task cards)
    if (e.target !== e.currentTarget) {
        return;
    }

    // Only allow drag if mousedown was on header (not action buttons)
    if (!columnDragAllowed) {
        e.preventDefault();
        return false;
    }

    draggedColumn = e.currentTarget;
    e.currentTarget.classList.add('dragging');
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', e.currentTarget.dataset.column);
}

function handleColumnDragEnd(e) {
    e.currentTarget.classList.remove('dragging');
    draggedColumn = null;

    document.querySelectorAll('.column').forEach(col => {
        col.classList.remove('drag-over-column');
    });
}

function handleColumnDragOver(e) {
    if (!draggedColumn) return;

    e.preventDefault();
    e.stopPropagation();
    e.dataTransfer.dropEffect = 'move';

    const column = e.currentTarget;
    if (column && column !== draggedColumn) {
        column.classList.add('drag-over-column');
    }
}

function handleColumnDragLeave(e) {
    if (!draggedColumn) return;
    e.currentTarget.classList.remove('drag-over-column');
}

async function handleColumnDrop(e) {
    if (!draggedColumn) return;

    e.preventDefault();
    e.stopPropagation();

    const targetColumn = e.currentTarget;
    if (!targetColumn || targetColumn === draggedColumn) return;

    targetColumn.classList.remove('drag-over-column');

    const draggedSlug = draggedColumn.dataset.column;
    const targetSlug = targetColumn.dataset.column;

    // Calculate new order
    const currentOrder = columns.map(c => c.slug);
    const draggedIndex = currentOrder.indexOf(draggedSlug);
    const targetIndex = currentOrder.indexOf(targetSlug);

    // Remove dragged and insert at target position
    currentOrder.splice(draggedIndex, 1);
    currentOrder.splice(targetIndex, 0, draggedSlug);

    try {
        columns = await reorderColumns(currentOrder);
        renderColumns();
        renderTasks();
    } catch (error) {
        console.error('Failed to reorder columns:', error);
        showNotification(error.message || 'Failed to reorder columns', 'error');
    }
}

// ============================================
// Task Modal Functions (New Task Dialog Only)
// ============================================

/**
 * Opens the new task dialog (for creating new tasks only)
 * Task editing is now handled by the Task Panel
 */
function openNewTaskModal() {
    if (!taskModal) {
        console.error('Task modal not found');
        return;
    }

    const titleInput = document.getElementById('title');

    // Reset form
    taskForm.reset();

    // Default to medium priority
    taskForm.querySelector('input[name="priority"][value="medium"]').checked = true;

    // Default requires_test to unchecked for new tasks
    if (requiresTestCheckbox) {
        requiresTestCheckbox.checked = false;
    }

    taskModal.showModal();
    titleInput.focus();
}

// Alias for backward compatibility
function openTaskModal(task = null) {
    if (task) {
        // If called with a task, open the panel instead
        openTaskPanel(task);
    } else {
        openNewTaskModal();
    }
}

// Track current task for the output modal's Accept button
let outputModalTask = null;

function showOutput(task, results = null) {
    outputModalTask = task;
    const output = task.last_output || 'No output available';

    // Clear previous content
    testOutput.innerHTML = '';

    // Check if output is gotestsum JSON format
    if (isGotestsumJSON(output)) {
        try {
            const parsed = parseGotestsumOutput(output);
            const richUI = renderGotestsumResults(parsed);
            testOutput.appendChild(richUI);
            testOutput.classList.add('rich-output');
        } catch (e) {
            console.error('Failed to parse gotestsum output:', e);
            // Fallback to raw text
            testOutput.textContent = output;
            testOutput.classList.remove('rich-output');
        }
    } else {
        // Raw text output (existing behavior)
        testOutput.textContent = output;
        testOutput.classList.remove('rich-output');
    }

    // Check if task is in Done column and tests failed
    const isInDone = task.column === 'done';
    const hasFailed = results && !results.all_passed;

    if (isInDone && hasFailed) {
        // Calculate failed count
        const failedCount = results.results ? results.results.filter(r => !r.passed).length : 0;
        const testWord = failedCount === 1 ? 'test' : 'tests';
        outputFailedCount.textContent = `${failedCount} ${testWord} failed. Move task to In Progress?`;
        outputMovePrompt.classList.remove('hidden');
    } else {
        outputMovePrompt.classList.add('hidden');
    }

    outputModal.showModal();
}

// ============================================
// Task Metadata Functions
// ============================================

function formatDateTime(dateString) {
    if (!dateString) return null;
    const date = new Date(dateString);
    if (isNaN(date.getTime())) return null;

    // Format: "Dec 28, 2025 at 10:44 AM"
    return date.toLocaleDateString('en-US', {
        month: 'short',
        day: 'numeric',
        year: 'numeric'
    }) + ' at ' + date.toLocaleTimeString('en-US', {
        hour: 'numeric',
        minute: '2-digit',
        hour12: true
    });
}

/**
 * Format a date for display on task cards (compact format)
 * Shows: "Updated Dec 30" or "Updated Yesterday" etc.
 */
function formatCardDate(dateString) {
    if (!dateString) return '';
    const date = new Date(dateString);
    if (isNaN(date.getTime())) return '';

    const now = new Date();
    const diffMs = now - date;
    const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24));

    if (diffDays === 0) {
        return 'Updated today';
    } else if (diffDays === 1) {
        return 'Updated yesterday';
    } else if (diffDays < 7) {
        return `Updated ${diffDays} days ago`;
    } else {
        // Format as "Updated Dec 30"
        return 'Updated ' + date.toLocaleDateString('en-US', {
            month: 'short',
            day: 'numeric'
        });
    }
}

function updateTaskMetadata(task) {
    const metadataContainer = document.getElementById('task-metadata');
    const createdSection = document.getElementById('metadata-created');
    const updatedSection = document.getElementById('metadata-updated');
    const createdAtEl = document.getElementById('metadata-created-at');
    const createdByEl = document.getElementById('metadata-created-by');
    const updatedAtEl = document.getElementById('metadata-updated-at');
    const updatedByEl = document.getElementById('metadata-updated-by');

    if (!metadataContainer) return;

    let hasAnyMetadata = false;

    // Handle created_at and created_by
    const createdAt = formatDateTime(task.created_at);
    if (createdAt) {
        createdAtEl.textContent = createdAt;
        createdByEl.textContent = task.created_by ? ` by ${task.created_by}` : '';
        createdSection.classList.remove('hidden');
        hasAnyMetadata = true;
    } else {
        createdSection.classList.add('hidden');
    }

    // Handle updated_at and updated_by
    const updatedAt = formatDateTime(task.updated_at);
    if (updatedAt) {
        updatedAtEl.textContent = updatedAt;
        updatedByEl.textContent = task.updated_by ? ` by ${task.updated_by}` : '';
        updatedSection.classList.remove('hidden');
        hasAnyMetadata = true;
    } else {
        updatedSection.classList.add('hidden');
    }

    // Show or hide the entire metadata container
    if (hasAnyMetadata) {
        metadataContainer.classList.remove('hidden');
    } else {
        metadataContainer.classList.add('hidden');
    }
}

function hideTaskMetadata() {
    const metadataContainer = document.getElementById('task-metadata');
    if (metadataContainer) {
        metadataContainer.classList.add('hidden');
    }
}

// ============================================
// Task Panel Functions (Slide-in Panel)
// ============================================

/**
 * Opens the task panel and populates it with task data
 */
function openTaskPanel(task) {
    if (!taskPanel || !task) return;

    // Always fetch the latest task data from the tasks array
    const freshTask = tasks.find(t => t.id === task.id) || task;
    currentPanelTask = freshTask;
    task = freshTask;

    // Populate form fields
    const idInput = document.getElementById('panel-task-id-input');
    const titleEl = document.getElementById('panel-title');
    const titleInput = document.getElementById('panel-title-input');
    const criteriaInput = document.getElementById('panel-criteria-input');
    const testsContainer = document.getElementById('panel-tests-container');
    const taskIdEl = document.getElementById('panel-task-id');

    if (idInput) idInput.value = task.id;
    if (titleEl) titleEl.textContent = task.title || '';
    if (titleInput) titleInput.value = task.title || '';
    if (criteriaInput) criteriaInput.value = task.acceptance_criteria || '';
    if (taskIdEl) taskIdEl.textContent = task.id;
    if (panelRequiresTestCheckbox) panelRequiresTestCheckbox.checked = task.requires_test || false;

    // Update stale badge visibility and tooltip
    const staleBadge = document.getElementById('panel-stale-badge');
    if (staleBadge) {
        const stale = isTaskStale(task);
        if (stale) {
            // Calculate days since last update
            const updatedAt = new Date(task.updated_at);
            const now = new Date();
            const diffMs = now - updatedAt;
            const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24));
            const lastUpdateDate = updatedAt.toLocaleDateString('en-US', {
                month: 'short',
                day: 'numeric',
                year: 'numeric'
            });
            staleBadge.title = `This task has not been updated in ${diffDays} days (last updated ${lastUpdateDate}). Tasks are marked stale after ${staleThresholdDays} days of inactivity.`;
            staleBadge.classList.remove('hidden');
        } else {
            staleBadge.classList.add('hidden');
        }
    }

    // Populate tests container with test entries
    if (testsContainer) {
        testsContainer.innerHTML = '';
        if (task.tests && task.tests.length > 0) {
            task.tests.forEach((test, i) => {
                testsContainer.insertAdjacentHTML('beforeend', createTestEntryHTML(test, i));
            });
        }
        attachTestEntryListeners();
        updateTestsHelperVisibility();
    }

    // Ensure title is in display mode (not edit mode)
    hideTitleEditMode();

    // Set priority radio button
    const priorityRadio = panelTaskForm?.querySelector(`input[name="panel-priority"][value="${task.priority || 'medium'}"]`);
    if (priorityRadio) priorityRadio.checked = true;

    // Update metadata display
    updatePanelMetadata(task);

    // Store original values for change detection
    panelOriginalValues = {
        title: task.title || '',
        acceptance_criteria: task.acceptance_criteria || '',
        priority: task.priority || 'medium',
        requires_test: task.requires_test || false,
        tests: JSON.stringify(task.tests || [])
    };

    // Disable save button initially (no changes yet)
    updatePanelSaveButton();

    // Show panel with animation
    taskPanelOverlay?.classList.add('visible');
    taskPanel.classList.add('open');

    // Focus the criteria textarea after animation
    setTimeout(() => criteriaInput?.focus(), 300);
}

/**
 * Closes the task panel
 */
function closeTaskPanel() {
    if (!taskPanel) return;

    taskPanel.classList.remove('open');
    taskPanelOverlay?.classList.remove('visible');
    currentPanelTask = null;
    panelOriginalValues = null;

    // Reset form and title edit mode after animation
    setTimeout(() => {
        panelTaskForm?.reset();
        hideTitleEditMode();
    }, 300);
}

/**
 * Gets the current form values from the panel
 */
function getPanelFormValues() {
    const titleEl = document.getElementById('panel-title');
    const criteriaInput = document.getElementById('panel-criteria-input');
    const priorityRadio = panelTaskForm?.querySelector('input[name="panel-priority"]:checked');

    // Collect tests array from test entries
    const tests = [];
    document.querySelectorAll('.test-entry').forEach((entry) => {
        const fileInput = entry.querySelector('input[name$="[file]"]');
        const funcInput = entry.querySelector('input[name$="[func]"]');
        const file = fileInput?.value?.trim() || '';
        const func = funcInput?.value?.trim() || '';
        // Only include entries with at least one field filled
        if (file || func) {
            tests.push({ file, func });
        }
    });

    return {
        title: titleEl?.textContent || '',
        acceptance_criteria: criteriaInput?.value || '',
        priority: priorityRadio?.value || 'medium',
        requires_test: panelRequiresTestCheckbox?.checked || false,
        tests: tests
    };
}

/**
 * Checks if the panel form has unsaved changes
 */
function panelHasChanges() {
    if (!panelOriginalValues) return false;

    const current = getPanelFormValues();
    const currentTestsJson = JSON.stringify(current.tests);

    return current.title !== panelOriginalValues.title ||
           current.acceptance_criteria !== panelOriginalValues.acceptance_criteria ||
           current.priority !== panelOriginalValues.priority ||
           current.requires_test !== panelOriginalValues.requires_test ||
           currentTestsJson !== panelOriginalValues.tests;
}

/**
 * Updates the save button state based on whether there are changes
 */
function updatePanelSaveButton() {
    if (!panelSaveBtn) return;

    const hasChanges = panelHasChanges();
    panelSaveBtn.disabled = !hasChanges;

    if (hasChanges) {
        panelSaveBtn.classList.remove('opacity-50', 'cursor-not-allowed');
    } else {
        panelSaveBtn.classList.add('opacity-50', 'cursor-not-allowed');
    }
}

/**
 * Shows the title edit mode
 */
function showTitleEditMode() {
    const displayEl = document.getElementById('panel-title-display');
    const editEl = document.getElementById('panel-title-edit');
    const titleInput = document.getElementById('panel-title-input');
    const titleEl = document.getElementById('panel-title');

    if (displayEl) displayEl.classList.add('hidden');
    if (editEl) editEl.classList.remove('hidden');

    // Sync current title to input and focus
    if (titleInput && titleEl) {
        titleInput.value = titleEl.textContent || '';
        titleInput.focus();
        titleInput.select();
    }
}

/**
 * Hides the title edit mode without saving
 */
function hideTitleEditMode() {
    const displayEl = document.getElementById('panel-title-display');
    const editEl = document.getElementById('panel-title-edit');

    if (displayEl) displayEl.classList.remove('hidden');
    if (editEl) editEl.classList.add('hidden');
}

/**
 * Saves the edited title
 */
async function saveTitleEdit() {
    const titleInput = document.getElementById('panel-title-input');
    const titleEl = document.getElementById('panel-title');

    if (!titleInput || !currentPanelTask) return;

    const newTitle = titleInput.value.trim();
    if (!newTitle) {
        showNotification('Title cannot be empty', 'error');
        titleInput.focus();
        return;
    }

    // Update display immediately
    if (titleEl) titleEl.textContent = newTitle;
    hideTitleEditMode();

    // Update the task
    try {
        const formData = new FormData(panelTaskForm);
        await updateTask(currentPanelTask.id, {
            title: newTitle,
            acceptance_criteria: formData.get('acceptance_criteria'),
            priority: formData.get('panel-priority')
        });
        showNotification('Title updated', 'success');
        // Reload tasks - this will update currentPanelTask reference via renderTasks
        await loadTasks();
        // Update currentPanelTask reference to the fresh task object
        currentPanelTask = tasks.find(t => t.id === currentPanelTask.id) || currentPanelTask;
        // Update original values to reflect the saved state
        if (panelOriginalValues) {
            panelOriginalValues.title = newTitle;
        }
        updatePanelSaveButton();
    } catch (error) {
        console.error('Failed to update title:', error);
        showNotification('Failed to update title', 'error');
        // Revert title display
        if (titleEl) titleEl.textContent = currentPanelTask.title || '';
        updatePanelSaveButton();
    }
}

/**
 * Updates the metadata display in the panel
 */
function updatePanelMetadata(task) {
    const metadataContainer = document.getElementById('panel-task-metadata');
    const createdSection = document.getElementById('panel-metadata-created');
    const updatedSection = document.getElementById('panel-metadata-updated');
    const createdAtEl = document.getElementById('panel-metadata-created-at');
    const createdByEl = document.getElementById('panel-metadata-created-by');
    const updatedAtEl = document.getElementById('panel-metadata-updated-at');
    const updatedByEl = document.getElementById('panel-metadata-updated-by');

    if (!metadataContainer) return;

    let hasAnyMetadata = false;

    // Handle created_at and created_by
    const createdAt = formatDateTime(task.created_at);
    if (createdAt && createdSection) {
        createdAtEl.textContent = createdAt;
        createdByEl.textContent = task.created_by ? ` by ${task.created_by}` : '';
        createdSection.classList.remove('hidden');
        hasAnyMetadata = true;
    } else if (createdSection) {
        createdSection.classList.add('hidden');
    }

    // Handle updated_at and updated_by
    const updatedAt = formatDateTime(task.updated_at);
    if (updatedAt && updatedSection) {
        updatedAtEl.textContent = updatedAt;
        updatedByEl.textContent = task.updated_by ? ` by ${task.updated_by}` : '';
        updatedSection.classList.remove('hidden');
        hasAnyMetadata = true;
    } else if (updatedSection) {
        updatedSection.classList.add('hidden');
    }

    // Show or hide the entire metadata container
    if (hasAnyMetadata) {
        metadataContainer.classList.remove('hidden');
    } else {
        metadataContainer.classList.add('hidden');
    }
}

/**
 * Handles form submission from the panel
 */
async function handlePanelFormSubmit(e) {
    e.preventDefault();

    if (!currentPanelTask) return;

    const formData = new FormData(panelTaskForm);
    const titleEl = document.getElementById('panel-title');
    const formValues = getPanelFormValues();

    // Use the displayed title (which may have been edited inline)
    const title = titleEl?.textContent || formData.get('title') || currentPanelTask.title;

    const data = {
        title: title,
        acceptance_criteria: formData.get('acceptance_criteria'),
        priority: formData.get('panel-priority'),
        requires_test: panelRequiresTestCheckbox?.checked || false,
        tests: formValues.tests
    };

    try {
        await updateTask(currentPanelTask.id, data);
        showNotification(`"${data.title}" was updated successfully`, 'success');
        closeTaskPanel();
        await loadTasks();
    } catch (error) {
        console.error('Failed to update task:', error);
        showNotification('Failed to update task. Please try again.', 'error');
    }
}

/**
 * Initializes task panel event listeners
 */
function initTaskPanel() {
    // Close button
    panelCloseBtn?.addEventListener('click', closeTaskPanel);

    // Cancel button
    panelCancelBtn?.addEventListener('click', closeTaskPanel);

    // Overlay click to close
    taskPanelOverlay?.addEventListener('click', closeTaskPanel);

    // Form submission
    panelTaskForm?.addEventListener('submit', handlePanelFormSubmit);

    // Delete button
    panelDeleteBtn?.addEventListener('click', async () => {
        if (!currentPanelTask) return;

        const confirmed = await showConfirmDialog('Are you sure you want to delete this task?', {
            title: 'Delete Task',
            confirmText: 'Delete',
            destructive: true
        });

        if (confirmed) {
            try {
                const taskTitle = currentPanelTask.title;
                await deleteTask(currentPanelTask.id);
                closeTaskPanel();
                await loadTasks();
                showNotification(`"${taskTitle}" was deleted successfully`, 'success');
            } catch (error) {
                console.error('Failed to delete task:', error);
                showNotification('Failed to delete task. Please try again.', 'error');
            }
        }
    });

    // Copy task ID button
    const copyBtn = document.getElementById('copy-panel-task-id-btn');
    const taskIdEl = document.getElementById('panel-task-id');
    copyBtn?.addEventListener('click', async () => {
        if (!taskIdEl?.textContent) return;
        try {
            await navigator.clipboard.writeText(taskIdEl.textContent);
            copyBtn.classList.add('copied');
            showNotification('Task ID copied to clipboard', 'success');
            setTimeout(() => copyBtn.classList.remove('copied'), 2000);
        } catch (err) {
            showNotification('Failed to copy task ID', 'error');
        }
    });

    // Title editing buttons
    const editTitleBtn = document.getElementById('panel-edit-title-btn');
    const saveTitleBtn = document.getElementById('panel-save-title-btn');
    const cancelTitleBtn = document.getElementById('panel-cancel-title-btn');
    const titleInput = document.getElementById('panel-title-input');

    editTitleBtn?.addEventListener('click', showTitleEditMode);
    saveTitleBtn?.addEventListener('click', saveTitleEdit);
    cancelTitleBtn?.addEventListener('click', hideTitleEditMode);

    // Enter key to save title, Escape to cancel
    titleInput?.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') {
            e.preventDefault();
            saveTitleEdit();
        } else if (e.key === 'Escape') {
            e.preventDefault();
            e.stopPropagation(); // Prevent closing the panel
            hideTitleEditMode();
        }
    });

    // Escape key to close panel (only if not editing title)
    document.addEventListener('keydown', (e) => {
        if (e.key === 'Escape' && taskPanel?.classList.contains('open')) {
            const editEl = document.getElementById('panel-title-edit');
            // Only close panel if title edit mode is hidden
            if (editEl?.classList.contains('hidden')) {
                closeTaskPanel();
            }
        }
    });

    // Change detection for form inputs
    const criteriaInput = document.getElementById('panel-criteria-input');
    const priorityRadios = panelTaskForm?.querySelectorAll('input[name="panel-priority"]');

    criteriaInput?.addEventListener('input', updatePanelSaveButton);
    priorityRadios?.forEach(radio => {
        radio.addEventListener('change', updatePanelSaveButton);
    });
    panelRequiresTestCheckbox?.addEventListener('change', updatePanelSaveButton);

    // Add Test button
    const addTestBtn = document.getElementById('panel-add-test-btn');
    addTestBtn?.addEventListener('click', () => {
        const container = document.getElementById('panel-tests-container');
        if (!container) return;
        const index = container.querySelectorAll('.test-entry').length;
        container.insertAdjacentHTML('beforeend', createTestEntryHTML({}, index));
        attachTestEntryListeners();
        updateTestsHelperVisibility();
        updatePanelSaveButton();
        // Focus the new file input
        const newEntry = container.querySelector(`.test-entry[data-index="${index}"]`);
        newEntry?.querySelector('input')?.focus();
    });

    // Initialize panel resize functionality
    initPanelResize();
}

// ============================================
// Task Form Handler (for New Task Dialog)
// ============================================

async function handleFormSubmit(e) {
    e.preventDefault();

    const formData = new FormData(taskForm);
    const data = {
        title: formData.get('title'),
        acceptance_criteria: formData.get('acceptance_criteria'),
        priority: formData.get('priority'),
        requires_test: requiresTestCheckbox ? requiresTestCheckbox.checked : false
    };

    try {
        await createTask(data);
        showNotification(`"${data.title}" was created successfully!`, 'success');
        taskModal.close();
        await loadTasks();
    } catch (error) {
        console.error('Failed to create task:', error);
        showNotification('Failed to create task. Please try again.', 'error');
    }
}

// ============================================
// Task Action Handlers
// ============================================

async function handleRunTest(taskId, button) {
    // Update UI to show running state
    button.classList.add('running');
    button.innerHTML = '&#8987;'; // Hourglass

    // Find task and update local state optimistically
    const task = tasks.find(t => t.id === taskId);
    const card = document.querySelector(`.task-card[data-id="${taskId}"]`);

    if (task && card) {
        task.test_status = 'running';
        // Update just this card's status badge instead of full render
        const statusEl = card.querySelector('.task-status');
        if (statusEl) {
            statusEl.className = 'task-status running';
            statusEl.textContent = formatStatus('running');
        }
    }

    try {
        const result = await runTest(taskId);

        // Reload tasks to get updated state
        await loadTasks();

        // Show output if there's an error
        if (!result.results.all_passed) {
            showOutput(result.task, result.results);
        }
    } catch (error) {
        console.error('Failed to run test:', error);
        showNotification('Failed to run test. Please try again.', 'error');
    } finally {
        button.classList.remove('running');
        button.innerHTML = '&#9658;'; // Play
    }
}

async function handleDeleteTask(taskId) {
    // Find the task to get its title for the confirmation message
    const task = tasks.find(t => t.id === taskId);
    const taskTitle = task ? task.title : 'this task';

    const confirmed = await showConfirmDialog(`Are you sure you want to delete "${taskTitle}"?`, {
        title: 'Delete Task',
        confirmText: 'Delete',
        destructive: true
    });

    if (!confirmed) {
        return;
    }

    try {
        await deleteTask(taskId);
        await loadTasks();
        showNotification(`"${taskTitle}" was deleted successfully`, 'success');
    } catch (error) {
        console.error('Failed to delete task:', error);
        showNotification('Failed to delete task. Please try again.', 'error');
    }
}

// ============================================
// Task Drag and Drop
// ============================================

function handleDragStart(e) {
    draggedTask = e.target;
    e.target.classList.add('dragging');
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', e.target.dataset.id);

    // Create custom drag ghost with lift effect
    createDragGhost(e.target, e.clientX, e.clientY);

    // Hide native drag ghost by setting a transparent image
    const transparentImg = new Image();
    transparentImg.src = 'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7';
    e.dataTransfer.setDragImage(transparentImg, 0, 0);

    // Initialize mouse tracking
    lastMouseX = e.clientX;
    lastMouseY = e.clientY;
    mouseVelocityX = 0;

    // Start listening for mouse movement during drag
    document.addEventListener('dragover', trackDragMovement);

    // Show the delete drop zone when dragging a task
    showDeleteDropZone();
}

function handleDragEnd(e) {
    e.target.classList.remove('dragging');
    draggedTask = null;

    // Stop tracking mouse movement
    document.removeEventListener('dragover', trackDragMovement);

    // Remove the custom drag ghost with settle animation
    removeDragGhost(e.target);

    // Hide drop indicator
    hideDropIndicator();

    // Remove all drag-over states
    document.querySelectorAll('.task-list').forEach(list => {
        list.classList.remove('drag-over');
    });

    // Hide the delete drop zone
    hideDeleteDropZone();
}

/**
 * Creates a custom drag ghost element that follows the mouse with physics-based effects.
 * This replaces the native drag ghost for better visual control.
 */
function createDragGhost(sourceCard, startX, startY) {
    // Clone the card for our custom ghost
    dragGhost = sourceCard.cloneNode(true);
    dragGhost.classList.remove('dragging');
    dragGhost.classList.add('drag-ghost');

    // Get the source card's dimensions and position
    const rect = sourceCard.getBoundingClientRect();
    dragGhost.style.width = rect.width + 'px';
    dragGhost.style.left = startX - (rect.width / 2) + 'px';
    dragGhost.style.top = startY - 20 + 'px';

    // Add to document - the pickup animation handles the initial lift
    document.body.appendChild(dragGhost);
}

/**
 * Tracks mouse movement during drag and updates the ghost position/rotation.
 */
function trackDragMovement(e) {
    if (!dragGhost) return;

    // Prevent default to allow drop
    e.preventDefault();

    // Calculate velocity based on mouse movement
    const deltaX = e.clientX - lastMouseX;

    // More responsive velocity tracking for snappier rotation
    mouseVelocityX = mouseVelocityX * 0.6 + deltaX * 0.4;

    // Update last position
    lastMouseX = e.clientX;
    lastMouseY = e.clientY;

    // Cancel any pending animation frame
    if (dragAnimationFrame) {
        cancelAnimationFrame(dragAnimationFrame);
    }

    // Use requestAnimationFrame for smooth updates
    dragAnimationFrame = requestAnimationFrame(() => {
        if (!dragGhost) return;

        // Calculate rotation based on horizontal velocity
        // Clamp rotation to ±12 degrees for noticeable but not extreme effect
        const rotation = Math.max(-12, Math.min(12, mouseVelocityX * 0.8));

        // Update ghost position (centered on cursor)
        const ghostWidth = dragGhost.offsetWidth;
        dragGhost.style.left = e.clientX - (ghostWidth / 2) + 'px';
        dragGhost.style.top = e.clientY - 20 + 'px';

        // Apply rotation and scale transform
        dragGhost.style.transform = `scale(1.03) rotate(${rotation}deg)`;
    });
}

/**
 * Removes the drag ghost and plays a settle animation on the original card.
 */
function removeDragGhost(sourceCard) {
    // Cancel any pending animation
    if (dragAnimationFrame) {
        cancelAnimationFrame(dragAnimationFrame);
        dragAnimationFrame = null;
    }

    // Remove the ghost element
    if (dragGhost) {
        dragGhost.remove();
        dragGhost = null;
    }

    // Play settle animation on the source card
    sourceCard.classList.add('drag-settling');

    // Remove the animation class after it completes
    sourceCard.addEventListener('animationend', function onAnimEnd() {
        sourceCard.classList.remove('drag-settling');
        sourceCard.removeEventListener('animationend', onAnimEnd);
    });
}

function handleDragOver(e) {
    // If dragging a column, let the event bubble up to column handler
    if (draggedColumn) {
        return;
    }
    // Only handle task drags
    if (!draggedTask) return;

    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';

    const taskList = e.currentTarget;
    taskList.classList.add('drag-over');

    // Calculate and show drop indicator at the correct position
    const dropIndex = calculateDropIndex(taskList, e.clientY);
    showDropIndicator(taskList, dropIndex);
}

function handleDragLeave(e) {
    const taskList = e.currentTarget;

    // Check if we're actually leaving the task list (not just entering a child)
    const relatedTarget = e.relatedTarget;
    if (relatedTarget && taskList.contains(relatedTarget)) {
        return; // Still inside the task list
    }

    taskList.classList.remove('drag-over');
    hideDropIndicator();
}

async function handleDrop(e) {
    if (draggedColumn) return; // Don't handle task drops during column drag
    e.preventDefault();
    e.currentTarget.classList.remove('drag-over');

    const taskId = e.dataTransfer.getData('text/plain');
    const newColumn = e.currentTarget.dataset.column;
    const targetList = e.currentTarget;

    // Capture drop position before hiding indicator
    const dropIndex = currentDropIndex;

    // Hide the drop indicator
    hideDropIndicator();

    if (!taskId || !newColumn) return;

    // Find the card and task
    const card = document.querySelector(`.task-card[data-id="${taskId}"]`);
    const task = tasks.find(t => t.id === taskId);

    if (!card || !task) return;

    const oldColumn = task.column;
    const oldIndex = getTaskIndexInColumn(taskId, oldColumn);

    // Check if anything is actually changing
    const sameColumn = oldColumn === newColumn;
    const samePosition = sameColumn && oldIndex === dropIndex;

    if (samePosition) return;

    // Optimistic update: immediately move the card in DOM at the correct position
    const cards = Array.from(targetList.querySelectorAll('.task-card:not(.dragging)'));

    if (dropIndex >= cards.length) {
        targetList.appendChild(card);
    } else {
        targetList.insertBefore(card, cards[dropIndex]);
    }

    // Update local state
    task.column = newColumn;

    // Update strikethrough based on new column
    const titleEl = card.querySelector('.task-title-clickable');
    if (titleEl) {
        titleEl.classList.toggle('task-done', newColumn === 'done');
    }

    // Reorder the tasks array to match the new visual order
    reorderLocalTasks(taskId, newColumn, dropIndex);

    // Update task counts
    updateTaskCounts();

    try {
        // Send reorder request to API
        await reorderTask(taskId, newColumn, dropIndex);
        // Success - state is already updated
    } catch (error) {
        console.error('Failed to move task:', error);

        // Rollback: reload tasks from server
        await loadTasks();

        showNotification('Failed to move task. Please try again.', 'error');
    }
}

/**
 * Gets the current index of a task within its column.
 */
function getTaskIndexInColumn(taskId, column) {
    const columnTasks = tasks.filter(t => t.column === column);
    return columnTasks.findIndex(t => t.id === taskId);
}

/**
 * Reorders the local tasks array to reflect a task move.
 */
function reorderLocalTasks(taskId, newColumn, newIndex) {
    // Remove task from its current position
    const taskIndex = tasks.findIndex(t => t.id === taskId);
    if (taskIndex === -1) return;

    const [task] = tasks.splice(taskIndex, 1);
    task.column = newColumn;

    // Find the correct position in the tasks array
    // by looking at tasks in the target column
    const columnTasks = tasks.filter(t => t.column === newColumn);

    if (newIndex >= columnTasks.length) {
        // Insert after the last task in this column
        const lastColumnTask = columnTasks[columnTasks.length - 1];
        if (lastColumnTask) {
            const insertAfterIndex = tasks.indexOf(lastColumnTask);
            tasks.splice(insertAfterIndex + 1, 0, task);
        } else {
            // No tasks in column, add at end
            tasks.push(task);
        }
    } else {
        // Insert before the task currently at this index
        const targetTask = columnTasks[newIndex];
        if (targetTask) {
            const insertBeforeIndex = tasks.indexOf(targetTask);
            tasks.splice(insertBeforeIndex, 0, task);
        } else {
            tasks.push(task);
        }
    }
}

/**
 * Sends a reorder request to the API.
 */
async function reorderTask(taskId, column, position) {
    const response = await fetch(`${API_BASE}/tasks/${taskId}/reorder`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ column, position })
    });

    if (!response.ok) {
        const err = await response.json();
        throw new Error(err.error || 'Failed to reorder task');
    }

    return response.json();
}

/**
 * Updates the task count badges for all columns based on current local state.
 */
function updateTaskCounts() {
    const counts = {};
    columns.forEach(col => counts[col.slug] = 0);

    tasks.forEach(task => {
        if (counts.hasOwnProperty(task.column)) {
            counts[task.column]++;
        }
    });

    Object.entries(counts).forEach(([column, count]) => {
        const countEl = document.querySelector(`.task-count[data-column="${column}"]`);
        if (countEl) countEl.textContent = count;
    });
}

// ============================================
// Panel Resize Functionality
// ============================================

const PANEL_MIN_WIDTH = 300;
const PANEL_MAX_WIDTH_RATIO = 0.8; // 80% of viewport width
const PANEL_WIDTH_STORAGE_KEY = 'kantext-panel-width';

/**
 * Initialize the panel resize functionality.
 * Allows users to drag the left edge of the panel to resize it.
 */
function initPanelResize() {
    const resizeHandle = document.getElementById('task-panel-resize-handle');
    if (!resizeHandle || !taskPanel) return;

    let isResizing = false;
    let startX = 0;
    let startWidth = 0;

    // Restore saved width from localStorage
    const savedWidth = localStorage.getItem(PANEL_WIDTH_STORAGE_KEY);
    if (savedWidth) {
        const width = parseInt(savedWidth, 10);
        if (width >= PANEL_MIN_WIDTH) {
            taskPanel.style.maxWidth = `${width}px`;
        }
    }

    function startResize(e) {
        e.preventDefault();
        isResizing = true;
        startX = e.clientX || e.touches?.[0]?.clientX || 0;
        startWidth = taskPanel.offsetWidth;

        resizeHandle.classList.add('resizing');
        document.body.classList.add('resizing-panel');

        document.addEventListener('mousemove', resize);
        document.addEventListener('mouseup', stopResize);
        document.addEventListener('touchmove', resize, { passive: false });
        document.addEventListener('touchend', stopResize);
    }

    function resize(e) {
        if (!isResizing) return;
        e.preventDefault();

        const clientX = e.clientX || e.touches?.[0]?.clientX || 0;
        const deltaX = startX - clientX; // Negative because panel is on the right
        const newWidth = startWidth + deltaX;

        // Calculate max width based on viewport
        const maxWidth = window.innerWidth * PANEL_MAX_WIDTH_RATIO;

        // Clamp the width between min and max
        const clampedWidth = Math.max(PANEL_MIN_WIDTH, Math.min(maxWidth, newWidth));

        taskPanel.style.maxWidth = `${clampedWidth}px`;
    }

    function stopResize() {
        if (!isResizing) return;
        isResizing = false;

        resizeHandle.classList.remove('resizing');
        document.body.classList.remove('resizing-panel');

        document.removeEventListener('mousemove', resize);
        document.removeEventListener('mouseup', stopResize);
        document.removeEventListener('touchmove', resize);
        document.removeEventListener('touchend', stopResize);

        // Save the width to localStorage
        const currentWidth = taskPanel.offsetWidth;
        localStorage.setItem(PANEL_WIDTH_STORAGE_KEY, currentWidth.toString());
    }

    // Mouse events
    resizeHandle.addEventListener('mousedown', startResize);

    // Touch events for mobile
    resizeHandle.addEventListener('touchstart', startResize, { passive: false });
}
