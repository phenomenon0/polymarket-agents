// Package clob provides a client for the Polymarket CLOB (Central Limit Order Book) API.
// CLOB is used for trading: placing orders, managing positions, and streaming market data.
package clob

import (
	"time"
)

const (
	// DefaultBaseURL is the CLOB API base URL
	DefaultBaseURL = "https://clob.polymarket.com"

	// DefaultWSSURL is the WebSocket URL for market channel
	DefaultWSSURL = "wss://ws-subscriptions-clob.polymarket.com/ws/market"

	// DefaultWSSUserURL is the WebSocket URL for user channel
	DefaultWSSUserURL = "wss://ws-subscriptions-clob.polymarket.com/ws/user"

	// ChainID for Polygon mainnet
	ChainIDPolygon = 137
)

// Order represents a trading order.
type Order struct {
	ID            string      `json:"id"`
	Status        OrderStatus `json:"status"`
	Owner         string      `json:"owner"`
	Maker         string      `json:"maker"`
	Taker         string      `json:"taker"`
	TokenID       string      `json:"asset_id"`
	MakerAmount   string      `json:"maker_amount"`
	TakerAmount   string      `json:"taker_amount"`
	Side          OrderSide   `json:"side"`
	Price         string      `json:"price"`
	Size          string      `json:"size"`
	SizeFilled    string      `json:"size_filled"`
	Expiration    string      `json:"expiration"`
	Nonce         string      `json:"nonce"`
	FeeRateBps    string      `json:"fee_rate_bps"`
	Signature     string      `json:"signature"`
	SignatureType int         `json:"signature_type"`
	CreatedAt     time.Time   `json:"created_at"`
	AssociatedTxn string      `json:"associated_txn,omitempty"`
}

// OrderStatus represents the status of an order.
type OrderStatus string

const (
	OrderStatusLive      OrderStatus = "LIVE"
	OrderStatusMatched   OrderStatus = "MATCHED"
	OrderStatusDelayed   OrderStatus = "DELAYED"
	OrderStatusCancelled OrderStatus = "CANCELLED"
)

// OrderSide represents the side of an order.
type OrderSide string

const (
	OrderSideBuy  OrderSide = "BUY"
	OrderSideSell OrderSide = "SELL"
)

// OrderType represents the type of an order.
type OrderType string

const (
	OrderTypeGTC OrderType = "GTC" // Good Till Cancelled
	OrderTypeFOK OrderType = "FOK" // Fill Or Kill
	OrderTypeGTD OrderType = "GTD" // Good Till Date
)

// OrderBookSummary represents the orderbook for a token.
type OrderBookSummary struct {
	Market    string       `json:"market"`
	TokenID   string       `json:"asset_id"`
	Hash      string       `json:"hash"`
	Timestamp string       `json:"timestamp"`
	Bids      []PriceLevel `json:"bids"`
	Asks      []PriceLevel `json:"asks"`
}

// PriceLevel represents a price level in the orderbook.
type PriceLevel struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

// Trade represents an executed trade.
type Trade struct {
	ID              string    `json:"id"`
	TakerOrderID    string    `json:"taker_order_id"`
	Market          string    `json:"market"`
	TokenID         string    `json:"asset_id"`
	Side            OrderSide `json:"side"`
	Size            string    `json:"size"`
	Price           string    `json:"price"`
	FeeRateBps      string    `json:"fee_rate_bps"`
	Status          string    `json:"status"`
	MatchTime       time.Time `json:"match_time"`
	LastUpdated     time.Time `json:"last_update"`
	TradeOwner      string    `json:"trader"`
	Maker           string    `json:"maker_address"`
	TransactionHash string    `json:"transaction_hash,omitempty"`
	BucketIndex     int       `json:"bucket_index"`
	Outcome         string    `json:"outcome"`
	Type            string    `json:"type"`
}

// MarketInfo represents market information from CLOB.
type MarketInfo struct {
	ConditionID      string  `json:"condition_id"`
	QuestionID       string  `json:"question_id"`
	Tokens           []Token `json:"tokens"`
	MinimumOrderSize string  `json:"minimum_order_size"`
	MinimumTickSize  string  `json:"minimum_tick_size"`
	Description      string  `json:"description"`
	Category         string  `json:"category"`
	EndDate          string  `json:"end_date"`
	GameStartTime    string  `json:"game_start_time,omitempty"`
	QuestionTitle    string  `json:"question"`
	MarketSlug       string  `json:"market_slug"`
	Active           bool    `json:"active"`
	Closed           bool    `json:"closed"`
	Funded           bool    `json:"funded"`
	RewardsMinSize   string  `json:"rewards_min_size"`
	RewardsMaxSpread string  `json:"rewards_max_spread"`
	AcceptingOrders  bool    `json:"accepting_orders"`
	NegRisk          bool    `json:"neg_risk"`
}

// Token represents a token in a market.
type Token struct {
	TokenID string `json:"token_id"`
	Outcome string `json:"outcome"`
	Price   string `json:"price"`
	Winner  bool   `json:"winner"`
}

// BalanceAllowance represents balance and allowance info.
type BalanceAllowance struct {
	Balance   string `json:"balance"`
	Allowance string `json:"allowance"`
}

// OrderArgs represents arguments for creating an order.
type OrderArgs struct {
	TokenID    string    `json:"token_id"`
	Side       OrderSide `json:"side"`
	Price      float64   `json:"price"`
	Size       float64   `json:"size"`
	OrderType  OrderType `json:"order_type,omitempty"`
	Expiration int64     `json:"expiration,omitempty"` // Unix timestamp
}

// MarketOrderArgs represents arguments for creating a market order.
type MarketOrderArgs struct {
	TokenID string    `json:"token_id"`
	Amount  float64   `json:"amount"`         // In USDC
	Side    OrderSide `json:"side,omitempty"` // Optional, inferred from token
}

// SignedOrder represents a signed order ready for submission.
type SignedOrder struct {
	Order     OrderPayload `json:"order"`
	Signature string       `json:"signature"`
	Owner     string       `json:"owner"`
	OrderType OrderType    `json:"orderType"`
}

// OrderPayload is the order data sent to the API.
type OrderPayload struct {
	Salt          string `json:"salt"`
	Maker         string `json:"maker"`
	Signer        string `json:"signer"`
	Taker         string `json:"taker"`
	TokenID       string `json:"tokenId"`
	MakerAmount   string `json:"makerAmount"`
	TakerAmount   string `json:"takerAmount"`
	Expiration    string `json:"expiration"`
	Nonce         string `json:"nonce"`
	FeeRateBps    string `json:"feeRateBps"`
	Side          string `json:"side"` // "BUY" or "SELL"
	SignatureType int    `json:"signatureType"`
}

// APICredentials holds CLOB L2 API credentials.
type APICredentials struct {
	APIKey     string `json:"apiKey"`
	Secret     string `json:"secret"`
	Passphrase string `json:"passphrase"`
}

// PostOrderResponse is the response from posting an order.
type PostOrderResponse struct {
	OrderID  string `json:"orderID"`
	Success  bool   `json:"success"`
	ErrorMsg string `json:"errorMsg,omitempty"`
}

// CancelOrderResponse is the response from canceling an order.
type CancelOrderResponse struct {
	Canceled    []string        `json:"canceled"`
	NotCanceled []CancelFailure `json:"not_canceled,omitempty"`
}

// CancelFailure describes why an order couldn't be canceled.
type CancelFailure struct {
	OrderID string `json:"order_id"`
	Reason  string `json:"reason"`
}

// OpenOrdersResponse is the response from getting open orders.
type OpenOrdersResponse []Order

// TradesResponse is the response from getting trades.
type TradesResponse []Trade
