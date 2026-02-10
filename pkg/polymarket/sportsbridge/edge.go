package sportsbridge

import (
	"context"
	"fmt"
	"math"
	"strconv"

	"github.com/phenomenon0/polymarket-agents/pkg/polymarket/clob"
)

// FeeModel calculates trading fees.
type FeeModel interface {
	// CalcFee returns the fee for a given order.
	// price is 0-1, size is in shares, side is BUY or SELL.
	CalcFee(price, size float64, side OrderSide) float64
}

// ZeroFeeModel implements FeeModel with no fees.
type ZeroFeeModel struct{}

func (m ZeroFeeModel) CalcFee(price, size float64, side OrderSide) float64 {
	return 0
}

// BpsFeeModel implements FeeModel with basis point fees.
type BpsFeeModel struct {
	TakerBps float64 // Taker fee in basis points (e.g., 10 = 0.10%)
	MakerBps float64 // Maker fee in basis points
}

func (m BpsFeeModel) CalcFee(price, size float64, side OrderSide) float64 {
	// Assume taker for now (market orders)
	notional := price * size
	return notional * (m.TakerBps / 10000)
}

// EdgeConfig configures edge calculation.
type EdgeConfig struct {
	// Fee model
	FeeModel FeeModel

	// Kelly parameters
	KellyExponent float64 // Apply pow(kelly, exponent) for fractional Kelly (e.g., 0.5)
	KellyCap      float64 // Maximum Kelly fraction (e.g., 0.10 = 10%)

	// Edge thresholds
	MinEdgeBps float64 // Minimum edge in bps to consider a value bet

	// Sizing
	DefaultSizeUSD float64 // Default target size for VWAP calculation
	Bankroll       float64 // Total bankroll for Kelly sizing

	// Liquidity
	MinLiquidity float64 // Minimum depth (in $) at best level
}

// DefaultEdgeConfig returns sensible defaults.
func DefaultEdgeConfig() *EdgeConfig {
	return &EdgeConfig{
		FeeModel:       ZeroFeeModel{},
		KellyExponent:  0.25, // Quarter Kelly
		KellyCap:       0.05, // Max 5% of bankroll
		MinEdgeBps:     200,  // 2% minimum edge
		DefaultSizeUSD: 100,  // $100 target size for VWAP
		Bankroll:       1000, // $1000 default bankroll
		MinLiquidity:   50,   // $50 minimum depth
	}
}

// EdgeCalculator calculates edge and sizing.
type EdgeCalculator struct {
	cfg        *EdgeConfig
	clobClient *clob.Client
}

// NewEdgeCalculator creates an edge calculator.
func NewEdgeCalculator(cfg *EdgeConfig, clobClient *clob.Client) *EdgeCalculator {
	if cfg == nil {
		cfg = DefaultEdgeConfig()
	}
	return &EdgeCalculator{
		cfg:        cfg,
		clobClient: clobClient,
	}
}

// BookLevel represents a price level in the order book.
type BookLevel struct {
	Price float64
	Size  float64
}

// CalculateVWAP computes the volume-weighted average price for a given size.
// side: BUY = walk up asks, SELL = walk down bids.
func CalculateVWAP(levels []BookLevel, targetSize float64) (vwap float64, filledSize float64, err error) {
	if len(levels) == 0 {
		return 0, 0, fmt.Errorf("empty order book")
	}

	if targetSize <= 0 {
		return levels[0].Price, 0, nil
	}

	var totalCost float64
	remaining := targetSize

	for _, level := range levels {
		if remaining <= 0 {
			break
		}

		fillSize := math.Min(level.Size, remaining)
		totalCost += level.Price * fillSize
		filledSize += fillSize
		remaining -= fillSize
	}

	if filledSize == 0 {
		return 0, 0, fmt.Errorf("no liquidity")
	}

	vwap = totalCost / filledSize
	return vwap, filledSize, nil
}

// FetchBookAndVWAP fetches the order book and calculates VWAP for buying YES.
func (e *EdgeCalculator) FetchBookAndVWAP(ctx context.Context, tokenID string, targetSizeUSD float64) (vwap, bestAsk, depth float64, err error) {
	if e.clobClient == nil {
		return 0, 0, 0, fmt.Errorf("no CLOB client")
	}

	book, err := e.clobClient.GetOrderBook(ctx, tokenID)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("fetch order book: %w", err)
	}

	if len(book.Asks) == 0 {
		return 0, 0, 0, fmt.Errorf("no asks in order book")
	}

	// Convert asks to BookLevel
	var asks []BookLevel
	var totalDepth float64
	for _, ask := range book.Asks {
		price, _ := strconv.ParseFloat(ask.Price, 64)
		size, _ := strconv.ParseFloat(ask.Size, 64)
		asks = append(asks, BookLevel{Price: price, Size: size})
		totalDepth += price * size // Depth in USD
	}

	// Best ask
	bestAsk = asks[0].Price

	// Target size in shares: USD / price (approximate using best ask)
	targetShares := targetSizeUSD / bestAsk

	// Calculate VWAP
	vwap, _, err = CalculateVWAP(asks, targetShares)
	if err != nil {
		return 0, bestAsk, totalDepth, err
	}

	return vwap, bestAsk, totalDepth, nil
}

// Calculate computes edge and sizing for a contract.
func (e *EdgeCalculator) Calculate(ctx context.Context, c *Contract, score *ScoreResult) (*EdgeResult, error) {
	if score == nil {
		return nil, fmt.Errorf("no score provided")
	}

	q := score.Q

	// Get effective price via VWAP
	targetSize := e.cfg.DefaultSizeUSD
	vwap, bestAsk, depth, err := e.FetchBookAndVWAP(ctx, c.TokenID, targetSize)
	if err != nil {
		// Fall back to mid price if available
		if c.MidPx > 0 {
			vwap = c.MidPx
			bestAsk = c.BestAsk
		} else if c.BestAsk > 0 {
			vwap = c.BestAsk
			bestAsk = c.BestAsk
		} else {
			return nil, fmt.Errorf("no price available: %w", err)
		}
	}

	// Calculate slippage
	slippage := vwap - bestAsk

	// Apply fee model
	// For buying YES: cost = price * shares, fee on notional
	fee := e.cfg.FeeModel.CalcFee(vwap, targetSize/vwap, OrderSideBuy)

	// Effective price includes slippage + fee (as % of notional)
	feeRate := 0.0
	if targetSize > 0 {
		feeRate = fee / targetSize
	}
	pEff := vwap + feeRate

	// Edge calculation
	edgeRaw := q - pEff
	edgeBps := edgeRaw * 10000

	// Kelly sizing: f* = (q - p) / (1 - p)
	// This is the correct formula for "pay p to win 1"
	kellyFrac := 0.0
	if pEff < 1 {
		kellyFrac = (q - pEff) / (1 - pEff)
	}

	// Apply Kelly exponent (fractional Kelly)
	if kellyFrac > 0 && e.cfg.KellyExponent > 0 && e.cfg.KellyExponent < 1 {
		kellyFrac = math.Pow(kellyFrac, 1/e.cfg.KellyExponent) * e.cfg.KellyExponent
		// Actually, fractional Kelly is simpler: just multiply
		kellyFrac = (q - pEff) / (1 - pEff) * e.cfg.KellyExponent
	}

	// Cap Kelly
	kellyCapped := math.Min(kellyFrac, e.cfg.KellyCap)
	if kellyCapped < 0 {
		kellyCapped = 0
	}

	// Suggested size
	suggestedSize := e.cfg.Bankroll * kellyCapped

	// Is this a value bet?
	isValueBet := edgeBps >= e.cfg.MinEdgeBps && suggestedSize > 0 && depth >= e.cfg.MinLiquidity

	result := &EdgeResult{
		Q:             q,
		PriceEff:      pEff,
		PriceMid:      (bestAsk + vwap) / 2, // Approximate
		EdgeRaw:       edgeRaw,
		EdgeBps:       edgeBps,
		KellyFrac:     (q - pEff) / (1 - pEff), // Raw Kelly
		KellyCapped:   kellyCapped,
		SuggestedSize: suggestedSize,
		IsValueBet:    isValueBet,
		FeePaid:       fee,
		Slippage:      slippage,
	}

	return result, nil
}

// CalculateSimple computes edge without CLOB lookup (uses contract's book state).
func (e *EdgeCalculator) CalculateSimple(c *Contract, score *ScoreResult) (*EdgeResult, error) {
	if score == nil {
		return nil, fmt.Errorf("no score provided")
	}

	q := score.Q

	// Use contract's book state
	pEff := c.BestAsk
	if pEff == 0 {
		pEff = c.MidPx
	}
	if pEff == 0 {
		return nil, fmt.Errorf("no price in contract")
	}

	// Edge calculation
	edgeRaw := q - pEff
	edgeBps := edgeRaw * 10000

	// Kelly sizing
	kellyFrac := 0.0
	if pEff < 1 {
		kellyFrac = (q - pEff) / (1 - pEff)
	}

	// Apply fractional Kelly
	kellyCapped := kellyFrac * e.cfg.KellyExponent
	kellyCapped = math.Min(kellyCapped, e.cfg.KellyCap)
	if kellyCapped < 0 {
		kellyCapped = 0
	}

	suggestedSize := e.cfg.Bankroll * kellyCapped
	isValueBet := edgeBps >= e.cfg.MinEdgeBps && suggestedSize > 0

	return &EdgeResult{
		Q:             q,
		PriceEff:      pEff,
		PriceMid:      c.MidPx,
		EdgeRaw:       edgeRaw,
		EdgeBps:       edgeBps,
		KellyFrac:     kellyFrac,
		KellyCapped:   kellyCapped,
		SuggestedSize: suggestedSize,
		IsValueBet:    isValueBet,
		FeePaid:       0,
		Slippage:      0,
	}, nil
}
