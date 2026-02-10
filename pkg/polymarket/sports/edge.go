package sports

import (
	"math"

	"github.com/shopspring/decimal"
)

// EdgeCalculator calculates trading edge and position sizing.
type EdgeCalculator struct {
	feeModel    FeeModel
	kellyFrac   decimal.Decimal // Kelly fraction (e.g., 0.1 for 10% Kelly)
	kellyExp    decimal.Decimal // Kelly exponent for small edges
	maxStakePct decimal.Decimal // Max stake as % of bankroll
	minEdgeBps  decimal.Decimal // Minimum edge in basis points
}

// EdgeCalculatorConfig configures the edge calculator.
type EdgeCalculatorConfig struct {
	FeeModel    FeeModel
	KellyFrac   float64 // Default: 0.10 (10% Kelly)
	KellyExp    float64 // Default: 0.85 (power law for small edges)
	MaxStakePct float64 // Default: 0.015 (1.5% max)
	MinEdgeBps  float64 // Default: 500 (5%)
}

// DefaultEdgeCalculatorConfig returns default configuration.
func DefaultEdgeCalculatorConfig() *EdgeCalculatorConfig {
	return &EdgeCalculatorConfig{
		FeeModel:    &ZeroFeeModel{},
		KellyFrac:   0.10,
		KellyExp:    0.85,
		MaxStakePct: 0.015,
		MinEdgeBps:  500,
	}
}

// NewEdgeCalculator creates a new edge calculator.
func NewEdgeCalculator(config *EdgeCalculatorConfig) *EdgeCalculator {
	if config == nil {
		config = DefaultEdgeCalculatorConfig()
	}

	// Apply defaults for unset values
	defaults := DefaultEdgeCalculatorConfig()
	if config.FeeModel == nil {
		config.FeeModel = defaults.FeeModel
	}
	if config.KellyFrac == 0 {
		config.KellyFrac = defaults.KellyFrac
	}
	if config.KellyExp == 0 {
		config.KellyExp = defaults.KellyExp
	}
	if config.MaxStakePct == 0 {
		config.MaxStakePct = defaults.MaxStakePct
	}
	// MinEdgeBps can be 0 intentionally, so don't default it

	return &EdgeCalculator{
		feeModel:    config.FeeModel,
		kellyFrac:   decimal.NewFromFloat(config.KellyFrac),
		kellyExp:    decimal.NewFromFloat(config.KellyExp),
		maxStakePct: decimal.NewFromFloat(config.MaxStakePct),
		minEdgeBps:  decimal.NewFromFloat(config.MinEdgeBps),
	}
}

// CalculateEdge calculates the edge for a market.
//
// Parameters:
//   - modelProb: The model's probability for the YES outcome (0-1)
//   - marketPrice: The current best ask price (0-1)
//   - bankroll: Current bankroll in dollars
//
// The edge formula for Polymarket (pay p to win 1):
//   - EV per share = q - p (where q is model prob, p is price)
//   - Edge = q - p_eff (after fees)
//
// Kelly fraction for this payoff structure:
//   - f* = (q - p) / (1 - p)
func (c *EdgeCalculator) CalculateEdge(modelProb, marketPrice, bankroll decimal.Decimal) *EdgeResult {
	// Calculate effective price (after fees)
	effectivePrice := c.feeModel.EffectivePrice("BUY", marketPrice)

	// Edge = q - p_eff
	edge := modelProb.Sub(effectivePrice)
	edgeBps := edge.Mul(decimal.NewFromInt(10000))

	result := &EdgeResult{
		ModelProb:      modelProb,
		MarketPrice:    marketPrice,
		EffectivePrice: effectivePrice,
		Edge:           edge,
		EdgeBps:        edgeBps,
	}

	// Check if this is a value bet
	if edgeBps.LessThan(c.minEdgeBps) {
		result.IsValueBet = false
		result.Reason = "Edge below minimum threshold"
		return result
	}

	// Kelly formula: f* = (q - p) / (1 - p)
	// where q = modelProb, p = effectivePrice
	denominator := decimal.NewFromInt(1).Sub(effectivePrice)
	if denominator.IsZero() {
		result.IsValueBet = false
		result.Reason = "Invalid price (would pay 100%)"
		return result
	}

	kellyRaw := edge.Div(denominator)
	result.KellyFraction = kellyRaw

	// Apply Kelly fraction and exponent
	// adjusted = kellyFrac * (f* ^ kellyExp)
	// For small edges, kellyExp < 1 makes bets more conservative
	var adjustedKelly decimal.Decimal
	if kellyRaw.IsPositive() {
		// Power law: f^exp (approximate for decimal)
		kellyFloat, _ := kellyRaw.Float64()
		expFloat, _ := c.kellyExp.Float64()
		powered := pow(kellyFloat, expFloat)
		adjustedKelly = c.kellyFrac.Mul(decimal.NewFromFloat(powered))
	} else {
		adjustedKelly = decimal.Zero
	}

	// Cap at max stake percentage
	if adjustedKelly.GreaterThan(c.maxStakePct) {
		adjustedKelly = c.maxStakePct
	}

	result.AdjustedKelly = adjustedKelly
	result.SuggestedSize = bankroll.Mul(adjustedKelly)
	result.IsValueBet = true
	result.Reason = "Positive edge above threshold"

	return result
}

// pow calculates x^y using standard library.
func pow(x, y float64) float64 {
	if x <= 0 {
		return 0
	}
	return math.Pow(x, y)
}

// CalculateVWAP calculates the volume-weighted average price for a given size.
// This is critical for accurate edge calculation - never use mid price!
func CalculateVWAP(asks []PriceLevel, targetSize decimal.Decimal) (decimal.Decimal, decimal.Decimal, bool) {
	if len(asks) == 0 || targetSize.IsZero() {
		return decimal.Zero, decimal.Zero, false
	}

	totalCost := decimal.Zero
	totalFilled := decimal.Zero

	for _, level := range asks {
		remaining := targetSize.Sub(totalFilled)
		if remaining.IsZero() {
			break
		}

		fillSize := level.Size
		if fillSize.GreaterThan(remaining) {
			fillSize = remaining
		}

		totalCost = totalCost.Add(level.Price.Mul(fillSize))
		totalFilled = totalFilled.Add(fillSize)
	}

	if totalFilled.IsZero() {
		return decimal.Zero, decimal.Zero, false
	}

	vwap := totalCost.Div(totalFilled)
	return vwap, totalFilled, true
}

// PriceLevel represents a price level in the orderbook.
type PriceLevel struct {
	Price decimal.Decimal
	Size  decimal.Decimal
}

// RiskLimits defines risk management limits.
type RiskLimits struct {
	MaxStakePerMarket decimal.Decimal // Max stake per individual market
	MaxDailyLoss      decimal.Decimal // Max daily loss before stopping
	MaxWeeklyExposure decimal.Decimal // Max total exposure per week
	MaxCorrelatedExp  decimal.Decimal // Max exposure to correlated bets (same match)
	MinLiquidity      decimal.Decimal // Min liquidity to trade
	CooldownAfterLoss int             // Cooldown periods after consecutive losses
}

// DefaultRiskLimits returns conservative default limits.
func DefaultRiskLimits() *RiskLimits {
	return &RiskLimits{
		MaxStakePerMarket: decimal.NewFromFloat(150),  // $150 max per bet
		MaxDailyLoss:      decimal.NewFromFloat(500),  // $500 daily loss limit
		MaxWeeklyExposure: decimal.NewFromFloat(1500), // $1500 weekly exposure
		MaxCorrelatedExp:  decimal.NewFromFloat(200),  // $200 max per match
		MinLiquidity:      decimal.NewFromFloat(1000), // $1000 min liquidity
		CooldownAfterLoss: 3,                          // 3 period cooldown
	}
}

// RiskManager manages trading risk.
type RiskManager struct {
	limits            *RiskLimits
	dailyPnL          decimal.Decimal
	weeklyExposure    decimal.Decimal
	matchExposure     map[string]decimal.Decimal // matchKey -> exposure
	consecutiveLosses int
}

// NewRiskManager creates a new risk manager.
func NewRiskManager(limits *RiskLimits) *RiskManager {
	if limits == nil {
		limits = DefaultRiskLimits()
	}
	return &RiskManager{
		limits:        limits,
		matchExposure: make(map[string]decimal.Decimal),
	}
}

// CheckLimits checks if a proposed trade is within limits.
func (r *RiskManager) CheckLimits(matchKey string, proposedSize, currentLiquidity decimal.Decimal) (bool, string) {
	// Check daily loss
	if r.dailyPnL.LessThan(r.limits.MaxDailyLoss.Neg()) {
		return false, "daily loss limit reached"
	}

	// Check cooldown
	if r.consecutiveLosses >= r.limits.CooldownAfterLoss {
		return false, "in cooldown after consecutive losses"
	}

	// Check per-market limit
	if proposedSize.GreaterThan(r.limits.MaxStakePerMarket) {
		return false, "exceeds per-market stake limit"
	}

	// Check weekly exposure
	newExposure := r.weeklyExposure.Add(proposedSize)
	if newExposure.GreaterThan(r.limits.MaxWeeklyExposure) {
		return false, "would exceed weekly exposure limit"
	}

	// Check correlated exposure (same match)
	matchExp := r.matchExposure[matchKey].Add(proposedSize)
	if matchExp.GreaterThan(r.limits.MaxCorrelatedExp) {
		return false, "would exceed correlated exposure limit for match"
	}

	// Check liquidity
	if currentLiquidity.LessThan(r.limits.MinLiquidity) {
		return false, "insufficient market liquidity"
	}

	return true, ""
}

// RecordTrade records a trade for risk tracking.
func (r *RiskManager) RecordTrade(matchKey string, size, pnl decimal.Decimal) {
	r.dailyPnL = r.dailyPnL.Add(pnl)
	r.weeklyExposure = r.weeklyExposure.Add(size)
	r.matchExposure[matchKey] = r.matchExposure[matchKey].Add(size)

	if pnl.IsNegative() {
		r.consecutiveLosses++
	} else {
		r.consecutiveLosses = 0
	}
}

// ResetDaily resets daily counters.
func (r *RiskManager) ResetDaily() {
	r.dailyPnL = decimal.Zero
}

// ResetWeekly resets weekly counters.
func (r *RiskManager) ResetWeekly() {
	r.weeklyExposure = decimal.Zero
	r.matchExposure = make(map[string]decimal.Decimal)
}
