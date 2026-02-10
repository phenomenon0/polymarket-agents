// Package metrics provides Prometheus metrics for the trading system.
package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/shopspring/decimal"
)

// TradingMetrics collects and exposes trading-related Prometheus metrics.
type TradingMetrics struct {
	mu       sync.RWMutex
	registry *prometheus.Registry

	// Order metrics
	OrdersTotal   *prometheus.CounterVec
	OrderDuration *prometheus.HistogramVec
	OrderSize     *prometheus.HistogramVec
	OpenOrders    *prometheus.GaugeVec

	// Trade metrics
	TradesTotal   *prometheus.CounterVec
	TradeVolume   *prometheus.CounterVec
	TradeFees     *prometheus.CounterVec
	TradeSlippage *prometheus.HistogramVec

	// Position metrics
	PositionSize  *prometheus.GaugeVec
	PositionValue *prometheus.GaugeVec
	UnrealizedPnL *prometheus.GaugeVec
	RealizedPnL   *prometheus.CounterVec

	// Account metrics
	AccountBalance *prometheus.GaugeVec
	TotalExposure  *prometheus.GaugeVec
	DailyPnL       *prometheus.GaugeVec
	DrawdownPct    *prometheus.GaugeVec

	// Forecaster metrics
	ForecastsTotal       *prometheus.CounterVec
	ForecastLatency      *prometheus.HistogramVec
	ForecastConfidence   *prometheus.HistogramVec
	ForecastDisagreement *prometheus.HistogramVec
	LLMErrors            *prometheus.CounterVec

	// Signal metrics
	SignalsTotal   *prometheus.CounterVec
	SignalEdge     *prometheus.HistogramVec
	SignalStrength *prometheus.HistogramVec

	// Policy metrics
	PolicyViolations *prometheus.CounterVec
	CooldownActive   *prometheus.GaugeVec
	DailyOrdersUsed  *prometheus.GaugeVec
	DailyVolumeUsed  *prometheus.GaugeVec

	// Orchestrator metrics
	WorkflowRuns     *prometheus.CounterVec
	WorkflowDuration *prometheus.HistogramVec
	StageLatency     *prometheus.HistogramVec
	ActiveMarkets    *prometheus.GaugeVec
}

// NewTradingMetrics creates a new trading metrics collector.
func NewTradingMetrics() *TradingMetrics {
	registry := prometheus.NewRegistry()

	tm := &TradingMetrics{
		registry: registry,

		// Order metrics
		OrdersTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "polymarket_orders_total",
				Help: "Total number of orders placed",
			},
			[]string{"side", "type", "status"},
		),
		OrderDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "polymarket_order_duration_seconds",
				Help:    "Time from order placement to fill",
				Buckets: prometheus.ExponentialBuckets(0.001, 2, 15), // 1ms to ~32s
			},
			[]string{"side", "type"},
		),
		OrderSize: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "polymarket_order_size_usd",
				Help:    "Order size in USD",
				Buckets: []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000},
			},
			[]string{"side"},
		),
		OpenOrders: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "polymarket_open_orders",
				Help: "Current number of open orders",
			},
			[]string{"market"},
		),

		// Trade metrics
		TradesTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "polymarket_trades_total",
				Help: "Total number of trades executed",
			},
			[]string{"side", "market"},
		),
		TradeVolume: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "polymarket_trade_volume_usd",
				Help: "Total trading volume in USD",
			},
			[]string{"side"},
		),
		TradeFees: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "polymarket_trade_fees_usd",
				Help: "Total fees paid in USD",
			},
			[]string{},
		),
		TradeSlippage: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "polymarket_trade_slippage_bps",
				Help:    "Trade slippage in basis points",
				Buckets: []float64{0, 1, 2, 5, 10, 25, 50, 100, 200, 500},
			},
			[]string{"side"},
		),

		// Position metrics
		PositionSize: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "polymarket_position_size",
				Help: "Current position size (shares)",
			},
			[]string{"token_id", "market", "side"},
		),
		PositionValue: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "polymarket_position_value_usd",
				Help: "Current position value in USD",
			},
			[]string{"token_id", "market"},
		),
		UnrealizedPnL: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "polymarket_unrealized_pnl_usd",
				Help: "Unrealized P&L in USD",
			},
			[]string{"token_id", "market"},
		),
		RealizedPnL: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "polymarket_realized_pnl_usd",
				Help: "Realized P&L in USD (can be negative)",
			},
			[]string{"market"},
		),

		// Account metrics
		AccountBalance: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "polymarket_account_balance_usd",
				Help: "Current account balance in USD",
			},
			[]string{"account_type"},
		),
		TotalExposure: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "polymarket_total_exposure_usd",
				Help: "Total market exposure in USD",
			},
			[]string{},
		),
		DailyPnL: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "polymarket_daily_pnl_usd",
				Help: "Today's P&L in USD",
			},
			[]string{},
		),
		DrawdownPct: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "polymarket_drawdown_pct",
				Help: "Current drawdown percentage from peak",
			},
			[]string{},
		),

		// Forecaster metrics
		ForecastsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "polymarket_forecasts_total",
				Help: "Total number of forecasts made",
			},
			[]string{"provider", "status"},
		),
		ForecastLatency: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "polymarket_forecast_latency_seconds",
				Help:    "LLM forecast latency in seconds",
				Buckets: prometheus.ExponentialBuckets(0.1, 2, 10), // 100ms to ~100s
			},
			[]string{"provider"},
		),
		ForecastConfidence: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "polymarket_forecast_confidence",
				Help:    "LLM forecast confidence (0-1)",
				Buckets: prometheus.LinearBuckets(0, 0.1, 11), // 0, 0.1, 0.2, ..., 1.0
			},
			[]string{"provider"},
		),
		ForecastDisagreement: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "polymarket_forecast_disagreement",
				Help:    "Ensemble forecast disagreement (std dev)",
				Buckets: prometheus.LinearBuckets(0, 0.05, 11), // 0 to 0.5
			},
			[]string{},
		),
		LLMErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "polymarket_llm_errors_total",
				Help: "Total number of LLM errors",
			},
			[]string{"provider", "error_type"},
		),

		// Signal metrics
		SignalsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "polymarket_signals_total",
				Help: "Total number of trading signals generated",
			},
			[]string{"signal", "side"},
		),
		SignalEdge: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "polymarket_signal_edge_bps",
				Help:    "Trading signal edge in basis points",
				Buckets: []float64{0, 25, 50, 100, 150, 200, 300, 500, 1000},
			},
			[]string{"side"},
		),
		SignalStrength: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "polymarket_signal_strength",
				Help:    "Trading signal strength (0-1)",
				Buckets: prometheus.LinearBuckets(0, 0.1, 11),
			},
			[]string{},
		),

		// Policy metrics
		PolicyViolations: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "polymarket_policy_violations_total",
				Help: "Total number of policy violations",
			},
			[]string{"violation_type"},
		),
		CooldownActive: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "polymarket_cooldown_active",
				Help: "Whether cooldown is currently active (1=yes, 0=no)",
			},
			[]string{},
		),
		DailyOrdersUsed: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "polymarket_daily_orders_used",
				Help: "Number of orders placed today",
			},
			[]string{},
		),
		DailyVolumeUsed: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "polymarket_daily_volume_used_usd",
				Help: "Volume traded today in USD",
			},
			[]string{},
		),

		// Orchestrator metrics
		WorkflowRuns: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "polymarket_workflow_runs_total",
				Help: "Total number of workflow runs",
			},
			[]string{"status"},
		),
		WorkflowDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "polymarket_workflow_duration_seconds",
				Help:    "Total workflow run duration",
				Buckets: prometheus.ExponentialBuckets(0.1, 2, 12), // 100ms to ~400s
			},
			[]string{},
		),
		StageLatency: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "polymarket_stage_latency_seconds",
				Help:    "Individual stage latency",
				Buckets: prometheus.ExponentialBuckets(0.01, 2, 12), // 10ms to ~40s
			},
			[]string{"stage"},
		),
		ActiveMarkets: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "polymarket_active_markets",
				Help: "Number of markets being tracked",
			},
			[]string{},
		),
	}

	// Register all metrics
	tm.registerAll()

	return tm
}

func (tm *TradingMetrics) registerAll() {
	tm.registry.MustRegister(
		tm.OrdersTotal,
		tm.OrderDuration,
		tm.OrderSize,
		tm.OpenOrders,
		tm.TradesTotal,
		tm.TradeVolume,
		tm.TradeFees,
		tm.TradeSlippage,
		tm.PositionSize,
		tm.PositionValue,
		tm.UnrealizedPnL,
		tm.RealizedPnL,
		tm.AccountBalance,
		tm.TotalExposure,
		tm.DailyPnL,
		tm.DrawdownPct,
		tm.ForecastsTotal,
		tm.ForecastLatency,
		tm.ForecastConfidence,
		tm.ForecastDisagreement,
		tm.LLMErrors,
		tm.SignalsTotal,
		tm.SignalEdge,
		tm.SignalStrength,
		tm.PolicyViolations,
		tm.CooldownActive,
		tm.DailyOrdersUsed,
		tm.DailyVolumeUsed,
		tm.WorkflowRuns,
		tm.WorkflowDuration,
		tm.StageLatency,
		tm.ActiveMarkets,
	)
}

// Registry returns the prometheus registry.
func (tm *TradingMetrics) Registry() *prometheus.Registry {
	return tm.registry
}

// --- Helper methods for recording metrics ---

// RecordOrder records an order placement.
func (tm *TradingMetrics) RecordOrder(side, orderType, status string, sizeUSD float64) {
	tm.OrdersTotal.WithLabelValues(side, orderType, status).Inc()
	if sizeUSD > 0 {
		tm.OrderSize.WithLabelValues(side).Observe(sizeUSD)
	}
}

// RecordOrderFill records an order fill.
func (tm *TradingMetrics) RecordOrderFill(side, orderType string, durationSec float64) {
	tm.OrderDuration.WithLabelValues(side, orderType).Observe(durationSec)
}

// RecordTrade records a completed trade.
func (tm *TradingMetrics) RecordTrade(side, market string, volumeUSD, feeUSD, slippageBps float64) {
	tm.TradesTotal.WithLabelValues(side, market).Inc()
	tm.TradeVolume.WithLabelValues(side).Add(volumeUSD)
	tm.TradeFees.WithLabelValues().Add(feeUSD)
	if slippageBps >= 0 {
		tm.TradeSlippage.WithLabelValues(side).Observe(slippageBps)
	}
}

// UpdatePosition updates position metrics.
func (tm *TradingMetrics) UpdatePosition(tokenID, market, side string, size, valueUSD, unrealizedPnL float64) {
	tm.PositionSize.WithLabelValues(tokenID, market, side).Set(size)
	tm.PositionValue.WithLabelValues(tokenID, market).Set(valueUSD)
	tm.UnrealizedPnL.WithLabelValues(tokenID, market).Set(unrealizedPnL)
}

// RecordRealizedPnL records realized P&L.
func (tm *TradingMetrics) RecordRealizedPnL(market string, pnlUSD float64) {
	tm.RealizedPnL.WithLabelValues(market).Add(pnlUSD)
}

// UpdateAccount updates account metrics.
func (tm *TradingMetrics) UpdateAccount(accountType string, balance, exposure, dailyPnL, drawdown float64) {
	tm.AccountBalance.WithLabelValues(accountType).Set(balance)
	tm.TotalExposure.WithLabelValues().Set(exposure)
	tm.DailyPnL.WithLabelValues().Set(dailyPnL)
	tm.DrawdownPct.WithLabelValues().Set(drawdown)
}

// RecordForecast records a forecast.
func (tm *TradingMetrics) RecordForecast(provider, status string, latencySec, confidence float64) {
	tm.ForecastsTotal.WithLabelValues(provider, status).Inc()
	if latencySec > 0 {
		tm.ForecastLatency.WithLabelValues(provider).Observe(latencySec)
	}
	if confidence >= 0 {
		tm.ForecastConfidence.WithLabelValues(provider).Observe(confidence)
	}
}

// RecordEnsembleForecast records an ensemble forecast.
func (tm *TradingMetrics) RecordEnsembleForecast(disagreement float64) {
	tm.ForecastDisagreement.WithLabelValues().Observe(disagreement)
}

// RecordLLMError records an LLM error.
func (tm *TradingMetrics) RecordLLMError(provider, errorType string) {
	tm.LLMErrors.WithLabelValues(provider, errorType).Inc()
}

// RecordSignal records a trading signal.
func (tm *TradingMetrics) RecordSignal(signal, side string, edgeBps, strength float64) {
	tm.SignalsTotal.WithLabelValues(signal, side).Inc()
	tm.SignalEdge.WithLabelValues(side).Observe(edgeBps)
	tm.SignalStrength.WithLabelValues().Observe(strength)
}

// RecordPolicyViolation records a policy violation.
func (tm *TradingMetrics) RecordPolicyViolation(violationType string) {
	tm.PolicyViolations.WithLabelValues(violationType).Inc()
}

// UpdatePolicy updates policy metrics.
func (tm *TradingMetrics) UpdatePolicy(cooldownActive bool, dailyOrders int, dailyVolumeUSD float64) {
	if cooldownActive {
		tm.CooldownActive.WithLabelValues().Set(1)
	} else {
		tm.CooldownActive.WithLabelValues().Set(0)
	}
	tm.DailyOrdersUsed.WithLabelValues().Set(float64(dailyOrders))
	tm.DailyVolumeUsed.WithLabelValues().Set(dailyVolumeUSD)
}

// RecordWorkflow records a workflow run.
func (tm *TradingMetrics) RecordWorkflow(status string, durationSec float64) {
	tm.WorkflowRuns.WithLabelValues(status).Inc()
	if durationSec > 0 {
		tm.WorkflowDuration.WithLabelValues().Observe(durationSec)
	}
}

// RecordStage records a stage execution.
func (tm *TradingMetrics) RecordStage(stage string, durationSec float64) {
	tm.StageLatency.WithLabelValues(stage).Observe(durationSec)
}

// UpdateActiveMarkets updates the active markets count.
func (tm *TradingMetrics) UpdateActiveMarkets(count int) {
	tm.ActiveMarkets.WithLabelValues().Set(float64(count))
}

// --- Decimal helpers ---

// DecimalToFloat64 safely converts decimal.Decimal to float64 for metrics.
func DecimalToFloat64(d decimal.Decimal) float64 {
	f, _ := d.Float64()
	return f
}

// Global instance for convenience
var defaultMetrics *TradingMetrics
var once sync.Once

// Default returns the default global metrics instance.
func Default() *TradingMetrics {
	once.Do(func() {
		defaultMetrics = NewTradingMetrics()
	})
	return defaultMetrics
}
