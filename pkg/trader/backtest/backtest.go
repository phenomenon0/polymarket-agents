// Package backtest provides historical backtesting functionality for trading strategies.
package backtest

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/phenomenon0/polymarket-agents/pkg/polymarket/book"
	"github.com/phenomenon0/polymarket-agents/pkg/trader/paper"

	"github.com/shopspring/decimal"
)

// PricePoint represents a historical price at a point in time.
type PricePoint struct {
	Timestamp time.Time       `json:"timestamp"`
	TokenID   string          `json:"token_id"`
	Market    string          `json:"market"`
	Price     decimal.Decimal `json:"price"`
	Volume    decimal.Decimal `json:"volume"`
	BidPrice  decimal.Decimal `json:"bid_price,omitempty"`
	AskPrice  decimal.Decimal `json:"ask_price,omitempty"`
	BidSize   decimal.Decimal `json:"bid_size,omitempty"`
	AskSize   decimal.Decimal `json:"ask_size,omitempty"`
}

// HistoricalData holds historical price data for backtesting.
type HistoricalData struct {
	TokenID    string       `json:"token_id"`
	Market     string       `json:"market"`
	StartTime  time.Time    `json:"start_time"`
	EndTime    time.Time    `json:"end_time"`
	Points     []PricePoint `json:"points"`
	Resolution time.Time    `json:"resolution,omitempty"` // Market resolution time
	Outcome    *bool        `json:"outcome,omitempty"`    // YES=true, NO=false
}

// Config holds backtest configuration.
type Config struct {
	StartTime      time.Time
	EndTime        time.Time
	InitialBalance decimal.Decimal
	TimeScale      float64       // Speed multiplier (1.0 = real-time, 0 = instant)
	TickInterval   time.Duration // How often to process ticks
	SlippageModel  paper.SlippageModel
	MakerFeeBps    decimal.Decimal
	TakerFeeBps    decimal.Decimal
	AllowShorts    bool
}

// DefaultConfig returns default backtest configuration.
func DefaultConfig() *Config {
	return &Config{
		InitialBalance: decimal.NewFromInt(10000),
		TimeScale:      0, // Instant (as fast as possible)
		TickInterval:   time.Minute,
		SlippageModel:  paper.SlippageLinear,
		MakerFeeBps:    decimal.Zero,
		TakerFeeBps:    decimal.NewFromFloat(0.5),
	}
}

// Result holds backtest results.
type Result struct {
	StartTime      time.Time       `json:"start_time"`
	EndTime        time.Time       `json:"end_time"`
	Duration       time.Duration   `json:"duration"`
	InitialBalance decimal.Decimal `json:"initial_balance"`
	FinalBalance   decimal.Decimal `json:"final_balance"`
	TotalPnL       decimal.Decimal `json:"total_pnl"`
	TotalReturn    decimal.Decimal `json:"total_return"` // Percentage
	TotalTrades    int             `json:"total_trades"`
	WinningTrades  int             `json:"winning_trades"`
	LosingTrades   int             `json:"losing_trades"`
	WinRate        decimal.Decimal `json:"win_rate"`
	MaxDrawdown    decimal.Decimal `json:"max_drawdown"`
	SharpeRatio    decimal.Decimal `json:"sharpe_ratio"`
	TotalVolume    decimal.Decimal `json:"total_volume"`
	TotalFees      decimal.Decimal `json:"total_fees"`
	Trades         []TradeRecord   `json:"trades,omitempty"`
	EquityCurve    []EquityPoint   `json:"equity_curve,omitempty"`
}

// TradeRecord records a single trade during backtest.
type TradeRecord struct {
	Timestamp time.Time       `json:"timestamp"`
	TokenID   string          `json:"token_id"`
	Side      string          `json:"side"`
	Price     decimal.Decimal `json:"price"`
	Size      decimal.Decimal `json:"size"`
	Fee       decimal.Decimal `json:"fee"`
	PnL       decimal.Decimal `json:"pnl"`
}

// EquityPoint records equity at a point in time.
type EquityPoint struct {
	Timestamp time.Time       `json:"timestamp"`
	Equity    decimal.Decimal `json:"equity"`
	Drawdown  decimal.Decimal `json:"drawdown"`
}

// Strategy is the interface for trading strategies.
type Strategy interface {
	// OnTick is called for each price update.
	OnTick(ctx context.Context, bt *Backtest, point PricePoint)

	// OnStart is called when backtest starts.
	OnStart(ctx context.Context, bt *Backtest)

	// OnEnd is called when backtest ends.
	OnEnd(ctx context.Context, bt *Backtest)
}

// Backtest runs a historical backtest.
type Backtest struct {
	config      *Config
	data        map[string]*HistoricalData // tokenID -> data
	engine      *paper.Engine
	strategy    Strategy
	currentTime time.Time

	// Results tracking
	trades      []TradeRecord
	equityCurve []EquityPoint
	peakEquity  decimal.Decimal
	maxDrawdown decimal.Decimal
}

// backtestPriceProvider provides prices from historical data.
type backtestPriceProvider struct {
	bt *Backtest
}

func (p *backtestPriceProvider) GetMidPrice(ctx context.Context, tokenID string) (decimal.Decimal, error) {
	price, ok := p.bt.GetPrice(tokenID)
	if !ok {
		return decimal.Zero, fmt.Errorf("no price data for token %s", tokenID)
	}
	return price, nil
}

func (p *backtestPriceProvider) GetOrderBook(ctx context.Context, tokenID string) (*book.OrderBook, error) {
	ob := p.bt.GetOrderBook(tokenID)
	if ob == nil {
		return nil, fmt.Errorf("no orderbook for token %s", tokenID)
	}
	return ob, nil
}

// New creates a new backtest.
func New(config *Config) *Backtest {
	if config == nil {
		config = DefaultConfig()
	}

	bt := &Backtest{
		config:      config,
		data:        make(map[string]*HistoricalData),
		trades:      make([]TradeRecord, 0),
		equityCurve: make([]EquityPoint, 0),
		peakEquity:  config.InitialBalance,
	}

	paperConfig := &paper.SimulationConfig{
		Mode:           paper.ModeSimple,
		InitialBalance: config.InitialBalance,
		MakerFeeBps:    config.MakerFeeBps,
		TakerFeeBps:    config.TakerFeeBps,
		SlippageModel:  config.SlippageModel,
	}

	// Create price provider that uses backtest data
	provider := &backtestPriceProvider{bt: bt}
	bt.engine = paper.NewEngine(paperConfig, provider)

	// Set up trade tracking
	bt.engine.OnTrade(func(trade *paper.Trade) {
		bt.trades = append(bt.trades, TradeRecord{
			Timestamp: bt.currentTime,
			TokenID:   trade.TokenID,
			Side:      trade.Side.String(),
			Price:     trade.Price,
			Size:      trade.Size,
			Fee:       trade.Fee,
			PnL:       trade.PnL,
		})
	})

	return bt
}

// LoadData loads historical data for a token.
func (bt *Backtest) LoadData(data *HistoricalData) {
	bt.data[data.TokenID] = data

	// Update time range
	if bt.config.StartTime.IsZero() || data.StartTime.Before(bt.config.StartTime) {
		bt.config.StartTime = data.StartTime
	}
	if bt.config.EndTime.IsZero() || data.EndTime.After(bt.config.EndTime) {
		bt.config.EndTime = data.EndTime
	}
}

// LoadDataFromJSON loads historical data from a JSON file.
func (bt *Backtest) LoadDataFromJSON(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	var data HistoricalData
	if err := json.NewDecoder(file).Decode(&data); err != nil {
		return fmt.Errorf("failed to decode JSON: %w", err)
	}

	bt.LoadData(&data)
	return nil
}

// LoadDataFromCSV loads historical data from a CSV file.
// Expected columns: timestamp, token_id, market, price, volume, bid_price, ask_price, bid_size, ask_size
func (bt *Backtest) LoadDataFromCSV(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)

	// Read header
	header, err := reader.Read()
	if err != nil {
		return fmt.Errorf("failed to read header: %w", err)
	}

	// Build column index
	colIndex := make(map[string]int)
	for i, col := range header {
		colIndex[col] = i
	}

	// Read data
	dataByToken := make(map[string][]PricePoint)

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read record: %w", err)
		}

		point := PricePoint{}

		if idx, ok := colIndex["timestamp"]; ok {
			if t, err := time.Parse(time.RFC3339, record[idx]); err == nil {
				point.Timestamp = t
			} else if ts, err := strconv.ParseInt(record[idx], 10, 64); err == nil {
				point.Timestamp = time.Unix(ts, 0)
			}
		}
		if idx, ok := colIndex["token_id"]; ok {
			point.TokenID = record[idx]
		}
		if idx, ok := colIndex["market"]; ok {
			point.Market = record[idx]
		}
		if idx, ok := colIndex["price"]; ok {
			point.Price, _ = decimal.NewFromString(record[idx])
		}
		if idx, ok := colIndex["volume"]; ok {
			point.Volume, _ = decimal.NewFromString(record[idx])
		}
		if idx, ok := colIndex["bid_price"]; ok {
			point.BidPrice, _ = decimal.NewFromString(record[idx])
		}
		if idx, ok := colIndex["ask_price"]; ok {
			point.AskPrice, _ = decimal.NewFromString(record[idx])
		}

		dataByToken[point.TokenID] = append(dataByToken[point.TokenID], point)
	}

	// Convert to HistoricalData
	for tokenID, points := range dataByToken {
		if len(points) == 0 {
			continue
		}

		// Sort by timestamp
		sort.Slice(points, func(i, j int) bool {
			return points[i].Timestamp.Before(points[j].Timestamp)
		})

		data := &HistoricalData{
			TokenID:   tokenID,
			Market:    points[0].Market,
			StartTime: points[0].Timestamp,
			EndTime:   points[len(points)-1].Timestamp,
			Points:    points,
		}
		bt.LoadData(data)
	}

	return nil
}

// Run executes the backtest with the given strategy.
func (bt *Backtest) Run(ctx context.Context, strategy Strategy) (*Result, error) {
	bt.strategy = strategy

	// Collect all price points and sort by time
	allPoints := make([]PricePoint, 0)
	for _, data := range bt.data {
		allPoints = append(allPoints, data.Points...)
	}
	sort.Slice(allPoints, func(i, j int) bool {
		return allPoints[i].Timestamp.Before(allPoints[j].Timestamp)
	})

	if len(allPoints) == 0 {
		return nil, fmt.Errorf("no historical data loaded")
	}

	// Initialize
	bt.currentTime = allPoints[0].Timestamp
	strategy.OnStart(ctx, bt)

	// Process each tick
	for _, point := range allPoints {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		bt.currentTime = point.Timestamp

		// Update price in engine
		bt.engine.ProcessTick(ctx, point.TokenID, point.Price)

		// Call strategy
		strategy.OnTick(ctx, bt, point)

		// Record equity
		bt.recordEquity()

		// Apply time scaling
		if bt.config.TimeScale > 0 {
			time.Sleep(time.Duration(float64(bt.config.TickInterval) / bt.config.TimeScale))
		}
	}

	// Handle market resolutions
	for _, data := range bt.data {
		if data.Outcome != nil {
			bt.resolveMarket(data)
		}
	}

	strategy.OnEnd(ctx, bt)

	return bt.calculateResult(), nil
}

func (bt *Backtest) recordEquity() {
	equity := bt.engine.GetBalance()

	// Add unrealized PnL from positions
	for _, pos := range bt.engine.GetPositions() {
		equity = equity.Add(pos.UnrealizedPnL)
	}

	// Track peak and drawdown
	if equity.GreaterThan(bt.peakEquity) {
		bt.peakEquity = equity
	}
	drawdown := bt.peakEquity.Sub(equity).Div(bt.peakEquity)
	if drawdown.GreaterThan(bt.maxDrawdown) {
		bt.maxDrawdown = drawdown
	}

	bt.equityCurve = append(bt.equityCurve, EquityPoint{
		Timestamp: bt.currentTime,
		Equity:    equity,
		Drawdown:  drawdown,
	})
}

func (bt *Backtest) resolveMarket(data *HistoricalData) {
	// Close any positions in this market at resolution price
	pos, ok := bt.engine.GetPosition(data.TokenID)
	if !ok || pos.Size.IsZero() {
		return
	}

	// Sell the position at resolution price
	// (The engine will use current market price; in a real backtest
	// we would want to simulate resolution at 1.0 or 0.0)
	bt.currentTime = data.Resolution
	ctx := context.Background()

	_, _ = bt.engine.PlaceOrder(ctx, &paper.OrderRequest{
		TokenID:   data.TokenID,
		Market:    data.Market,
		Side:      paper.SideSell,
		OrderType: paper.OrderTypeMarket,
		Size:      pos.Size,
	})
}

func (bt *Backtest) calculateResult() *Result {
	stats := bt.engine.GetStats()

	result := &Result{
		StartTime:      bt.config.StartTime,
		EndTime:        bt.config.EndTime,
		Duration:       bt.config.EndTime.Sub(bt.config.StartTime),
		InitialBalance: bt.config.InitialBalance,
		FinalBalance:   bt.engine.GetBalance(),
		TotalPnL:       stats.TotalPnL,
		TotalTrades:    stats.TotalTrades,
		WinningTrades:  stats.WinningTrades,
		LosingTrades:   stats.LosingTrades,
		WinRate:        stats.WinRate,
		MaxDrawdown:    bt.maxDrawdown,
		TotalVolume:    stats.TotalVolume,
		TotalFees:      stats.TotalFees,
		Trades:         bt.trades,
		EquityCurve:    bt.equityCurve,
	}

	// Calculate return
	if !bt.config.InitialBalance.IsZero() {
		result.TotalReturn = result.TotalPnL.Div(bt.config.InitialBalance).Mul(decimal.NewFromInt(100))
	}

	// Simple Sharpe ratio approximation
	// (This is a simplified version - proper Sharpe needs returns distribution)
	if len(bt.equityCurve) > 1 && !bt.maxDrawdown.IsZero() {
		// Risk-adjusted return: return / max drawdown
		result.SharpeRatio = result.TotalReturn.Div(bt.maxDrawdown.Mul(decimal.NewFromInt(100)))
	}

	return result
}

// --- Trading methods for strategies ---

// CurrentTime returns the current simulated time.
func (bt *Backtest) CurrentTime() time.Time {
	return bt.currentTime
}

// Balance returns the current balance.
func (bt *Backtest) Balance() decimal.Decimal {
	return bt.engine.GetBalance()
}

// Position returns the position for a token.
func (bt *Backtest) Position(tokenID string) (*paper.Position, bool) {
	return bt.engine.GetPosition(tokenID)
}

// Positions returns all positions.
func (bt *Backtest) Positions() []*paper.Position {
	return bt.engine.GetPositions()
}

// Buy places a buy order.
func (bt *Backtest) Buy(tokenID, market string, size decimal.Decimal) error {
	_, err := bt.engine.PlaceOrder(context.Background(), &paper.OrderRequest{
		TokenID:   tokenID,
		Market:    market,
		Side:      paper.SideBuy,
		OrderType: paper.OrderTypeMarket,
		Size:      size,
	})
	return err
}

// Sell places a sell order.
func (bt *Backtest) Sell(tokenID, market string, size decimal.Decimal) error {
	_, err := bt.engine.PlaceOrder(context.Background(), &paper.OrderRequest{
		TokenID:   tokenID,
		Market:    market,
		Side:      paper.SideSell,
		OrderType: paper.OrderTypeMarket,
		Size:      size,
	})
	return err
}

// BuyLimit places a limit buy order.
func (bt *Backtest) BuyLimit(tokenID, market string, size, price decimal.Decimal) error {
	_, err := bt.engine.PlaceOrder(context.Background(), &paper.OrderRequest{
		TokenID:   tokenID,
		Market:    market,
		Side:      paper.SideBuy,
		OrderType: paper.OrderTypeLimit,
		Price:     price,
		Size:      size,
	})
	return err
}

// SellLimit places a limit sell order.
func (bt *Backtest) SellLimit(tokenID, market string, size, price decimal.Decimal) error {
	_, err := bt.engine.PlaceOrder(context.Background(), &paper.OrderRequest{
		TokenID:   tokenID,
		Market:    market,
		Side:      paper.SideSell,
		OrderType: paper.OrderTypeLimit,
		Price:     price,
		Size:      size,
	})
	return err
}

// GetPrice returns the last price for a token.
func (bt *Backtest) GetPrice(tokenID string) (decimal.Decimal, bool) {
	data, ok := bt.data[tokenID]
	if !ok {
		return decimal.Zero, false
	}

	// Find the latest price at or before current time
	for i := len(data.Points) - 1; i >= 0; i-- {
		if !data.Points[i].Timestamp.After(bt.currentTime) {
			return data.Points[i].Price, true
		}
	}
	return decimal.Zero, false
}

// GetOrderBook returns a simulated order book.
func (bt *Backtest) GetOrderBook(tokenID string) *book.OrderBook {
	price, ok := bt.GetPrice(tokenID)
	if !ok {
		return nil
	}

	data := bt.data[tokenID]
	ob := book.NewOrderBook(tokenID, data.Market)

	// Create synthetic orderbook around the price
	spread := decimal.NewFromFloat(0.01) // 1% spread
	bidPrice := price.Sub(spread.Div(decimal.NewFromInt(2)))
	askPrice := price.Add(spread.Div(decimal.NewFromInt(2)))

	ob.SetBids([]book.PriceLevel{
		{Price: bidPrice, Size: decimal.NewFromInt(1000)},
	})
	ob.SetAsks([]book.PriceLevel{
		{Price: askPrice, Size: decimal.NewFromInt(1000)},
	})

	return ob
}
