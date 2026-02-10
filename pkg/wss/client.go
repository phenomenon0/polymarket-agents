// Package wss provides a generic WebSocket client with automatic reconnection,
// subscription management, and heartbeat support.
package wss

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// State represents the connection state.
type State int32

const (
	StateDisconnected State = iota
	StateConnecting
	StateConnected
	StateReconnecting
	StateClosed
)

func (s State) String() string {
	switch s {
	case StateDisconnected:
		return "disconnected"
	case StateConnecting:
		return "connecting"
	case StateConnected:
		return "connected"
	case StateReconnecting:
		return "reconnecting"
	case StateClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// Handlers contains callback functions for WebSocket events.
type Handlers struct {
	OnConnect     func()
	OnDisconnect  func(err error)
	OnMessage     func(msgType int, data []byte)
	OnError       func(err error)
	OnStateChange func(old, new State)
}

// Config holds WebSocket client configuration.
type Config struct {
	// URL is the WebSocket server URL
	URL string

	// Headers for the initial connection
	Headers map[string]string

	// Reconnect settings
	ReconnectEnabled     bool
	ReconnectMinDelay    time.Duration
	ReconnectMaxDelay    time.Duration
	ReconnectMaxAttempts int // 0 = unlimited

	// Heartbeat settings
	HeartbeatInterval time.Duration
	HeartbeatTimeout  time.Duration

	// Read/Write timeouts
	WriteTimeout time.Duration
	ReadTimeout  time.Duration

	// Message buffer sizes
	ReadBufferSize  int
	WriteBufferSize int
}

// DefaultConfig returns a config with sensible defaults.
func DefaultConfig(url string) Config {
	return Config{
		URL:                  url,
		ReconnectEnabled:     true,
		ReconnectMinDelay:    1 * time.Second,
		ReconnectMaxDelay:    30 * time.Second,
		ReconnectMaxAttempts: 0, // unlimited
		HeartbeatInterval:    30 * time.Second,
		HeartbeatTimeout:     10 * time.Second,
		WriteTimeout:         10 * time.Second,
		ReadTimeout:          60 * time.Second,
		ReadBufferSize:       4096,
		WriteBufferSize:      4096,
	}
}

// Client is a WebSocket client with reconnection support.
type Client struct {
	config   Config
	handlers Handlers

	conn   *websocket.Conn
	connMu sync.RWMutex
	state  int32 // atomic State

	writeCh   chan writeRequest
	closeCh   chan struct{}
	closeOnce sync.Once

	subscriptions map[string]*Subscription
	subsMu        sync.RWMutex

	reconnectAttempts int
	lastError         error
	lastErrorMu       sync.RWMutex
}

type writeRequest struct {
	msgType int
	data    []byte
	result  chan error
}

// NewClient creates a new WebSocket client.
func NewClient(config Config, handlers Handlers) *Client {
	return &Client{
		config:        config,
		handlers:      handlers,
		writeCh:       make(chan writeRequest, 100),
		closeCh:       make(chan struct{}),
		subscriptions: make(map[string]*Subscription),
	}
}

// Connect establishes the WebSocket connection.
func (c *Client) Connect(ctx context.Context) error {
	if c.getState() == StateClosed {
		return errors.New("client is closed")
	}

	c.setState(StateConnecting)

	dialer := websocket.Dialer{
		ReadBufferSize:  c.config.ReadBufferSize,
		WriteBufferSize: c.config.WriteBufferSize,
	}

	// Build headers
	headers := make(map[string][]string)
	for k, v := range c.config.Headers {
		headers[k] = []string{v}
	}

	conn, _, err := dialer.DialContext(ctx, c.config.URL, headers)
	if err != nil {
		c.setState(StateDisconnected)
		c.setLastError(err)
		return fmt.Errorf("dial failed: %w", err)
	}

	c.connMu.Lock()
	c.conn = conn
	c.connMu.Unlock()

	c.setState(StateConnected)
	c.reconnectAttempts = 0

	if c.handlers.OnConnect != nil {
		c.handlers.OnConnect()
	}

	// Start background goroutines
	go c.readLoop()
	go c.writeLoop()
	if c.config.HeartbeatInterval > 0 {
		go c.heartbeatLoop()
	}

	return nil
}

// Close closes the WebSocket connection.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.setState(StateClosed)
		close(c.closeCh)

		c.connMu.Lock()
		if c.conn != nil {
			c.conn.Close()
		}
		c.connMu.Unlock()
	})
	return nil
}

// Send sends a message over the WebSocket.
func (c *Client) Send(data []byte) error {
	return c.SendMessage(websocket.TextMessage, data)
}

// SendJSON sends a JSON-encoded message.
func (c *Client) SendJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("json marshal failed: %w", err)
	}
	return c.Send(data)
}

// SendMessage sends a message with a specific type.
func (c *Client) SendMessage(msgType int, data []byte) error {
	if c.getState() != StateConnected {
		return errors.New("not connected")
	}

	result := make(chan error, 1)
	select {
	case c.writeCh <- writeRequest{msgType: msgType, data: data, result: result}:
		return <-result
	case <-c.closeCh:
		return errors.New("client closed")
	}
}

// State returns the current connection state.
func (c *Client) State() State {
	return c.getState()
}

// IsConnected returns true if the client is connected.
func (c *Client) IsConnected() bool {
	return c.getState() == StateConnected
}

// LastError returns the last error that occurred.
func (c *Client) LastError() error {
	c.lastErrorMu.RLock()
	defer c.lastErrorMu.RUnlock()
	return c.lastError
}

// --- Internal methods ---

func (c *Client) getState() State {
	return State(atomic.LoadInt32(&c.state))
}

func (c *Client) setState(s State) {
	old := State(atomic.SwapInt32(&c.state, int32(s)))
	if old != s && c.handlers.OnStateChange != nil {
		c.handlers.OnStateChange(old, s)
	}
}

func (c *Client) setLastError(err error) {
	c.lastErrorMu.Lock()
	c.lastError = err
	c.lastErrorMu.Unlock()
}

func (c *Client) readLoop() {
	defer func() {
		if c.getState() != StateClosed {
			c.handleDisconnect(c.lastError)
		}
	}()

	for {
		select {
		case <-c.closeCh:
			return
		default:
		}

		c.connMu.RLock()
		conn := c.conn
		c.connMu.RUnlock()

		if conn == nil {
			return
		}

		if c.config.ReadTimeout > 0 {
			conn.SetReadDeadline(time.Now().Add(c.config.ReadTimeout))
		}

		msgType, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				return
			}
			c.setLastError(err)
			if c.handlers.OnError != nil {
				c.handlers.OnError(err)
			}
			return
		}

		if c.handlers.OnMessage != nil {
			c.handlers.OnMessage(msgType, data)
		}

		// Route to subscriptions
		c.routeMessage(data)
	}
}

func (c *Client) writeLoop() {
	for {
		select {
		case <-c.closeCh:
			return
		case req := <-c.writeCh:
			c.connMu.RLock()
			conn := c.conn
			c.connMu.RUnlock()

			if conn == nil {
				req.result <- errors.New("not connected")
				continue
			}

			if c.config.WriteTimeout > 0 {
				conn.SetWriteDeadline(time.Now().Add(c.config.WriteTimeout))
			}

			err := conn.WriteMessage(req.msgType, req.data)
			req.result <- err

			if err != nil {
				c.setLastError(err)
				if c.handlers.OnError != nil {
					c.handlers.OnError(err)
				}
			}
		}
	}
}

func (c *Client) heartbeatLoop() {
	ticker := time.NewTicker(c.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.closeCh:
			return
		case <-ticker.C:
			if c.getState() != StateConnected {
				continue
			}

			c.connMu.RLock()
			conn := c.conn
			c.connMu.RUnlock()

			if conn == nil {
				continue
			}

			deadline := time.Now().Add(c.config.HeartbeatTimeout)
			if err := conn.WriteControl(websocket.PingMessage, nil, deadline); err != nil {
				c.setLastError(err)
				if c.handlers.OnError != nil {
					c.handlers.OnError(fmt.Errorf("heartbeat failed: %w", err))
				}
			}
		}
	}
}

func (c *Client) handleDisconnect(err error) {
	c.setState(StateDisconnected)

	if c.handlers.OnDisconnect != nil {
		c.handlers.OnDisconnect(err)
	}

	if c.config.ReconnectEnabled && c.getState() != StateClosed {
		go c.reconnect()
	}
}

func (c *Client) reconnect() {
	c.setState(StateReconnecting)

	for {
		if c.getState() == StateClosed {
			return
		}

		c.reconnectAttempts++

		if c.config.ReconnectMaxAttempts > 0 && c.reconnectAttempts > c.config.ReconnectMaxAttempts {
			c.setState(StateDisconnected)
			if c.handlers.OnError != nil {
				c.handlers.OnError(fmt.Errorf("max reconnect attempts (%d) exceeded", c.config.ReconnectMaxAttempts))
			}
			return
		}

		// Calculate backoff delay
		delay := c.config.ReconnectMinDelay * time.Duration(1<<uint(c.reconnectAttempts-1))
		if delay > c.config.ReconnectMaxDelay {
			delay = c.config.ReconnectMaxDelay
		}

		select {
		case <-c.closeCh:
			return
		case <-time.After(delay):
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := c.Connect(ctx)
		cancel()

		if err == nil {
			// Resubscribe to all subscriptions
			c.resubscribe()
			return
		}

		if c.handlers.OnError != nil {
			c.handlers.OnError(fmt.Errorf("reconnect attempt %d failed: %w", c.reconnectAttempts, err))
		}
	}
}

func (c *Client) routeMessage(data []byte) {
	c.subsMu.RLock()
	defer c.subsMu.RUnlock()

	for _, sub := range c.subscriptions {
		if sub.filter == nil || sub.filter(data) {
			select {
			case sub.msgCh <- data:
			default:
				// Channel full, drop message
			}
		}
	}
}

func (c *Client) resubscribe() {
	c.subsMu.RLock()
	defer c.subsMu.RUnlock()

	for _, sub := range c.subscriptions {
		if sub.subscribeMsg != nil {
			c.SendJSON(sub.subscribeMsg)
		}
	}
}
