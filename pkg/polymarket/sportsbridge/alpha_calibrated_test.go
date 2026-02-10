package sportsbridge

import (
	"context"
	"math"
	"testing"
	"time"
)

// stubProb3Source is a test stub that returns fixed probabilities.
type stubProb3Source struct {
	probs Prob3
}

func (s *stubProb3Source) Prob3(ctx context.Context, mc *MatchContracts) (Prob3, error) {
	return s.probs, nil
}

// createTestMatchContracts creates a MatchContracts with given market prices.
func createTestMatchContracts(home, draw, away float64) *MatchContracts {
	date := time.Date(2025, 12, 27, 15, 0, 0, 0, time.UTC)
	return &MatchContracts{
		MatchKey:  "epl_2025-12-27_Liverpool_Wolves",
		League:    "epl",
		HomeTeam:  "Liverpool",
		AwayTeam:  "Wolves",
		MatchDate: date,
		HomeWin: &Contract{
			Slug:  "epl-liv-wol-2025-12-27-liv",
			MidPx: home,
			Event: Soccer1X2Event{League: "epl", HomeTeam: "Liverpool", AwayTeam: "Wolves", MatchDate: date, Outcome: OutcomeHome, IsYesSide: true},
		},
		Draw: &Contract{
			Slug:  "epl-liv-wol-2025-12-27-draw",
			MidPx: draw,
			Event: Soccer1X2Event{League: "epl", HomeTeam: "Liverpool", AwayTeam: "Wolves", MatchDate: date, Outcome: OutcomeDraw, IsYesSide: true},
		},
		AwayWin: &Contract{
			Slug:  "epl-liv-wol-2025-12-27-wol",
			MidPx: away,
			Event: Soccer1X2Event{League: "epl", HomeTeam: "Liverpool", AwayTeam: "Wolves", MatchDate: date, Outcome: OutcomeAway, IsYesSide: true},
		},
	}
}

func TestCalibratedProvider_V0_ReturnsMarketProbs(t *testing.T) {
	// Market: 79/14/7
	mc := createTestMatchContracts(0.79, 0.14, 0.07)

	// MathShard (doesn't matter for v0)
	stub := &stubProb3Source{probs: Prob3{Home: 0.61, Draw: 0.20, Away: 0.19}}

	provider := NewCalibratedProvider(ModelV0, 0, stub)
	provider.SetMatchGroups([]*MatchContracts{mc})

	ctx := context.Background()

	// Score home contract
	score, err := provider.Score(ctx, mc.HomeWin)
	if err != nil {
		t.Fatalf("Score failed: %v", err)
	}

	// v0 should return normalized market probs
	// 0.79 / (0.79 + 0.14 + 0.07) = 0.79
	expected := 0.79
	if math.Abs(score.Q-expected) > 0.01 {
		t.Errorf("v0 home prob: got %.3f, want %.3f", score.Q, expected)
	}
}

func TestCalibratedProvider_MathShard_ReturnsRawModelProbs(t *testing.T) {
	// Market: 79/14/7
	mc := createTestMatchContracts(0.79, 0.14, 0.07)

	// MathShard: 61/20/19
	stub := &stubProb3Source{probs: Prob3{Home: 0.61, Draw: 0.20, Away: 0.19}}

	provider := NewCalibratedProvider(ModelMathShard, 0, stub)
	provider.SetMatchGroups([]*MatchContracts{mc})

	ctx := context.Background()

	// Score home contract
	score, err := provider.Score(ctx, mc.HomeWin)
	if err != nil {
		t.Fatalf("Score failed: %v", err)
	}

	// mathshard should return raw MathShard probs
	expected := 0.61
	if math.Abs(score.Q-expected) > 0.01 {
		t.Errorf("mathshard home prob: got %.3f, want %.3f", score.Q, expected)
	}
}

func TestCalibratedProvider_V0Blend_PreservesFavorite(t *testing.T) {
	// The Liverpool case: market 79%, MathShard 61%
	// With alpha=0.10, blend should stay close to market (preserve favorite)
	mc := createTestMatchContracts(0.79, 0.14, 0.07)

	// MathShard: 61/20/19 (significantly lower on home)
	stub := &stubProb3Source{probs: Prob3{Home: 0.61, Draw: 0.20, Away: 0.19}}

	provider := NewCalibratedProvider(ModelV0Blend, 0.10, stub)
	provider.SetMatchGroups([]*MatchContracts{mc})

	ctx := context.Background()

	// Score home contract
	score, err := provider.Score(ctx, mc.HomeWin)
	if err != nil {
		t.Fatalf("Score failed: %v", err)
	}

	// v0blend with alpha=0.10 should:
	// 1. Stay close to market (not drop to 61%)
	// 2. Be slightly nudged by MathShard
	// Expected: somewhere around 0.77-0.78 (close to 0.79, not 0.61)

	if score.Q < 0.70 {
		t.Errorf("v0blend home prob too low: got %.3f, want > 0.70 (market is 0.79)", score.Q)
	}
	if score.Q > 0.80 {
		t.Errorf("v0blend home prob too high: got %.3f, want < 0.80", score.Q)
	}

	t.Logf("v0blend home prob: %.3f (market: 0.79, mathshard: 0.61)", score.Q)
}

func TestCalibratedProvider_V0Blend_AlphaScaling(t *testing.T) {
	mc := createTestMatchContracts(0.79, 0.14, 0.07)
	stub := &stubProb3Source{probs: Prob3{Home: 0.61, Draw: 0.20, Away: 0.19}}
	ctx := context.Background()

	tests := []struct {
		alpha   float64
		minHome float64
		maxHome float64
	}{
		{0.00, 0.78, 0.80}, // alpha=0 -> pure market
		{0.10, 0.70, 0.79}, // alpha=0.10 -> slight blend
		{0.25, 0.65, 0.78}, // alpha=0.25 -> more MS influence
		{0.50, 0.60, 0.75}, // alpha=0.50 -> half/half
		{1.00, 0.60, 0.62}, // alpha=1 -> pure MathShard
	}

	for _, tt := range tests {
		t.Run("alpha="+string(rune(int(tt.alpha*100)+'0')), func(t *testing.T) {
			provider := NewCalibratedProvider(ModelV0Blend, tt.alpha, stub)
			provider.SetMatchGroups([]*MatchContracts{mc})

			score, err := provider.Score(ctx, mc.HomeWin)
			if err != nil {
				t.Fatalf("Score failed: %v", err)
			}

			if score.Q < tt.minHome || score.Q > tt.maxHome {
				t.Errorf("alpha=%.2f: got %.3f, want [%.2f, %.2f]", tt.alpha, score.Q, tt.minHome, tt.maxHome)
			}
		})
	}
}

func TestAgreeDirectionGate(t *testing.T) {
	tests := []struct {
		name    string
		outcome Outcome
		pMkt    Prob3
		pMS     Prob3
		q       Prob3
		want    bool
	}{
		{
			name:    "MS agrees with market direction",
			outcome: OutcomeHome,
			pMkt:    Prob3{Home: 0.50, Draw: 0.25, Away: 0.25},
			pMS:     Prob3{Home: 0.55, Draw: 0.23, Away: 0.22}, // MS thinks HOME is higher
			q:       Prob3{Home: 0.52, Draw: 0.24, Away: 0.24}, // blend also higher
			want:    true,
		},
		{
			name:    "MS thinks overpriced, but blend shows higher",
			outcome: OutcomeHome,
			pMkt:    Prob3{Home: 0.79, Draw: 0.14, Away: 0.07},
			pMS:     Prob3{Home: 0.61, Draw: 0.20, Away: 0.19}, // MS thinks HOME is overpriced
			q:       Prob3{Home: 0.80, Draw: 0.13, Away: 0.07}, // hypothetical bad blend
			want:    false,                                     // should gate because MS < mkt but q > mkt
		},
		{
			name:    "MS thinks overpriced, blend also lower",
			outcome: OutcomeHome,
			pMkt:    Prob3{Home: 0.79, Draw: 0.14, Away: 0.07},
			pMS:     Prob3{Home: 0.61, Draw: 0.20, Away: 0.19}, // MS thinks HOME is overpriced
			q:       Prob3{Home: 0.77, Draw: 0.15, Away: 0.08}, // blend agrees (lower)
			want:    true,                                      // direction agrees (both < market) - but still no positive edge
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := agreeDirectionGate(tt.outcome, tt.pMkt, tt.pMS, tt.q)
			if got != tt.want {
				t.Errorf("agreeDirectionGate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMarketProbsFromGroup_Normalization(t *testing.T) {
	// Market prices that don't sum to 1 (typical of Polymarket)
	mc := createTestMatchContracts(0.79, 0.14, 0.07)

	probs, err := marketProbsFromGroup(mc)
	if err != nil {
		t.Fatalf("marketProbsFromGroup failed: %v", err)
	}

	sum := probs.Home + probs.Draw + probs.Away
	if math.Abs(sum-1.0) > 0.001 {
		t.Errorf("probs don't sum to 1: got %.4f", sum)
	}
}

func TestMarketProbsFromGroup_MissingMarket(t *testing.T) {
	mc := &MatchContracts{
		MatchKey: "test",
		HomeWin:  &Contract{MidPx: 0.50},
		// Draw is nil
		AwayWin: &Contract{MidPx: 0.30},
	}

	_, err := marketProbsFromGroup(mc)
	if err == nil {
		t.Error("expected error for incomplete market group")
	}
}
