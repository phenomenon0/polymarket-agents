package agents

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// mockLLMClient implements LLMClient for testing.
type mockLLMClient struct {
	provider  LLMProvider
	response  string
	err       error
	latencyMs int
	callCount int
}

func newMockLLMClient(provider LLMProvider, probability float64, confidence float64) *mockLLMClient {
	response, _ := json.Marshal(map[string]interface{}{
		"probability": probability,
		"confidence":  confidence,
		"reasoning":   "Test reasoning from " + string(provider),
	})
	return &mockLLMClient{
		provider: provider,
		response: string(response),
	}
}

func (m *mockLLMClient) Complete(ctx context.Context, prompt string, systemPrompt string) (string, error) {
	m.callCount++
	if m.latencyMs > 0 {
		time.Sleep(time.Duration(m.latencyMs) * time.Millisecond)
	}
	if m.err != nil {
		return "", m.err
	}
	return m.response, nil
}

func (m *mockLLMClient) Provider() LLMProvider {
	return m.provider
}

func TestNewForecaster(t *testing.T) {
	// Test with nil config
	f := NewForecaster(nil)
	if f == nil {
		t.Fatal("NewForecaster returned nil")
	}
	if f.systemPrompt == "" {
		t.Error("System prompt should have default value")
	}

	// Test with custom config
	client := newMockLLMClient(ProviderClaude, 0.7, 0.8)
	config := &ForecasterConfig{
		Clients: map[LLMProvider]LLMClient{
			ProviderClaude: client,
		},
		Weights: map[LLMProvider]float64{
			ProviderClaude: 1.0,
		},
		CacheTTL: 10 * time.Minute,
	}
	f = NewForecaster(config)
	if len(f.clients) != 1 {
		t.Errorf("Expected 1 client, got %d", len(f.clients))
	}
	if f.cacheTTL != 10*time.Minute {
		t.Errorf("Expected cache TTL of 10m, got %v", f.cacheTTL)
	}
}

func TestAddClient(t *testing.T) {
	f := NewForecaster(nil)

	client := newMockLLMClient(ProviderGPT4, 0.6, 0.9)
	f.AddClient(client, 0.5)

	if _, ok := f.clients[ProviderGPT4]; !ok {
		t.Error("Client should be added")
	}
	if !f.weights[ProviderGPT4].Equal(decimal.NewFromFloat(0.5)) {
		t.Errorf("Weight should be 0.5, got %s", f.weights[ProviderGPT4])
	}
}

func TestForecastSingle(t *testing.T) {
	client := newMockLLMClient(ProviderClaude, 0.75, 0.85)
	config := &ForecasterConfig{
		Clients: map[LLMProvider]LLMClient{
			ProviderClaude: client,
		},
	}
	f := NewForecaster(config)

	ctx := context.Background()
	mktCtx := &MarketContext{
		TokenID:      "token1",
		Market:       "market1",
		Question:     "Will event X happen?",
		Description:  "Test event",
		CurrentPrice: decimal.NewFromFloat(0.5),
		Volume24h:    decimal.NewFromInt(10000),
		EndDate:      time.Now().Add(24 * time.Hour),
	}

	forecast, err := f.ForecastSingle(ctx, mktCtx, ProviderClaude)
	if err != nil {
		t.Fatalf("ForecastSingle failed: %v", err)
	}

	if !forecast.Probability.Equal(decimal.NewFromFloat(0.75)) {
		t.Errorf("Expected probability 0.75, got %s", forecast.Probability)
	}
	if !forecast.Confidence.Equal(decimal.NewFromFloat(0.85)) {
		t.Errorf("Expected confidence 0.85, got %s", forecast.Confidence)
	}
	if forecast.Provider != ProviderClaude {
		t.Errorf("Expected provider Claude, got %s", forecast.Provider)
	}
	if forecast.TokenID != "token1" {
		t.Errorf("Expected token1, got %s", forecast.TokenID)
	}
}

func TestForecastSingle_ProviderNotFound(t *testing.T) {
	f := NewForecaster(nil)

	ctx := context.Background()
	mktCtx := &MarketContext{
		TokenID: "token1",
	}

	_, err := f.ForecastSingle(ctx, mktCtx, ProviderClaude)
	if err == nil {
		t.Error("Expected error for unconfigured provider")
	}
}

func TestForecastEnsemble(t *testing.T) {
	claudeClient := newMockLLMClient(ProviderClaude, 0.7, 0.9)
	gpt4Client := newMockLLMClient(ProviderGPT4, 0.8, 0.8)
	deepseekClient := newMockLLMClient(ProviderDeepSeek, 0.65, 0.7)

	config := &ForecasterConfig{
		Clients: map[LLMProvider]LLMClient{
			ProviderClaude:   claudeClient,
			ProviderGPT4:     gpt4Client,
			ProviderDeepSeek: deepseekClient,
		},
		Weights: map[LLMProvider]float64{
			ProviderClaude:   0.4,
			ProviderGPT4:     0.4,
			ProviderDeepSeek: 0.2,
		},
	}
	f := NewForecaster(config)

	ctx := context.Background()
	mktCtx := &MarketContext{
		TokenID:      "token1",
		Market:       "market1",
		Question:     "Will event X happen?",
		CurrentPrice: decimal.NewFromFloat(0.5),
	}

	ensemble, err := f.ForecastEnsemble(ctx, mktCtx)
	if err != nil {
		t.Fatalf("ForecastEnsemble failed: %v", err)
	}

	if len(ensemble.IndividualForecasts) != 3 {
		t.Errorf("Expected 3 individual forecasts, got %d", len(ensemble.IndividualForecasts))
	}

	// Ensemble probability should be weighted average
	if ensemble.Probability.IsZero() {
		t.Error("Ensemble probability should not be zero")
	}

	// Should be between min and max individual forecasts
	if ensemble.Probability.LessThan(decimal.NewFromFloat(0.65)) ||
		ensemble.Probability.GreaterThan(decimal.NewFromFloat(0.80)) {
		t.Errorf("Ensemble probability %s should be between 0.65 and 0.80", ensemble.Probability)
	}
}

func TestForecastEnsemble_NoClients(t *testing.T) {
	f := NewForecaster(nil)

	ctx := context.Background()
	mktCtx := &MarketContext{
		TokenID: "token1",
	}

	_, err := f.ForecastEnsemble(ctx, mktCtx)
	if err == nil {
		t.Error("Expected error when no clients configured")
	}
}

func TestForecastWithFallback(t *testing.T) {
	// Claude fails, GPT4 succeeds
	claudeClient := newMockLLMClient(ProviderClaude, 0.7, 0.9)
	claudeClient.err = context.DeadlineExceeded

	gpt4Client := newMockLLMClient(ProviderGPT4, 0.6, 0.8)

	config := &ForecasterConfig{
		Clients: map[LLMProvider]LLMClient{
			ProviderClaude: claudeClient,
			ProviderGPT4:   gpt4Client,
		},
	}
	f := NewForecaster(config)

	ctx := context.Background()
	mktCtx := &MarketContext{
		TokenID: "token1",
	}

	forecast, err := f.ForecastWithFallback(ctx, mktCtx)
	if err != nil {
		t.Fatalf("ForecastWithFallback failed: %v", err)
	}

	// Should have used GPT4 as fallback
	if forecast.Provider != ProviderGPT4 {
		t.Errorf("Expected GPT4 as fallback, got %s", forecast.Provider)
	}
}

func TestForecastWithFallback_AllFail(t *testing.T) {
	claudeClient := newMockLLMClient(ProviderClaude, 0.7, 0.9)
	claudeClient.err = context.DeadlineExceeded

	gpt4Client := newMockLLMClient(ProviderGPT4, 0.6, 0.8)
	gpt4Client.err = context.DeadlineExceeded

	config := &ForecasterConfig{
		Clients: map[LLMProvider]LLMClient{
			ProviderClaude: claudeClient,
			ProviderGPT4:   gpt4Client,
		},
	}
	f := NewForecaster(config)

	ctx := context.Background()
	mktCtx := &MarketContext{
		TokenID: "token1",
	}

	_, err := f.ForecastWithFallback(ctx, mktCtx)
	if err == nil {
		t.Error("Expected error when all providers fail")
	}
}

func TestGetCachedForecast(t *testing.T) {
	client := newMockLLMClient(ProviderClaude, 0.75, 0.85)
	config := &ForecasterConfig{
		Clients: map[LLMProvider]LLMClient{
			ProviderClaude: client,
		},
		CacheTTL: 5 * time.Minute,
	}
	f := NewForecaster(config)

	// No cache initially
	_, ok := f.GetCachedForecast("token1")
	if ok {
		t.Error("Should not have cached forecast initially")
	}

	// Make a forecast (which caches via ensemble)
	ctx := context.Background()
	mktCtx := &MarketContext{
		TokenID: "token1",
	}
	f.ForecastEnsemble(ctx, mktCtx)

	// Should now be cached
	cached, ok := f.GetCachedForecast("token1")
	if !ok {
		t.Error("Should have cached forecast after ensemble")
	}
	if cached.TokenID != "token1" {
		t.Errorf("Expected token1, got %s", cached.TokenID)
	}
}

func TestParseResponse(t *testing.T) {
	f := NewForecaster(nil)

	testCases := []struct {
		name        string
		response    string
		expectProb  float64
		expectConf  float64
		expectError bool
	}{
		{
			name:       "Valid JSON",
			response:   `{"probability": 0.75, "confidence": 0.85, "reasoning": "test"}`,
			expectProb: 0.75,
			expectConf: 0.85,
		},
		{
			name:       "JSON with prefix",
			response:   `Here is my analysis: {"probability": 0.60, "confidence": 0.70, "reasoning": "test"}`,
			expectProb: 0.60,
			expectConf: 0.70,
		},
		{
			name:       "JSON with suffix",
			response:   `{"probability": 0.55, "confidence": 0.65, "reasoning": "test"} That's my forecast.`,
			expectProb: 0.55,
			expectConf: 0.65,
		},
		{
			name:        "Invalid probability",
			response:    `{"probability": 150, "confidence": 0.5, "reasoning": "test"}`, // > 100 is invalid
			expectError: true,
		},
		{
			name:        "No JSON",
			response:    `I think the probability is about 75%`,
			expectError: true,
		},
		{
			name:       "Missing confidence uses default",
			response:   `{"probability": 0.7, "reasoning": "test"}`,
			expectProb: 0.7,
			expectConf: 0.7, // default confidence when missing
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			forecast, err := f.parseResponse(tc.response)

			if tc.expectError {
				if err == nil {
					t.Error("Expected error")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if !forecast.Probability.Equal(decimal.NewFromFloat(tc.expectProb)) {
				t.Errorf("Expected probability %f, got %s", tc.expectProb, forecast.Probability)
			}
			if !forecast.Confidence.Equal(decimal.NewFromFloat(tc.expectConf)) {
				t.Errorf("Expected confidence %f, got %s", tc.expectConf, forecast.Confidence)
			}
		})
	}
}

func TestGenerateSignal_BuyYES(t *testing.T) {
	f := NewForecaster(nil)

	// Forecast higher than market price -> buy YES
	ensemble := &EnsembleForecast{
		TokenID:     "token1",
		Probability: decimal.NewFromFloat(0.7), // We think 70%
		Confidence:  decimal.NewFromFloat(0.8),
	}
	currentPrice := decimal.NewFromFloat(0.5) // Market says 50%

	signal := f.GenerateSignal(ensemble, currentPrice, 100) // 100 bps min edge

	if signal.Signal != SignalBuy {
		t.Errorf("Expected BUY signal, got %s", signal.Signal)
	}
	if signal.Side != "YES" {
		t.Errorf("Expected YES side, got %s", signal.Side)
	}
	if signal.EdgeBps.LessThan(decimal.NewFromInt(100)) {
		t.Errorf("Edge should be >= 100 bps, got %s", signal.EdgeBps)
	}
}

func TestGenerateSignal_BuyNO(t *testing.T) {
	f := NewForecaster(nil)

	// Forecast lower than market price -> buy NO
	ensemble := &EnsembleForecast{
		TokenID:     "token1",
		Probability: decimal.NewFromFloat(0.3), // We think 30%
		Confidence:  decimal.NewFromFloat(0.8),
	}
	currentPrice := decimal.NewFromFloat(0.5) // Market says 50%

	signal := f.GenerateSignal(ensemble, currentPrice, 100)

	if signal.Signal != SignalBuy {
		t.Errorf("Expected BUY signal, got %s", signal.Signal)
	}
	if signal.Side != "NO" {
		t.Errorf("Expected NO side, got %s", signal.Side)
	}
}

func TestGenerateSignal_Hold(t *testing.T) {
	f := NewForecaster(nil)

	// Forecast very close to market price -> hold
	// Edge = (0.505 - 0.50) / 0.50 * 10000 = 100 bps (not > 100, so HOLD)
	ensemble := &EnsembleForecast{
		TokenID:     "token1",
		Probability: decimal.NewFromFloat(0.505), // We think 50.5%
		Confidence:  decimal.NewFromFloat(0.8),
	}
	currentPrice := decimal.NewFromFloat(0.5) // Market says 50%

	signal := f.GenerateSignal(ensemble, currentPrice, 100) // 100 bps min edge

	if signal.Signal != SignalHold {
		t.Errorf("Expected HOLD signal, got %s (edge=%s)", signal.Signal, signal.EdgeBps)
	}
}

func TestRankSignals(t *testing.T) {
	signals := []*TradingSignal{
		{Signal: SignalBuy, EdgeBps: decimal.NewFromInt(50), Strength: decimal.NewFromFloat(0.5)},
		{Signal: SignalBuy, EdgeBps: decimal.NewFromInt(200), Strength: decimal.NewFromFloat(0.8)},
		{Signal: SignalBuy, EdgeBps: decimal.NewFromInt(100), Strength: decimal.NewFromFloat(0.6)},
	}

	ranked := RankSignals(signals)

	// Should be sorted by edge * strength (expected value)
	// Signal 2: 200 * 0.8 = 160
	// Signal 3: 100 * 0.6 = 60
	// Signal 1: 50 * 0.5 = 25
	if ranked[0].EdgeBps.IntPart() != 200 {
		t.Errorf("Expected signal with edge 200 first, got %d", ranked[0].EdgeBps.IntPart())
	}
	if ranked[1].EdgeBps.IntPart() != 100 {
		t.Errorf("Expected signal with edge 100 second, got %d", ranked[1].EdgeBps.IntPart())
	}
}

func TestSignalString(t *testing.T) {
	if SignalBuy.String() != "BUY" {
		t.Error("SignalBuy should be BUY")
	}
	if SignalSell.String() != "SELL" {
		t.Error("SignalSell should be SELL")
	}
	if SignalHold.String() != "HOLD" {
		t.Error("SignalHold should be HOLD")
	}
}

func TestBuildPrompt(t *testing.T) {
	f := NewForecaster(nil)

	mktCtx := &MarketContext{
		TokenID:        "token1",
		Market:         "market1",
		Question:       "Will BTC reach $100k by end of 2024?",
		Description:    "This market resolves YES if Bitcoin trades at or above $100,000.",
		CurrentPrice:   decimal.NewFromFloat(0.45),
		Volume24h:      decimal.NewFromInt(50000),
		EndDate:        time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC),
		Tags:           []string{"crypto", "bitcoin"},
		NewsSnippets:   []string{"Bitcoin ETF approved", "Fed signals rate cuts"},
		RelatedMarkets: []string{"Will BTC reach $80k?", "Will ETH reach $5k?"},
	}

	prompt := f.buildPrompt(mktCtx)

	// Should contain key elements
	if len(prompt) == 0 {
		t.Error("Prompt should not be empty")
	}
	if !containsString(prompt, "Will BTC reach $100k") {
		t.Error("Prompt should contain the question")
	}
	if !containsString(prompt, "Bitcoin ETF approved") {
		t.Error("Prompt should contain news snippets")
	}
	if !containsString(prompt, "Related Markets") {
		t.Error("Prompt should contain related markets section")
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && (s[:len(substr)] == substr || containsString(s[1:], substr)))
}

func TestCombineForecasts(t *testing.T) {
	f := NewForecaster(nil)

	mktCtx := &MarketContext{
		TokenID:  "token1",
		Market:   "market1",
		Question: "Test question",
	}

	forecasts := []Forecast{
		{Probability: decimal.NewFromFloat(0.7), Confidence: decimal.NewFromFloat(0.9), Provider: ProviderClaude},
		{Probability: decimal.NewFromFloat(0.8), Confidence: decimal.NewFromFloat(0.8), Provider: ProviderGPT4},
		{Probability: decimal.NewFromFloat(0.6), Confidence: decimal.NewFromFloat(0.7), Provider: ProviderDeepSeek},
	}

	weights := map[LLMProvider]decimal.Decimal{
		ProviderClaude:   decimal.NewFromFloat(0.4),
		ProviderGPT4:     decimal.NewFromFloat(0.4),
		ProviderDeepSeek: decimal.NewFromFloat(0.2),
	}

	ensemble := f.combineForecasts(mktCtx, forecasts, weights)

	// Check ensemble has all data
	if ensemble.TokenID != "token1" {
		t.Errorf("Expected token1, got %s", ensemble.TokenID)
	}
	if len(ensemble.IndividualForecasts) != 3 {
		t.Errorf("Expected 3 forecasts, got %d", len(ensemble.IndividualForecasts))
	}

	// Probability should be reasonable weighted average
	if ensemble.Probability.LessThan(decimal.NewFromFloat(0.6)) ||
		ensemble.Probability.GreaterThan(decimal.NewFromFloat(0.8)) {
		t.Errorf("Ensemble probability %s seems wrong", ensemble.Probability)
	}

	// Disagreement should be non-zero since forecasts differ
	if ensemble.Disagreement.IsZero() {
		t.Error("Disagreement should be non-zero")
	}
}

func TestCombineForecasts_Empty(t *testing.T) {
	f := NewForecaster(nil)

	mktCtx := &MarketContext{
		TokenID: "token1",
	}

	ensemble := f.combineForecasts(mktCtx, []Forecast{}, nil)

	if !ensemble.Probability.IsZero() {
		t.Error("Empty forecasts should result in zero probability")
	}
}
