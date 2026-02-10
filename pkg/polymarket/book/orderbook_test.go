package book

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestNewOrderBook(t *testing.T) {
	ob := NewOrderBook("token123", "market456")

	if ob.AssetID != "token123" {
		t.Errorf("Wrong asset ID: %s", ob.AssetID)
	}

	if ob.Market != "market456" {
		t.Errorf("Wrong market: %s", ob.Market)
	}

	if ob.BidDepth() != 0 {
		t.Error("New orderbook should have no bids")
	}

	if ob.AskDepth() != 0 {
		t.Error("New orderbook should have no asks")
	}
}

func TestSetBidsAsks(t *testing.T) {
	ob := NewOrderBook("token123", "market456")

	bids := []PriceLevel{
		{Price: decimal.NewFromFloat(0.49), Size: decimal.NewFromInt(100)},
		{Price: decimal.NewFromFloat(0.50), Size: decimal.NewFromInt(200)}, // best bid
		{Price: decimal.NewFromFloat(0.48), Size: decimal.NewFromInt(150)},
	}

	asks := []PriceLevel{
		{Price: decimal.NewFromFloat(0.52), Size: decimal.NewFromInt(180)},
		{Price: decimal.NewFromFloat(0.51), Size: decimal.NewFromInt(120)}, // best ask
		{Price: decimal.NewFromFloat(0.53), Size: decimal.NewFromInt(250)},
	}

	ob.SetBids(bids)
	ob.SetAsks(asks)

	if ob.BidDepth() != 3 {
		t.Errorf("Expected 3 bids, got %d", ob.BidDepth())
	}

	if ob.AskDepth() != 3 {
		t.Errorf("Expected 3 asks, got %d", ob.AskDepth())
	}

	// Check best bid (should be 0.50 after sorting)
	bestBidPrice, bestBidSize := ob.BestBid()
	if !bestBidPrice.Equal(decimal.NewFromFloat(0.50)) {
		t.Errorf("Wrong best bid price: %s", bestBidPrice)
	}
	if !bestBidSize.Equal(decimal.NewFromInt(200)) {
		t.Errorf("Wrong best bid size: %s", bestBidSize)
	}

	// Check best ask (should be 0.51 after sorting)
	bestAskPrice, bestAskSize := ob.BestAsk()
	if !bestAskPrice.Equal(decimal.NewFromFloat(0.51)) {
		t.Errorf("Wrong best ask price: %s", bestAskPrice)
	}
	if !bestAskSize.Equal(decimal.NewFromInt(120)) {
		t.Errorf("Wrong best ask size: %s", bestAskSize)
	}
}

func TestMidpointAndSpread(t *testing.T) {
	ob := NewOrderBook("token123", "market456")

	ob.SetBids([]PriceLevel{
		{Price: decimal.NewFromFloat(0.50), Size: decimal.NewFromInt(100)},
	})
	ob.SetAsks([]PriceLevel{
		{Price: decimal.NewFromFloat(0.52), Size: decimal.NewFromInt(100)},
	})

	mid := ob.Midpoint()
	expected := decimal.NewFromFloat(0.51)
	if !mid.Equal(expected) {
		t.Errorf("Wrong midpoint: got %s, want %s", mid, expected)
	}

	spread := ob.Spread()
	expectedSpread := decimal.NewFromFloat(0.02)
	if !spread.Equal(expectedSpread) {
		t.Errorf("Wrong spread: got %s, want %s", spread, expectedSpread)
	}

	// Spread bps = (0.02 / 0.51) * 10000 ≈ 392
	spreadBps := ob.SpreadBps()
	if spreadBps.LessThan(decimal.NewFromInt(390)) || spreadBps.GreaterThan(decimal.NewFromInt(395)) {
		t.Errorf("Wrong spread bps: %s", spreadBps)
	}
}

func TestEmptyOrderbook(t *testing.T) {
	ob := NewOrderBook("token123", "market456")

	// Midpoint and spread should be zero for empty book
	if !ob.Midpoint().IsZero() {
		t.Error("Midpoint should be zero for empty book")
	}

	if !ob.Spread().IsZero() {
		t.Error("Spread should be zero for empty book")
	}

	// Best bid/ask should be zero
	price, size := ob.BestBid()
	if !price.IsZero() || !size.IsZero() {
		t.Error("Best bid should be zero for empty book")
	}

	price, size = ob.BestAsk()
	if !price.IsZero() || !size.IsZero() {
		t.Error("Best ask should be zero for empty book")
	}
}

func TestUpdateLevel(t *testing.T) {
	ob := NewOrderBook("token123", "market456")

	// Add a bid
	ob.UpdateLevel(SideBuy, decimal.NewFromFloat(0.50), decimal.NewFromInt(100))
	if ob.BidDepth() != 1 {
		t.Error("Should have 1 bid")
	}

	// Add another bid (better price)
	ob.UpdateLevel(SideBuy, decimal.NewFromFloat(0.51), decimal.NewFromInt(150))
	if ob.BidDepth() != 2 {
		t.Error("Should have 2 bids")
	}

	bestPrice, _ := ob.BestBid()
	if !bestPrice.Equal(decimal.NewFromFloat(0.51)) {
		t.Errorf("Best bid should be 0.51, got %s", bestPrice)
	}

	// Update existing level
	ob.UpdateLevel(SideBuy, decimal.NewFromFloat(0.51), decimal.NewFromInt(200))
	_, bestSize := ob.BestBid()
	if !bestSize.Equal(decimal.NewFromInt(200)) {
		t.Errorf("Best bid size should be 200, got %s", bestSize)
	}

	// Remove level by setting size to 0
	ob.UpdateLevel(SideBuy, decimal.NewFromFloat(0.51), decimal.Zero)
	if ob.BidDepth() != 1 {
		t.Error("Should have 1 bid after removal")
	}

	bestPrice, _ = ob.BestBid()
	if !bestPrice.Equal(decimal.NewFromFloat(0.50)) {
		t.Errorf("Best bid should be 0.50, got %s", bestPrice)
	}
}

func TestUpdateAskLevel(t *testing.T) {
	ob := NewOrderBook("token123", "market456")

	// Add asks
	ob.UpdateLevel(SideSell, decimal.NewFromFloat(0.52), decimal.NewFromInt(100))
	ob.UpdateLevel(SideSell, decimal.NewFromFloat(0.51), decimal.NewFromInt(150))
	ob.UpdateLevel(SideSell, decimal.NewFromFloat(0.53), decimal.NewFromInt(200))

	if ob.AskDepth() != 3 {
		t.Errorf("Should have 3 asks, got %d", ob.AskDepth())
	}

	// Best ask should be 0.51 (lowest)
	bestPrice, _ := ob.BestAsk()
	if !bestPrice.Equal(decimal.NewFromFloat(0.51)) {
		t.Errorf("Best ask should be 0.51, got %s", bestPrice)
	}

	// Check ordering (ascending)
	asks := ob.Asks()
	if !asks[0].Price.Equal(decimal.NewFromFloat(0.51)) {
		t.Error("First ask should be 0.51")
	}
	if !asks[1].Price.Equal(decimal.NewFromFloat(0.52)) {
		t.Error("Second ask should be 0.52")
	}
	if !asks[2].Price.Equal(decimal.NewFromFloat(0.53)) {
		t.Error("Third ask should be 0.53")
	}
}

func TestTotalSize(t *testing.T) {
	ob := NewOrderBook("token123", "market456")

	ob.SetBids([]PriceLevel{
		{Price: decimal.NewFromFloat(0.50), Size: decimal.NewFromInt(100)},
		{Price: decimal.NewFromFloat(0.49), Size: decimal.NewFromInt(200)},
	})

	ob.SetAsks([]PriceLevel{
		{Price: decimal.NewFromFloat(0.51), Size: decimal.NewFromInt(150)},
		{Price: decimal.NewFromFloat(0.52), Size: decimal.NewFromInt(250)},
	})

	totalBids := ob.TotalBidSize()
	if !totalBids.Equal(decimal.NewFromInt(300)) {
		t.Errorf("Wrong total bid size: %s", totalBids)
	}

	totalAsks := ob.TotalAskSize()
	if !totalAsks.Equal(decimal.NewFromInt(400)) {
		t.Errorf("Wrong total ask size: %s", totalAsks)
	}
}

func TestVolumeWeightedPrice(t *testing.T) {
	ob := NewOrderBook("token123", "market456")

	ob.SetAsks([]PriceLevel{
		{Price: decimal.NewFromFloat(0.51), Size: decimal.NewFromInt(100)},
		{Price: decimal.NewFromFloat(0.52), Size: decimal.NewFromInt(100)},
		{Price: decimal.NewFromFloat(0.53), Size: decimal.NewFromInt(100)},
	})

	// VWAP for buying 150 units:
	// 100 @ 0.51 + 50 @ 0.52 = (51 + 26) / 150 = 0.5133...
	vwap, err := ob.VolumeWeightedPrice(SideBuy, decimal.NewFromInt(150))
	if err != nil {
		t.Fatalf("VolumeWeightedPrice failed: %v", err)
	}

	expected := decimal.NewFromFloat(0.5133)
	if vwap.Sub(expected).Abs().GreaterThan(decimal.NewFromFloat(0.001)) {
		t.Errorf("Wrong VWAP: got %s, want ~%s", vwap, expected)
	}

	// VWAP for buying all 300 units
	vwap, err = ob.VolumeWeightedPrice(SideBuy, decimal.NewFromInt(300))
	if err != nil {
		t.Fatalf("VolumeWeightedPrice failed: %v", err)
	}

	// (100*0.51 + 100*0.52 + 100*0.53) / 300 = 156/300 = 0.52
	expected = decimal.NewFromFloat(0.52)
	if !vwap.Equal(expected) {
		t.Errorf("Wrong VWAP: got %s, want %s", vwap, expected)
	}

	// VWAP for buying more than available should fail
	_, err = ob.VolumeWeightedPrice(SideBuy, decimal.NewFromInt(500))
	if err == nil {
		t.Error("Expected error for insufficient liquidity")
	}
}

func TestSimulateMarketOrder(t *testing.T) {
	ob := NewOrderBook("token123", "market456")

	ob.SetAsks([]PriceLevel{
		{Price: decimal.NewFromFloat(0.51), Size: decimal.NewFromInt(100)},
		{Price: decimal.NewFromFloat(0.52), Size: decimal.NewFromInt(100)},
		{Price: decimal.NewFromFloat(0.53), Size: decimal.NewFromInt(100)},
	})

	// Simulate buying 150 units
	result := ob.SimulateMarketOrder(SideBuy, decimal.NewFromInt(150))

	if result.Side != SideBuy {
		t.Error("Wrong side")
	}

	if !result.TotalSize.Equal(decimal.NewFromInt(150)) {
		t.Errorf("Wrong total size: %s", result.TotalSize)
	}

	if len(result.Fills) != 2 {
		t.Errorf("Expected 2 fills, got %d", len(result.Fills))
	}

	// First fill: 100 @ 0.51
	if !result.Fills[0].Size.Equal(decimal.NewFromInt(100)) {
		t.Errorf("Wrong first fill size: %s", result.Fills[0].Size)
	}
	if !result.Fills[0].Price.Equal(decimal.NewFromFloat(0.51)) {
		t.Errorf("Wrong first fill price: %s", result.Fills[0].Price)
	}

	// Second fill: 50 @ 0.52
	if !result.Fills[1].Size.Equal(decimal.NewFromInt(50)) {
		t.Errorf("Wrong second fill size: %s", result.Fills[1].Size)
	}

	// No unfilled
	if !result.Unfilled.IsZero() {
		t.Errorf("Should have no unfilled: %s", result.Unfilled)
	}

	// Price impact should be positive
	if result.PriceImpact.LessThanOrEqual(decimal.Zero) {
		t.Error("Price impact should be positive")
	}
}

func TestSimulateMarketOrderPartialFill(t *testing.T) {
	ob := NewOrderBook("token123", "market456")

	ob.SetAsks([]PriceLevel{
		{Price: decimal.NewFromFloat(0.51), Size: decimal.NewFromInt(100)},
	})

	// Try to buy 200 but only 100 available
	result := ob.SimulateMarketOrder(SideBuy, decimal.NewFromInt(200))

	if !result.TotalSize.Equal(decimal.NewFromInt(100)) {
		t.Errorf("Wrong total size: %s", result.TotalSize)
	}

	if !result.Unfilled.Equal(decimal.NewFromInt(100)) {
		t.Errorf("Wrong unfilled: %s", result.Unfilled)
	}
}

func TestSnapshot(t *testing.T) {
	ob := NewOrderBook("token123", "market456")
	ob.SetTimestamp(1234567890)

	ob.SetBids([]PriceLevel{
		{Price: decimal.NewFromFloat(0.50), Size: decimal.NewFromInt(100)},
	})
	ob.SetAsks([]PriceLevel{
		{Price: decimal.NewFromFloat(0.51), Size: decimal.NewFromInt(150)},
	})

	snap := ob.GetSnapshot()

	if snap.AssetID != "token123" {
		t.Error("Wrong asset ID in snapshot")
	}

	if snap.Timestamp != 1234567890 {
		t.Error("Wrong timestamp in snapshot")
	}

	if len(snap.Bids) != 1 {
		t.Error("Wrong number of bids in snapshot")
	}

	if len(snap.Asks) != 1 {
		t.Error("Wrong number of asks in snapshot")
	}

	// Modify original - snapshot should be unchanged
	ob.Clear()
	if len(snap.Bids) != 1 {
		t.Error("Snapshot should be independent of original")
	}
}

func TestClear(t *testing.T) {
	ob := NewOrderBook("token123", "market456")

	ob.SetBids([]PriceLevel{
		{Price: decimal.NewFromFloat(0.50), Size: decimal.NewFromInt(100)},
	})
	ob.SetAsks([]PriceLevel{
		{Price: decimal.NewFromFloat(0.51), Size: decimal.NewFromInt(150)},
	})
	ob.SetTimestamp(123)

	ob.Clear()

	if ob.BidDepth() != 0 {
		t.Error("Bids should be cleared")
	}

	if ob.AskDepth() != 0 {
		t.Error("Asks should be cleared")
	}

	if ob.Timestamp != 0 {
		t.Error("Timestamp should be cleared")
	}
}

func TestConcurrentAccess(t *testing.T) {
	ob := NewOrderBook("token123", "market456")

	done := make(chan bool)

	// Writer goroutine
	go func() {
		for i := 0; i < 1000; i++ {
			price := decimal.NewFromFloat(0.50 + float64(i%10)*0.01)
			ob.UpdateLevel(SideBuy, price, decimal.NewFromInt(int64(i)))
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		for i := 0; i < 1000; i++ {
			_, _ = ob.BestBid()
			_ = ob.Midpoint()
			_ = ob.GetSnapshot()
		}
		done <- true
	}()

	<-done
	<-done
}

func TestSideString(t *testing.T) {
	if SideBuy.String() != "BUY" {
		t.Error("Wrong string for SideBuy")
	}

	if SideSell.String() != "SELL" {
		t.Error("Wrong string for SideSell")
	}
}
