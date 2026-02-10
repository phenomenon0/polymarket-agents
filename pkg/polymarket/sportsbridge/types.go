// Package sportsbridge bridges sports prediction models (like MathShard) with
// Polymarket's binary contract market structure.
//
// Key abstractions:
//   - EventSpec: typed representation of what a contract resolves on
//   - AlphaProvider: interface for probability sources (MathShard, LLM, ensemble)
//   - FeeModel: pluggable fee calculation
//   - EdgeCalculator: VWAP-based edge with correct Kelly
package sportsbridge

import (
	"time"
)

// EventSpec is a typed representation of what a Polymarket contract resolves on.
// This replaces string-based matching with structured event definitions.
type EventSpec interface {
	// Category returns the event category (e.g., "soccer", "politics")
	Category() string
	// Key returns a unique identifier for the event (for caching/dedup)
	Key() string
}

// Soccer1X2Event represents a soccer 1X2 outcome event.
type Soccer1X2Event struct {
	League    string    // e.g., "epl", "la_liga"
	HomeTeam  string    // Canonical team name
	AwayTeam  string    // Canonical team name
	MatchDate time.Time // Match date (kickoff)
	Outcome   Outcome   // HOME, DRAW, AWAY
	IsYesSide bool      // True if this is the YES contract, false for NO
}

func (e Soccer1X2Event) Category() string {
	return "soccer"
}

func (e Soccer1X2Event) Key() string {
	return e.League + "_" + e.MatchDate.Format("2006-01-02") + "_" + e.HomeTeam + "_" + e.AwayTeam + "_" + string(e.Outcome)
}

// Outcome represents the 1X2 outcome type.
type Outcome string

const (
	OutcomeHome Outcome = "HOME"
	OutcomeDraw Outcome = "DRAW"
	OutcomeAway Outcome = "AWAY"
)

// Contract represents a Polymarket contract with parsed event semantics.
type Contract struct {
	// Polymarket identifiers
	MarketID    string // Polymarket market ID
	TokenID     string // CLOB token ID for YES side
	ConditionID string // CTF condition ID
	Slug        string // Market slug
	Question    string // Human-readable question

	// Parsed event semantics
	Event    EventSpec // Typed event this contract resolves on
	Category string    // "soccer", "politics", etc.

	// Book state (populated at scoring time)
	BestBid float64 // Best bid price (0-1)
	BestAsk float64 // Best ask price (0-1)
	MidPx   float64 // Mid price

	// Metadata
	Closed    bool
	EndDate   time.Time
	Liquidity float64
}

// Prob3 holds 3-way probabilities (must sum to 1).
type Prob3 struct {
	Home float64
	Draw float64
	Away float64
}

// Normalize ensures probabilities sum to 1.
func (p Prob3) Normalize() Prob3 {
	sum := p.Home + p.Draw + p.Away
	if sum == 0 {
		return Prob3{Home: 1.0 / 3, Draw: 1.0 / 3, Away: 1.0 / 3}
	}
	return Prob3{
		Home: p.Home / sum,
		Draw: p.Draw / sum,
		Away: p.Away / sum,
	}
}

// ProbFor returns the probability for a specific outcome.
func (p Prob3) ProbFor(o Outcome) float64 {
	switch o {
	case OutcomeHome:
		return p.Home
	case OutcomeDraw:
		return p.Draw
	case OutcomeAway:
		return p.Away
	default:
		return 0
	}
}

// ScoreResult contains the alpha provider's output.
type ScoreResult struct {
	Q          float64        // Model probability for this contract's YES outcome
	Confidence float64        // Model confidence (0-1), optional
	Aux        map[string]any // Additional metadata (model version, features, etc.)
}

// EdgeResult contains the calculated edge and sizing.
type EdgeResult struct {
	// Inputs
	Q        float64 // Model probability
	PriceEff float64 // Effective price (VWAP + fees)
	PriceMid float64 // Mid price (for reference)

	// Edge calculation
	EdgeRaw float64 // q - p_eff (raw edge)
	EdgeBps float64 // Edge in basis points

	// Kelly sizing
	KellyFrac   float64 // Optimal Kelly fraction: (q - p) / (1 - p)
	KellyCapped float64 // After applying Kelly exponent and caps

	// Suggested trade
	SuggestedSize float64 // Dollar amount to trade
	IsValueBet    bool    // True if edge > threshold

	// Metadata
	FeePaid  float64 // Estimated fee
	Slippage float64 // VWAP slippage from best price
}

// OrderSide represents buy or sell.
type OrderSide string

const (
	OrderSideBuy  OrderSide = "BUY"
	OrderSideSell OrderSide = "SELL"
)

// Signal represents a trading signal ready for execution.
type Signal struct {
	Contract *Contract
	Score    *ScoreResult
	Edge     *EdgeResult

	// Trade parameters
	Side       OrderSide
	LimitPrice float64
	Size       float64 // In shares
	SizeUSD    float64 // Dollar equivalent

	// Risk checks
	PassedPolicy bool
	RejectReason string

	// Timestamps
	ScoredAt  time.Time
	ExpiresAt time.Time // Don't trade after this (e.g., kickoff - 5min)
}
