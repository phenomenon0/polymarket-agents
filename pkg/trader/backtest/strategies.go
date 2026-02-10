package backtest

import (
	"context"
	"log"
	"time"

	"github.com/phenomenon0/polymarket-agents/pkg/trader/agents"

	"github.com/shopspring/decimal"
)

// MomentumStrategy is a simple momentum-based strategy.
// Buys when price is above moving average, sells when below.
type MomentumStrategy struct {
	LookbackPeriod int             // Number of periods for moving average
	PositionSize   decimal.Decimal // Size per trade
	ThresholdPct   decimal.Decimal // % above/below MA to trigger

	priceHistory map[string][]decimal.Decimal
}

// NewMomentumStrategy creates a new momentum strategy.
func NewMomentumStrategy(lookback int, positionSize, threshold float64) *MomentumStrategy {
	return &MomentumStrategy{
		LookbackPeriod: lookback,
		PositionSize:   decimal.NewFromFloat(positionSize),
		ThresholdPct:   decimal.NewFromFloat(threshold),
		priceHistory:   make(map[string][]decimal.Decimal),
	}
}

func (s *MomentumStrategy) OnStart(ctx context.Context, bt *Backtest) {
	// Initialize
}

func (s *MomentumStrategy) OnEnd(ctx context.Context, bt *Backtest) {
	// Close all positions
	for _, pos := range bt.Positions() {
		bt.Sell(pos.TokenID, pos.Market, pos.Size)
	}
}

func (s *MomentumStrategy) OnTick(ctx context.Context, bt *Backtest, point PricePoint) {
	// Update price history
	history := s.priceHistory[point.TokenID]
	history = append(history, point.Price)
	if len(history) > s.LookbackPeriod {
		history = history[len(history)-s.LookbackPeriod:]
	}
	s.priceHistory[point.TokenID] = history

	// Need enough history
	if len(history) < s.LookbackPeriod {
		return
	}

	// Calculate moving average
	sum := decimal.Zero
	for _, p := range history {
		sum = sum.Add(p)
	}
	ma := sum.Div(decimal.NewFromInt(int64(len(history))))

	// Current price vs MA
	currentPrice := point.Price
	deviation := currentPrice.Sub(ma).Div(ma).Mul(decimal.NewFromInt(100))

	pos, hasPos := bt.Position(point.TokenID)

	// Buy signal: price above MA by threshold
	if deviation.GreaterThan(s.ThresholdPct) && !hasPos {
		bt.Buy(point.TokenID, point.Market, s.PositionSize)
	}

	// Sell signal: price below MA by threshold
	if deviation.LessThan(s.ThresholdPct.Neg()) && hasPos {
		bt.Sell(point.TokenID, point.Market, pos.Size)
	}
}

// MeanReversionStrategy buys when price drops significantly and sells when it rebounds.
type MeanReversionStrategy struct {
	LookbackPeriod int
	PositionSize   decimal.Decimal
	EntryThreshold decimal.Decimal // % below MA to buy
	ExitThreshold  decimal.Decimal // % above entry to sell

	priceHistory map[string][]decimal.Decimal
	entryPrices  map[string]decimal.Decimal
}

// NewMeanReversionStrategy creates a new mean reversion strategy.
func NewMeanReversionStrategy(lookback int, positionSize, entryThreshold, exitThreshold float64) *MeanReversionStrategy {
	return &MeanReversionStrategy{
		LookbackPeriod: lookback,
		PositionSize:   decimal.NewFromFloat(positionSize),
		EntryThreshold: decimal.NewFromFloat(entryThreshold),
		ExitThreshold:  decimal.NewFromFloat(exitThreshold),
		priceHistory:   make(map[string][]decimal.Decimal),
		entryPrices:    make(map[string]decimal.Decimal),
	}
}

func (s *MeanReversionStrategy) OnStart(ctx context.Context, bt *Backtest) {}

func (s *MeanReversionStrategy) OnEnd(ctx context.Context, bt *Backtest) {
	for _, pos := range bt.Positions() {
		bt.Sell(pos.TokenID, pos.Market, pos.Size)
	}
}

func (s *MeanReversionStrategy) OnTick(ctx context.Context, bt *Backtest, point PricePoint) {
	history := s.priceHistory[point.TokenID]
	history = append(history, point.Price)
	if len(history) > s.LookbackPeriod {
		history = history[len(history)-s.LookbackPeriod:]
	}
	s.priceHistory[point.TokenID] = history

	if len(history) < s.LookbackPeriod {
		return
	}

	// Calculate MA
	sum := decimal.Zero
	for _, p := range history {
		sum = sum.Add(p)
	}
	ma := sum.Div(decimal.NewFromInt(int64(len(history))))

	currentPrice := point.Price
	deviation := currentPrice.Sub(ma).Div(ma).Mul(decimal.NewFromInt(100))

	pos, hasPos := bt.Position(point.TokenID)

	// Buy when price drops below MA by threshold
	if deviation.LessThan(s.EntryThreshold.Neg()) && !hasPos {
		bt.Buy(point.TokenID, point.Market, s.PositionSize)
		s.entryPrices[point.TokenID] = currentPrice
	}

	// Sell when price rebounds above entry by threshold
	if hasPos {
		entryPrice := s.entryPrices[point.TokenID]
		gain := currentPrice.Sub(entryPrice).Div(entryPrice).Mul(decimal.NewFromInt(100))
		if gain.GreaterThan(s.ExitThreshold) {
			bt.Sell(point.TokenID, point.Market, pos.Size)
			delete(s.entryPrices, point.TokenID)
		}
	}
}

// BuyAndHoldStrategy simply buys at the start and holds until the end.
type BuyAndHoldStrategy struct {
	PositionSize decimal.Decimal
	bought       bool
}

// NewBuyAndHoldStrategy creates a buy and hold strategy.
func NewBuyAndHoldStrategy(positionSize float64) *BuyAndHoldStrategy {
	return &BuyAndHoldStrategy{
		PositionSize: decimal.NewFromFloat(positionSize),
	}
}

func (s *BuyAndHoldStrategy) OnStart(ctx context.Context, bt *Backtest) {}

func (s *BuyAndHoldStrategy) OnEnd(ctx context.Context, bt *Backtest) {
	for _, pos := range bt.Positions() {
		bt.Sell(pos.TokenID, pos.Market, pos.Size)
	}
}

func (s *BuyAndHoldStrategy) OnTick(ctx context.Context, bt *Backtest, point PricePoint) {
	if s.bought {
		return
	}

	// Buy on first tick
	bt.Buy(point.TokenID, point.Market, s.PositionSize)
	s.bought = true
}

// ForecasterStrategy uses an LLM forecaster to make trading decisions.
// It buys when the forecasted probability is higher than the current price (edge > threshold),
// and sells when the edge disappears or reverses.
type ForecasterStrategy struct {
	Forecaster      *agents.Forecaster
	PositionSize    decimal.Decimal
	MinEdgeBps      decimal.Decimal // Minimum edge in basis points to trade
	MinConfidence   decimal.Decimal // Minimum confidence to trade
	ForecastEveryN  int             // Forecast every N ticks (to reduce LLM calls)
	MaxPositionSize decimal.Decimal // Maximum position size per market

	priceHistory map[string][]PricePoint
	lastForecast map[string]*agents.Forecast
	tickCount    map[string]int
	verbose      bool
}

// ForecasterStrategyConfig configures the forecaster strategy.
type ForecasterStrategyConfig struct {
	Forecaster      *agents.Forecaster
	PositionSize    float64
	MinEdgeBps      float64 // Minimum edge in basis points (e.g., 500 = 5%)
	MinConfidence   float64 // Minimum confidence (0-1)
	ForecastEveryN  int     // How often to call the forecaster
	MaxPositionSize float64
	Verbose         bool
}

// NewForecasterStrategy creates a new LLM forecaster strategy.
func NewForecasterStrategy(config *ForecasterStrategyConfig) *ForecasterStrategy {
	if config == nil {
		config = &ForecasterStrategyConfig{
			PositionSize:    100,
			MinEdgeBps:      500, // 5% edge
			MinConfidence:   0.6,
			ForecastEveryN:  10, // Every 10 ticks
			MaxPositionSize: 1000,
		}
	}

	return &ForecasterStrategy{
		Forecaster:      config.Forecaster,
		PositionSize:    decimal.NewFromFloat(config.PositionSize),
		MinEdgeBps:      decimal.NewFromFloat(config.MinEdgeBps),
		MinConfidence:   decimal.NewFromFloat(config.MinConfidence),
		ForecastEveryN:  config.ForecastEveryN,
		MaxPositionSize: decimal.NewFromFloat(config.MaxPositionSize),
		priceHistory:    make(map[string][]PricePoint),
		lastForecast:    make(map[string]*agents.Forecast),
		tickCount:       make(map[string]int),
		verbose:         config.Verbose,
	}
}

func (s *ForecasterStrategy) OnStart(ctx context.Context, bt *Backtest) {
	if s.verbose {
		log.Println("ForecasterStrategy: Starting backtest")
	}
}

func (s *ForecasterStrategy) OnEnd(ctx context.Context, bt *Backtest) {
	// Close all positions
	for _, pos := range bt.Positions() {
		if s.verbose {
			log.Printf("ForecasterStrategy: Closing position %s (size: %s)", pos.TokenID, pos.Size)
		}
		bt.Sell(pos.TokenID, pos.Market, pos.Size)
	}
}

func (s *ForecasterStrategy) OnTick(ctx context.Context, bt *Backtest, point PricePoint) {
	// Track price history
	s.priceHistory[point.TokenID] = append(s.priceHistory[point.TokenID], point)
	if len(s.priceHistory[point.TokenID]) > 100 {
		s.priceHistory[point.TokenID] = s.priceHistory[point.TokenID][1:]
	}

	// Increment tick count
	s.tickCount[point.TokenID]++

	// Only forecast every N ticks to reduce LLM calls
	if s.tickCount[point.TokenID]%s.ForecastEveryN != 0 {
		// Use last forecast if available
		forecast := s.lastForecast[point.TokenID]
		if forecast != nil {
			s.evaluateSignal(ctx, bt, point, forecast)
		}
		return
	}

	// Get forecast
	var forecast *agents.Forecast
	var err error

	if s.Forecaster != nil {
		// Use real LLM forecaster
		marketCtx := s.buildMarketContext(point)
		forecast, err = s.Forecaster.ForecastWithFallback(ctx, marketCtx)
		if err != nil {
			if s.verbose {
				log.Printf("ForecasterStrategy: Forecast error: %v", err)
			}
			return
		}
	} else {
		// Use simulated forecast for backtesting
		forecast = s.simulateForecast(point)
	}

	s.lastForecast[point.TokenID] = forecast
	s.evaluateSignal(ctx, bt, point, forecast)
}

func (s *ForecasterStrategy) buildMarketContext(point PricePoint) *agents.MarketContext {
	return &agents.MarketContext{
		TokenID:      point.TokenID,
		Market:       point.Market,
		Question:     point.Market, // Use market as question for backtest
		CurrentPrice: point.Price,
		Volume24h:    point.Volume,
		EndDate:      time.Now().Add(24 * time.Hour), // Placeholder
	}
}

// simulateForecast creates a simulated forecast for backtesting.
// This uses a simple model based on recent price trends and mean reversion.
func (s *ForecasterStrategy) simulateForecast(point PricePoint) *agents.Forecast {
	history := s.priceHistory[point.TokenID]
	currentPrice := point.Price

	// Base forecast: current price with small random adjustment
	forecastProb := currentPrice

	if len(history) >= 5 {
		// Calculate recent trend
		recent := history[len(history)-5:]
		avgRecent := decimal.Zero
		for _, p := range recent {
			avgRecent = avgRecent.Add(p.Price)
		}
		avgRecent = avgRecent.Div(decimal.NewFromInt(5))

		// Mean reversion signal: if price is below average, expect it to rise
		if currentPrice.LessThan(avgRecent) {
			// Price is low, forecast higher probability
			diff := avgRecent.Sub(currentPrice)
			forecastProb = currentPrice.Add(diff.Mul(decimal.NewFromFloat(0.5)))
		} else {
			// Price is high, forecast lower probability
			diff := currentPrice.Sub(avgRecent)
			forecastProb = currentPrice.Sub(diff.Mul(decimal.NewFromFloat(0.3)))
		}
	}

	// Clamp to valid probability range
	if forecastProb.LessThan(decimal.NewFromFloat(0.01)) {
		forecastProb = decimal.NewFromFloat(0.01)
	}
	if forecastProb.GreaterThan(decimal.NewFromFloat(0.99)) {
		forecastProb = decimal.NewFromFloat(0.99)
	}

	// Confidence based on how much history we have
	confidence := decimal.NewFromFloat(0.5)
	if len(history) >= 20 {
		confidence = decimal.NewFromFloat(0.7)
	}
	if len(history) >= 50 {
		confidence = decimal.NewFromFloat(0.8)
	}

	return &agents.Forecast{
		TokenID:     point.TokenID,
		Market:      point.Market,
		Probability: forecastProb,
		Confidence:  confidence,
		Reasoning:   "Simulated forecast based on mean reversion",
		Timestamp:   point.Timestamp,
	}
}

func (s *ForecasterStrategy) evaluateSignal(ctx context.Context, bt *Backtest, point PricePoint, forecast *agents.Forecast) {
	currentPrice := point.Price
	forecastProb := forecast.Probability
	confidence := forecast.Confidence

	// Check minimum confidence
	if confidence.LessThan(s.MinConfidence) {
		return
	}

	// Calculate edge: (forecast - price) / price * 10000 (in basis points)
	edge := forecastProb.Sub(currentPrice).Div(currentPrice).Mul(decimal.NewFromInt(10000))

	pos, hasPos := bt.Position(point.TokenID)

	// BUY signal: forecast > price by MinEdgeBps
	if edge.GreaterThan(s.MinEdgeBps) && !hasPos {
		// Calculate position size based on edge and confidence
		size := s.PositionSize.Mul(confidence)
		if size.GreaterThan(s.MaxPositionSize) {
			size = s.MaxPositionSize
		}

		if s.verbose {
			log.Printf("ForecasterStrategy: BUY %s @ %s (forecast: %s, edge: %s bps)",
				point.TokenID, currentPrice, forecastProb, edge.Round(0))
		}
		bt.Buy(point.TokenID, point.Market, size)
	}

	// SELL signal: forecast < price (negative edge) or edge dropped below threshold
	if hasPos {
		negativeEdge := edge.LessThan(decimal.Zero)
		smallEdge := edge.LessThan(s.MinEdgeBps.Div(decimal.NewFromInt(2))) // Exit at half the entry threshold

		if negativeEdge || smallEdge {
			if s.verbose {
				log.Printf("ForecasterStrategy: SELL %s @ %s (forecast: %s, edge: %s bps)",
					point.TokenID, currentPrice, forecastProb, edge.Round(0))
			}
			bt.Sell(point.TokenID, point.Market, pos.Size)
		}
	}
}

// EdgeStrategy is a simplified edge-based strategy that trades when price
// deviates significantly from a fair value estimate.
type EdgeStrategy struct {
	PositionSize   decimal.Decimal
	MinEdgeBps     decimal.Decimal // Minimum edge to enter
	ExitEdgeBps    decimal.Decimal // Edge threshold to exit
	LookbackPeriod int
	UseEMA         bool // Use EMA instead of SMA for fair value

	priceHistory map[string][]decimal.Decimal
	ema          map[string]decimal.Decimal
}

// NewEdgeStrategy creates a new edge-based strategy.
func NewEdgeStrategy(positionSize, minEdgeBps, exitEdgeBps float64, lookback int, useEMA bool) *EdgeStrategy {
	return &EdgeStrategy{
		PositionSize:   decimal.NewFromFloat(positionSize),
		MinEdgeBps:     decimal.NewFromFloat(minEdgeBps),
		ExitEdgeBps:    decimal.NewFromFloat(exitEdgeBps),
		LookbackPeriod: lookback,
		UseEMA:         useEMA,
		priceHistory:   make(map[string][]decimal.Decimal),
		ema:            make(map[string]decimal.Decimal),
	}
}

func (s *EdgeStrategy) OnStart(ctx context.Context, bt *Backtest) {}

func (s *EdgeStrategy) OnEnd(ctx context.Context, bt *Backtest) {
	for _, pos := range bt.Positions() {
		bt.Sell(pos.TokenID, pos.Market, pos.Size)
	}
}

func (s *EdgeStrategy) OnTick(ctx context.Context, bt *Backtest, point PricePoint) {
	history := s.priceHistory[point.TokenID]
	history = append(history, point.Price)
	if len(history) > s.LookbackPeriod {
		history = history[len(history)-s.LookbackPeriod:]
	}
	s.priceHistory[point.TokenID] = history

	if len(history) < s.LookbackPeriod {
		return
	}

	// Calculate fair value
	var fairValue decimal.Decimal
	if s.UseEMA {
		// EMA calculation
		alpha := decimal.NewFromFloat(2.0 / float64(s.LookbackPeriod+1))
		ema, exists := s.ema[point.TokenID]
		if !exists {
			// Initialize EMA with SMA
			sum := decimal.Zero
			for _, p := range history {
				sum = sum.Add(p)
			}
			ema = sum.Div(decimal.NewFromInt(int64(len(history))))
		}
		// EMA = alpha * price + (1-alpha) * EMA
		ema = alpha.Mul(point.Price).Add(decimal.NewFromInt(1).Sub(alpha).Mul(ema))
		s.ema[point.TokenID] = ema
		fairValue = ema
	} else {
		// Simple moving average
		sum := decimal.Zero
		for _, p := range history {
			sum = sum.Add(p)
		}
		fairValue = sum.Div(decimal.NewFromInt(int64(len(history))))
	}

	// Calculate edge: (fairValue - price) / price * 10000
	edge := fairValue.Sub(point.Price).Div(point.Price).Mul(decimal.NewFromInt(10000))

	pos, hasPos := bt.Position(point.TokenID)

	// BUY when price is below fair value by MinEdgeBps
	if edge.GreaterThan(s.MinEdgeBps) && !hasPos {
		bt.Buy(point.TokenID, point.Market, s.PositionSize)
	}

	// SELL when edge drops below ExitEdgeBps (or reverses)
	if hasPos && edge.LessThan(s.ExitEdgeBps) {
		bt.Sell(point.TokenID, point.Market, pos.Size)
	}
}
