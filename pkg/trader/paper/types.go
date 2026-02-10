// Package paper provides paper trading simulation for Polymarket.
// Three modes are supported:
// - Simple: Instant fills at mid-price
// - Realistic: Book matching with slippage simulation
// - Backtest: Historical data replay
package paper

import (
	"time"

	"github.com/shopspring/decimal"
)

// Mode represents the paper trading mode.
type Mode int

const (
	// ModeSimple fills orders instantly at mid-price
	ModeSimple Mode = iota
	// ModeRealistic matches against the orderbook with slippage
	ModeRealistic
	// ModeBacktest replays historical data
	ModeBacktest
)

func (m Mode) String() string {
	switch m {
	case ModeSimple:
		return "simple"
	case ModeRealistic:
		return "realistic"
	case ModeBacktest:
		return "backtest"
	default:
		return "unknown"
	}
}

// Order represents a paper trading order.
type Order struct {
	ID           string          `json:"id"`
	TokenID      string          `json:"token_id"`
	Market       string          `json:"market"`
	Side         Side            `json:"side"`
	OrderType    OrderType       `json:"order_type"`
	Price        decimal.Decimal `json:"price"`
	Size         decimal.Decimal `json:"size"`
	FilledSize   decimal.Decimal `json:"filled_size"`
	AvgFillPrice decimal.Decimal `json:"avg_fill_price"`
	Status       OrderStatus     `json:"status"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
	Expiration   time.Time       `json:"expiration,omitempty"`
	Fills        []Fill          `json:"fills,omitempty"`
}

// Side represents order side.
type Side int

const (
	SideBuy Side = iota
	SideSell
)

func (s Side) String() string {
	if s == SideBuy {
		return "BUY"
	}
	return "SELL"
}

// OrderType represents order type.
type OrderType int

const (
	OrderTypeLimit OrderType = iota
	OrderTypeMarket
)

func (t OrderType) String() string {
	if t == OrderTypeMarket {
		return "MARKET"
	}
	return "LIMIT"
}

// OrderStatus represents order status.
type OrderStatus int

const (
	OrderStatusOpen OrderStatus = iota
	OrderStatusPartiallyFilled
	OrderStatusFilled
	OrderStatusCanceled
	OrderStatusExpired
	OrderStatusRejected
)

func (s OrderStatus) String() string {
	switch s {
	case OrderStatusOpen:
		return "OPEN"
	case OrderStatusPartiallyFilled:
		return "PARTIALLY_FILLED"
	case OrderStatusFilled:
		return "FILLED"
	case OrderStatusCanceled:
		return "CANCELED"
	case OrderStatusExpired:
		return "EXPIRED"
	case OrderStatusRejected:
		return "REJECTED"
	default:
		return "UNKNOWN"
	}
}

// Fill represents a single fill.
type Fill struct {
	Price     decimal.Decimal `json:"price"`
	Size      decimal.Decimal `json:"size"`
	Timestamp time.Time       `json:"timestamp"`
	Fee       decimal.Decimal `json:"fee"`
}

// Position represents a position in a market.
type Position struct {
	TokenID       string          `json:"token_id"`
	Market        string          `json:"market"`
	Side          Side            `json:"side"` // Long (bought YES/NO) or Short
	Size          decimal.Decimal `json:"size"`
	AvgEntry      decimal.Decimal `json:"avg_entry"`
	CurrentPrice  decimal.Decimal `json:"current_price"`
	UnrealizedPnL decimal.Decimal `json:"unrealized_pnl"`
	RealizedPnL   decimal.Decimal `json:"realized_pnl"`
	OpenedAt      time.Time       `json:"opened_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// Trade represents a completed trade.
type Trade struct {
	ID        string          `json:"id"`
	OrderID   string          `json:"order_id"`
	TokenID   string          `json:"token_id"`
	Market    string          `json:"market"`
	Side      Side            `json:"side"`
	Price     decimal.Decimal `json:"price"`
	Size      decimal.Decimal `json:"size"`
	Fee       decimal.Decimal `json:"fee"`
	PnL       decimal.Decimal `json:"pnl"`
	Timestamp time.Time       `json:"timestamp"`
}

// Account represents a paper trading account.
type Account struct {
	ID             string               `json:"id"`
	Name           string               `json:"name"`
	InitialBalance decimal.Decimal      `json:"initial_balance"`
	Balance        decimal.Decimal      `json:"balance"`
	Positions      map[string]*Position `json:"positions"`   // tokenID -> position
	OpenOrders     map[string]*Order    `json:"open_orders"` // orderID -> order
	TradeHistory   []Trade              `json:"trade_history"`
	CreatedAt      time.Time            `json:"created_at"`
	UpdatedAt      time.Time            `json:"updated_at"`
}

// AccountStats provides account statistics.
type AccountStats struct {
	TotalPnL      decimal.Decimal `json:"total_pnl"`
	RealizedPnL   decimal.Decimal `json:"realized_pnl"`
	UnrealizedPnL decimal.Decimal `json:"unrealized_pnl"`
	TotalTrades   int             `json:"total_trades"`
	WinningTrades int             `json:"winning_trades"`
	LosingTrades  int             `json:"losing_trades"`
	WinRate       decimal.Decimal `json:"win_rate"`
	AvgWin        decimal.Decimal `json:"avg_win"`
	AvgLoss       decimal.Decimal `json:"avg_loss"`
	LargestWin    decimal.Decimal `json:"largest_win"`
	LargestLoss   decimal.Decimal `json:"largest_loss"`
	Sharpe        decimal.Decimal `json:"sharpe_ratio"`
	MaxDrawdown   decimal.Decimal `json:"max_drawdown"`
	TotalVolume   decimal.Decimal `json:"total_volume"`
	TotalFees     decimal.Decimal `json:"total_fees"`
}

// OrderRequest is a request to place an order.
type OrderRequest struct {
	TokenID    string          `json:"token_id"`
	Market     string          `json:"market"`
	Side       Side            `json:"side"`
	OrderType  OrderType       `json:"order_type"`
	Price      decimal.Decimal `json:"price"` // Required for limit orders
	Size       decimal.Decimal `json:"size"`
	Expiration time.Duration   `json:"expiration"` // Optional TTL
}

// SimulationConfig configures the paper trading simulation.
type SimulationConfig struct {
	Mode           Mode            `json:"mode"`
	InitialBalance decimal.Decimal `json:"initial_balance"`

	// Fee settings
	MakerFeeBps decimal.Decimal `json:"maker_fee_bps"`
	TakerFeeBps decimal.Decimal `json:"taker_fee_bps"`

	// Realistic mode settings
	SlippageModel   SlippageModel   `json:"slippage_model"`
	FillProbability decimal.Decimal `json:"fill_probability"` // 0-1, chance of fill per tick
	LatencyMs       int             `json:"latency_ms"`       // Simulated latency

	// Backtest settings
	StartTime  time.Time `json:"start_time"`
	EndTime    time.Time `json:"end_time"`
	DataSource string    `json:"data_source"` // Path or URL to historical data
}

// SlippageModel defines how slippage is calculated.
type SlippageModel int

const (
	// SlippageNone - no slippage (fills at limit price)
	SlippageNone SlippageModel = iota
	// SlippageFixed - fixed percentage slippage
	SlippageFixed
	// SlippageLinear - slippage proportional to order size
	SlippageLinear
	// SlippageSquareRoot - slippage proportional to sqrt(size)
	SlippageSquareRoot
	// SlippageOrderbook - slippage based on actual orderbook depth
	SlippageOrderbook
)

// DefaultSimulationConfig returns default configuration.
func DefaultSimulationConfig() *SimulationConfig {
	return &SimulationConfig{
		Mode:            ModeSimple,
		InitialBalance:  decimal.NewFromInt(10000),
		MakerFeeBps:     decimal.Zero,
		TakerFeeBps:     decimal.NewFromFloat(0.5), // 0.05% = 5 bps
		SlippageModel:   SlippageNone,
		FillProbability: decimal.NewFromInt(1),
		LatencyMs:       0,
	}
}

// RealisticSimulationConfig returns config for realistic simulation.
func RealisticSimulationConfig() *SimulationConfig {
	return &SimulationConfig{
		Mode:            ModeRealistic,
		InitialBalance:  decimal.NewFromInt(10000),
		MakerFeeBps:     decimal.Zero,
		TakerFeeBps:     decimal.NewFromFloat(0.5),
		SlippageModel:   SlippageOrderbook,
		FillProbability: decimal.NewFromFloat(0.8),
		LatencyMs:       100,
	}
}
