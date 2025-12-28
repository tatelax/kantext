// Kantext - Frontend Application

const API_BASE = '/api';

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

// DOM Elements
const addTaskBtn = document.getElementById('add-task-btn');
const taskModal = document.getElementById('task-modal');
const outputModal = document.getElementById('output-modal');
const taskForm = document.getElementById('task-form');
const testOutput = document.getElementById('test-output');
const advancedDetails = document.getElementById('advanced-details');
const board = document.getElementById('board');
const autoGenerateCheckbox = document.getElementById('auto_generate_test');
const testConfigHint = document.getElementById('test-config-hint');
const generateTestFileCheckbox = document.getElementById('generate_test_file');
const modalDeleteBtn = document.getElementById('modal-delete-btn');

// Initialize
document.addEventListener('DOMContentLoaded', () => {
    console.log('[Init] Page loaded, initializing Kantext...');
    initNotificationSystem();
    initThemeToggle();
    initDeleteDropZone();
    loadColumns().then(() => loadTasks());
    setupEventListeners();
    console.log('[Init] Setting up WebSocket connection...');
    connectWebSocket();
});

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
    e.stopPropagation();
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

    // Auto-remove after duration
    setTimeout(() => {
        notification.classList.add('fade-out');
        setTimeout(() => {
            if (notification.parentNode) {
                notification.remove();
            }
        }, 300); // Wait for fade-out animation
    }, duration);

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
            // Reload both columns and tasks when file changes
            loadColumns().then(() => loadTasks());
            break;
        default:
            console.log('Unknown message type:', msg.type);
    }
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

    // Form submission
    if (taskForm) {
        taskForm.addEventListener('submit', handleFormSubmit);
    }

    // Title input - update test names as user types
    const titleInput = document.getElementById('title');
    if (titleInput) {
        titleInput.addEventListener('input', updateTestNamesFromTitle);
    }

    // Auto-generate checkbox toggle
    if (autoGenerateCheckbox) {
        autoGenerateCheckbox.addEventListener('change', toggleAutoGenerate);
    }

    // Generate test file checkbox toggle
    if (generateTestFileCheckbox) {
        generateTestFileCheckbox.addEventListener('change', toggleAdvancedSection);
    }

    // Copy task ID button
    const copyTaskIdBtn = document.getElementById('copy-task-id-btn');
    if (copyTaskIdBtn) {
        copyTaskIdBtn.addEventListener('click', handleCopyTaskId);
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

// ============================================
// Column API Functions
// ============================================

async function loadColumns() {
    try {
        const response = await fetch(`${API_BASE}/columns`);
        columns = await response.json();
        renderColumns();
    } catch (error) {
        console.error('Failed to load columns:', error);
    }
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
        tasks = await response.json();
        renderTasks();
    } catch (error) {
        console.error('Failed to load tasks:', error);
    }
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
                <button class="delete-column-btn" title="Delete Column">
                    <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <polyline points="3 6 5 6 21 6"></polyline>
                        <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
                    </svg>
                </button>
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

    // Delete column button
    const deleteBtn = column.querySelector('.delete-column-btn');
    deleteBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        handleDeleteColumn(col.slug);
    });

    return column;
}

// ============================================
// Task Render Functions
// ============================================

function renderTasks() {
    // Clear all task lists
    document.querySelectorAll('.task-list').forEach(list => {
        list.innerHTML = '';
    });

    // Count tasks per column
    const counts = {};
    columns.forEach(col => counts[col.slug] = 0);

    // Sort tasks by priority within each column
    const sortedTasks = [...tasks].sort((a, b) => {
        const priorityOrder = { high: 0, medium: 1, low: 2 };
        return (priorityOrder[a.priority] || 1) - (priorityOrder[b.priority] || 1);
    });

    // Render tasks
    sortedTasks.forEach(task => {
        const list = document.querySelector(`.task-list[data-column="${task.column}"]`);
        if (list) {
            const card = createTaskCard(task);
            list.appendChild(card);
            counts[task.column] = (counts[task.column] || 0) + 1;
        }
    });

    // Update counts
    Object.entries(counts).forEach(([column, count]) => {
        const countEl = document.querySelector(`.task-count[data-column="${column}"]`);
        if (countEl) countEl.textContent = count;
    });
}

/**
 * Check if a task has a test associated with it
 */
function taskHasTest(task) {
    return task.test_file && task.test_file.trim() !== '' &&
           task.test_func && task.test_func.trim() !== '';
}

function createTaskCard(task) {
    const card = document.createElement('div');
    const hasTest = taskHasTest(task);
    const priorityClass = `priority-${task.priority || 'medium'}`;
    card.className = `task-card ${priorityClass}` + (hasTest ? '' : ' no-test');
    card.draggable = true;
    card.dataset.id = task.id;

    // Build actions HTML - only include play button if task has a test
    const actionsHtml = hasTest
        ? `<div class="task-actions">
               <button class="play-btn" title="Run Test">&#9658;</button>
           </div>`
        : '';

    // Build meta HTML - only include test info if task has a test
    const metaHtml = hasTest
        ? `<div class="task-meta">
               <span class="task-test">${escapeHtml(task.test_file)}:${escapeHtml(task.test_func)}</span>
               <span class="task-status ${task.test_status}">${formatStatus(task.test_status)}</span>
           </div>`
        : '';

    card.innerHTML = `
        <div class="task-header">
            <span class="task-title task-title-clickable" title="Click to copy task ID">${escapeHtml(task.title)}</span>
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

    // Click on card (not title) to edit task
    card.addEventListener('click', (e) => {
        // Only open modal if click wasn't on title
        if (!e.target.classList.contains('task-title-clickable')) {
            openTaskModal(task);
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
// Test Name Generation
// ============================================

/**
 * Convert a title to a snake_case test file name
 * e.g., "User Login" -> "user_login_test.go"
 */
function titleToTestFile(title) {
    if (!title || !title.trim()) return '';
    return title
        .trim()
        .toLowerCase()
        .replace(/[^a-z0-9\s]/g, '') // Remove special characters
        .replace(/\s+/g, '_')        // Replace spaces with underscores
        + '_test.go';
}

/**
 * Convert a title to a PascalCase test function name
 * e.g., "User Login" -> "TestUserLogin"
 */
function titleToTestFunc(title) {
    if (!title || !title.trim()) return '';
    const pascalCase = title
        .trim()
        .replace(/[^a-zA-Z0-9\s]/g, '') // Remove special characters
        .split(/\s+/)
        .map(word => word.charAt(0).toUpperCase() + word.slice(1).toLowerCase())
        .join('');
    return 'Test' + pascalCase;
}

/**
 * Update test file and function inputs based on title
 */
function updateTestNamesFromTitle() {
    const titleInput = document.getElementById('title');
    const testFileInput = document.getElementById('test_file');
    const testFuncInput = document.getElementById('test_func');

    if (!autoGenerateCheckbox || !autoGenerateCheckbox.checked) return;

    const title = titleInput.value;
    testFileInput.value = titleToTestFile(title);
    testFuncInput.value = titleToTestFunc(title);
}

/**
 * Toggle auto-generate mode for test names
 */
function toggleAutoGenerate() {
    const testFileInput = document.getElementById('test_file');
    const testFuncInput = document.getElementById('test_func');
    const isAutoGenerate = autoGenerateCheckbox.checked;

    testFileInput.readOnly = isAutoGenerate;
    testFuncInput.readOnly = isAutoGenerate;

    if (testConfigHint) {
        testConfigHint.textContent = isAutoGenerate
            ? 'Test names will be generated from the task title.'
            : 'Specify an existing test file and function to link.';
    }

    if (isAutoGenerate) {
        updateTestNamesFromTitle();
    }
}

/**
 * Toggle advanced section based on generate test file checkbox
 */
function toggleAdvancedSection() {
    if (!advancedDetails) return;

    const isEnabled = generateTestFileCheckbox && generateTestFileCheckbox.checked;

    if (isEnabled) {
        advancedDetails.classList.remove('advanced-disabled');
        advancedDetails.removeAttribute('inert');
    } else {
        advancedDetails.classList.add('advanced-disabled');
        advancedDetails.setAttribute('inert', '');
        // Collapse the section when disabled
        advancedDetails.removeAttribute('open');
    }
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
        alert(error.message || 'Failed to reorder columns');
    }
}

// ============================================
// Task Modal Functions
// ============================================

function openTaskModal(task = null) {
    if (!taskModal) {
        console.error('Task modal not found');
        return;
    }

    const modalTitle = document.getElementById('modal-title');
    const taskIdInput = document.getElementById('task-id');
    const titleInput = document.getElementById('title');
    const criteriaInput = document.getElementById('acceptance_criteria');
    const testFileInput = document.getElementById('test_file');
    const testFuncInput = document.getElementById('test_func');

    // Reset advanced section to collapsed
    if (advancedDetails) {
        advancedDetails.removeAttribute('open');
    }

    const modalTaskIdEl = document.getElementById('modal-task-id');
    const modalTaskIdWrapper = document.getElementById('modal-task-id-wrapper');

    if (task) {
        modalTitle.textContent = 'Edit Task';
        if (modalTaskIdEl && modalTaskIdWrapper) {
            modalTaskIdEl.textContent = task.id;
            modalTaskIdWrapper.classList.remove('hidden');
        }
        taskIdInput.value = task.id;
        titleInput.value = task.title;
        criteriaInput.value = task.acceptance_criteria || '';
        testFileInput.value = task.test_file || '';
        testFuncInput.value = task.test_func || '';

        // Set priority radio button
        const priorityRadio = taskForm.querySelector(`input[name="priority"][value="${task.priority || 'medium'}"]`);
        if (priorityRadio) priorityRadio.checked = true;

        // Check if task has custom test names (different from auto-generated)
        const expectedFile = titleToTestFile(task.title);
        const expectedFunc = titleToTestFunc(task.title);
        const hasCustomTest = (task.test_file && task.test_file !== expectedFile) ||
                              (task.test_func && task.test_func !== expectedFunc);

        // Disable auto-generate if task has custom test names
        if (autoGenerateCheckbox) {
            autoGenerateCheckbox.checked = !hasCustomTest;
            toggleAutoGenerate();
        }

        // Show advanced section if task has custom test
        if (hasCustomTest) {
            if (advancedDetails) {
                advancedDetails.setAttribute('open', '');
            }
        }

        // Hide generate test file checkbox for existing tasks (already created)
        if (generateTestFileCheckbox) {
            generateTestFileCheckbox.closest('.flex').style.display = 'none';
        }

        // Ensure advanced section is enabled for existing tasks (checkbox is hidden)
        if (advancedDetails) {
            advancedDetails.classList.remove('advanced-disabled');
            advancedDetails.removeAttribute('inert');
        }

        // Show delete button for existing tasks
        if (modalDeleteBtn) {
            modalDeleteBtn.classList.remove('hidden');
            modalDeleteBtn.onclick = async () => {
                const confirmed = await showConfirmDialog('Are you sure you want to delete this task?', {
                    title: 'Delete Task',
                    confirmText: 'Delete',
                    destructive: true
                });
                if (confirmed) {
                    try {
                        await deleteTask(task.id);
                        taskModal.close();
                        await loadTasks();
                        showNotification(`"${task.title}" was deleted successfully`, 'success');
                    } catch (error) {
                        console.error('Failed to delete task:', error);
                        showNotification('Failed to delete task. Please try again.', 'error');
                    }
                }
            };
        }
    } else {
        modalTitle.textContent = 'New Task';
        if (modalTaskIdWrapper) {
            modalTaskIdWrapper.classList.add('hidden');
        }
        if (modalTaskIdEl) {
            modalTaskIdEl.textContent = '';
        }
        taskForm.reset();
        taskIdInput.value = '';
        // Default to medium priority
        taskForm.querySelector('input[name="priority"][value="medium"]').checked = true;

        // Enable auto-generate by default for new tasks
        if (autoGenerateCheckbox) {
            autoGenerateCheckbox.checked = true;
            toggleAutoGenerate();
        }

        // Show and enable generate test file checkbox for new tasks
        if (generateTestFileCheckbox) {
            generateTestFileCheckbox.closest('.flex').style.display = 'flex';
            generateTestFileCheckbox.checked = true;
        }

        // Enable advanced section for new tasks (since generate test file is checked)
        toggleAdvancedSection();

        // Hide delete button for new tasks
        if (modalDeleteBtn) {
            modalDeleteBtn.classList.add('hidden');
        }
    }

    taskModal.showModal();
    titleInput.focus();
}

function showOutput(task) {
    testOutput.textContent = task.last_output || 'No output available';
    outputModal.showModal();
}

// ============================================
// Task Form Handler
// ============================================

async function handleFormSubmit(e) {
    e.preventDefault();

    const formData = new FormData(taskForm);
    const id = formData.get('id');
    const data = {
        title: formData.get('title'),
        acceptance_criteria: formData.get('acceptance_criteria'),
        priority: formData.get('priority')
    };

    // For new tasks, only include test file/function if "Generate test file" is checked
    // For existing tasks (editing), always include test file/function if specified
    const testFile = formData.get('test_file');
    const testFunc = formData.get('test_func');
    const isNewTask = !id;
    const shouldIncludeTestInfo = isNewTask
        ? (generateTestFileCheckbox && generateTestFileCheckbox.checked)
        : true;

    if (shouldIncludeTestInfo && testFile && testFunc) {
        data.test_file = testFile;
        data.test_func = testFunc;
    }

    // Include generate_test_file flag for new tasks
    if (isNewTask && generateTestFileCheckbox) {
        data.generate_test_file = generateTestFileCheckbox.checked;
    }

    try {
        if (id) {
            await updateTask(id, data);
        } else {
            await createTask(data);
            showNotification(`"${data.title}" was created successfully!`, 'success');
        }
        taskModal.close();
        await loadTasks();
    } catch (error) {
        console.error('Failed to save task:', error);
        alert('Failed to save task. Please try again.');
    }
}

// ============================================
// Task Action Handlers
// ============================================

async function handleRunTest(taskId, button) {
    // Update UI to show running state
    button.classList.add('running');
    button.innerHTML = '&#8987;'; // Hourglass

    // Find task and update local state
    const task = tasks.find(t => t.id === taskId);
    if (task) {
        task.test_status = 'running';
        renderTasks();
    }

    try {
        const result = await runTest(taskId);

        // Reload tasks to get updated state
        await loadTasks();

        // Show output if there's an error
        if (!result.result.passed) {
            showOutput(result.task);
        }
    } catch (error) {
        console.error('Failed to run test:', error);
        alert('Failed to run test. Please try again.');
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

    // Show the delete drop zone when dragging a task
    showDeleteDropZone();
}

function handleDragEnd(e) {
    e.target.classList.remove('dragging');
    draggedTask = null;

    // Remove all drag-over states
    document.querySelectorAll('.task-list').forEach(list => {
        list.classList.remove('drag-over');
    });

    // Hide the delete drop zone
    hideDeleteDropZone();
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
    e.currentTarget.classList.add('drag-over');
}

function handleDragLeave(e) {
    e.currentTarget.classList.remove('drag-over');
}

async function handleDrop(e) {
    if (draggedColumn) return; // Don't handle task drops during column drag
    e.preventDefault();
    e.currentTarget.classList.remove('drag-over');

    const taskId = e.dataTransfer.getData('text/plain');
    const newColumn = e.currentTarget.dataset.column;

    if (!taskId || !newColumn) return;

    try {
        await updateTask(taskId, { column: newColumn });
    } catch (error) {
        console.error('Failed to move task:', error);
    }
}
