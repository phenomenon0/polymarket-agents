package wss

import (
	"sync"
)

// MessageFilter is a function that determines if a message should be routed to a subscription.
type MessageFilter func(data []byte) bool

// Subscription represents a WebSocket subscription.
type Subscription struct {
	id           string
	client       *Client
	filter       MessageFilter
	subscribeMsg interface{} // Message to send on subscribe/resubscribe
	msgCh        chan []byte
	closed       bool
	closeMu      sync.RWMutex
}

// SubscriptionConfig holds subscription configuration.
type SubscriptionConfig struct {
	// ID is a unique identifier for this subscription
	ID string

	// Filter determines which messages are routed to this subscription
	// If nil, all messages are routed
	Filter MessageFilter

	// SubscribeMessage is sent when subscribing (and on reconnect)
	SubscribeMessage interface{}

	// BufferSize is the size of the message buffer
	BufferSize int
}

// Subscribe creates a new subscription.
func (c *Client) Subscribe(config SubscriptionConfig) (*Subscription, error) {
	bufSize := config.BufferSize
	if bufSize <= 0 {
		bufSize = 100
	}

	sub := &Subscription{
		id:           config.ID,
		client:       c,
		filter:       config.Filter,
		subscribeMsg: config.SubscribeMessage,
		msgCh:        make(chan []byte, bufSize),
	}

	c.subsMu.Lock()
	c.subscriptions[config.ID] = sub
	c.subsMu.Unlock()

	// Send subscribe message if connected and message is provided
	if c.IsConnected() && config.SubscribeMessage != nil {
		if err := c.SendJSON(config.SubscribeMessage); err != nil {
			return sub, err
		}
	}

	return sub, nil
}

// Unsubscribe removes a subscription.
func (c *Client) Unsubscribe(id string) {
	c.subsMu.Lock()
	if sub, ok := c.subscriptions[id]; ok {
		sub.close()
		delete(c.subscriptions, id)
	}
	c.subsMu.Unlock()
}

// ID returns the subscription ID.
func (s *Subscription) ID() string {
	return s.id
}

// Messages returns the channel for receiving messages.
func (s *Subscription) Messages() <-chan []byte {
	return s.msgCh
}

// Close closes the subscription.
func (s *Subscription) Close() {
	s.client.Unsubscribe(s.id)
}

func (s *Subscription) close() {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()

	if !s.closed {
		s.closed = true
		close(s.msgCh)
	}
}

// IsClosed returns true if the subscription is closed.
func (s *Subscription) IsClosed() bool {
	s.closeMu.RLock()
	defer s.closeMu.RUnlock()
	return s.closed
}
