package wss

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Test WebSocket server
func newTestServer(handler func(*websocket.Conn)) *httptest.Server {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		handler(conn)
	}))
}

func TestClientConnect(t *testing.T) {
	server := newTestServer(func(conn *websocket.Conn) {
		// Echo server
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			conn.WriteMessage(mt, msg)
		}
	})
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	config := DefaultConfig(url)
	config.ReconnectEnabled = false

	var connected bool
	var mu sync.Mutex

	client := NewClient(config, Handlers{
		OnConnect: func() {
			mu.Lock()
			connected = true
			mu.Unlock()
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	mu.Lock()
	if !connected {
		t.Error("OnConnect was not called")
	}
	mu.Unlock()

	if !client.IsConnected() {
		t.Error("Client should be connected")
	}

	if client.State() != StateConnected {
		t.Errorf("Wrong state: got %v, want %v", client.State(), StateConnected)
	}
}

func TestClientSendReceive(t *testing.T) {
	server := newTestServer(func(conn *websocket.Conn) {
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			// Echo back with prefix
			conn.WriteMessage(mt, append([]byte("echo:"), msg...))
		}
	})
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	config := DefaultConfig(url)
	config.ReconnectEnabled = false

	received := make(chan []byte, 1)
	client := NewClient(config, Handlers{
		OnMessage: func(msgType int, data []byte) {
			received <- data
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	// Send a message
	if err := client.Send([]byte("hello")); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	// Wait for echo
	select {
	case msg := <-received:
		if string(msg) != "echo:hello" {
			t.Errorf("Wrong message: got %s, want echo:hello", string(msg))
		}
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for message")
	}
}

func TestClientSendJSON(t *testing.T) {
	server := newTestServer(func(conn *websocket.Conn) {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			// Parse and echo back
			var data map[string]interface{}
			json.Unmarshal(msg, &data)
			data["echoed"] = true
			resp, _ := json.Marshal(data)
			conn.WriteMessage(websocket.TextMessage, resp)
		}
	})
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	config := DefaultConfig(url)
	config.ReconnectEnabled = false

	received := make(chan []byte, 1)
	client := NewClient(config, Handlers{
		OnMessage: func(msgType int, data []byte) {
			received <- data
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	// Send JSON
	if err := client.SendJSON(map[string]string{"type": "test"}); err != nil {
		t.Fatalf("SendJSON failed: %v", err)
	}

	// Wait for echo
	select {
	case msg := <-received:
		var data map[string]interface{}
		if err := json.Unmarshal(msg, &data); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}
		if data["type"] != "test" {
			t.Errorf("Wrong type: got %v", data["type"])
		}
		if data["echoed"] != true {
			t.Error("Message was not echoed")
		}
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for message")
	}
}

func TestSubscription(t *testing.T) {
	server := newTestServer(func(conn *websocket.Conn) {
		// Send a few messages
		for i := 0; i < 3; i++ {
			msg := map[string]interface{}{
				"channel": "test",
				"index":   i,
			}
			data, _ := json.Marshal(msg)
			conn.WriteMessage(websocket.TextMessage, data)
			time.Sleep(50 * time.Millisecond)
		}
		// Keep connection open
		time.Sleep(1 * time.Second)
	})
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	config := DefaultConfig(url)
	config.ReconnectEnabled = false

	client := NewClient(config, Handlers{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	// Create subscription with filter
	sub, err := client.Subscribe(SubscriptionConfig{
		ID: "test-sub",
		Filter: func(data []byte) bool {
			var msg map[string]interface{}
			json.Unmarshal(data, &msg)
			return msg["channel"] == "test"
		},
		BufferSize: 10,
	})
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Collect messages
	var messages [][]byte
	timeout := time.After(2 * time.Second)
	for i := 0; i < 3; i++ {
		select {
		case msg := <-sub.Messages():
			messages = append(messages, msg)
		case <-timeout:
			break
		}
	}

	if len(messages) < 3 {
		t.Errorf("Expected 3 messages, got %d", len(messages))
	}

	sub.Close()
	if !sub.IsClosed() {
		t.Error("Subscription should be closed")
	}
}

func TestClientClose(t *testing.T) {
	server := newTestServer(func(conn *websocket.Conn) {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	})
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	config := DefaultConfig(url)
	config.ReconnectEnabled = false

	client := NewClient(config, Handlers{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	client.Close()

	if client.State() != StateClosed {
		t.Errorf("Wrong state: got %v, want %v", client.State(), StateClosed)
	}

	// Send should fail
	if err := client.Send([]byte("test")); err == nil {
		t.Error("Send should fail after close")
	}
}

func TestStateString(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{StateDisconnected, "disconnected"},
		{StateConnecting, "connecting"},
		{StateConnected, "connected"},
		{StateReconnecting, "reconnecting"},
		{StateClosed, "closed"},
		{State(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("State(%d).String() = %s, want %s", tt.state, got, tt.want)
		}
	}
}
