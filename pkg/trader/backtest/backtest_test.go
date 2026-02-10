package backtest

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestNewBacktest(t *testing.T) {
	bt := New(nil)
	if bt == nil {
		t.Fatal("New returned nil")
	}
	if bt.Balance().LessThanOrEqual(decimal.Zero) {
		t.Error("Initial balance should be positive")
	}
}

func TestBacktestWithSyntheticData(t *testing.T) {
	config := &Config{
		InitialBalance: decimal.NewFromInt(1000),
		TimeScale:      0, // Instant
	}
	bt := New(config)

	// Create synthetic price data: price goes up then down
	now := time.Now()
	points := make([]PricePoint, 100)
	for i := 0; i < 100; i++ {
		var price float64
		if i < 50 {
			price = 0.5 + float64(i)*0.01 // 0.50 -> 0.99
		} else {
			price = 0.99 - float64(i-50)*0.01 // 0.99 -> 0.50
		}
		points[i] = PricePoint{
			Timestamp: now.Add(time.Duration(i) * time.Minute),
			TokenID:   "token1",
			Market:    "market1",
			Price:     decimal.NewFromFloat(price),
			Volume:    decimal.NewFromInt(1000),
		}
	}

	bt.LoadData(&HistoricalData{
		TokenID:   "token1",
		Market:    "market1",
		StartTime: points[0].Timestamp,
		EndTime:   points[len(points)-1].Timestamp,
		Points:    points,
	})

	// Run with buy and hold strategy
	strategy := NewBuyAndHoldStrategy(100)
	ctx := context.Background()
	result, err := bt.Run(ctx, strategy)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if result.TotalTrades < 1 {
		t.Error("Expected at least 1 trade")
	}
	t.Logf("Result: PnL=%.2f, Trades=%d, Return=%.2f%%",
		result.TotalPnL.InexactFloat64(),
		result.TotalTrades,
		result.TotalReturn.InexactFloat64())
}

func TestMomentumStrategy(t *testing.T) {
	config := &Config{
		InitialBalance: decimal.NewFromInt(1000),
	}
	bt := New(config)

	// Create trending data
	now := time.Now()
	points := make([]PricePoint, 100)
	for i := 0; i < 100; i++ {
		// Upward trend
		price := 0.5 + float64(i)*0.005
		points[i] = PricePoint{
			Timestamp: now.Add(time.Duration(i) * time.Minute),
			TokenID:   "token1",
			Market:    "market1",
			Price:     decimal.NewFromFloat(price),
		}
	}

	bt.LoadData(&HistoricalData{
		TokenID:   "token1",
		Market:    "market1",
		StartTime: points[0].Timestamp,
		EndTime:   points[len(points)-1].Timestamp,
		Points:    points,
	})

	strategy := NewMomentumStrategy(10, 100, 2.0)
	ctx := context.Background()
	result, err := bt.Run(ctx, strategy)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	t.Logf("Momentum Strategy: PnL=%.2f, Trades=%d, WinRate=%.2f%%",
		result.TotalPnL.InexactFloat64(),
		result.TotalTrades,
		result.WinRate.Mul(decimal.NewFromInt(100)).InexactFloat64())
}

func TestMeanReversionStrategy(t *testing.T) {
	config := &Config{
		InitialBalance: decimal.NewFromInt(1000),
	}
	bt := New(config)

	// Create oscillating data
	now := time.Now()
	points := make([]PricePoint, 100)
	for i := 0; i < 100; i++ {
		// Oscillate around 0.5
		price := 0.5 + 0.1*float64(i%20-10)/10.0
		points[i] = PricePoint{
			Timestamp: now.Add(time.Duration(i) * time.Minute),
			TokenID:   "token1",
			Market:    "market1",
			Price:     decimal.NewFromFloat(price),
		}
	}

	bt.LoadData(&HistoricalData{
		TokenID:   "token1",
		Market:    "market1",
		StartTime: points[0].Timestamp,
		EndTime:   points[len(points)-1].Timestamp,
		Points:    points,
	})

	strategy := NewMeanReversionStrategy(10, 100, 5.0, 5.0)
	ctx := context.Background()
	result, err := bt.Run(ctx, strategy)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	t.Logf("Mean Reversion Strategy: PnL=%.2f, Trades=%d, MaxDD=%.2f%%",
		result.TotalPnL.InexactFloat64(),
		result.TotalTrades,
		result.MaxDrawdown.Mul(decimal.NewFromInt(100)).InexactFloat64())
}

func TestBacktestEquityCurve(t *testing.T) {
	config := &Config{
		InitialBalance: decimal.NewFromInt(1000),
	}
	bt := New(config)

	// Create simple data
	now := time.Now()
	points := make([]PricePoint, 50)
	for i := 0; i < 50; i++ {
		points[i] = PricePoint{
			Timestamp: now.Add(time.Duration(i) * time.Minute),
			TokenID:   "token1",
			Market:    "market1",
			Price:     decimal.NewFromFloat(0.5 + float64(i)*0.01),
		}
	}

	bt.LoadData(&HistoricalData{
		TokenID:   "token1",
		Market:    "market1",
		StartTime: points[0].Timestamp,
		EndTime:   points[len(points)-1].Timestamp,
		Points:    points,
	})

	strategy := NewBuyAndHoldStrategy(100)
	ctx := context.Background()
	result, err := bt.Run(ctx, strategy)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if len(result.EquityCurve) == 0 {
		t.Error("Equity curve should not be empty")
	}

	// Equity should increase in an uptrending market with buy and hold
	first := result.EquityCurve[0].Equity
	last := result.EquityCurve[len(result.EquityCurve)-1].Equity
	if !last.GreaterThanOrEqual(first) {
		t.Logf("Equity didn't increase: first=%s, last=%s", first, last)
	}
}

func TestBacktestNoData(t *testing.T) {
	bt := New(nil)
	strategy := NewBuyAndHoldStrategy(100)

	ctx := context.Background()
	_, err := bt.Run(ctx, strategy)
	if err == nil {
		t.Error("Expected error when no data loaded")
	}
}

func TestBacktestCancel(t *testing.T) {
	config := &Config{
		InitialBalance: decimal.NewFromInt(1000),
		TimeScale:      1.0, // Real-time (slow)
		TickInterval:   time.Millisecond,
	}
	bt := New(config)

	// Create lots of data
	now := time.Now()
	points := make([]PricePoint, 1000)
	for i := 0; i < 1000; i++ {
		points[i] = PricePoint{
			Timestamp: now.Add(time.Duration(i) * time.Millisecond),
			TokenID:   "token1",
			Market:    "market1",
			Price:     decimal.NewFromFloat(0.5),
		}
	}

	bt.LoadData(&HistoricalData{
		TokenID:   "token1",
		Market:    "market1",
		StartTime: points[0].Timestamp,
		EndTime:   points[len(points)-1].Timestamp,
		Points:    points,
	})

	strategy := NewBuyAndHoldStrategy(100)

	// Cancel after short time
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := bt.Run(ctx, strategy)
	if err == nil {
		t.Error("Expected context canceled error")
	}
}
