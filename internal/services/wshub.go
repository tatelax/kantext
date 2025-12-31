package services

import (
	"log"
	"sync"

	"github.com/gorilla/websocket"
)

// Message types for WebSocket communication
const (
	MsgTypeTasksUpdated = "tasks_updated"
)

// WSMessage represents a WebSocket message sent to clients
type WSMessage struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

// WSHub manages WebSocket connections and broadcasts messages
type WSHub struct {
	clients    map[*websocket.Conn]bool
	mu         sync.RWMutex
	broadcast  chan WSMessage
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
}

// NewWSHub creates a new WebSocket hub
func NewWSHub() *WSHub {
	return &WSHub{
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan WSMessage, 256),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
	}
}

// Run starts the hub's message processing loop
func (h *WSHub) Run() {
	for {
		select {
		case conn := <-h.register:
			h.mu.Lock()
			h.clients[conn] = true
			h.mu.Unlock()
			log.Printf("WebSocket client connected. Total clients: %d", len(h.clients))

		case conn := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[conn]; ok {
				delete(h.clients, conn)
				conn.Close()
			}
			h.mu.Unlock()
			log.Printf("WebSocket client disconnected. Total clients: %d", len(h.clients))

		case msg := <-h.broadcast:
			h.mu.RLock()
			for conn := range h.clients {
				err := conn.WriteJSON(msg)
				if err != nil {
					log.Printf("WebSocket write error: %v", err)
					conn.Close()
					// Schedule for removal (can't modify map during iteration)
					// Use non-blocking send to prevent goroutine leak
					go func(c *websocket.Conn) {
						select {
						case h.unregister <- c:
						default:
							// Channel full or hub stopped, connection already closed
						}
					}(conn)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Register adds a new client connection
func (h *WSHub) Register(conn *websocket.Conn) {
	h.register <- conn
}

// Unregister removes a client connection
func (h *WSHub) Unregister(conn *websocket.Conn) {
	h.unregister <- conn
}

// Broadcast sends a message to all connected clients
func (h *WSHub) Broadcast(msg WSMessage) {
	select {
	case h.broadcast <- msg:
	default:
		log.Println("WebSocket broadcast channel full, dropping message")
	}
}

// NotifyTasksUpdated broadcasts a tasks_updated event to all clients
func (h *WSHub) NotifyTasksUpdated() {
	log.Printf("Broadcasting tasks_updated to %d clients", h.ClientCount())
	h.Broadcast(WSMessage{
		Type: MsgTypeTasksUpdated,
	})
}

// ClientCount returns the number of connected clients
func (h *WSHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
