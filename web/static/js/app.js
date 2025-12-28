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
    initDropIndicator();
    initDialogBackdropClose();
    loadColumns().then(() => loadTasks());
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

        // Compare relevant fields
        if (oldTask.title !== newTask.title ||
            oldTask.column !== newTask.column ||
            oldTask.priority !== newTask.priority ||
            oldTask.test_status !== newTask.test_status ||
            oldTask.test_file !== newTask.test_file ||
            oldTask.test_func !== newTask.test_func ||
            oldTask.acceptance_criteria !== newTask.acceptance_criteria) {
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
    const priorityClass = `priority-${task.priority || 'medium'}`;

    // Update priority class if changed
    const expectedClasses = `task-card ${priorityClass}` + (hasTest ? '' : ' no-test');
    if (card.className !== expectedClasses) {
        card.className = expectedClasses;
    }

    // Update title if changed
    const titleEl = card.querySelector('.task-title-clickable');
    if (titleEl && titleEl.textContent !== task.title) {
        titleEl.textContent = task.title;
    }

    // Handle test meta section
    const existingMeta = card.querySelector('.task-meta');

    if (hasTest) {
        const testText = `${task.test_file}:${task.test_func}`;
        const statusText = formatStatus(task.test_status);

        if (existingMeta) {
            // Update existing meta
            const testEl = existingMeta.querySelector('.task-test');
            if (testEl && testEl.textContent !== testText) {
                testEl.textContent = testText;
            }

            const statusEl = existingMeta.querySelector('.task-status');
            if (statusEl) {
                if (statusEl.textContent !== statusText) {
                    statusEl.textContent = statusText;
                }
                // Update status class
                statusEl.className = `task-status ${task.test_status}`;
            }
        } else {
            // Need to add meta section
            const metaHtml = `<div class="task-meta">
                <span class="task-test">${escapeHtml(task.test_file)}:${escapeHtml(task.test_func)}</span>
                <span class="task-status ${task.test_status}">${formatStatus(task.test_status)}</span>
            </div>`;
            card.insertAdjacentHTML('beforeend', metaHtml);
        }

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
        // Remove meta and play button if task no longer has test
        if (existingMeta) {
            existingMeta.remove();
        }
        const actionsContainer = card.querySelector('.task-actions');
        if (actionsContainer) {
            actionsContainer.remove();
        }
    }
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
    const deltaY = e.clientY - lastMouseY;

    // Smooth velocity with exponential moving average
    mouseVelocityX = mouseVelocityX * 0.7 + deltaX * 0.3;

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
        // Clamp rotation to ±8 degrees for subtle effect
        const rotation = Math.max(-8, Math.min(8, mouseVelocityX * 0.5));

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
