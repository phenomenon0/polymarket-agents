package clob

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/phenomenon0/polymarket-agents/pkg/wss"
)

// --- WebSocket Message Types ---

// WSMessageType is the type of WebSocket message.
type WSMessageType string

const (
	// Market channels
	WSTypePriceChange    WSMessageType = "price_change"
	WSTypeBookUpdate     WSMessageType = "book"
	WSTypeTradeEvent     WSMessageType = "last_trade_price" // Actual API event type
	WSTypeTickSizeChange WSMessageType = "tick_size_change"

	// User channels
	WSTypeOrderUpdate    WSMessageType = "order"
	WSTypeUserTradeEvent WSMessageType = "user_trade"
)

// WSMessage is a generic WebSocket message.
type WSMessage struct {
	Type    string          `json:"event_type"`
	Asset   string          `json:"asset_id,omitempty"`
	Market  string          `json:"market,omitempty"`
	RawData json.RawMessage `json:"-"`
}

// PriceChangeEvent is emitted when a token's price changes.
type PriceChangeEvent struct {
	AssetID  string `json:"asset_id"`
	Price    string `json:"price"`
	OldPrice string `json:"old_price,omitempty"`
}

// BookUpdateEvent is emitted when the orderbook changes.
type BookUpdateEvent struct {
	AssetID   string       `json:"asset_id"`
	Market    string       `json:"market"`
	Hash      string       `json:"hash"`
	Timestamp string       `json:"timestamp"`
	Bids      []PriceLevel `json:"bids"`
	Asks      []PriceLevel `json:"asks"`
}

// TradeEvent is emitted when a trade occurs on the market.
// Maps to "last_trade_price" event from Polymarket WebSocket.
type TradeEvent struct {
	ID              string    `json:"id"`
	Market          string    `json:"market"`
	AssetID         string    `json:"asset_id"`
	Side            OrderSide `json:"side"`
	Price           string    `json:"price"`
	Size            string    `json:"size"`
	Timestamp       int64     `json:"timestamp,string"` // API sends as string
	TransactionHash string    `json:"transaction_hash"`
	FeeRateBps      string    `json:"fee_rate_bps"`
}

// OrderUpdateEvent is emitted when a user's order changes.
type OrderUpdateEvent struct {
	OrderID    string      `json:"id"`
	Status     OrderStatus `json:"status"`
	AssetID    string      `json:"asset_id"`
	Side       OrderSide   `json:"side"`
	Price      string      `json:"price"`
	Size       string      `json:"size"`
	SizeFilled string      `json:"size_filled"`
	Timestamp  int64       `json:"timestamp"`
}

// UserTradeEvent is emitted when a user's trade is executed.
type UserTradeEvent struct {
	TradeID   string    `json:"id"`
	OrderID   string    `json:"order_id"`
	Market    string    `json:"market"`
	AssetID   string    `json:"asset_id"`
	Side      OrderSide `json:"side"`
	Price     string    `json:"price"`
	Size      string    `json:"size"`
	Maker     bool      `json:"maker"`
	Timestamp int64     `json:"timestamp"`
}

// --- Subscription Messages ---

type subscribeMsg struct {
	Type    string   `json:"type"`
	Channel string   `json:"channel,omitempty"`
	Assets  []string `json:"assets_ids,omitempty"`
	Markets []string `json:"markets,omitempty"`
	Auth    *wsAuth  `json:"auth,omitempty"`
}

type wsAuth struct {
	APIKey     string `json:"apiKey"`
	Secret     string `json:"secret"`
	Passphrase string `json:"passphrase"`
}

// --- WSClient ---

// WSClient is a WebSocket client for Polymarket CLOB real-time data.
type WSClient struct {
	client *wss.Client
	url    string
	creds  *APICredentials

	handlers WSHandlers

	mu         sync.RWMutex
	assetSubs  map[string]bool // asset_id -> subscribed
	marketSubs map[string]bool // market_id -> subscribed
	userChan   bool
}

// WSHandlers contains callback functions for WebSocket events.
type WSHandlers struct {
	// Market events
	OnPriceChange func(PriceChangeEvent)
	OnBookUpdate  func(BookUpdateEvent)
	OnTrade       func(TradeEvent)

	// User events (requires auth)
	OnOrderUpdate func(OrderUpdateEvent)
	OnUserTrade   func(UserTradeEvent)

	// Connection events
	OnConnect    func()
	OnDisconnect func(err error)
	OnError      func(err error)
}

// WSConfig holds WebSocket client configuration.
type WSConfig struct {
	URL         string
	Credentials *APICredentials // Required for user channel
	Handlers    WSHandlers

	// Reconnection settings
	ReconnectEnabled  bool
	ReconnectMinDelay time.Duration
	ReconnectMaxDelay time.Duration
}

// DefaultWSConfig returns default configuration.
func DefaultWSConfig() WSConfig {
	return WSConfig{
		URL:               DefaultWSSURL,
		ReconnectEnabled:  true,
		ReconnectMinDelay: 1 * time.Second,
		ReconnectMaxDelay: 30 * time.Second,
	}
}

// NewWSClient creates a new Polymarket WebSocket client.
func NewWSClient(config WSConfig) *WSClient {
	wsConfig := wss.Config{
		URL:                  config.URL,
		ReconnectEnabled:     config.ReconnectEnabled,
		ReconnectMinDelay:    config.ReconnectMinDelay,
		ReconnectMaxDelay:    config.ReconnectMaxDelay,
		ReconnectMaxAttempts: 0, // unlimited
		HeartbeatInterval:    30 * time.Second,
		HeartbeatTimeout:     10 * time.Second,
		WriteTimeout:         10 * time.Second,
		ReadTimeout:          60 * time.Second,
		ReadBufferSize:       8192,
		WriteBufferSize:      4096,
	}

	wsc := &WSClient{
		url:        config.URL,
		creds:      config.Credentials,
		handlers:   config.Handlers,
		assetSubs:  make(map[string]bool),
		marketSubs: make(map[string]bool),
	}

	handlers := wss.Handlers{
		OnConnect: func() {
			wsc.onConnect()
			if wsc.handlers.OnConnect != nil {
				wsc.handlers.OnConnect()
			}
		},
		OnDisconnect: func(err error) {
			if wsc.handlers.OnDisconnect != nil {
				wsc.handlers.OnDisconnect(err)
			}
		},
		OnMessage: func(msgType int, data []byte) {
			wsc.handleMessage(data)
		},
		OnError: func(err error) {
			if wsc.handlers.OnError != nil {
				wsc.handlers.OnError(err)
			}
		},
	}

	wsc.client = wss.NewClient(wsConfig, handlers)
	return wsc
}

// Connect connects to the WebSocket server.
func (w *WSClient) Connect(ctx context.Context) error {
	return w.client.Connect(ctx)
}

// Close closes the WebSocket connection.
func (w *WSClient) Close() error {
	return w.client.Close()
}

// IsConnected returns true if connected.
func (w *WSClient) IsConnected() bool {
	return w.client.IsConnected()
}

// --- Market Channel Subscriptions ---

// SubscribeToAssets subscribes to price changes for the given asset IDs.
func (w *WSClient) SubscribeToAssets(assetIDs ...string) error {
	if len(assetIDs) == 0 {
		return nil
	}

	msg := subscribeMsg{
		Type:    "subscribe",
		Channel: "market",
		Assets:  assetIDs,
	}

	if err := w.client.SendJSON(msg); err != nil {
		return fmt.Errorf("subscribe failed: %w", err)
	}

	w.mu.Lock()
	for _, id := range assetIDs {
		w.assetSubs[id] = true
	}
	w.mu.Unlock()

	return nil
}

// UnsubscribeFromAssets unsubscribes from price changes for the given asset IDs.
func (w *WSClient) UnsubscribeFromAssets(assetIDs ...string) error {
	if len(assetIDs) == 0 {
		return nil
	}

	msg := subscribeMsg{
		Type:    "unsubscribe",
		Channel: "market",
		Assets:  assetIDs,
	}

	if err := w.client.SendJSON(msg); err != nil {
		return fmt.Errorf("unsubscribe failed: %w", err)
	}

	w.mu.Lock()
	for _, id := range assetIDs {
		delete(w.assetSubs, id)
	}
	w.mu.Unlock()

	return nil
}

// SubscribeToMarkets subscribes to orderbook updates for the given market IDs.
func (w *WSClient) SubscribeToMarkets(marketIDs ...string) error {
	if len(marketIDs) == 0 {
		return nil
	}

	msg := subscribeMsg{
		Type:    "subscribe",
		Channel: "market",
		Markets: marketIDs,
	}

	if err := w.client.SendJSON(msg); err != nil {
		return fmt.Errorf("subscribe failed: %w", err)
	}

	w.mu.Lock()
	for _, id := range marketIDs {
		w.marketSubs[id] = true
	}
	w.mu.Unlock()

	return nil
}

// UnsubscribeFromMarkets unsubscribes from orderbook updates for the given market IDs.
func (w *WSClient) UnsubscribeFromMarkets(marketIDs ...string) error {
	if len(marketIDs) == 0 {
		return nil
	}

	msg := subscribeMsg{
		Type:    "unsubscribe",
		Channel: "market",
		Markets: marketIDs,
	}

	if err := w.client.SendJSON(msg); err != nil {
		return fmt.Errorf("unsubscribe failed: %w", err)
	}

	w.mu.Lock()
	for _, id := range marketIDs {
		delete(w.marketSubs, id)
	}
	w.mu.Unlock()

	return nil
}

// --- User Channel Subscriptions ---

// SubscribeToUserChannel subscribes to user-specific events (orders, trades).
// Requires API credentials.
func (w *WSClient) SubscribeToUserChannel() error {
	if w.creds == nil {
		return fmt.Errorf("API credentials required for user channel")
	}

	msg := subscribeMsg{
		Type:    "subscribe",
		Channel: "user",
		Auth: &wsAuth{
			APIKey:     w.creds.APIKey,
			Secret:     w.creds.Secret,
			Passphrase: w.creds.Passphrase,
		},
	}

	if err := w.client.SendJSON(msg); err != nil {
		return fmt.Errorf("subscribe failed: %w", err)
	}

	w.mu.Lock()
	w.userChan = true
	w.mu.Unlock()

	return nil
}

// UnsubscribeFromUserChannel unsubscribes from user-specific events.
func (w *WSClient) UnsubscribeFromUserChannel() error {
	msg := subscribeMsg{
		Type:    "unsubscribe",
		Channel: "user",
	}

	if err := w.client.SendJSON(msg); err != nil {
		return fmt.Errorf("unsubscribe failed: %w", err)
	}

	w.mu.Lock()
	w.userChan = false
	w.mu.Unlock()

	return nil
}

// --- Internal methods ---

func (w *WSClient) onConnect() {
	// Resubscribe to all channels on reconnect
	w.mu.RLock()
	assets := make([]string, 0, len(w.assetSubs))
	for id := range w.assetSubs {
		assets = append(assets, id)
	}
	markets := make([]string, 0, len(w.marketSubs))
	for id := range w.marketSubs {
		markets = append(markets, id)
	}
	userChan := w.userChan
	w.mu.RUnlock()

	if len(assets) > 0 {
		w.SubscribeToAssets(assets...)
	}
	if len(markets) > 0 {
		w.SubscribeToMarkets(markets...)
	}
	if userChan {
		w.SubscribeToUserChannel()
	}
}

func (w *WSClient) handleMessage(data []byte) {
	// Try to parse as array first (batch messages)
	if len(data) > 0 && data[0] == '[' {
		var messages []json.RawMessage
		if err := json.Unmarshal(data, &messages); err == nil {
			for _, msg := range messages {
				w.handleSingleMessage(msg)
			}
			return
		}
	}

	w.handleSingleMessage(data)
}

func (w *WSClient) handleSingleMessage(data []byte) {
	var msg WSMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	msgType := WSMessageType(strings.ToLower(msg.Type))

	switch msgType {
	case WSTypePriceChange:
		if w.handlers.OnPriceChange != nil {
			var event PriceChangeEvent
			if json.Unmarshal(data, &event) == nil {
				w.handlers.OnPriceChange(event)
			}
		}

	case WSTypeBookUpdate:
		if w.handlers.OnBookUpdate != nil {
			var event BookUpdateEvent
			if json.Unmarshal(data, &event) == nil {
				w.handlers.OnBookUpdate(event)
			}
		}

	case WSTypeTradeEvent:
		if w.handlers.OnTrade != nil {
			var event TradeEvent
			if json.Unmarshal(data, &event) == nil {
				w.handlers.OnTrade(event)
			}
		}

	case WSTypeOrderUpdate:
		if w.handlers.OnOrderUpdate != nil {
			var event OrderUpdateEvent
			if json.Unmarshal(data, &event) == nil {
				w.handlers.OnOrderUpdate(event)
			}
		}

	case WSTypeUserTradeEvent:
		if w.handlers.OnUserTrade != nil {
			var event UserTradeEvent
			if json.Unmarshal(data, &event) == nil {
				w.handlers.OnUserTrade(event)
			}
		}
	}
}

// --- Streaming API (channel-based) ---

// StreamConfig configures a streaming subscription.
type StreamConfig struct {
	// AssetIDs to subscribe to for price changes
	AssetIDs []string

	// MarketIDs to subscribe to for orderbook updates
	MarketIDs []string

	// IncludeUserEvents enables user channel (requires credentials)
	IncludeUserEvents bool

	// BufferSize for each channel (default 100)
	BufferSize int
}

// Streams holds channels for streaming data.
type Streams struct {
	PriceChanges <-chan PriceChangeEvent
	BookUpdates  <-chan BookUpdateEvent
	Trades       <-chan TradeEvent
	OrderUpdates <-chan OrderUpdateEvent // nil if no credentials
	UserTrades   <-chan UserTradeEvent   // nil if no credentials

	closeCh chan struct{}
	client  *WSClient
}

// Close closes all streams.
func (s *Streams) Close() {
	close(s.closeCh)
	s.client.Close()
}

// StartStreaming creates a streaming client with channels for events.
// This is an alternative to callback-based handling.
func StartStreaming(ctx context.Context, config WSConfig, streamConfig StreamConfig) (*Streams, error) {
	bufSize := streamConfig.BufferSize
	if bufSize <= 0 {
		bufSize = 100
	}

	priceChangeCh := make(chan PriceChangeEvent, bufSize)
	bookUpdateCh := make(chan BookUpdateEvent, bufSize)
	tradeCh := make(chan TradeEvent, bufSize)
	var orderUpdateCh chan OrderUpdateEvent
	var userTradeCh chan UserTradeEvent

	if streamConfig.IncludeUserEvents && config.Credentials != nil {
		orderUpdateCh = make(chan OrderUpdateEvent, bufSize)
		userTradeCh = make(chan UserTradeEvent, bufSize)
	}

	closeCh := make(chan struct{})

	config.Handlers = WSHandlers{
		OnPriceChange: func(e PriceChangeEvent) {
			select {
			case priceChangeCh <- e:
			case <-closeCh:
			default:
				// Drop if buffer full
			}
		},
		OnBookUpdate: func(e BookUpdateEvent) {
			select {
			case bookUpdateCh <- e:
			case <-closeCh:
			default:
			}
		},
		OnTrade: func(e TradeEvent) {
			select {
			case tradeCh <- e:
			case <-closeCh:
			default:
			}
		},
		OnOrderUpdate: func(e OrderUpdateEvent) {
			if orderUpdateCh != nil {
				select {
				case orderUpdateCh <- e:
				case <-closeCh:
				default:
				}
			}
		},
		OnUserTrade: func(e UserTradeEvent) {
			if userTradeCh != nil {
				select {
				case userTradeCh <- e:
				case <-closeCh:
				default:
				}
			}
		},
	}

	client := NewWSClient(config)

	if err := client.Connect(ctx); err != nil {
		return nil, fmt.Errorf("connect failed: %w", err)
	}

	// Subscribe to requested channels
	if len(streamConfig.AssetIDs) > 0 {
		if err := client.SubscribeToAssets(streamConfig.AssetIDs...); err != nil {
			client.Close()
			return nil, fmt.Errorf("subscribe to assets failed: %w", err)
		}
	}

	if len(streamConfig.MarketIDs) > 0 {
		if err := client.SubscribeToMarkets(streamConfig.MarketIDs...); err != nil {
			client.Close()
			return nil, fmt.Errorf("subscribe to markets failed: %w", err)
		}
	}

	if streamConfig.IncludeUserEvents && config.Credentials != nil {
		if err := client.SubscribeToUserChannel(); err != nil {
			client.Close()
			return nil, fmt.Errorf("subscribe to user channel failed: %w", err)
		}
	}

	streams := &Streams{
		PriceChanges: priceChangeCh,
		BookUpdates:  bookUpdateCh,
		Trades:       tradeCh,
		OrderUpdates: orderUpdateCh,
		UserTrades:   userTradeCh,
		closeCh:      closeCh,
		client:       client,
	}

	return streams, nil
}
