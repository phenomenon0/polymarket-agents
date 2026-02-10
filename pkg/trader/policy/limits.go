// Package policy provides risk management and policy enforcement for trading.
package policy

import (
	"fmt"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// RiskLimits defines the risk parameters for trading.
type RiskLimits struct {
	// Position limits
	MaxPositionSize  decimal.Decimal // Max size per market
	MaxTotalExposure decimal.Decimal // Max total exposure across all markets
	MaxConcentration decimal.Decimal // Max % of exposure in single market (0-1)
	MaxOpenOrders    int             // Max concurrent open orders

	// Daily limits
	MaxDailyLoss   decimal.Decimal // Max loss per day
	MaxDailyVolume decimal.Decimal // Max volume per day
	MaxDailyOrders int             // Max orders per day

	// Per-trade limits
	MaxOrderSize decimal.Decimal // Max single order size
	MinOrderSize decimal.Decimal // Min single order size
	MaxSlippage  decimal.Decimal // Max acceptable slippage (0-1)

	// Time limits
	CooldownAfterLoss  time.Duration // Cooldown after significant loss
	MaxSessionDuration time.Duration // Max continuous trading session

	// Market restrictions
	AllowedMarkets []string // If set, only trade these markets
	BlockedMarkets []string // Markets to never trade
}

// DefaultRiskLimits returns conservative default limits.
func DefaultRiskLimits() *RiskLimits {
	return &RiskLimits{
		MaxPositionSize:  decimal.NewFromInt(1000),  // $1000 max per market
		MaxTotalExposure: decimal.NewFromInt(5000),  // $5000 total
		MaxConcentration: decimal.NewFromFloat(0.3), // 30% max in one market
		MaxOpenOrders:    10,

		MaxDailyLoss:   decimal.NewFromInt(500),   // $500 max daily loss
		MaxDailyVolume: decimal.NewFromInt(10000), // $10000 daily volume
		MaxDailyOrders: 100,

		MaxOrderSize: decimal.NewFromInt(500),    // $500 max single order
		MinOrderSize: decimal.NewFromInt(5),      // $5 min single order
		MaxSlippage:  decimal.NewFromFloat(0.02), // 2% max slippage

		CooldownAfterLoss:  15 * time.Minute,
		MaxSessionDuration: 8 * time.Hour,
	}
}

// TightRiskLimits returns very conservative limits for testing.
func TightRiskLimits() *RiskLimits {
	return &RiskLimits{
		MaxPositionSize:  decimal.NewFromInt(100),
		MaxTotalExposure: decimal.NewFromInt(500),
		MaxConcentration: decimal.NewFromFloat(0.2),
		MaxOpenOrders:    5,

		MaxDailyLoss:   decimal.NewFromInt(50),
		MaxDailyVolume: decimal.NewFromInt(1000),
		MaxDailyOrders: 20,

		MaxOrderSize: decimal.NewFromInt(50),
		MinOrderSize: decimal.NewFromInt(5),
		MaxSlippage:  decimal.NewFromFloat(0.01),

		CooldownAfterLoss:  30 * time.Minute,
		MaxSessionDuration: 2 * time.Hour,
	}
}

// PolicyEngine enforces risk limits and tracks trading state.
type PolicyEngine struct {
	limits *RiskLimits

	mu           sync.RWMutex
	positions    map[string]decimal.Decimal // market -> size
	openOrders   int
	dailyLoss    decimal.Decimal
	dailyVolume  decimal.Decimal
	dailyOrders  int
	lastLossTime time.Time
	sessionStart time.Time
	lastTradeDay int // Day of year
}

// NewPolicyEngine creates a new policy engine with the given limits.
func NewPolicyEngine(limits *RiskLimits) *PolicyEngine {
	if limits == nil {
		limits = DefaultRiskLimits()
	}
	return &PolicyEngine{
		limits:       limits,
		positions:    make(map[string]decimal.Decimal),
		sessionStart: time.Now(),
		lastTradeDay: time.Now().YearDay(),
	}
}

// CheckOrder validates an order against risk limits.
func (p *PolicyEngine) CheckOrder(market string, size, price decimal.Decimal, isBuy bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Reset daily counters if new day
	p.resetDailyIfNeeded()

	// Check if market is allowed
	if err := p.checkMarketAllowed(market); err != nil {
		return err
	}

	// Check order size limits
	orderValue := size.Mul(price)
	if orderValue.GreaterThan(p.limits.MaxOrderSize) {
		return fmt.Errorf("order size $%s exceeds max $%s", orderValue, p.limits.MaxOrderSize)
	}
	if orderValue.LessThan(p.limits.MinOrderSize) {
		return fmt.Errorf("order size $%s below min $%s", orderValue, p.limits.MinOrderSize)
	}

	// Check open orders limit
	if p.openOrders >= p.limits.MaxOpenOrders {
		return fmt.Errorf("too many open orders: %d >= %d", p.openOrders, p.limits.MaxOpenOrders)
	}

	// Check daily limits
	if p.dailyOrders >= p.limits.MaxDailyOrders {
		return fmt.Errorf("daily order limit reached: %d", p.limits.MaxDailyOrders)
	}
	if p.dailyVolume.Add(orderValue).GreaterThan(p.limits.MaxDailyVolume) {
		return fmt.Errorf("would exceed daily volume limit $%s", p.limits.MaxDailyVolume)
	}
	if p.dailyLoss.GreaterThan(p.limits.MaxDailyLoss) {
		return fmt.Errorf("daily loss limit exceeded: $%s", p.dailyLoss)
	}

	// Check position limits
	currentPos := p.positions[market]
	var newPos decimal.Decimal
	if isBuy {
		newPos = currentPos.Add(size)
	} else {
		newPos = currentPos.Sub(size)
	}

	if newPos.Abs().GreaterThan(p.limits.MaxPositionSize) {
		return fmt.Errorf("position size would exceed limit: $%s > $%s", newPos.Abs(), p.limits.MaxPositionSize)
	}

	// Check total exposure (using position sizes as exposure proxy)
	totalExposure := p.calculateTotalExposure()
	newTotalExposure := totalExposure
	if isBuy {
		newTotalExposure = totalExposure.Add(size)
	}
	if newTotalExposure.GreaterThan(p.limits.MaxTotalExposure) {
		return fmt.Errorf("total exposure would exceed limit: $%s > $%s", newTotalExposure, p.limits.MaxTotalExposure)
	}

	// Check concentration (position size as % of total exposure)
	if !newTotalExposure.IsZero() && len(p.positions) > 0 {
		// Only check concentration if we have multiple markets
		// Single market is always 100% concentration by definition
		concentration := newPos.Abs().Div(newTotalExposure)
		if concentration.GreaterThan(p.limits.MaxConcentration) {
			return fmt.Errorf("concentration would exceed limit: %.2f%% > %.2f%%",
				concentration.Mul(decimal.NewFromInt(100)).InexactFloat64(),
				p.limits.MaxConcentration.Mul(decimal.NewFromInt(100)).InexactFloat64())
		}
	}

	// Check cooldown after loss
	if !p.lastLossTime.IsZero() && time.Since(p.lastLossTime) < p.limits.CooldownAfterLoss {
		remaining := p.limits.CooldownAfterLoss - time.Since(p.lastLossTime)
		return fmt.Errorf("in cooldown period after loss, %v remaining", remaining)
	}

	// Check session duration
	if time.Since(p.sessionStart) > p.limits.MaxSessionDuration {
		return fmt.Errorf("max session duration exceeded: %v", p.limits.MaxSessionDuration)
	}

	return nil
}

// RecordOrder records an order being placed.
func (p *PolicyEngine) RecordOrder(market string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.openOrders++
	p.dailyOrders++
}

// RecordOrderCanceled records an order being canceled.
func (p *PolicyEngine) RecordOrderCanceled() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.openOrders > 0 {
		p.openOrders--
	}
}

// RecordFill records a trade fill.
func (p *PolicyEngine) RecordFill(market string, size, price decimal.Decimal, isBuy bool, pnl decimal.Decimal) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Update position
	currentPos := p.positions[market]
	if isBuy {
		p.positions[market] = currentPos.Add(size)
	} else {
		p.positions[market] = currentPos.Sub(size)
	}

	// Update daily stats
	p.dailyVolume = p.dailyVolume.Add(size.Mul(price))

	if pnl.LessThan(decimal.Zero) {
		p.dailyLoss = p.dailyLoss.Add(pnl.Abs())
		p.lastLossTime = time.Now()
	}

	// Decrement open orders (order was filled)
	if p.openOrders > 0 {
		p.openOrders--
	}
}

// CheckSlippage checks if slippage is acceptable.
func (p *PolicyEngine) CheckSlippage(expectedPrice, actualPrice decimal.Decimal) error {
	if expectedPrice.IsZero() {
		return nil
	}

	slippage := actualPrice.Sub(expectedPrice).Abs().Div(expectedPrice)
	if slippage.GreaterThan(p.limits.MaxSlippage) {
		return fmt.Errorf("slippage %.2f%% exceeds max %.2f%%",
			slippage.Mul(decimal.NewFromInt(100)).InexactFloat64(),
			p.limits.MaxSlippage.Mul(decimal.NewFromInt(100)).InexactFloat64())
	}
	return nil
}

// GetPosition returns the current position in a market.
func (p *PolicyEngine) GetPosition(market string) decimal.Decimal {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.positions[market]
}

// GetTotalExposure returns total exposure across all markets.
func (p *PolicyEngine) GetTotalExposure() decimal.Decimal {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.calculateTotalExposure()
}

// GetDailyStats returns daily trading statistics.
func (p *PolicyEngine) GetDailyStats() (loss, volume decimal.Decimal, orders int) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.dailyLoss, p.dailyVolume, p.dailyOrders
}

// ResetSession resets the session timer.
func (p *PolicyEngine) ResetSession() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sessionStart = time.Now()
}

// --- Internal helpers ---

func (p *PolicyEngine) resetDailyIfNeeded() {
	now := time.Now()
	if p.lastTradeDay != now.YearDay() {
		p.dailyLoss = decimal.Zero
		p.dailyVolume = decimal.Zero
		p.dailyOrders = 0
		p.lastTradeDay = now.YearDay()
	}
}

func (p *PolicyEngine) calculateTotalExposure() decimal.Decimal {
	total := decimal.Zero
	for _, pos := range p.positions {
		total = total.Add(pos.Abs())
	}
	return total
}

func (p *PolicyEngine) checkMarketAllowed(market string) error {
	// Check blocklist
	for _, blocked := range p.limits.BlockedMarkets {
		if market == blocked {
			return fmt.Errorf("market %s is blocked", market)
		}
	}

	// Check allowlist (if set)
	if len(p.limits.AllowedMarkets) > 0 {
		for _, allowed := range p.limits.AllowedMarkets {
			if market == allowed {
				return nil
			}
		}
		return fmt.Errorf("market %s is not in allowed list", market)
	}

	return nil
}

// PolicyStatus returns a summary of the current policy state.
type PolicyStatus struct {
	OpenOrders      int    `json:"open_orders"`
	MaxOpenOrders   int    `json:"max_open_orders"`
	TotalExposure   string `json:"total_exposure"`
	MaxExposure     string `json:"max_exposure"`
	DailyLoss       string `json:"daily_loss"`
	MaxDailyLoss    string `json:"max_daily_loss"`
	DailyVolume     string `json:"daily_volume"`
	MaxDailyVolume  string `json:"max_daily_volume"`
	DailyOrders     int    `json:"daily_orders"`
	MaxDailyOrders  int    `json:"max_daily_orders"`
	SessionDuration string `json:"session_duration"`
	MaxSessionDur   string `json:"max_session_duration"`
	InCooldown      bool   `json:"in_cooldown"`
	CooldownRemain  string `json:"cooldown_remaining,omitempty"`
}

// Status returns the current policy status.
func (p *PolicyEngine) Status() PolicyStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	status := PolicyStatus{
		OpenOrders:      p.openOrders,
		MaxOpenOrders:   p.limits.MaxOpenOrders,
		TotalExposure:   p.calculateTotalExposure().String(),
		MaxExposure:     p.limits.MaxTotalExposure.String(),
		DailyLoss:       p.dailyLoss.String(),
		MaxDailyLoss:    p.limits.MaxDailyLoss.String(),
		DailyVolume:     p.dailyVolume.String(),
		MaxDailyVolume:  p.limits.MaxDailyVolume.String(),
		DailyOrders:     p.dailyOrders,
		MaxDailyOrders:  p.limits.MaxDailyOrders,
		SessionDuration: time.Since(p.sessionStart).Round(time.Second).String(),
		MaxSessionDur:   p.limits.MaxSessionDuration.String(),
	}

	if !p.lastLossTime.IsZero() && time.Since(p.lastLossTime) < p.limits.CooldownAfterLoss {
		status.InCooldown = true
		status.CooldownRemain = (p.limits.CooldownAfterLoss - time.Since(p.lastLossTime)).Round(time.Second).String()
	}

	return status
}
