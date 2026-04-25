package api

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nats-io/nats.go"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // TODO: Restrict in production
	},
}

// Client represents a connected WebSocket client
type Client struct {
	Conn    *websocket.Conn
	AgentID string
}

// Hub manages active WebSocket connections and broadcasts NATS messages
type Hub struct {
	// Map of AgentID to active Client to enforce single connection rule
	clients    map[string]*Client
	register   chan *Client
	unregister chan *Client
	broadcast  chan []byte
	mu         sync.Mutex
	nc         *nats.Conn
}

func NewHub(nc *nats.Conn) *Hub {
	return &Hub{
		clients:    make(map[string]*Client),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan []byte, 256),
		nc:         nc,
	}
}

// Run starts the Hub and NATS subscription
func (h *Hub) Run(ctx context.Context) {
	sub, err := h.nc.Subscribe("board.>", func(m *nats.Msg) {
		select {
		case h.broadcast <- m.Data:
		default:
			log.Println("WebSocket Hub broadcast channel full, dropping message")
		}
	})
	if err != nil {
		log.Printf("WebSocket Hub failed to subscribe to NATS: %v", err)
		return
	}

	log.Println("WebSocket Hub started and listening to NATS board.>")

	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			// The single connection rule (409 Conflict) is enforced in ServeWS before upgrading.
			// This is just mapping the connection.
			h.clients[client.AgentID] = client
			h.mu.Unlock()
			log.Printf("New WebSocket client connected: %s", client.AgentID)

		case client := <-h.unregister:
			h.mu.Lock()
			if c, ok := h.clients[client.AgentID]; ok && c.Conn == client.Conn {
				delete(h.clients, client.AgentID)
				client.Conn.Close()
			}
			h.mu.Unlock()
			log.Printf("WebSocket client disconnected: %s", client.AgentID)

		case message := <-h.broadcast:
			h.mu.Lock()
			for agentID, client := range h.clients {
				client.Conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				err := client.Conn.WriteMessage(websocket.TextMessage, message)
				if err != nil {
					log.Printf("WebSocket write error for %s: %v", agentID, err)
					client.Conn.Close()
					delete(h.clients, agentID)
				}
			}
			h.mu.Unlock()

		case <-ctx.Done():
			log.Println("WebSocket Hub stopping...")
			if err := sub.Unsubscribe(); err != nil {
				log.Printf("Error unsubscribing from NATS: %v", err)
			}
			h.mu.Lock()
			for _, client := range h.clients {
				client.Conn.Close()
			}
			h.mu.Unlock()
			return
		}
	}
}

// ServeWS handles WebSocket upgrade requests and enforces the single connection rule
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	// Extract agent_id from query parameters (e.g., /ws?agent_id=admin-1)
	agentID := r.URL.Query().Get("agent_id")
	if agentID == "" {
		http.Error(w, "agent_id is required", http.StatusBadRequest)
		return
	}

	// Enforce single connection rule (reject subsequent connections with 409)
	h.mu.Lock()
	if _, exists := h.clients[agentID]; exists {
		h.mu.Unlock()
		http.Error(w, "conflict: active session already exists for this agent", http.StatusConflict)
		return
	}
	h.mu.Unlock()

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade to WebSocket: %v", err)
		return
	}

	client := &Client{
		Conn:    conn,
		AgentID: agentID,
	}

	h.register <- client

	// Keep-alive/Read loop
	go func() {
		defer func() {
			h.unregister <- client
		}()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
	}()
}
