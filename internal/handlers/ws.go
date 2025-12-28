package handlers

import (
	"log"
	"net/http"

	"kantext/internal/services"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Allow connections from any origin (for development)
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// WSHandler handles WebSocket connections
type WSHandler struct {
	hub *services.WSHub
}

// NewWSHandler creates a new WebSocket handler
func NewWSHandler(hub *services.WSHub) *WSHandler {
	return &WSHandler{hub: hub}
}

// ServeWS handles WebSocket upgrade requests
func (h *WSHandler) ServeWS(w http.ResponseWriter, r *http.Request) {
	log.Printf("WebSocket upgrade request from %s", r.RemoteAddr)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	log.Printf("WebSocket connection upgraded successfully")

	// Register the connection with the hub
	h.hub.Register(conn)

	// Handle incoming messages (ping/pong, close)
	go h.readPump(conn)
}

// readPump handles reading from the WebSocket connection
func (h *WSHandler) readPump(conn *websocket.Conn) {
	defer func() {
		h.hub.Unregister(conn)
	}()

	// Set read limit and deadline handling
	conn.SetReadLimit(512)

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket read error: %v", err)
			}
			break
		}
		// We don't process incoming messages - clients just listen for updates
	}
}
