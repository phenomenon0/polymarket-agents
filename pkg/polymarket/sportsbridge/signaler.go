package sportsbridge

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/phenomenon0/polymarket-agents/pkg/polymarket/clob"
	"github.com/phenomenon0/polymarket-agents/pkg/polymarket/gamma"
)

// Signaler generates trading signals using the sportsbridge architecture.
type Signaler struct {
	parser     *Parser
	provider   *CalibratedProvider
	msProvider *MathShardProvider
	edgeCalc   *EdgeCalculator
	gamma      *gamma.Client
	clob       *clob.Client
	cfg        *SignalerConfig
}

// SignalerConfig configures the signaler.
type SignalerConfig struct {
	MathShardURL   string
	Bankroll       float64
	MinEdgeBps     float64
	KellyExponent  float64
	KellyCap       float64
	DefaultSizeUSD float64

	// Model selection (v0, v0blend, mathshard)
	Model      ModelMode
	BlendAlpha float64 // only used for v0blend (0 = market, 1 = mathshard)
}

// DefaultSignalerConfig returns sensible defaults.
// Default is v0blend with alpha=0.10 (safe, recommended).
func DefaultSignalerConfig() *SignalerConfig {
	return &SignalerConfig{
		MathShardURL:   "http://localhost:8081",
		Bankroll:       1000,
		MinEdgeBps:     200,
		KellyExponent:  0.25,
		KellyCap:       0.05,
		DefaultSizeUSD: 100,
		Model:          ModelV0Blend, // Safe default
		BlendAlpha:     0.10,
	}
}

// NewSignaler creates a new signaler.
func NewSignaler(cfg *SignalerConfig) *Signaler {
	if cfg == nil {
		cfg = DefaultSignalerConfig()
	}

	// Create MathShard provider
	msProvider := NewMathShardProvider(cfg.MathShardURL)

	// Create calibrated provider with selected mode
	provider := NewCalibratedProvider(cfg.Model, cfg.BlendAlpha, msProvider)

	// Create edge calculator
	edgeCfg := &EdgeConfig{
		FeeModel:       ZeroFeeModel{},
		KellyExponent:  cfg.KellyExponent,
		KellyCap:       cfg.KellyCap,
		MinEdgeBps:     cfg.MinEdgeBps,
		DefaultSizeUSD: cfg.DefaultSizeUSD,
		Bankroll:       cfg.Bankroll,
		MinLiquidity:   50,
	}
	clobClient := clob.NewPublicClient()
	edgeCalc := NewEdgeCalculator(edgeCfg, clobClient)

	return &Signaler{
		parser:     NewParser(),
		provider:   provider,
		msProvider: msProvider,
		edgeCalc:   edgeCalc,
		gamma:      gamma.NewClient(),
		clob:       clobClient,
		cfg:        cfg,
	}
}

// SignalOutput represents the output for a single signal.
type SignalOutput struct {
	MatchKey  string
	HomeTeam  string
	AwayTeam  string
	MatchDate time.Time
	Outcome   Outcome

	// Prices
	ModelProb  float64
	MarketProb float64

	// Edge
	EdgeBps    float64
	IsValueBet bool

	// Sizing
	KellyFrac     float64
	SuggestedSize float64

	// Action
	Action string // "BUY YES @ 0.xx"

	// Metadata
	Slug    string
	TokenID string
}

// GenerateSignals generates trading signals for a league.
func (s *Signaler) GenerateSignals(ctx context.Context, league string) ([]SignalOutput, error) {
	// Get league tag ID
	leagueTagIDs := map[string]string{
		"epl":        "82",
		"la_liga":    "780",
		"bundesliga": "1494",
		"serie_a":    "101962",
		"ligue_1":    "102070",
		"ucl":        "100977",
	}

	tagID, ok := leagueTagIDs[league]
	if !ok {
		return nil, fmt.Errorf("unknown league: %s", league)
	}

	// Fetch events
	events, err := s.gamma.ListEvents(ctx, &gamma.EventsFilter{
		TagID:  tagID,
		Closed: gamma.BoolPtr(false),
		Limit:  100,
	})
	if err != nil {
		return nil, fmt.Errorf("fetch events: %w", err)
	}

	// Parse all contracts
	var allContracts []*Contract
	for _, event := range events {
		contracts, err := s.parser.ParseGammaEvent(&event, league)
		if err != nil {
			continue
		}
		allContracts = append(allContracts, contracts...)
	}

	// Group contracts by match (needed for 3-way normalization)
	matchGroups := GroupedByMatch(allContracts)

	// Set match groups on the provider for 3-way normalization
	s.provider.SetMatchGroups(matchGroups)

	// Generate signals
	var signals []SignalOutput

	for _, contract := range allContracts {
		// Skip if provider can't score
		if !s.provider.CanScore(contract) {
			continue
		}

		// Get calibrated probability
		score, err := s.provider.Score(ctx, contract)
		if err != nil {
			continue
		}

		// Calculate edge
		edge, err := s.edgeCalc.CalculateSimple(contract, score)
		if err != nil {
			continue
		}

		// Extract event info
		event, ok := contract.Event.(Soccer1X2Event)
		if !ok {
			continue
		}

		// Build output
		output := SignalOutput{
			MatchKey:      MatchKey(contract),
			HomeTeam:      event.HomeTeam,
			AwayTeam:      event.AwayTeam,
			MatchDate:     event.MatchDate,
			Outcome:       event.Outcome,
			ModelProb:     score.Q,
			MarketProb:    contract.MidPx,
			EdgeBps:       edge.EdgeBps,
			IsValueBet:    edge.IsValueBet,
			KellyFrac:     edge.KellyCapped,
			SuggestedSize: edge.SuggestedSize,
			Slug:          contract.Slug,
			TokenID:       contract.TokenID,
		}

		if edge.IsValueBet {
			output.Action = fmt.Sprintf("BUY YES @ %.2f", contract.BestAsk)
		} else {
			output.Action = "NO TRADE"
		}

		signals = append(signals, output)
	}

	// Sort by edge (descending)
	sort.Slice(signals, func(i, j int) bool {
		return signals[i].EdgeBps > signals[j].EdgeBps
	})

	return signals, nil
}

// ModelName returns the name of the model being used.
func (s *Signaler) ModelName() string {
	return s.provider.Name()
}

// GenerateValueBets returns only signals that pass the value threshold.
func (s *Signaler) GenerateValueBets(ctx context.Context, league string) ([]SignalOutput, error) {
	all, err := s.GenerateSignals(ctx, league)
	if err != nil {
		return nil, err
	}

	var valueBets []SignalOutput
	for _, sig := range all {
		if sig.IsValueBet {
			valueBets = append(valueBets, sig)
		}
	}

	return valueBets, nil
}
