// Package streaming provides real-time WebSocket streaming for trading events.
package streaming

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// EventType represents the type of streaming event.
type EventType string

const (
	EventTypeSignal    EventType = "signal"
	EventTypeTrade     EventType = "trade"
	EventTypeOrder     EventType = "order"
	EventTypePosition  EventType = "position"
	EventTypePrice     EventType = "price"
	EventTypeStatus    EventType = "status"
	EventTypeError     EventType = "error"
	EventTypeHeartbeat EventType = "heartbeat"
)

// Event is a streaming event sent to clients.
type Event struct {
	Type      EventType   `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data"`
}

// Hub manages WebSocket connections and broadcasts events.
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan Event
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex

	upgrader websocket.Upgrader
}

// Client represents a WebSocket client connection.
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte

	// Subscription filters
	subscriptions map[EventType]bool
	subMu         sync.RWMutex
}

// NewHub creates a new streaming hub.
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan Event, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for now
			},
		},
	}
}

// Run starts the hub's event loop.
func (h *Hub) Run() {
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Printf("[WS] Client connected (%d total)", len(h.clients))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			log.Printf("[WS] Client disconnected (%d remaining)", len(h.clients))

		case event := <-h.broadcast:
			h.broadcastEvent(event)

		case <-heartbeat.C:
			h.Broadcast(Event{
				Type:      EventTypeHeartbeat,
				Timestamp: time.Now(),
				Data:      map[string]interface{}{"clients": len(h.clients)},
			})
		}
	}
}

func (h *Hub) broadcastEvent(event Event) {
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("[WS] Failed to marshal event: %v", err)
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for client := range h.clients {
		// Check if client is subscribed to this event type
		if !client.isSubscribed(event.Type) {
			continue
		}

		select {
		case client.send <- data:
		default:
			// Client buffer full, close connection
			close(client.send)
			delete(h.clients, client)
		}
	}
}

// Broadcast sends an event to all connected clients.
func (h *Hub) Broadcast(event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	select {
	case h.broadcast <- event:
	default:
		log.Printf("[WS] Broadcast channel full, dropping event")
	}
}

// BroadcastSignal broadcasts a trading signal event.
func (h *Hub) BroadcastSignal(signal interface{}) {
	h.Broadcast(Event{
		Type:      EventTypeSignal,
		Timestamp: time.Now(),
		Data:      signal,
	})
}

// BroadcastTrade broadcasts a trade event.
func (h *Hub) BroadcastTrade(trade interface{}) {
	h.Broadcast(Event{
		Type:      EventTypeTrade,
		Timestamp: time.Now(),
		Data:      trade,
	})
}

// BroadcastOrder broadcasts an order event.
func (h *Hub) BroadcastOrder(order interface{}) {
	h.Broadcast(Event{
		Type:      EventTypeOrder,
		Timestamp: time.Now(),
		Data:      order,
	})
}

// BroadcastPosition broadcasts a position update.
func (h *Hub) BroadcastPosition(position interface{}) {
	h.Broadcast(Event{
		Type:      EventTypePosition,
		Timestamp: time.Now(),
		Data:      position,
	})
}

// BroadcastPrice broadcasts a price update.
func (h *Hub) BroadcastPrice(tokenID string, price float64) {
	h.Broadcast(Event{
		Type:      EventTypePrice,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"token_id": tokenID,
			"price":    price,
		},
	})
}

// BroadcastStatus broadcasts a status update.
func (h *Hub) BroadcastStatus(status interface{}) {
	h.Broadcast(Event{
		Type:      EventTypeStatus,
		Timestamp: time.Now(),
		Data:      status,
	})
}

// BroadcastError broadcasts an error event.
func (h *Hub) BroadcastError(err error, context string) {
	h.Broadcast(Event{
		Type:      EventTypeError,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"error":   err.Error(),
			"context": context,
		},
	})
}

// ClientCount returns the number of connected clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// ServeWS handles WebSocket upgrade requests.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WS] Upgrade failed: %v", err)
		return
	}

	client := &Client{
		hub:           h,
		conn:          conn,
		send:          make(chan []byte, 256),
		subscriptions: make(map[EventType]bool),
	}

	// Subscribe to all events by default
	client.subscriptions[EventTypeSignal] = true
	client.subscriptions[EventTypeTrade] = true
	client.subscriptions[EventTypeOrder] = true
	client.subscriptions[EventTypePosition] = true
	client.subscriptions[EventTypePrice] = true
	client.subscriptions[EventTypeStatus] = true
	client.subscriptions[EventTypeError] = true
	client.subscriptions[EventTypeHeartbeat] = true

	h.register <- client

	go client.writePump()
	go client.readPump()
}

// isSubscribed checks if client is subscribed to an event type.
func (c *Client) isSubscribed(eventType EventType) bool {
	c.subMu.RLock()
	defer c.subMu.RUnlock()
	return c.subscriptions[eventType]
}

// readPump reads messages from the WebSocket connection.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[WS] Read error: %v", err)
			}
			break
		}

		// Handle subscription messages
		c.handleMessage(message)
	}
}

// handleMessage processes incoming client messages.
func (c *Client) handleMessage(message []byte) {
	var msg struct {
		Type   string   `json:"type"`
		Events []string `json:"events"`
	}

	if err := json.Unmarshal(message, &msg); err != nil {
		return
	}

	switch msg.Type {
	case "subscribe":
		c.subMu.Lock()
		for _, event := range msg.Events {
			c.subscriptions[EventType(event)] = true
		}
		c.subMu.Unlock()

	case "unsubscribe":
		c.subMu.Lock()
		for _, event := range msg.Events {
			delete(c.subscriptions, EventType(event))
		}
		c.subMu.Unlock()
	}
}

// writePump writes messages to the WebSocket connection.
func (c *Client) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Write queued messages
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
