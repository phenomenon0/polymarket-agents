// Package orchestrator provides a DAG-based workflow coordinator for trading.
package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/phenomenon0/polymarket-agents/pkg/polymarket/clob"
	"github.com/phenomenon0/polymarket-agents/pkg/polymarket/gamma"
	"github.com/phenomenon0/polymarket-agents/pkg/trader/agents"
	"github.com/phenomenon0/polymarket-agents/pkg/trader/paper"
	"github.com/phenomenon0/polymarket-agents/pkg/trader/policy"

	"github.com/shopspring/decimal"
)

// Stage represents a stage in the trading workflow.
type Stage string

const (
	// Core workflow stages
	StageMarketDiscovery Stage = "market_discovery"
	StageDataCollection  Stage = "data_collection"
	StageForecasting     Stage = "forecasting"
	StageSignalGen       Stage = "signal_generation"
	StageRiskCheck       Stage = "risk_check"
	StageOrderExecution  Stage = "order_execution"
	StageMonitoring      Stage = "monitoring"
)

// StageResult holds the result of a stage execution.
type StageResult struct {
	Stage     Stage         `json:"stage"`
	Success   bool          `json:"success"`
	Error     string        `json:"error,omitempty"`
	Data      interface{}   `json:"data,omitempty"`
	Duration  time.Duration `json:"duration"`
	Timestamp time.Time     `json:"timestamp"`
}

// WorkflowConfig configures the trading workflow.
type WorkflowConfig struct {
	// Market filters
	MinVolume    decimal.Decimal
	MaxSpreadBps decimal.Decimal
	Categories   []string
	MaxMarkets   int

	// Forecasting
	MinEdgeBps    int
	MinConfidence decimal.Decimal

	// Execution
	MaxOrderSize  decimal.Decimal
	UsePaperTrade bool

	// Timing
	DiscoveryInterval time.Duration
	ForecastInterval  time.Duration
	MonitorInterval   time.Duration
}

// DefaultWorkflowConfig returns default configuration.
func DefaultWorkflowConfig() *WorkflowConfig {
	return &WorkflowConfig{
		MinVolume:         decimal.NewFromInt(10000),
		MaxSpreadBps:      decimal.NewFromInt(500),
		MaxMarkets:        20,
		MinEdgeBps:        100, // 1% minimum edge
		MinConfidence:     decimal.NewFromFloat(0.6),
		MaxOrderSize:      decimal.NewFromInt(100),
		UsePaperTrade:     true,
		DiscoveryInterval: 5 * time.Minute,
		ForecastInterval:  1 * time.Minute,
		MonitorInterval:   10 * time.Second,
	}
}

// Orchestrator coordinates the trading workflow.
type Orchestrator struct {
	config       *WorkflowConfig
	gammaClient  *gamma.Client
	clobClient   *clob.Client
	forecaster   *agents.Forecaster
	policyEngine *policy.PolicyEngine
	paperEngine  *paper.Engine

	mu      sync.RWMutex
	running bool
	stopCh  chan struct{}

	// State
	activeMarkets []gamma.Market
	forecasts     map[string]*agents.EnsembleForecast // tokenID -> forecast
	signals       []*agents.TradingSignal
	pendingOrders []string

	// Callbacks
	onStageComplete func(*StageResult)
	onSignal        func(*agents.TradingSignal)
	onError         func(error)
}

// NewOrchestrator creates a new workflow orchestrator.
func NewOrchestrator(
	config *WorkflowConfig,
	gammaClient *gamma.Client,
	clobClient *clob.Client,
	forecaster *agents.Forecaster,
	policyEngine *policy.PolicyEngine,
	paperEngine *paper.Engine,
) *Orchestrator {
	if config == nil {
		config = DefaultWorkflowConfig()
	}

	return &Orchestrator{
		config:       config,
		gammaClient:  gammaClient,
		clobClient:   clobClient,
		forecaster:   forecaster,
		policyEngine: policyEngine,
		paperEngine:  paperEngine,
		stopCh:       make(chan struct{}),
		forecasts:    make(map[string]*agents.EnsembleForecast),
	}
}

// OnStageComplete sets a callback for stage completions.
func (o *Orchestrator) OnStageComplete(fn func(*StageResult)) {
	o.onStageComplete = fn
}

// OnSignal sets a callback for trading signals.
func (o *Orchestrator) OnSignal(fn func(*agents.TradingSignal)) {
	o.onSignal = fn
}

// OnError sets a callback for errors.
func (o *Orchestrator) OnError(fn func(error)) {
	o.onError = fn
}

// Start starts the trading workflow.
func (o *Orchestrator) Start(ctx context.Context) error {
	o.mu.Lock()
	if o.running {
		o.mu.Unlock()
		return fmt.Errorf("orchestrator already running")
	}
	o.running = true
	o.stopCh = make(chan struct{})
	o.mu.Unlock()

	// Run initial market discovery
	if err := o.runStage(ctx, StageMarketDiscovery); err != nil {
		o.handleError(fmt.Errorf("initial discovery failed: %w", err))
	}

	// Start background loops
	go o.discoveryLoop(ctx)
	go o.forecastLoop(ctx)
	go o.monitorLoop(ctx)

	return nil
}

// Stop stops the trading workflow.
func (o *Orchestrator) Stop() {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.running {
		close(o.stopCh)
		o.running = false
	}
}

// IsRunning returns true if the orchestrator is running.
func (o *Orchestrator) IsRunning() bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.running
}

// RunOnce executes a single workflow cycle.
func (o *Orchestrator) RunOnce(ctx context.Context) error {
	stages := []Stage{
		StageMarketDiscovery,
		StageDataCollection,
		StageForecasting,
		StageSignalGen,
		StageRiskCheck,
		StageOrderExecution,
	}

	for _, stage := range stages {
		if err := o.runStage(ctx, stage); err != nil {
			return fmt.Errorf("stage %s failed: %w", stage, err)
		}
	}

	return nil
}

// GetActiveMarkets returns currently active markets.
func (o *Orchestrator) GetActiveMarkets() []gamma.Market {
	o.mu.RLock()
	defer o.mu.RUnlock()

	markets := make([]gamma.Market, len(o.activeMarkets))
	copy(markets, o.activeMarkets)
	return markets
}

// GetSignals returns current trading signals.
func (o *Orchestrator) GetSignals() []*agents.TradingSignal {
	o.mu.RLock()
	defer o.mu.RUnlock()

	signals := make([]*agents.TradingSignal, len(o.signals))
	copy(signals, o.signals)
	return signals
}

// GetForecast returns a forecast for a token.
func (o *Orchestrator) GetForecast(tokenID string) (*agents.EnsembleForecast, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()

	forecast, ok := o.forecasts[tokenID]
	return forecast, ok
}

// --- Background Loops ---

func (o *Orchestrator) discoveryLoop(ctx context.Context) {
	ticker := time.NewTicker(o.config.DiscoveryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-o.stopCh:
			return
		case <-ticker.C:
			if err := o.runStage(ctx, StageMarketDiscovery); err != nil {
				o.handleError(fmt.Errorf("discovery failed: %w", err))
			}
		}
	}
}

func (o *Orchestrator) forecastLoop(ctx context.Context) {
	ticker := time.NewTicker(o.config.ForecastInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-o.stopCh:
			return
		case <-ticker.C:
			stages := []Stage{
				StageDataCollection,
				StageForecasting,
				StageSignalGen,
				StageRiskCheck,
				StageOrderExecution,
			}

			for _, stage := range stages {
				if err := o.runStage(ctx, stage); err != nil {
					o.handleError(fmt.Errorf("stage %s failed: %w", stage, err))
					break
				}
			}
		}
	}
}

func (o *Orchestrator) monitorLoop(ctx context.Context) {
	ticker := time.NewTicker(o.config.MonitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-o.stopCh:
			return
		case <-ticker.C:
			if err := o.runStage(ctx, StageMonitoring); err != nil {
				o.handleError(fmt.Errorf("monitoring failed: %w", err))
			}
		}
	}
}

// --- Stage Execution ---

func (o *Orchestrator) runStage(ctx context.Context, stage Stage) error {
	start := time.Now()
	var err error
	var data interface{}

	switch stage {
	case StageMarketDiscovery:
		data, err = o.executeMarketDiscovery(ctx)
	case StageDataCollection:
		data, err = o.executeDataCollection(ctx)
	case StageForecasting:
		data, err = o.executeForecasting(ctx)
	case StageSignalGen:
		data, err = o.executeSignalGen(ctx)
	case StageRiskCheck:
		data, err = o.executeRiskCheck(ctx)
	case StageOrderExecution:
		data, err = o.executeOrderExecution(ctx)
	case StageMonitoring:
		data, err = o.executeMonitoring(ctx)
	default:
		err = fmt.Errorf("unknown stage: %s", stage)
	}

	result := &StageResult{
		Stage:     stage,
		Success:   err == nil,
		Data:      data,
		Duration:  time.Since(start),
		Timestamp: time.Now(),
	}
	if err != nil {
		result.Error = err.Error()
	}

	if o.onStageComplete != nil {
		o.onStageComplete(result)
	}

	return err
}

func (o *Orchestrator) executeMarketDiscovery(ctx context.Context) (interface{}, error) {
	// Fetch tradeable markets
	markets, err := o.gammaClient.ListTradeableMarkets(ctx, o.config.MaxMarkets*2, 0)
	if err != nil {
		return nil, fmt.Errorf("list markets failed: %w", err)
	}

	// Filter by volume and spread
	filtered := make([]gamma.Market, 0, o.config.MaxMarkets)
	for _, m := range markets {
		if m.Volume.Float64() < o.config.MinVolume.InexactFloat64() {
			continue
		}
		if decimal.NewFromFloat(m.Spread.Float64()).GreaterThan(o.config.MaxSpreadBps) {
			continue
		}

		filtered = append(filtered, m)
		if len(filtered) >= o.config.MaxMarkets {
			break
		}
	}

	o.mu.Lock()
	o.activeMarkets = filtered
	o.mu.Unlock()

	return map[string]interface{}{
		"total_fetched": len(markets),
		"filtered":      len(filtered),
	}, nil
}

func (o *Orchestrator) executeDataCollection(ctx context.Context) (interface{}, error) {
	o.mu.RLock()
	markets := o.activeMarkets
	o.mu.RUnlock()

	if len(markets) == 0 {
		return nil, nil
	}

	// Fetch orderbooks for active markets
	collected := 0
	for _, m := range markets {
		tokenID := m.YesTokenID()
		if tokenID == "" {
			continue
		}

		_, err := o.clobClient.GetOrderBook(ctx, tokenID)
		if err != nil {
			continue
		}
		collected++
	}

	return map[string]interface{}{
		"markets_collected": collected,
	}, nil
}

func (o *Orchestrator) executeForecasting(ctx context.Context) (interface{}, error) {
	o.mu.RLock()
	markets := o.activeMarkets
	o.mu.RUnlock()

	if len(markets) == 0 || o.forecaster == nil {
		return nil, nil
	}

	forecasted := 0
	for _, m := range markets {
		tokenID := m.YesTokenID()
		if tokenID == "" {
			continue
		}

		// Build context
		mktCtx := &agents.MarketContext{
			TokenID:      tokenID,
			Market:       m.ConditionID,
			Question:     m.Question,
			Description:  m.Description,
			CurrentPrice: decimal.NewFromFloat(m.YesPrice()),
			Volume24h:    decimal.NewFromFloat(m.Volume24hr.Float64()),
			EndDate:      m.EndDate,
		}

		// Get ensemble forecast
		forecast, err := o.forecaster.ForecastEnsemble(ctx, mktCtx)
		if err != nil {
			continue
		}

		o.mu.Lock()
		o.forecasts[tokenID] = forecast
		o.mu.Unlock()
		forecasted++
	}

	return map[string]interface{}{
		"markets_forecasted": forecasted,
	}, nil
}

func (o *Orchestrator) executeSignalGen(ctx context.Context) (interface{}, error) {
	o.mu.RLock()
	markets := o.activeMarkets
	forecasts := o.forecasts
	o.mu.RUnlock()

	signals := make([]*agents.TradingSignal, 0)

	for _, m := range markets {
		tokenID := m.YesTokenID()
		forecast, ok := forecasts[tokenID]
		if !ok {
			continue
		}

		signal := o.forecaster.GenerateSignal(
			forecast,
			decimal.NewFromFloat(m.YesPrice()),
			o.config.MinEdgeBps,
		)

		if signal.Signal == agents.SignalBuy &&
			signal.Forecast.Confidence.GreaterThanOrEqual(o.config.MinConfidence) {
			signals = append(signals, signal)

			if o.onSignal != nil {
				o.onSignal(signal)
			}
		}
	}

	// Rank signals by expected value
	signals = agents.RankSignals(signals)

	o.mu.Lock()
	o.signals = signals
	o.mu.Unlock()

	return map[string]interface{}{
		"signals_generated": len(signals),
	}, nil
}

func (o *Orchestrator) executeRiskCheck(ctx context.Context) (interface{}, error) {
	o.mu.RLock()
	signals := o.signals
	o.mu.RUnlock()

	if o.policyEngine == nil {
		return nil, nil
	}

	approved := 0
	for _, signal := range signals {
		if signal.Signal != agents.SignalBuy {
			continue
		}

		// Calculate order size
		size := o.config.MaxOrderSize
		price := signal.CurrentPrice
		if signal.Side == "NO" {
			price = decimal.NewFromInt(1).Sub(price)
		}

		err := o.policyEngine.CheckOrder(
			signal.TokenID,
			size,
			price,
			true, // isBuy
		)

		if err == nil {
			approved++
		}
	}

	return map[string]interface{}{
		"signals_checked": len(signals),
		"approved":        approved,
	}, nil
}

func (o *Orchestrator) executeOrderExecution(ctx context.Context) (interface{}, error) {
	o.mu.RLock()
	signals := o.signals
	o.mu.RUnlock()

	if len(signals) == 0 {
		return nil, nil
	}

	executed := 0
	for _, signal := range signals {
		if signal.Signal != agents.SignalBuy {
			continue
		}

		// Re-check risk
		if o.policyEngine != nil {
			size := o.config.MaxOrderSize
			price := signal.CurrentPrice
			if signal.Side == "NO" {
				price = decimal.NewFromInt(1).Sub(price)
			}

			if err := o.policyEngine.CheckOrder(signal.TokenID, size, price, true); err != nil {
				continue
			}
		}

		if o.config.UsePaperTrade && o.paperEngine != nil {
			// Paper trade
			var side paper.Side
			if signal.Side == "YES" {
				side = paper.SideBuy
			} else {
				side = paper.SideSell
			}

			req := &paper.OrderRequest{
				TokenID:   signal.TokenID,
				Side:      side,
				OrderType: paper.OrderTypeMarket,
				Size:      o.config.MaxOrderSize,
			}

			_, err := o.paperEngine.PlaceOrder(ctx, req)
			if err != nil {
				continue
			}
			executed++
		} else if o.clobClient != nil && o.clobClient.HasCredentials() {
			// Live trade
			var side clob.OrderSide
			tokenID := signal.TokenID
			if signal.Side == "YES" {
				side = clob.OrderSideBuy
			} else {
				side = clob.OrderSideSell
			}

			args := &clob.OrderArgs{
				TokenID: tokenID,
				Side:    side,
				Price:   signal.CurrentPrice.InexactFloat64(),
				Size:    o.config.MaxOrderSize.InexactFloat64(),
			}

			_, err := o.clobClient.CreateAndPostOrder(ctx, args, "0.01", false)
			if err != nil {
				continue
			}
			executed++
		}

		// Record with policy engine
		if o.policyEngine != nil {
			o.policyEngine.RecordOrder(signal.TokenID)
		}
	}

	return map[string]interface{}{
		"orders_executed": executed,
	}, nil
}

func (o *Orchestrator) executeMonitoring(ctx context.Context) (interface{}, error) {
	// Update prices if using paper trading
	if o.paperEngine != nil {
		o.paperEngine.UpdatePrices(ctx)
	}

	// Get stats
	var stats interface{}
	if o.paperEngine != nil {
		stats = o.paperEngine.GetStats()
	}
	if o.policyEngine != nil {
		stats = o.policyEngine.Status()
	}

	return stats, nil
}

func (o *Orchestrator) handleError(err error) {
	if o.onError != nil {
		o.onError(err)
	}
}

// Status returns the current orchestrator status.
type Status struct {
	Running       bool                 `json:"running"`
	ActiveMarkets int                  `json:"active_markets"`
	Forecasts     int                  `json:"forecasts"`
	Signals       int                  `json:"signals"`
	PolicyStatus  *policy.PolicyStatus `json:"policy_status,omitempty"`
	PaperStats    *paper.AccountStats  `json:"paper_stats,omitempty"`
}

// GetStatus returns the current status.
func (o *Orchestrator) GetStatus() *Status {
	o.mu.RLock()
	defer o.mu.RUnlock()

	status := &Status{
		Running:       o.running,
		ActiveMarkets: len(o.activeMarkets),
		Forecasts:     len(o.forecasts),
		Signals:       len(o.signals),
	}

	if o.policyEngine != nil {
		ps := o.policyEngine.Status()
		status.PolicyStatus = &ps
	}

	if o.paperEngine != nil {
		status.PaperStats = o.paperEngine.GetStats()
	}

	return status
}
