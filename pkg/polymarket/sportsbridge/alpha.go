package sportsbridge

import (
	"context"
	"fmt"

	"github.com/phenomenon0/polymarket-agents/pkg/polymarket/sports"
)

// AlphaProvider is the interface for probability sources.
// Implementations include MathShard, LLMs, ensembles, etc.
type AlphaProvider interface {
	// Name returns the provider name (for logging/debugging).
	Name() string

	// CanScore returns true if this provider can score the given contract.
	CanScore(c *Contract) bool

	// Score returns the model probability for the contract's YES outcome.
	Score(ctx context.Context, c *Contract) (*ScoreResult, error)
}

// MathShardProvider implements AlphaProvider using MathShard for soccer 1X2.
type MathShardProvider struct {
	client *sports.MathShardClient

	// Cache for 3-way probs (keyed by match_uid)
	cache map[string]*Prob3
}

// NewMathShardProvider creates a MathShard alpha provider.
func NewMathShardProvider(mathshardURL string) *MathShardProvider {
	return &MathShardProvider{
		client: sports.NewMathShardClient(mathshardURL),
		cache:  make(map[string]*Prob3),
	}
}

func (p *MathShardProvider) Name() string {
	return "mathshard"
}

// CanScore returns true for soccer 1X2 contracts.
func (p *MathShardProvider) CanScore(c *Contract) bool {
	if c == nil || c.Event == nil {
		return false
	}

	// Only handle soccer 1X2 events
	_, ok := c.Event.(Soccer1X2Event)
	return ok
}

// Score returns the MathShard probability for this contract.
func (p *MathShardProvider) Score(ctx context.Context, c *Contract) (*ScoreResult, error) {
	event, ok := c.Event.(Soccer1X2Event)
	if !ok {
		return nil, fmt.Errorf("not a soccer 1X2 event")
	}

	// Check cache first
	matchKey := event.League + "_" + event.MatchDate.Format("2006-01-02") + "_" + event.HomeTeam + "_" + event.AwayTeam
	probs, ok := p.cache[matchKey]
	if !ok {
		// Fetch from MathShard
		pred, err := p.client.Predict(ctx, &sports.PredictRequest{
			League:   event.League,
			HomeTeam: event.HomeTeam,
			AwayTeam: event.AwayTeam,
		})
		if err != nil {
			return nil, fmt.Errorf("mathshard predict: %w", err)
		}

		probs = &Prob3{
			Home: pred.HomeProb.InexactFloat64(),
			Draw: pred.DrawProb.InexactFloat64(),
			Away: pred.AwayProb.InexactFloat64(),
		}
		p.cache[matchKey] = probs
	}

	// Get probability for this specific outcome
	var q float64
	switch event.Outcome {
	case OutcomeHome:
		q = probs.Home
	case OutcomeDraw:
		q = probs.Draw
	case OutcomeAway:
		q = probs.Away
	default:
		return nil, fmt.Errorf("unknown outcome: %s", event.Outcome)
	}

	// For NO side, q = 1 - q (complement)
	// But we decided to only evaluate YES sides in V1
	if !event.IsYesSide {
		q = 1 - q
	}

	return &ScoreResult{
		Q:          q,
		Confidence: 1.0, // MathShard doesn't provide confidence
		Aux: map[string]any{
			"provider":  "mathshard",
			"home_prob": probs.Home,
			"draw_prob": probs.Draw,
			"away_prob": probs.Away,
			"match_key": matchKey,
		},
	}, nil
}

// ClearCache clears the prediction cache.
func (p *MathShardProvider) ClearCache() {
	p.cache = make(map[string]*Prob3)
}

// Prob3 implements Prob3Source for use with CalibratedProvider.
// It returns the 3-way MathShard probabilities for a match.
func (p *MathShardProvider) Prob3(ctx context.Context, mc *MatchContracts) (Prob3, error) {
	if mc == nil {
		return Prob3{}, fmt.Errorf("nil match contracts")
	}

	// Check cache first
	matchKey := mc.MatchKey
	probs, ok := p.cache[matchKey]
	if ok {
		return *probs, nil
	}

	// Fetch from MathShard
	pred, err := p.client.Predict(ctx, &sports.PredictRequest{
		League:   mc.League,
		HomeTeam: mc.HomeTeam,
		AwayTeam: mc.AwayTeam,
	})
	if err != nil {
		return Prob3{}, fmt.Errorf("mathshard predict: %w", err)
	}

	probs = &Prob3{
		Home: pred.HomeProb.InexactFloat64(),
		Draw: pred.DrawProb.InexactFloat64(),
		Away: pred.AwayProb.InexactFloat64(),
	}

	// Normalize (MathShard should already sum to 1, but be safe)
	normalized := probs.Normalize()
	p.cache[matchKey] = &normalized

	return normalized, nil
}

// AlphaRegistry selects the appropriate provider for a contract.
type AlphaRegistry struct {
	providers []AlphaProvider
	fallback  AlphaProvider // Optional fallback (e.g., LLM)
}

// NewAlphaRegistry creates a registry with the given providers.
func NewAlphaRegistry(providers ...AlphaProvider) *AlphaRegistry {
	return &AlphaRegistry{
		providers: providers,
	}
}

// SetFallback sets a fallback provider for contracts no provider can score.
func (r *AlphaRegistry) SetFallback(p AlphaProvider) {
	r.fallback = p
}

// GetProvider returns the first provider that can score the contract.
func (r *AlphaRegistry) GetProvider(c *Contract) AlphaProvider {
	for _, p := range r.providers {
		if p.CanScore(c) {
			return p
		}
	}
	return r.fallback
}

// Score uses the appropriate provider to score the contract.
func (r *AlphaRegistry) Score(ctx context.Context, c *Contract) (*ScoreResult, error) {
	provider := r.GetProvider(c)
	if provider == nil {
		return nil, fmt.Errorf("no provider can score contract: %s", c.Slug)
	}
	return provider.Score(ctx, c)
}
