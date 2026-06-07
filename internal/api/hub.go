package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second
	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second
	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10
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
	send    chan []byte
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
	db         *pgxpool.Pool
}

func NewHub(nc *nats.Conn, db *pgxpool.Pool) *Hub {
	return &Hub{
		clients:    make(map[string]*Client),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan []byte, 256),
		nc:         nc,
		db:         db,
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
			h.clients[client.AgentID] = client
			h.mu.Unlock()
			log.Printf("New WebSocket client connected: %s", client.AgentID)

		case client := <-h.unregister:
			h.mu.Lock()
			if c, ok := h.clients[client.AgentID]; ok && c == client {
				delete(h.clients, client.AgentID)
				close(client.send)
			}
			h.mu.Unlock()
			log.Printf("WebSocket client disconnected: %s", client.AgentID)

		case message := <-h.broadcast:
			h.mu.Lock()
			for agentID, client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
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
				close(client.send)
			}
			h.mu.Unlock()
			return
		}
	}
}

// readPump pumps messages from the websocket connection to the hub.
func (c *Client) readPump(h *Hub) {
	defer func() {
		h.unregister <- c
		c.Conn.Close()
	}()
	c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error { c.Conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, _, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			break
		}
	}
}

// writePump pumps messages from the hub to the websocket connection.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.Conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ServeWS handles WebSocket upgrade requests and enforces the single connection rule
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	agentID := r.URL.Query().Get("agent_id")
	if agentID == "" {
		http.Error(w, "agent_id is required", http.StatusBadRequest)
		return
	}

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
		send:    make(chan []byte, 256),
	}

	// Fetch history from DB and push to client before registration
	// to ensure they get missed messages.
	if h.db != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Get last 50 messages.
			rows, err := h.db.Query(ctx, `
				SELECT id, payload, type, agent_id, thread_id, to_agents, observers, created_at 
				FROM tasks 
				ORDER BY created_at DESC 
				LIMIT 50
			`)
			if err != nil {
				log.Printf("Failed to fetch history for %s: %v", agentID, err)
				return
			}
			defer rows.Close()

			var history [][]byte
			for rows.Next() {
				var taskID string
				var p []byte
				var msgType, fromAgent string
				var threadID *string
				var toAgents, observers []string
				var createdAt time.Time

				if err := rows.Scan(&taskID, &p, &msgType, &fromAgent, &threadID, &toAgents, &observers, &createdAt); err == nil {
					// Re-construct the envelope for the UI
					env := map[string]interface{}{
						"id":        taskID,
						"type":      msgType,
						"from":      fromAgent,
						"thread_id": threadID,
						"to":        toAgents,
						"observers": observers,
						"timestamp": createdAt.Unix(),
						"payload":   json.RawMessage(p),
					}
					envBytes, _ := json.Marshal(env)
					history = append(history, envBytes)
				}
			}

			// Send history in chronological order (fetched as DESC, so reverse it)
			for i := len(history) - 1; i >= 0; i-- {
				select {
				case client.send <- history[i]:
				default:
					log.Printf("History buffer full for %s, skipping remaining history", agentID)
					return
				}
			}
		}()
	}

	h.register <- client

	go client.writePump()
	go client.readPump(h)
}
