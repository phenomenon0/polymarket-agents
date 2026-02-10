package agents

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/phenomenon0/polymarket-agents/tools"

	"github.com/shopspring/decimal"
)

// TestLLMIntegration tests the LLM client integration with real providers.
// Run with: go test -v -run TestLLMIntegration ./pkg/trader/agents/...
func TestLLMIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping LLM integration test in short mode")
	}

	router := tools.NewModelRouter()

	// Test with local preset first (Ollama)
	t.Run("LocalPreset", func(t *testing.T) {
		forecaster, err := CreateForecasterWithPreset(router, PresetLocal)
		if err != nil {
			t.Skipf("Local LLM not available: %v", err)
		}

		testForecaster(t, forecaster)
	})

	// Test with fast preset (Cerebras if available)
	t.Run("FastPreset", func(t *testing.T) {
		if os.Getenv("CEREBRAS_API_KEY") == "" {
			t.Skip("CEREBRAS_API_KEY not set")
		}

		forecaster, err := CreateForecasterWithPreset(router, PresetFast)
		if err != nil {
			t.Skipf("Fast preset not available: %v", err)
		}

		testForecaster(t, forecaster)
	})

	// Test with cheap preset (DeepSeek if available)
	t.Run("CheapPreset", func(t *testing.T) {
		if os.Getenv("DEEPSEEK_API_KEY") == "" {
			t.Skip("DEEPSEEK_API_KEY not set")
		}

		forecaster, err := CreateForecasterWithPreset(router, PresetCheap)
		if err != nil {
			t.Skipf("Cheap preset not available: %v", err)
		}

		testForecaster(t, forecaster)
	})
}

func testForecaster(t *testing.T, forecaster *Forecaster) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	mktCtx := &MarketContext{
		TokenID:      "test-token",
		Market:       "test-market",
		Question:     "Will Bitcoin reach $100,000 by end of 2025?",
		Description:  "This market resolves YES if Bitcoin trades at or above $100,000 USD on any major exchange before January 1, 2026.",
		CurrentPrice: decFromFloat(0.45),
		Volume24h:    decFromFloat(50000),
		EndDate:      time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC),
		Tags:         []string{"crypto", "bitcoin"},
	}

	// Try single forecast
	t.Log("Calling ForecastWithFallback...")
	forecast, err := forecaster.ForecastWithFallback(ctx, mktCtx)
	if err != nil {
		t.Fatalf("Forecast failed: %v", err)
	}

	t.Logf("Forecast result:")
	t.Logf("  Provider: %s", forecast.Provider)
	t.Logf("  Probability: %s", forecast.Probability)
	t.Logf("  Confidence: %s", forecast.Confidence)
	t.Logf("  Latency: %dms", forecast.LatencyMs)
	t.Logf("  Reasoning: %s", truncate(forecast.Reasoning, 200))

	// Validate result
	if forecast.Probability.IsNegative() || forecast.Probability.GreaterThan(decFromFloat(1.0)) {
		t.Errorf("Invalid probability: %s", forecast.Probability)
	}

	// Generate trading signal
	signal := forecaster.GenerateSignal(&EnsembleForecast{
		TokenID:             mktCtx.TokenID,
		Probability:         forecast.Probability,
		Confidence:          forecast.Confidence,
		IndividualForecasts: []Forecast{*forecast},
	}, mktCtx.CurrentPrice, 100)

	t.Logf("Trading signal:")
	t.Logf("  Signal: %s", signal.Signal)
	t.Logf("  Side: %s", signal.Side)
	t.Logf("  Edge: %s bps", signal.EdgeBps)
	t.Logf("  Strength: %s", signal.Strength)
	t.Logf("  Reasoning: %s", signal.Reasoning)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func decFromFloat(f float64) decimal.Decimal {
	return decimal.NewFromFloat(f)
}
