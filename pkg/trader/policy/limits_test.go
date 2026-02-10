package policy

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestDefaultRiskLimits(t *testing.T) {
	limits := DefaultRiskLimits()

	if limits.MaxPositionSize.LessThanOrEqual(decimal.Zero) {
		t.Error("MaxPositionSize should be positive")
	}
	if limits.MaxTotalExposure.LessThanOrEqual(decimal.Zero) {
		t.Error("MaxTotalExposure should be positive")
	}
	if limits.MaxConcentration.LessThanOrEqual(decimal.Zero) || limits.MaxConcentration.GreaterThan(decimal.NewFromInt(1)) {
		t.Error("MaxConcentration should be between 0 and 1")
	}
}

func TestTightRiskLimits(t *testing.T) {
	tight := TightRiskLimits()
	defaults := DefaultRiskLimits()

	if tight.MaxPositionSize.GreaterThanOrEqual(defaults.MaxPositionSize) {
		t.Error("Tight limits should have smaller position size than defaults")
	}
	if tight.MaxDailyLoss.GreaterThanOrEqual(defaults.MaxDailyLoss) {
		t.Error("Tight limits should have smaller daily loss than defaults")
	}
}

func TestNewPolicyEngine(t *testing.T) {
	// Test with nil limits (should use defaults)
	engine := NewPolicyEngine(nil)
	if engine == nil {
		t.Fatal("NewPolicyEngine returned nil")
	}

	// Test with custom limits
	custom := &RiskLimits{
		MaxPositionSize:    decimal.NewFromInt(100),
		MaxTotalExposure:   decimal.NewFromInt(500),
		MaxOrderSize:       decimal.NewFromInt(50),
		MinOrderSize:       decimal.NewFromInt(5),
		MaxOpenOrders:      5,
		MaxDailyOrders:     10,
		MaxDailyVolume:     decimal.NewFromInt(1000),
		MaxDailyLoss:       decimal.NewFromInt(50),
		MaxSlippage:        decimal.NewFromFloat(0.01),
		MaxSessionDuration: 24 * time.Hour,
	}
	engine = NewPolicyEngine(custom)
	if engine == nil {
		t.Fatal("NewPolicyEngine with custom limits returned nil")
	}
}

// Helper to create a policy engine with permissive settings for basic tests
func newPermissiveEngine() *PolicyEngine {
	return NewPolicyEngine(&RiskLimits{
		MaxPositionSize:    decimal.NewFromInt(10000),
		MaxTotalExposure:   decimal.NewFromInt(50000),
		MaxConcentration:   decimal.NewFromInt(1), // 100% allowed
		MaxOrderSize:       decimal.NewFromInt(5000),
		MinOrderSize:       decimal.NewFromInt(1),
		MaxOpenOrders:      100,
		MaxDailyOrders:     1000,
		MaxDailyVolume:     decimal.NewFromInt(100000),
		MaxDailyLoss:       decimal.NewFromInt(5000),
		MaxSlippage:        decimal.NewFromFloat(0.10),
		MaxSessionDuration: 24 * time.Hour,
	})
}

func TestCheckOrder_ValidOrder(t *testing.T) {
	engine := newPermissiveEngine()

	err := engine.CheckOrder("market1", decimal.NewFromInt(10), decimal.NewFromFloat(0.5), true)
	if err != nil {
		t.Errorf("Valid order should pass: %v", err)
	}
}

func TestCheckOrder_OrderSizeTooLarge(t *testing.T) {
	limits := &RiskLimits{
		MaxPositionSize:    decimal.NewFromInt(10000),
		MaxTotalExposure:   decimal.NewFromInt(50000),
		MaxConcentration:   decimal.NewFromInt(1),
		MaxOrderSize:       decimal.NewFromInt(50), // $50 max order
		MinOrderSize:       decimal.NewFromInt(1),
		MaxOpenOrders:      100,
		MaxDailyOrders:     1000,
		MaxDailyVolume:     decimal.NewFromInt(100000),
		MaxDailyLoss:       decimal.NewFromInt(5000),
		MaxSessionDuration: 24 * time.Hour,
	}
	engine := NewPolicyEngine(limits)

	// Order value of $100 exceeds MaxOrderSize of $50
	size := decimal.NewFromInt(200)
	price := decimal.NewFromFloat(0.5)
	err := engine.CheckOrder("market1", size, price, true)
	if err == nil {
		t.Error("Order exceeding MaxOrderSize should be rejected")
	}
}

func TestCheckOrder_OrderSizeTooSmall(t *testing.T) {
	limits := &RiskLimits{
		MaxPositionSize:    decimal.NewFromInt(10000),
		MaxTotalExposure:   decimal.NewFromInt(50000),
		MaxConcentration:   decimal.NewFromInt(1),
		MaxOrderSize:       decimal.NewFromInt(5000),
		MinOrderSize:       decimal.NewFromInt(10), // $10 min order
		MaxOpenOrders:      100,
		MaxDailyOrders:     1000,
		MaxDailyVolume:     decimal.NewFromInt(100000),
		MaxDailyLoss:       decimal.NewFromInt(5000),
		MaxSessionDuration: 24 * time.Hour,
	}
	engine := NewPolicyEngine(limits)

	// Order value of $1 below MinOrderSize of $10
	size := decimal.NewFromInt(2)
	price := decimal.NewFromFloat(0.5)
	err := engine.CheckOrder("market1", size, price, true)
	if err == nil {
		t.Error("Order below MinOrderSize should be rejected")
	}
}

func TestCheckOrder_TooManyOpenOrders(t *testing.T) {
	limits := &RiskLimits{
		MaxPositionSize:    decimal.NewFromInt(10000),
		MaxTotalExposure:   decimal.NewFromInt(50000),
		MaxConcentration:   decimal.NewFromInt(1),
		MaxOrderSize:       decimal.NewFromInt(5000),
		MinOrderSize:       decimal.NewFromInt(1),
		MaxOpenOrders:      2, // Only allow 2 open orders
		MaxDailyOrders:     100,
		MaxDailyVolume:     decimal.NewFromInt(100000),
		MaxDailyLoss:       decimal.NewFromInt(5000),
		MaxSessionDuration: 24 * time.Hour,
	}
	engine := NewPolicyEngine(limits)

	// Record 2 orders
	engine.RecordOrder("market1")
	engine.RecordOrder("market2")

	// Third order should fail
	err := engine.CheckOrder("market3", decimal.NewFromInt(10), decimal.NewFromFloat(0.5), true)
	if err == nil {
		t.Error("Should reject order when max open orders reached")
	}
}

func TestCheckOrder_DailyOrderLimit(t *testing.T) {
	limits := &RiskLimits{
		MaxPositionSize:    decimal.NewFromInt(10000),
		MaxTotalExposure:   decimal.NewFromInt(50000),
		MaxConcentration:   decimal.NewFromInt(1),
		MaxOrderSize:       decimal.NewFromInt(5000),
		MinOrderSize:       decimal.NewFromInt(1),
		MaxOpenOrders:      100,
		MaxDailyOrders:     3, // Only 3 orders per day
		MaxDailyVolume:     decimal.NewFromInt(100000),
		MaxDailyLoss:       decimal.NewFromInt(5000),
		MaxSessionDuration: 24 * time.Hour,
	}
	engine := NewPolicyEngine(limits)

	// Place 3 orders
	for i := 0; i < 3; i++ {
		engine.RecordOrder("market1")
		engine.RecordOrderCanceled() // Cancel so we don't hit open order limit
	}

	// Fourth order should fail
	err := engine.CheckOrder("market1", decimal.NewFromInt(10), decimal.NewFromFloat(0.5), true)
	if err == nil {
		t.Error("Should reject order when daily order limit reached")
	}
}

func TestCheckOrder_DailyVolumeLimit(t *testing.T) {
	limits := &RiskLimits{
		MaxPositionSize:    decimal.NewFromInt(10000),
		MaxTotalExposure:   decimal.NewFromInt(50000),
		MaxConcentration:   decimal.NewFromInt(1),
		MaxOrderSize:       decimal.NewFromInt(5000),
		MinOrderSize:       decimal.NewFromInt(1),
		MaxOpenOrders:      100,
		MaxDailyOrders:     100,
		MaxDailyVolume:     decimal.NewFromInt(100), // $100 daily volume
		MaxDailyLoss:       decimal.NewFromInt(5000),
		MaxSessionDuration: 24 * time.Hour,
	}
	engine := NewPolicyEngine(limits)

	// First order of $90 should pass
	err := engine.CheckOrder("market1", decimal.NewFromInt(90), decimal.NewFromInt(1), true)
	if err != nil {
		t.Errorf("First order should pass: %v", err)
	}

	// Record a fill of $90
	engine.RecordFill("market1", decimal.NewFromInt(90), decimal.NewFromInt(1), true, decimal.Zero)

	// Second order of $20 should fail (would exceed $100 daily volume)
	err = engine.CheckOrder("market1", decimal.NewFromInt(20), decimal.NewFromInt(1), true)
	if err == nil {
		t.Error("Should reject order when it would exceed daily volume limit")
	}
}

func TestCheckOrder_PositionLimit(t *testing.T) {
	limits := &RiskLimits{
		MaxPositionSize:    decimal.NewFromInt(100), // $100 max per market
		MaxTotalExposure:   decimal.NewFromInt(50000),
		MaxConcentration:   decimal.NewFromInt(1),
		MaxOrderSize:       decimal.NewFromInt(5000),
		MinOrderSize:       decimal.NewFromInt(1),
		MaxOpenOrders:      100,
		MaxDailyOrders:     100,
		MaxDailyVolume:     decimal.NewFromInt(100000),
		MaxDailyLoss:       decimal.NewFromInt(5000),
		MaxSessionDuration: 24 * time.Hour,
	}
	engine := NewPolicyEngine(limits)

	// First order of $80 should pass
	err := engine.CheckOrder("market1", decimal.NewFromInt(80), decimal.NewFromInt(1), true)
	if err != nil {
		t.Errorf("First order should pass: %v", err)
	}
	engine.RecordFill("market1", decimal.NewFromInt(80), decimal.NewFromInt(1), true, decimal.Zero)

	// Second order of $30 would exceed position limit
	err = engine.CheckOrder("market1", decimal.NewFromInt(30), decimal.NewFromInt(1), true)
	if err == nil {
		t.Error("Should reject order when position would exceed limit")
	}
}

func TestCheckOrder_TotalExposureLimit(t *testing.T) {
	limits := &RiskLimits{
		MaxPositionSize:    decimal.NewFromInt(10000),
		MaxTotalExposure:   decimal.NewFromInt(100), // $100 max total exposure
		MaxConcentration:   decimal.NewFromInt(1),
		MaxOrderSize:       decimal.NewFromInt(5000),
		MinOrderSize:       decimal.NewFromInt(1),
		MaxOpenOrders:      100,
		MaxDailyOrders:     100,
		MaxDailyVolume:     decimal.NewFromInt(100000),
		MaxDailyLoss:       decimal.NewFromInt(5000),
		MaxSessionDuration: 24 * time.Hour,
	}
	engine := NewPolicyEngine(limits)

	// First order of $80 should pass
	err := engine.CheckOrder("market1", decimal.NewFromInt(80), decimal.NewFromInt(1), true)
	if err != nil {
		t.Errorf("First order should pass: %v", err)
	}
	engine.RecordFill("market1", decimal.NewFromInt(80), decimal.NewFromInt(1), true, decimal.Zero)

	// Second order of $30 in a different market would exceed total exposure
	err = engine.CheckOrder("market2", decimal.NewFromInt(30), decimal.NewFromInt(1), true)
	if err == nil {
		t.Error("Should reject order when it would exceed total exposure limit")
	}
}

func TestCheckOrder_BlockedMarket(t *testing.T) {
	engine := newPermissiveEngine()
	engine.limits.BlockedMarkets = []string{"blocked-market"}

	err := engine.CheckOrder("blocked-market", decimal.NewFromInt(10), decimal.NewFromFloat(0.5), true)
	if err == nil {
		t.Error("Should reject order for blocked market")
	}
}

func TestCheckOrder_AllowedMarketsOnly(t *testing.T) {
	engine := newPermissiveEngine()
	engine.limits.AllowedMarkets = []string{"allowed-market-1", "allowed-market-2"}

	// Order on allowed market should pass
	err := engine.CheckOrder("allowed-market-1", decimal.NewFromInt(10), decimal.NewFromFloat(0.5), true)
	if err != nil {
		t.Errorf("Order on allowed market should pass: %v", err)
	}

	// Order on non-allowed market should fail
	err = engine.CheckOrder("other-market", decimal.NewFromInt(10), decimal.NewFromFloat(0.5), true)
	if err == nil {
		t.Error("Should reject order for non-allowed market")
	}
}

func TestCheckOrder_ConcentrationLimit(t *testing.T) {
	limits := &RiskLimits{
		MaxPositionSize:    decimal.NewFromInt(10000),
		MaxTotalExposure:   decimal.NewFromInt(50000),
		MaxConcentration:   decimal.NewFromFloat(0.5), // 50% max in one market
		MaxOrderSize:       decimal.NewFromInt(5000),
		MinOrderSize:       decimal.NewFromInt(1),
		MaxOpenOrders:      100,
		MaxDailyOrders:     100,
		MaxDailyVolume:     decimal.NewFromInt(100000),
		MaxDailyLoss:       decimal.NewFromInt(5000),
		MaxSessionDuration: 24 * time.Hour,
	}
	engine := NewPolicyEngine(limits)

	// Create some existing exposure in market2
	engine.RecordFill("market2", decimal.NewFromInt(100), decimal.NewFromInt(1), true, decimal.Zero)

	// First small order in market1 should pass (50/150 = 33%)
	err := engine.CheckOrder("market1", decimal.NewFromInt(50), decimal.NewFromInt(1), true)
	if err != nil {
		t.Errorf("First order should pass: %v", err)
	}
	engine.RecordFill("market1", decimal.NewFromInt(50), decimal.NewFromInt(1), true, decimal.Zero)

	// Large order in market1 would exceed concentration (200/300 = 67% > 50%)
	err = engine.CheckOrder("market1", decimal.NewFromInt(150), decimal.NewFromInt(1), true)
	if err == nil {
		t.Error("Should reject order when it would exceed concentration limit")
	}
}

func TestCheckOrder_CooldownAfterLoss(t *testing.T) {
	limits := &RiskLimits{
		MaxPositionSize:    decimal.NewFromInt(10000),
		MaxTotalExposure:   decimal.NewFromInt(50000),
		MaxConcentration:   decimal.NewFromInt(1),
		MaxOrderSize:       decimal.NewFromInt(5000),
		MinOrderSize:       decimal.NewFromInt(1),
		MaxOpenOrders:      100,
		MaxDailyOrders:     100,
		MaxDailyVolume:     decimal.NewFromInt(100000),
		MaxDailyLoss:       decimal.NewFromInt(5000),
		CooldownAfterLoss:  1 * time.Hour, // 1 hour cooldown
		MaxSessionDuration: 24 * time.Hour,
	}
	engine := NewPolicyEngine(limits)

	// Record a loss
	engine.RecordFill("market1", decimal.NewFromInt(100), decimal.NewFromInt(1), false, decimal.NewFromInt(-50))

	// Should be in cooldown
	err := engine.CheckOrder("market2", decimal.NewFromInt(10), decimal.NewFromFloat(0.5), true)
	if err == nil {
		t.Error("Should reject order during cooldown period")
	}
}

func TestCheckOrder_DailyLossExceeded(t *testing.T) {
	limits := &RiskLimits{
		MaxPositionSize:    decimal.NewFromInt(10000),
		MaxTotalExposure:   decimal.NewFromInt(50000),
		MaxConcentration:   decimal.NewFromInt(1),
		MaxOrderSize:       decimal.NewFromInt(5000),
		MinOrderSize:       decimal.NewFromInt(1),
		MaxOpenOrders:      100,
		MaxDailyOrders:     100,
		MaxDailyVolume:     decimal.NewFromInt(100000),
		MaxDailyLoss:       decimal.NewFromInt(50), // $50 max daily loss
		MaxSessionDuration: 24 * time.Hour,
	}
	engine := NewPolicyEngine(limits)

	// Record a loss that exceeds limit
	engine.RecordFill("market1", decimal.NewFromInt(100), decimal.NewFromInt(1), false, decimal.NewFromInt(-60))

	// Should reject due to daily loss limit exceeded
	err := engine.CheckOrder("market2", decimal.NewFromInt(10), decimal.NewFromFloat(0.5), true)
	if err == nil {
		t.Error("Should reject order when daily loss limit exceeded")
	}
}

func TestCheckSlippage(t *testing.T) {
	limits := DefaultRiskLimits() // 2% max slippage
	engine := NewPolicyEngine(limits)

	// Acceptable slippage
	err := engine.CheckSlippage(decimal.NewFromFloat(0.5), decimal.NewFromFloat(0.505))
	if err != nil {
		t.Errorf("1%% slippage should be acceptable: %v", err)
	}

	// Excessive slippage
	err = engine.CheckSlippage(decimal.NewFromFloat(0.5), decimal.NewFromFloat(0.55))
	if err == nil {
		t.Error("10% slippage should be rejected")
	}

	// Zero expected price
	err = engine.CheckSlippage(decimal.Zero, decimal.NewFromFloat(0.5))
	if err != nil {
		t.Errorf("Zero expected price should be acceptable: %v", err)
	}
}

func TestRecordOrder(t *testing.T) {
	engine := newPermissiveEngine()

	engine.RecordOrder("market1")
	engine.RecordOrder("market2")

	loss, volume, orders := engine.GetDailyStats()
	_ = loss
	_ = volume
	if orders != 2 {
		t.Errorf("Expected 2 daily orders, got %d", orders)
	}
}

func TestRecordOrderCanceled(t *testing.T) {
	engine := newPermissiveEngine()

	engine.RecordOrder("market1")
	engine.RecordOrder("market2")
	engine.RecordOrderCanceled()

	// Open orders should decrease, but daily orders stay the same
	loss, volume, orders := engine.GetDailyStats()
	_ = loss
	_ = volume
	if orders != 2 {
		t.Errorf("Daily orders should still be 2, got %d", orders)
	}
}

func TestRecordFill(t *testing.T) {
	engine := newPermissiveEngine()

	engine.RecordOrder("market1")
	engine.RecordFill("market1", decimal.NewFromInt(100), decimal.NewFromFloat(0.5), true, decimal.NewFromInt(-10))

	pos := engine.GetPosition("market1")
	if !pos.Equal(decimal.NewFromInt(100)) {
		t.Errorf("Expected position of 100, got %s", pos)
	}

	loss, volume, _ := engine.GetDailyStats()
	if !loss.Equal(decimal.NewFromInt(10)) {
		t.Errorf("Expected daily loss of 10, got %s", loss)
	}
	if !volume.Equal(decimal.NewFromInt(50)) {
		t.Errorf("Expected daily volume of 50, got %s", volume)
	}
}

func TestGetTotalExposure(t *testing.T) {
	engine := newPermissiveEngine()

	engine.RecordFill("market1", decimal.NewFromInt(100), decimal.NewFromFloat(0.5), true, decimal.Zero)
	engine.RecordFill("market2", decimal.NewFromInt(50), decimal.NewFromFloat(0.6), true, decimal.Zero)

	exposure := engine.GetTotalExposure()
	expected := decimal.NewFromInt(150)
	if !exposure.Equal(expected) {
		t.Errorf("Expected total exposure of 150, got %s", exposure)
	}
}

func TestResetSession(t *testing.T) {
	limits := &RiskLimits{
		MaxPositionSize:    decimal.NewFromInt(10000),
		MaxTotalExposure:   decimal.NewFromInt(50000),
		MaxConcentration:   decimal.NewFromInt(1),
		MaxOrderSize:       decimal.NewFromInt(5000),
		MinOrderSize:       decimal.NewFromInt(1),
		MaxOpenOrders:      100,
		MaxDailyOrders:     100,
		MaxDailyVolume:     decimal.NewFromInt(100000),
		MaxDailyLoss:       decimal.NewFromInt(5000),
		MaxSessionDuration: 1 * time.Millisecond, // Very short session
	}
	engine := NewPolicyEngine(limits)

	// Wait for session to expire
	time.Sleep(5 * time.Millisecond)

	// Should fail due to session timeout
	err := engine.CheckOrder("market1", decimal.NewFromInt(10), decimal.NewFromFloat(0.5), true)
	if err == nil {
		t.Error("Should reject order when session expired")
	}

	// Reset session
	engine.ResetSession()

	// Should pass now
	err = engine.CheckOrder("market1", decimal.NewFromInt(10), decimal.NewFromFloat(0.5), true)
	if err != nil {
		t.Errorf("After reset, order should pass: %v", err)
	}
}

func TestStatus(t *testing.T) {
	engine := newPermissiveEngine()

	engine.RecordOrder("market1")
	engine.RecordFill("market1", decimal.NewFromInt(100), decimal.NewFromFloat(0.5), true, decimal.NewFromInt(-5))

	status := engine.Status()

	if status.OpenOrders != 0 { // Order was filled
		t.Errorf("Expected 0 open orders after fill, got %d", status.OpenOrders)
	}
	if status.DailyOrders != 1 {
		t.Errorf("Expected 1 daily order, got %d", status.DailyOrders)
	}
	if status.DailyLoss != "5" {
		t.Errorf("Expected daily loss of 5, got %s", status.DailyLoss)
	}
}

func TestSellPositionUpdates(t *testing.T) {
	engine := newPermissiveEngine()

	// Buy position
	engine.RecordFill("market1", decimal.NewFromInt(100), decimal.NewFromFloat(0.5), true, decimal.Zero)
	pos := engine.GetPosition("market1")
	if !pos.Equal(decimal.NewFromInt(100)) {
		t.Errorf("Expected position of 100 after buy, got %s", pos)
	}

	// Sell part of position
	engine.RecordFill("market1", decimal.NewFromInt(40), decimal.NewFromFloat(0.5), false, decimal.Zero)
	pos = engine.GetPosition("market1")
	if !pos.Equal(decimal.NewFromInt(60)) {
		t.Errorf("Expected position of 60 after partial sell, got %s", pos)
	}

	// Sell rest of position
	engine.RecordFill("market1", decimal.NewFromInt(60), decimal.NewFromFloat(0.5), false, decimal.Zero)
	pos = engine.GetPosition("market1")
	if !pos.IsZero() {
		t.Errorf("Expected zero position after full sell, got %s", pos)
	}
}
