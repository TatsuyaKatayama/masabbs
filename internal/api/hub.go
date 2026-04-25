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

// Hub manages active WebSocket connections and broadcasts NATS messages
type Hub struct {
	clients    map[*websocket.Conn]bool
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	broadcast  chan []byte
	mu         sync.Mutex
	nc         *nats.Conn
}

func NewHub(nc *nats.Conn) *Hub {
	return &Hub{
		clients:    make(map[*websocket.Conn]bool),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
		broadcast:  make(chan []byte, 256), // 1. Buffered channel to avoid blocking NATS callback
		nc:         nc,
	}
}

// Run starts the Hub and NATS subscription
func (h *Hub) Run(ctx context.Context) {
	// Subscribe to all board messages in real-time
	sub, err := h.nc.Subscribe("board.>", func(m *nats.Msg) {
		select {
		case h.broadcast <- m.Data:
			// message queued successfully
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
			h.clients[client] = true
			h.mu.Unlock()
			log.Println("New WebSocket client connected")

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				client.Close()
			}
			h.mu.Unlock()
			log.Println("WebSocket client disconnected")

		case message := <-h.broadcast:
			h.mu.Lock()
			for client := range h.clients {
				// 2. Prevent slow clients from blocking the loop
				client.SetWriteDeadline(time.Now().Add(5 * time.Second))
				err := client.WriteMessage(websocket.TextMessage, message)
				if err != nil {
					log.Printf("WebSocket write error or timeout: %v", err)
					client.Close()
					delete(h.clients, client)
				}
			}
			h.mu.Unlock()

		case <-ctx.Done():
			log.Println("WebSocket Hub stopping...")
			// 3. Cleanly unsubscribe to prevent leaks
			if err := sub.Unsubscribe(); err != nil {
				log.Printf("Error unsubscribing from NATS: %v", err)
			}
			h.mu.Lock()
			for client := range h.clients {
				client.Close()
			}
			h.mu.Unlock()
			return
		}
	}
}

// ServeWS handles WebSocket upgrade requests
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade to WebSocket: %v", err)
		return
	}

	h.register <- conn

	// Keep-alive/Read loop
	go func() {
		defer func() {
			h.unregister <- conn
		}()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
	}()
}
