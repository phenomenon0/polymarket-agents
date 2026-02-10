// Package book provides an L2 orderbook implementation optimized for
// Polymarket prediction markets with price-time priority matching.
package book

import (
	"fmt"
	"sort"
	"sync"

	"github.com/shopspring/decimal"
)

// Side represents the order side.
type Side int

const (
	SideBuy  Side = 0
	SideSell Side = 1
)

func (s Side) String() string {
	if s == SideBuy {
		return "BUY"
	}
	return "SELL"
}

// PriceLevel represents an aggregated price level in the orderbook.
type PriceLevel struct {
	Price    decimal.Decimal
	Size     decimal.Decimal
	OrderCnt int
}

// OrderBook is an L2 orderbook with aggregated price levels.
type OrderBook struct {
	AssetID   string
	Market    string
	Timestamp int64

	bids []PriceLevel // sorted by price descending (best bid first)
	asks []PriceLevel // sorted by price ascending (best ask first)
	mu   sync.RWMutex
}

// NewOrderBook creates a new empty orderbook.
func NewOrderBook(assetID, market string) *OrderBook {
	return &OrderBook{
		AssetID: assetID,
		Market:  market,
		bids:    make([]PriceLevel, 0),
		asks:    make([]PriceLevel, 0),
	}
}

// Snapshot represents a point-in-time copy of the orderbook.
type Snapshot struct {
	AssetID   string
	Market    string
	Timestamp int64
	Bids      []PriceLevel
	Asks      []PriceLevel
}

// --- Read Operations ---

// GetSnapshot returns a copy of the current orderbook state.
func (ob *OrderBook) GetSnapshot() Snapshot {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	bids := make([]PriceLevel, len(ob.bids))
	copy(bids, ob.bids)

	asks := make([]PriceLevel, len(ob.asks))
	copy(asks, ob.asks)

	return Snapshot{
		AssetID:   ob.AssetID,
		Market:    ob.Market,
		Timestamp: ob.Timestamp,
		Bids:      bids,
		Asks:      asks,
	}
}

// BestBid returns the best (highest) bid price and size.
// Returns zero values if no bids exist.
func (ob *OrderBook) BestBid() (price, size decimal.Decimal) {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	if len(ob.bids) == 0 {
		return decimal.Zero, decimal.Zero
	}
	return ob.bids[0].Price, ob.bids[0].Size
}

// BestAsk returns the best (lowest) ask price and size.
// Returns zero values if no asks exist.
func (ob *OrderBook) BestAsk() (price, size decimal.Decimal) {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	if len(ob.asks) == 0 {
		return decimal.Zero, decimal.Zero
	}
	return ob.asks[0].Price, ob.asks[0].Size
}

// Midpoint returns the midpoint between best bid and ask.
// Returns zero if either side is empty.
func (ob *OrderBook) Midpoint() decimal.Decimal {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	if len(ob.bids) == 0 || len(ob.asks) == 0 {
		return decimal.Zero
	}

	return ob.bids[0].Price.Add(ob.asks[0].Price).Div(decimal.NewFromInt(2))
}

// Spread returns the bid-ask spread.
// Returns zero if either side is empty.
func (ob *OrderBook) Spread() decimal.Decimal {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	if len(ob.bids) == 0 || len(ob.asks) == 0 {
		return decimal.Zero
	}

	return ob.asks[0].Price.Sub(ob.bids[0].Price)
}

// SpreadBps returns the spread in basis points relative to midpoint.
func (ob *OrderBook) SpreadBps() decimal.Decimal {
	mid := ob.Midpoint()
	if mid.IsZero() {
		return decimal.Zero
	}

	spread := ob.Spread()
	return spread.Div(mid).Mul(decimal.NewFromInt(10000))
}

// Bids returns the bid levels (best first).
func (ob *OrderBook) Bids() []PriceLevel {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	bids := make([]PriceLevel, len(ob.bids))
	copy(bids, ob.bids)
	return bids
}

// Asks returns the ask levels (best first).
func (ob *OrderBook) Asks() []PriceLevel {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	asks := make([]PriceLevel, len(ob.asks))
	copy(asks, ob.asks)
	return asks
}

// BidDepth returns the number of bid levels.
func (ob *OrderBook) BidDepth() int {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	return len(ob.bids)
}

// AskDepth returns the number of ask levels.
func (ob *OrderBook) AskDepth() int {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	return len(ob.asks)
}

// TotalBidSize returns the total size on the bid side.
func (ob *OrderBook) TotalBidSize() decimal.Decimal {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	total := decimal.Zero
	for _, level := range ob.bids {
		total = total.Add(level.Size)
	}
	return total
}

// TotalAskSize returns the total size on the ask side.
func (ob *OrderBook) TotalAskSize() decimal.Decimal {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	total := decimal.Zero
	for _, level := range ob.asks {
		total = total.Add(level.Size)
	}
	return total
}

// VolumeWeightedPrice calculates the VWAP for a given size on specified side.
// Returns the average price to fill the given size.
func (ob *OrderBook) VolumeWeightedPrice(side Side, size decimal.Decimal) (decimal.Decimal, error) {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	var levels []PriceLevel
	if side == SideBuy {
		// Buying = take from asks
		levels = ob.asks
	} else {
		// Selling = take from bids
		levels = ob.bids
	}

	if len(levels) == 0 {
		return decimal.Zero, fmt.Errorf("no liquidity on %s side", side)
	}

	remaining := size
	totalCost := decimal.Zero

	for _, level := range levels {
		if remaining.IsZero() {
			break
		}

		fillSize := level.Size
		if fillSize.GreaterThan(remaining) {
			fillSize = remaining
		}

		totalCost = totalCost.Add(level.Price.Mul(fillSize))
		remaining = remaining.Sub(fillSize)
	}

	if remaining.GreaterThan(decimal.Zero) {
		return decimal.Zero, fmt.Errorf("insufficient liquidity: needed %s, missing %s", size, remaining)
	}

	return totalCost.Div(size), nil
}

// PriceImpact calculates the price impact of a trade of given size.
// Returns the difference between VWAP and best price as a percentage.
func (ob *OrderBook) PriceImpact(side Side, size decimal.Decimal) (decimal.Decimal, error) {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	var bestPrice decimal.Decimal
	if side == SideBuy {
		if len(ob.asks) == 0 {
			return decimal.Zero, fmt.Errorf("no asks")
		}
		bestPrice = ob.asks[0].Price
	} else {
		if len(ob.bids) == 0 {
			return decimal.Zero, fmt.Errorf("no bids")
		}
		bestPrice = ob.bids[0].Price
	}

	ob.mu.RUnlock()
	vwap, err := ob.VolumeWeightedPrice(side, size)
	ob.mu.RLock()

	if err != nil {
		return decimal.Zero, err
	}

	diff := vwap.Sub(bestPrice).Abs()
	return diff.Div(bestPrice).Mul(decimal.NewFromInt(100)), nil
}

// --- Write Operations ---

// SetBids replaces all bid levels.
func (ob *OrderBook) SetBids(levels []PriceLevel) {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	ob.bids = make([]PriceLevel, len(levels))
	copy(ob.bids, levels)

	// Ensure sorted by price descending
	sort.Slice(ob.bids, func(i, j int) bool {
		return ob.bids[i].Price.GreaterThan(ob.bids[j].Price)
	})
}

// SetAsks replaces all ask levels.
func (ob *OrderBook) SetAsks(levels []PriceLevel) {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	ob.asks = make([]PriceLevel, len(levels))
	copy(ob.asks, levels)

	// Ensure sorted by price ascending
	sort.Slice(ob.asks, func(i, j int) bool {
		return ob.asks[i].Price.LessThan(ob.asks[j].Price)
	})
}

// UpdateLevel updates a single price level on the specified side.
// If size is zero, the level is removed.
func (ob *OrderBook) UpdateLevel(side Side, price, size decimal.Decimal) {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	if side == SideBuy {
		ob.updateBidLevel(price, size)
	} else {
		ob.updateAskLevel(price, size)
	}
}

func (ob *OrderBook) updateBidLevel(price, size decimal.Decimal) {
	// Find existing level
	idx := -1
	for i, level := range ob.bids {
		if level.Price.Equal(price) {
			idx = i
			break
		}
	}

	if size.IsZero() {
		// Remove level
		if idx >= 0 {
			ob.bids = append(ob.bids[:idx], ob.bids[idx+1:]...)
		}
		return
	}

	if idx >= 0 {
		// Update existing
		ob.bids[idx].Size = size
	} else {
		// Insert new level in sorted position (descending by price)
		newLevel := PriceLevel{Price: price, Size: size}
		insertIdx := sort.Search(len(ob.bids), func(i int) bool {
			return ob.bids[i].Price.LessThan(price)
		})
		ob.bids = append(ob.bids, PriceLevel{})
		copy(ob.bids[insertIdx+1:], ob.bids[insertIdx:])
		ob.bids[insertIdx] = newLevel
	}
}

func (ob *OrderBook) updateAskLevel(price, size decimal.Decimal) {
	// Find existing level
	idx := -1
	for i, level := range ob.asks {
		if level.Price.Equal(price) {
			idx = i
			break
		}
	}

	if size.IsZero() {
		// Remove level
		if idx >= 0 {
			ob.asks = append(ob.asks[:idx], ob.asks[idx+1:]...)
		}
		return
	}

	if idx >= 0 {
		// Update existing
		ob.asks[idx].Size = size
	} else {
		// Insert new level in sorted position (ascending by price)
		newLevel := PriceLevel{Price: price, Size: size}
		insertIdx := sort.Search(len(ob.asks), func(i int) bool {
			return ob.asks[i].Price.GreaterThan(price)
		})
		ob.asks = append(ob.asks, PriceLevel{})
		copy(ob.asks[insertIdx+1:], ob.asks[insertIdx:])
		ob.asks[insertIdx] = newLevel
	}
}

// SetTimestamp updates the orderbook timestamp.
func (ob *OrderBook) SetTimestamp(ts int64) {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	ob.Timestamp = ts
}

// Clear removes all levels from the orderbook.
func (ob *OrderBook) Clear() {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	ob.bids = ob.bids[:0]
	ob.asks = ob.asks[:0]
	ob.Timestamp = 0
}

// --- Matching Simulation ---

// MatchResult represents the result of simulating a trade.
type MatchResult struct {
	Side        Side
	TotalSize   decimal.Decimal
	TotalCost   decimal.Decimal
	AvgPrice    decimal.Decimal
	Fills       []Fill
	Unfilled    decimal.Decimal
	PriceImpact decimal.Decimal // as percentage
}

// Fill represents a single fill against a price level.
type Fill struct {
	Price decimal.Decimal
	Size  decimal.Decimal
}

// SimulateMarketOrder simulates executing a market order against the book.
// This does NOT modify the orderbook.
func (ob *OrderBook) SimulateMarketOrder(side Side, size decimal.Decimal) MatchResult {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	var levels []PriceLevel
	if side == SideBuy {
		levels = ob.asks
	} else {
		levels = ob.bids
	}

	result := MatchResult{
		Side:      side,
		TotalSize: decimal.Zero,
		TotalCost: decimal.Zero,
		Fills:     make([]Fill, 0),
	}

	remaining := size
	var firstPrice decimal.Decimal

	for _, level := range levels {
		if remaining.IsZero() {
			break
		}

		if result.TotalSize.IsZero() {
			firstPrice = level.Price
		}

		fillSize := level.Size
		if fillSize.GreaterThan(remaining) {
			fillSize = remaining
		}

		result.Fills = append(result.Fills, Fill{
			Price: level.Price,
			Size:  fillSize,
		})

		result.TotalCost = result.TotalCost.Add(level.Price.Mul(fillSize))
		result.TotalSize = result.TotalSize.Add(fillSize)
		remaining = remaining.Sub(fillSize)
	}

	result.Unfilled = remaining

	if result.TotalSize.GreaterThan(decimal.Zero) {
		result.AvgPrice = result.TotalCost.Div(result.TotalSize)

		// Calculate price impact
		if !firstPrice.IsZero() {
			diff := result.AvgPrice.Sub(firstPrice).Abs()
			result.PriceImpact = diff.Div(firstPrice).Mul(decimal.NewFromInt(100))
		}
	}

	return result
}

// --- String representation ---

func (ob *OrderBook) String() string {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	mid := ob.Midpoint()
	spread := ob.Spread()

	return fmt.Sprintf("OrderBook{asset=%s, bids=%d, asks=%d, mid=%s, spread=%s}",
		ob.AssetID, len(ob.bids), len(ob.asks), mid, spread)
}

// PrettyPrint returns a formatted string representation of the orderbook.
func (ob *OrderBook) PrettyPrint(depth int) string {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	if depth <= 0 {
		depth = 5
	}

	var result string
	result += fmt.Sprintf("=== OrderBook: %s ===\n", ob.AssetID)
	result += fmt.Sprintf("Timestamp: %d\n\n", ob.Timestamp)

	// Print asks in reverse (highest first, so visual representation is correct)
	askDepth := depth
	if len(ob.asks) < askDepth {
		askDepth = len(ob.asks)
	}

	result += "ASKS:\n"
	for i := askDepth - 1; i >= 0; i-- {
		level := ob.asks[i]
		result += fmt.Sprintf("  %s @ %s\n", level.Size, level.Price)
	}

	result += fmt.Sprintf("\n--- Spread: %s ---\n\n", ob.Spread())

	// Print bids (highest first)
	result += "BIDS:\n"
	bidDepth := depth
	if len(ob.bids) < bidDepth {
		bidDepth = len(ob.bids)
	}

	for i := 0; i < bidDepth; i++ {
		level := ob.bids[i]
		result += fmt.Sprintf("  %s @ %s\n", level.Size, level.Price)
	}

	return result
}
