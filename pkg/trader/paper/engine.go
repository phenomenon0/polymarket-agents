package paper

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/phenomenon0/polymarket-agents/pkg/polymarket/book"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// PriceProvider provides current prices for tokens.
type PriceProvider interface {
	GetMidPrice(ctx context.Context, tokenID string) (decimal.Decimal, error)
	GetOrderBook(ctx context.Context, tokenID string) (*book.OrderBook, error)
}

// Engine is the paper trading simulation engine.
type Engine struct {
	config   *SimulationConfig
	account  *Account
	provider PriceProvider

	mu       sync.RWMutex
	orderSeq int64
	tradeSeq int64

	// Callbacks
	onOrder func(*Order)
	onTrade func(*Trade)
	onFill  func(*Order, *Fill)
}

// NewEngine creates a new paper trading engine.
func NewEngine(config *SimulationConfig, provider PriceProvider) *Engine {
	if config == nil {
		config = DefaultSimulationConfig()
	}

	return &Engine{
		config:   config,
		provider: provider,
		account: &Account{
			ID:             uuid.New().String(),
			Name:           "Paper Trading Account",
			InitialBalance: config.InitialBalance,
			Balance:        config.InitialBalance,
			Positions:      make(map[string]*Position),
			OpenOrders:     make(map[string]*Order),
			TradeHistory:   make([]Trade, 0),
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		},
	}
}

// OnOrder sets a callback for order events.
func (e *Engine) OnOrder(fn func(*Order)) {
	e.onOrder = fn
}

// OnTrade sets a callback for trade events.
func (e *Engine) OnTrade(fn func(*Trade)) {
	e.onTrade = fn
}

// OnFill sets a callback for fill events.
func (e *Engine) OnFill(fn func(*Order, *Fill)) {
	e.onFill = fn
}

// PlaceOrder places a new order.
func (e *Engine) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Validate request
	if req.Size.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf("order size must be positive")
	}
	if req.OrderType == OrderTypeLimit && req.Price.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf("limit order requires positive price")
	}

	// Check balance for buys
	if req.Side == SideBuy {
		cost := req.Size.Mul(req.Price)
		if req.OrderType == OrderTypeMarket {
			// Estimate cost at current price
			midPrice, err := e.provider.GetMidPrice(ctx, req.TokenID)
			if err != nil {
				return nil, fmt.Errorf("failed to get price: %w", err)
			}
			cost = req.Size.Mul(midPrice)
		}
		if cost.GreaterThan(e.account.Balance) {
			return nil, fmt.Errorf("insufficient balance: have %s, need %s", e.account.Balance, cost)
		}
	}

	// Create order
	e.orderSeq++
	order := &Order{
		ID:         fmt.Sprintf("paper-%d", e.orderSeq),
		TokenID:    req.TokenID,
		Market:     req.Market,
		Side:       req.Side,
		OrderType:  req.OrderType,
		Price:      req.Price,
		Size:       req.Size,
		FilledSize: decimal.Zero,
		Status:     OrderStatusOpen,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		Fills:      make([]Fill, 0),
	}

	if req.Expiration > 0 {
		order.Expiration = time.Now().Add(req.Expiration)
	}

	// Store order
	e.account.OpenOrders[order.ID] = order
	e.account.UpdatedAt = time.Now()

	// Notify
	if e.onOrder != nil {
		e.onOrder(order)
	}

	// Try to fill immediately based on mode
	switch e.config.Mode {
	case ModeSimple:
		e.tryFillSimple(ctx, order)
	case ModeRealistic:
		e.tryFillRealistic(ctx, order)
	}

	return order, nil
}

// CancelOrder cancels an open order.
func (e *Engine) CancelOrder(orderID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	order, ok := e.account.OpenOrders[orderID]
	if !ok {
		return fmt.Errorf("order not found: %s", orderID)
	}

	if order.Status != OrderStatusOpen && order.Status != OrderStatusPartiallyFilled {
		return fmt.Errorf("order cannot be canceled: status is %s", order.Status)
	}

	order.Status = OrderStatusCanceled
	order.UpdatedAt = time.Now()
	delete(e.account.OpenOrders, orderID)

	if e.onOrder != nil {
		e.onOrder(order)
	}

	return nil
}

// CancelAllOrders cancels all open orders.
func (e *Engine) CancelAllOrders() int {
	e.mu.Lock()
	defer e.mu.Unlock()

	count := 0
	for id, order := range e.account.OpenOrders {
		order.Status = OrderStatusCanceled
		order.UpdatedAt = time.Now()
		delete(e.account.OpenOrders, id)
		count++

		if e.onOrder != nil {
			e.onOrder(order)
		}
	}

	return count
}

// GetOrder returns an order by ID.
func (e *Engine) GetOrder(orderID string) (*Order, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	order, ok := e.account.OpenOrders[orderID]
	return order, ok
}

// GetOpenOrders returns all open orders.
func (e *Engine) GetOpenOrders() []*Order {
	e.mu.RLock()
	defer e.mu.RUnlock()

	orders := make([]*Order, 0, len(e.account.OpenOrders))
	for _, order := range e.account.OpenOrders {
		orders = append(orders, order)
	}
	return orders
}

// GetPosition returns a position by token ID.
func (e *Engine) GetPosition(tokenID string) (*Position, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	pos, ok := e.account.Positions[tokenID]
	return pos, ok
}

// GetPositions returns all positions.
func (e *Engine) GetPositions() []*Position {
	e.mu.RLock()
	defer e.mu.RUnlock()

	positions := make([]*Position, 0, len(e.account.Positions))
	for _, pos := range e.account.Positions {
		positions = append(positions, pos)
	}
	return positions
}

// GetBalance returns the current balance.
func (e *Engine) GetBalance() decimal.Decimal {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.account.Balance
}

// GetAccount returns the full account.
func (e *Engine) GetAccount() *Account {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Return a copy
	acc := *e.account
	return &acc
}

// GetStats calculates account statistics.
func (e *Engine) GetStats() *AccountStats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	stats := &AccountStats{}

	// Calculate realized P&L from trades
	var totalWins, totalLosses decimal.Decimal
	for _, trade := range e.account.TradeHistory {
		stats.TotalTrades++
		stats.TotalVolume = stats.TotalVolume.Add(trade.Price.Mul(trade.Size))
		stats.TotalFees = stats.TotalFees.Add(trade.Fee)
		stats.RealizedPnL = stats.RealizedPnL.Add(trade.PnL)

		if trade.PnL.GreaterThan(decimal.Zero) {
			stats.WinningTrades++
			totalWins = totalWins.Add(trade.PnL)
			if trade.PnL.GreaterThan(stats.LargestWin) {
				stats.LargestWin = trade.PnL
			}
		} else if trade.PnL.LessThan(decimal.Zero) {
			stats.LosingTrades++
			totalLosses = totalLosses.Add(trade.PnL.Abs())
			if trade.PnL.Abs().GreaterThan(stats.LargestLoss) {
				stats.LargestLoss = trade.PnL.Abs()
			}
		}
	}

	// Calculate unrealized P&L from positions
	for _, pos := range e.account.Positions {
		stats.UnrealizedPnL = stats.UnrealizedPnL.Add(pos.UnrealizedPnL)
	}

	stats.TotalPnL = stats.RealizedPnL.Add(stats.UnrealizedPnL)

	// Win rate
	if stats.TotalTrades > 0 {
		stats.WinRate = decimal.NewFromInt(int64(stats.WinningTrades)).Div(decimal.NewFromInt(int64(stats.TotalTrades)))
	}

	// Average win/loss
	if stats.WinningTrades > 0 {
		stats.AvgWin = totalWins.Div(decimal.NewFromInt(int64(stats.WinningTrades)))
	}
	if stats.LosingTrades > 0 {
		stats.AvgLoss = totalLosses.Div(decimal.NewFromInt(int64(stats.LosingTrades)))
	}

	return stats
}

// UpdatePrices updates position prices and unrealized P&L.
func (e *Engine) UpdatePrices(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	for tokenID, pos := range e.account.Positions {
		midPrice, err := e.provider.GetMidPrice(ctx, tokenID)
		if err != nil {
			continue // Skip on error
		}

		pos.CurrentPrice = midPrice

		// Calculate unrealized P&L
		if pos.Side == SideBuy {
			// Long: profit if price went up
			pos.UnrealizedPnL = midPrice.Sub(pos.AvgEntry).Mul(pos.Size)
		} else {
			// Short: profit if price went down
			pos.UnrealizedPnL = pos.AvgEntry.Sub(midPrice).Mul(pos.Size)
		}

		pos.UpdatedAt = time.Now()
	}

	return nil
}

// Reset resets the account to initial state.
func (e *Engine) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.account = &Account{
		ID:             uuid.New().String(),
		Name:           "Paper Trading Account",
		InitialBalance: e.config.InitialBalance,
		Balance:        e.config.InitialBalance,
		Positions:      make(map[string]*Position),
		OpenOrders:     make(map[string]*Order),
		TradeHistory:   make([]Trade, 0),
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	e.orderSeq = 0
	e.tradeSeq = 0
}

// --- Fill Logic ---

func (e *Engine) tryFillSimple(ctx context.Context, order *Order) {
	// Simple mode: fill at mid price instantly
	midPrice, err := e.provider.GetMidPrice(ctx, order.TokenID)
	if err != nil {
		return
	}

	// For limit orders, check if price is acceptable
	if order.OrderType == OrderTypeLimit {
		if order.Side == SideBuy && midPrice.GreaterThan(order.Price) {
			return // Price too high
		}
		if order.Side == SideSell && midPrice.LessThan(order.Price) {
			return // Price too low
		}
	}

	// Fill the entire order at mid price
	e.executeFill(order, midPrice, order.Size)
}

func (e *Engine) tryFillRealistic(ctx context.Context, order *Order) {
	// Realistic mode: simulate against orderbook
	ob, err := e.provider.GetOrderBook(ctx, order.TokenID)
	if err != nil {
		return
	}

	// Determine which side of the book to match against
	var side book.Side
	if order.Side == SideBuy {
		side = book.SideBuy // Buying = take from asks
	} else {
		side = book.SideSell // Selling = take from bids
	}

	// Simulate the match
	result := ob.SimulateMarketOrder(side, order.Size)

	if result.TotalSize.IsZero() {
		return // No liquidity
	}

	// Apply slippage model
	fillPrice := result.AvgPrice
	fillPrice = e.applySlippage(fillPrice, order.Side, result.TotalSize)

	// For limit orders, check price
	if order.OrderType == OrderTypeLimit {
		if order.Side == SideBuy && fillPrice.GreaterThan(order.Price) {
			return
		}
		if order.Side == SideSell && fillPrice.LessThan(order.Price) {
			return
		}
	}

	// Apply fill probability
	if e.config.FillProbability.LessThan(decimal.NewFromInt(1)) {
		// Random fill based on probability - simplified
		// In production, use proper random
		return
	}

	// Execute fill
	e.executeFill(order, fillPrice, result.TotalSize)
}

func (e *Engine) applySlippage(price decimal.Decimal, side Side, size decimal.Decimal) decimal.Decimal {
	switch e.config.SlippageModel {
	case SlippageNone:
		return price

	case SlippageFixed:
		// Apply 0.1% fixed slippage
		slippage := price.Mul(decimal.NewFromFloat(0.001))
		if side == SideBuy {
			return price.Add(slippage)
		}
		return price.Sub(slippage)

	case SlippageLinear:
		// Slippage proportional to size (0.01% per unit)
		slippage := price.Mul(size).Mul(decimal.NewFromFloat(0.0001))
		if side == SideBuy {
			return price.Add(slippage)
		}
		return price.Sub(slippage)

	case SlippageSquareRoot:
		// Slippage proportional to sqrt(size)
		sqrtSize, _ := size.Float64()
		if sqrtSize > 0 {
			sqrtSize = decimal.NewFromFloat(sqrtSize).Pow(decimal.NewFromFloat(0.5)).InexactFloat64()
		}
		slippage := price.Mul(decimal.NewFromFloat(sqrtSize * 0.001))
		if side == SideBuy {
			return price.Add(slippage)
		}
		return price.Sub(slippage)

	default:
		return price
	}
}

func (e *Engine) executeFill(order *Order, price, size decimal.Decimal) {
	// Calculate fee
	var feeBps decimal.Decimal
	if order.OrderType == OrderTypeLimit {
		feeBps = e.config.MakerFeeBps
	} else {
		feeBps = e.config.TakerFeeBps
	}
	fee := price.Mul(size).Mul(feeBps).Div(decimal.NewFromInt(10000))

	// Create fill
	fill := Fill{
		Price:     price,
		Size:      size,
		Timestamp: time.Now(),
		Fee:       fee,
	}
	order.Fills = append(order.Fills, fill)

	// Update order
	order.FilledSize = order.FilledSize.Add(size)
	if order.FilledSize.GreaterThanOrEqual(order.Size) {
		order.Status = OrderStatusFilled
		delete(e.account.OpenOrders, order.ID)
	} else {
		order.Status = OrderStatusPartiallyFilled
	}

	// Calculate average fill price
	totalCost := decimal.Zero
	totalSize := decimal.Zero
	for _, f := range order.Fills {
		totalCost = totalCost.Add(f.Price.Mul(f.Size))
		totalSize = totalSize.Add(f.Size)
	}
	if !totalSize.IsZero() {
		order.AvgFillPrice = totalCost.Div(totalSize)
	}
	order.UpdatedAt = time.Now()

	// Update balance
	cost := price.Mul(size).Add(fee)
	if order.Side == SideBuy {
		e.account.Balance = e.account.Balance.Sub(cost)
	} else {
		e.account.Balance = e.account.Balance.Add(cost.Sub(fee.Mul(decimal.NewFromInt(2))))
	}

	// Update position and get PnL for this trade
	tradePnL := e.updatePositionWithPnL(order.TokenID, order.Market, order.Side, size, price)

	// Create trade record
	e.tradeSeq++
	trade := Trade{
		ID:        fmt.Sprintf("trade-%d", e.tradeSeq),
		OrderID:   order.ID,
		TokenID:   order.TokenID,
		Market:    order.Market,
		Side:      order.Side,
		Price:     price,
		Size:      size,
		Fee:       fee,
		PnL:       tradePnL,
		Timestamp: time.Now(),
	}
	e.account.TradeHistory = append(e.account.TradeHistory, trade)
	e.account.UpdatedAt = time.Now()

	// Notify
	if e.onFill != nil {
		e.onFill(order, &fill)
	}
	if e.onTrade != nil {
		e.onTrade(&trade)
	}
	if e.onOrder != nil {
		e.onOrder(order)
	}
}

// updatePositionWithPnL updates position and returns the PnL realized on this trade (if any).
func (e *Engine) updatePositionWithPnL(tokenID, market string, side Side, size, price decimal.Decimal) decimal.Decimal {
	pos, exists := e.account.Positions[tokenID]

	if !exists {
		// Create new position - no PnL on opening
		pos = &Position{
			TokenID:      tokenID,
			Market:       market,
			Side:         side,
			Size:         size,
			AvgEntry:     price,
			CurrentPrice: price,
			OpenedAt:     time.Now(),
			UpdatedAt:    time.Now(),
		}
		e.account.Positions[tokenID] = pos
		return decimal.Zero
	}

	var tradePnL decimal.Decimal

	// Update existing position
	if pos.Side == side {
		// Adding to position - no PnL on adding
		totalCost := pos.AvgEntry.Mul(pos.Size).Add(price.Mul(size))
		pos.Size = pos.Size.Add(size)
		pos.AvgEntry = totalCost.Div(pos.Size)
	} else {
		// Reducing or reversing position
		if size.GreaterThanOrEqual(pos.Size) {
			// Close and possibly reverse
			closeSize := pos.Size
			reverseSize := size.Sub(closeSize)

			// Calculate P&L on closed portion
			if pos.Side == SideBuy {
				tradePnL = price.Sub(pos.AvgEntry).Mul(closeSize)
			} else {
				tradePnL = pos.AvgEntry.Sub(price).Mul(closeSize)
			}
			pos.RealizedPnL = pos.RealizedPnL.Add(tradePnL)

			if reverseSize.GreaterThan(decimal.Zero) {
				// Reverse position
				pos.Side = side
				pos.Size = reverseSize
				pos.AvgEntry = price
			} else {
				// Close position
				delete(e.account.Positions, tokenID)
				return tradePnL
			}
		} else {
			// Partial close
			if pos.Side == SideBuy {
				tradePnL = price.Sub(pos.AvgEntry).Mul(size)
			} else {
				tradePnL = pos.AvgEntry.Sub(price).Mul(size)
			}
			pos.RealizedPnL = pos.RealizedPnL.Add(tradePnL)
			pos.Size = pos.Size.Sub(size)
		}
	}

	pos.CurrentPrice = price
	pos.UpdatedAt = time.Now()
	return tradePnL
}

// ProcessTick processes market updates (for limit order matching).
func (e *Engine) ProcessTick(ctx context.Context, tokenID string, midPrice decimal.Decimal) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, order := range e.account.OpenOrders {
		if order.TokenID != tokenID {
			continue
		}
		if order.OrderType != OrderTypeLimit {
			continue
		}

		// Check if limit order can be filled
		canFill := false
		if order.Side == SideBuy && midPrice.LessThanOrEqual(order.Price) {
			canFill = true
		}
		if order.Side == SideSell && midPrice.GreaterThanOrEqual(order.Price) {
			canFill = true
		}

		if canFill {
			remainingSize := order.Size.Sub(order.FilledSize)
			e.executeFill(order, order.Price, remainingSize)
		}

		// Check expiration
		if !order.Expiration.IsZero() && time.Now().After(order.Expiration) {
			order.Status = OrderStatusExpired
			order.UpdatedAt = time.Now()
			delete(e.account.OpenOrders, order.ID)
			if e.onOrder != nil {
				e.onOrder(order)
			}
		}
	}
}
