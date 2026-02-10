// Package agents provides AI agents for trading decisions.
package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// LLMProvider represents an LLM provider.
type LLMProvider string

const (
	ProviderClaude   LLMProvider = "claude"
	ProviderGPT4     LLMProvider = "gpt4"
	ProviderDeepSeek LLMProvider = "deepseek"
)

// LLMClient is an interface for LLM providers.
type LLMClient interface {
	Complete(ctx context.Context, prompt string, systemPrompt string) (string, error)
	Provider() LLMProvider
}

// Forecast represents a probability forecast for a market.
type Forecast struct {
	TokenID     string          `json:"token_id"`
	Market      string          `json:"market"`
	Question    string          `json:"question"`
	Probability decimal.Decimal `json:"probability"` // 0-1
	Confidence  decimal.Decimal `json:"confidence"`  // 0-1
	Reasoning   string          `json:"reasoning"`
	Provider    LLMProvider     `json:"provider"`
	Timestamp   time.Time       `json:"timestamp"`
	LatencyMs   int64           `json:"latency_ms"`
}

// EnsembleForecast combines forecasts from multiple models.
type EnsembleForecast struct {
	TokenID             string          `json:"token_id"`
	Market              string          `json:"market"`
	Question            string          `json:"question"`
	Probability         decimal.Decimal `json:"probability"` // Weighted average
	Confidence          decimal.Decimal `json:"confidence"`
	Disagreement        decimal.Decimal `json:"disagreement"` // Std dev of forecasts
	IndividualForecasts []Forecast      `json:"individual_forecasts"`
	Timestamp           time.Time       `json:"timestamp"`
}

// MarketContext provides context for forecasting.
type MarketContext struct {
	TokenID      string          `json:"token_id"`
	Market       string          `json:"market"`
	Question     string          `json:"question"`
	Description  string          `json:"description"`
	CurrentPrice decimal.Decimal `json:"current_price"`
	Volume24h    decimal.Decimal `json:"volume_24h"`
	EndDate      time.Time       `json:"end_date"`
	Tags         []string        `json:"tags"`
	// Additional context
	NewsSnippets   []string `json:"news_snippets,omitempty"`
	RelatedMarkets []string `json:"related_markets,omitempty"`
}

// Forecaster uses multiple LLMs to forecast market probabilities.
type Forecaster struct {
	clients      map[LLMProvider]LLMClient
	weights      map[LLMProvider]decimal.Decimal
	systemPrompt string

	mu       sync.RWMutex
	cache    map[string]*Forecast // tokenID -> latest forecast
	cacheTTL time.Duration
}

// ForecasterConfig configures the forecaster.
type ForecasterConfig struct {
	Clients      map[LLMProvider]LLMClient
	Weights      map[LLMProvider]float64
	CacheTTL     time.Duration
	SystemPrompt string
}

// DefaultSystemPrompt is the default superforecaster prompt.
const DefaultSystemPrompt = `You are an expert superforecaster trained in probabilistic reasoning and calibration.
Your task is to estimate the probability of an event occurring based on the provided information.

Guidelines:
1. Consider base rates and reference classes
2. Update based on specific evidence
3. Account for both inside and outside view
4. Be well-calibrated: when you say 70%, you should be right 70% of the time
5. Avoid overconfidence - use the full probability range
6. Consider multiple scenarios and weight them appropriately
7. Be explicit about your reasoning process

Output format (JSON):
{
  "probability": 0.XX,  // Your probability estimate (0-1)
  "confidence": 0.XX,   // Your confidence in this estimate (0-1)
  "reasoning": "Your step-by-step reasoning process"
}

Important: Only output valid JSON, nothing else.`

// NewForecaster creates a new forecaster.
func NewForecaster(config *ForecasterConfig) *Forecaster {
	f := &Forecaster{
		clients:  make(map[LLMProvider]LLMClient),
		weights:  make(map[LLMProvider]decimal.Decimal),
		cache:    make(map[string]*Forecast),
		cacheTTL: 5 * time.Minute,
	}

	if config != nil {
		f.clients = config.Clients
		for provider, weight := range config.Weights {
			f.weights[provider] = decimal.NewFromFloat(weight)
		}
		if config.CacheTTL > 0 {
			f.cacheTTL = config.CacheTTL
		}
		if config.SystemPrompt != "" {
			f.systemPrompt = config.SystemPrompt
		}
	}

	if f.systemPrompt == "" {
		f.systemPrompt = DefaultSystemPrompt
	}

	// Default weights if not specified
	if len(f.weights) == 0 {
		f.weights = map[LLMProvider]decimal.Decimal{
			ProviderClaude:   decimal.NewFromFloat(0.4), // Primary
			ProviderGPT4:     decimal.NewFromFloat(0.4), // Secondary
			ProviderDeepSeek: decimal.NewFromFloat(0.2), // Tertiary
		}
	}

	return f
}

// AddClient adds an LLM client.
func (f *Forecaster) AddClient(client LLMClient, weight float64) {
	f.mu.Lock()
	defer f.mu.Unlock()

	provider := client.Provider()
	f.clients[provider] = client
	f.weights[provider] = decimal.NewFromFloat(weight)
}

// ForecastSingle gets a forecast from a single provider.
func (f *Forecaster) ForecastSingle(ctx context.Context, mktCtx *MarketContext, provider LLMProvider) (*Forecast, error) {
	f.mu.RLock()
	client, ok := f.clients[provider]
	f.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("provider %s not configured", provider)
	}

	prompt := f.buildPrompt(mktCtx)

	start := time.Now()
	response, err := client.Complete(ctx, prompt, f.systemPrompt)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	forecast, err := f.parseResponse(response)
	if err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	forecast.TokenID = mktCtx.TokenID
	forecast.Market = mktCtx.Market
	forecast.Question = mktCtx.Question
	forecast.Provider = provider
	forecast.Timestamp = time.Now()
	forecast.LatencyMs = latency

	return forecast, nil
}

// ForecastEnsemble gets forecasts from all providers and combines them.
func (f *Forecaster) ForecastEnsemble(ctx context.Context, mktCtx *MarketContext) (*EnsembleForecast, error) {
	f.mu.RLock()
	clients := make(map[LLMProvider]LLMClient, len(f.clients))
	weights := make(map[LLMProvider]decimal.Decimal, len(f.weights))
	for k, v := range f.clients {
		clients[k] = v
	}
	for k, v := range f.weights {
		weights[k] = v
	}
	f.mu.RUnlock()

	if len(clients) == 0 {
		return nil, fmt.Errorf("no LLM clients configured")
	}

	// Run forecasts in parallel
	var wg sync.WaitGroup
	results := make(chan *Forecast, len(clients))
	errors := make(chan error, len(clients))

	for provider := range clients {
		wg.Add(1)
		go func(p LLMProvider) {
			defer wg.Done()

			forecast, err := f.ForecastSingle(ctx, mktCtx, p)
			if err != nil {
				errors <- fmt.Errorf("%s: %w", p, err)
				return
			}
			results <- forecast
		}(provider)
	}

	wg.Wait()
	close(results)
	close(errors)

	// Collect results
	forecasts := make([]Forecast, 0, len(clients))
	for forecast := range results {
		forecasts = append(forecasts, *forecast)
	}

	if len(forecasts) == 0 {
		// Return first error if all failed
		for err := range errors {
			return nil, err
		}
		return nil, fmt.Errorf("no forecasts generated")
	}

	// Calculate weighted ensemble
	ensemble := f.combineForecasts(mktCtx, forecasts, weights)

	// Cache the result
	f.mu.Lock()
	for _, forecast := range forecasts {
		f.cache[forecast.TokenID] = &forecast
	}
	f.mu.Unlock()

	return ensemble, nil
}

// ForecastWithFallback tries providers in order until one succeeds.
func (f *Forecaster) ForecastWithFallback(ctx context.Context, mktCtx *MarketContext) (*Forecast, error) {
	// Order: Claude -> GPT-4 -> DeepSeek
	providers := []LLMProvider{ProviderClaude, ProviderGPT4, ProviderDeepSeek}

	var lastErr error
	for _, provider := range providers {
		f.mu.RLock()
		_, ok := f.clients[provider]
		f.mu.RUnlock()

		if !ok {
			continue
		}

		forecast, err := f.ForecastSingle(ctx, mktCtx, provider)
		if err != nil {
			lastErr = err
			continue
		}

		return forecast, nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("all providers failed, last error: %w", lastErr)
	}
	return nil, fmt.Errorf("no providers configured")
}

// GetCachedForecast returns a cached forecast if available and fresh.
func (f *Forecaster) GetCachedForecast(tokenID string) (*Forecast, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	forecast, ok := f.cache[tokenID]
	if !ok {
		return nil, false
	}

	if time.Since(forecast.Timestamp) > f.cacheTTL {
		return nil, false
	}

	return forecast, true
}

// --- Internal methods ---

func (f *Forecaster) buildPrompt(mktCtx *MarketContext) string {
	prompt := fmt.Sprintf(`Market Question: %s

Description: %s

Current Information:
- Current market price: %s (implied probability)
- 24h trading volume: $%s
- Resolution date: %s
- Categories: %v

`, mktCtx.Question, mktCtx.Description,
		mktCtx.CurrentPrice.StringFixed(2),
		mktCtx.Volume24h.StringFixed(0),
		mktCtx.EndDate.Format("January 2, 2006"),
		mktCtx.Tags)

	if len(mktCtx.NewsSnippets) > 0 {
		prompt += "Recent News:\n"
		for i, news := range mktCtx.NewsSnippets {
			if i >= 5 {
				break
			}
			prompt += fmt.Sprintf("- %s\n", news)
		}
		prompt += "\n"
	}

	if len(mktCtx.RelatedMarkets) > 0 {
		prompt += "Related Markets:\n"
		for _, related := range mktCtx.RelatedMarkets {
			prompt += fmt.Sprintf("- %s\n", related)
		}
		prompt += "\n"
	}

	prompt += `Based on all available information, what is your probability estimate that this event will occur?

Consider:
1. Historical base rates for similar events
2. Current specific circumstances
3. Time remaining until resolution
4. Market sentiment (current price may contain information)
5. Any relevant recent developments

Provide your forecast in JSON format.`

	return prompt
}

func (f *Forecaster) parseResponse(response string) (*Forecast, error) {
	// Strip markdown code blocks if present
	response = stripMarkdownCodeBlocks(response)

	// Extract JSON from response
	jsonStr := extractJSON(response)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in response")
	}

	// Parse into generic map first to handle various structures
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Extract probability - check multiple possible locations
	prob := extractFloat(raw, "probability")
	if prob == 0 {
		// Try nested under "forecast"
		if forecast, ok := raw["forecast"].(map[string]interface{}); ok {
			prob = extractFloat(forecast, "probability")
		}
	}

	// Extract confidence - check multiple possible locations
	conf := extractFloat(raw, "confidence")
	if conf == 0 {
		if forecast, ok := raw["forecast"].(map[string]interface{}); ok {
			conf = extractFloat(forecast, "confidence")
		}
	}

	// Extract reasoning - check multiple possible locations
	reasoning := extractString(raw, "reasoning")
	if reasoning == "" {
		reasoning = extractString(raw, "rationale")
		if reasoning == "" {
			if forecast, ok := raw["forecast"].(map[string]interface{}); ok {
				reasoning = extractString(forecast, "reasoning")
				if reasoning == "" {
					reasoning = extractString(forecast, "rationale")
				}
			}
		}
	}

	// Handle rationale as array
	if reasoning == "" {
		if rationale, ok := raw["rationale"].([]interface{}); ok {
			var parts []string
			for _, r := range rationale {
				if s, ok := r.(string); ok {
					parts = append(parts, s)
				}
			}
			reasoning = strings.Join(parts, " ")
		}
		if forecast, ok := raw["forecast"].(map[string]interface{}); ok {
			if rationale, ok := forecast["rationale"].([]interface{}); ok {
				var parts []string
				for _, r := range rationale {
					if s, ok := r.(string); ok {
						parts = append(parts, s)
					}
				}
				reasoning = strings.Join(parts, " ")
			}
		}
	}

	// Normalize probability if given as percentage (e.g., 30 instead of 0.30)
	if prob > 1 && prob <= 100 {
		prob = prob / 100.0
	}

	// Validate probability
	if prob < 0 || prob > 1 {
		return nil, fmt.Errorf("probability out of range: %f", prob)
	}

	// Default confidence if not found or invalid
	if conf <= 0 || conf > 1 {
		conf = 0.7 // Default confidence
	}

	return &Forecast{
		Probability: decimal.NewFromFloat(prob),
		Confidence:  decimal.NewFromFloat(conf),
		Reasoning:   reasoning,
	}, nil
}

// stripMarkdownCodeBlocks removes ```json ... ``` wrappers
func stripMarkdownCodeBlocks(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```json") {
		s = strings.TrimPrefix(s, "```json")
		if idx := strings.LastIndex(s, "```"); idx != -1 {
			s = s[:idx]
		}
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		if idx := strings.LastIndex(s, "```"); idx != -1 {
			s = s[:idx]
		}
	}
	return strings.TrimSpace(s)
}

// extractJSON finds the first complete JSON object in a string
func extractJSON(s string) string {
	start := -1
	braceCount := 0

	for i, c := range s {
		if c == '{' {
			if start == -1 {
				start = i
			}
			braceCount++
		} else if c == '}' {
			braceCount--
			if braceCount == 0 && start != -1 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// extractFloat extracts a float from a map
func extractFloat(m map[string]interface{}, key string) float64 {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case float64:
			return val
		case int:
			return float64(val)
		case string:
			if f, err := strconv.ParseFloat(val, 64); err == nil {
				return f
			}
		}
	}
	return 0
}

// extractString extracts a string from a map
func extractString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func (f *Forecaster) combineForecasts(mktCtx *MarketContext, forecasts []Forecast, weights map[LLMProvider]decimal.Decimal) *EnsembleForecast {
	ensemble := &EnsembleForecast{
		TokenID:             mktCtx.TokenID,
		Market:              mktCtx.Market,
		Question:            mktCtx.Question,
		IndividualForecasts: forecasts,
		Timestamp:           time.Now(),
	}

	if len(forecasts) == 0 {
		return ensemble
	}

	// Calculate weighted average
	totalWeight := decimal.Zero
	weightedSum := decimal.Zero
	confidenceSum := decimal.Zero

	for _, forecast := range forecasts {
		weight := weights[forecast.Provider]
		if weight.IsZero() {
			weight = decimal.NewFromFloat(1.0 / float64(len(forecasts)))
		}

		// Weight by both provider weight and confidence
		effectiveWeight := weight.Mul(forecast.Confidence)
		totalWeight = totalWeight.Add(effectiveWeight)
		weightedSum = weightedSum.Add(forecast.Probability.Mul(effectiveWeight))
		confidenceSum = confidenceSum.Add(forecast.Confidence)
	}

	if !totalWeight.IsZero() {
		ensemble.Probability = weightedSum.Div(totalWeight)
	}

	ensemble.Confidence = confidenceSum.Div(decimal.NewFromInt(int64(len(forecasts))))

	// Calculate disagreement (standard deviation)
	if len(forecasts) > 1 {
		sumSquaredDiff := decimal.Zero
		for _, forecast := range forecasts {
			diff := forecast.Probability.Sub(ensemble.Probability)
			sumSquaredDiff = sumSquaredDiff.Add(diff.Mul(diff))
		}
		variance := sumSquaredDiff.Div(decimal.NewFromInt(int64(len(forecasts))))
		// Approximate sqrt
		ensemble.Disagreement = variance.Pow(decimal.NewFromFloat(0.5))
	}

	return ensemble
}

// --- Trading Signal Generation ---

// Signal represents a trading signal.
type Signal int

const (
	SignalHold Signal = iota
	SignalBuy
	SignalSell
)

func (s Signal) String() string {
	switch s {
	case SignalBuy:
		return "BUY"
	case SignalSell:
		return "SELL"
	default:
		return "HOLD"
	}
}

// TradingSignal contains a trading recommendation.
type TradingSignal struct {
	Signal       Signal            `json:"signal"`
	TokenID      string            `json:"token_id"`
	Side         string            `json:"side"`     // "YES" or "NO"
	Strength     decimal.Decimal   `json:"strength"` // 0-1
	EdgeBps      decimal.Decimal   `json:"edge_bps"` // Expected edge in basis points
	Forecast     *EnsembleForecast `json:"forecast"`
	CurrentPrice decimal.Decimal   `json:"current_price"`
	Reasoning    string            `json:"reasoning"`
	Timestamp    time.Time         `json:"timestamp"`
}

// GenerateSignal generates a trading signal from a forecast.
func (f *Forecaster) GenerateSignal(forecast *EnsembleForecast, currentYesPrice decimal.Decimal, minEdgeBps int) *TradingSignal {
	signal := &TradingSignal{
		Signal:       SignalHold,
		TokenID:      forecast.TokenID,
		Forecast:     forecast,
		CurrentPrice: currentYesPrice,
		Timestamp:    time.Now(),
	}

	// Calculate edge
	// Edge = (Forecast Probability - Market Price) / Market Price * 10000
	marketProb := currentYesPrice
	forecastProb := forecast.Probability

	var edge decimal.Decimal
	var side string

	if forecastProb.GreaterThan(marketProb) {
		// We think YES is underpriced
		edge = forecastProb.Sub(marketProb).Div(marketProb).Mul(decimal.NewFromInt(10000))
		side = "YES"
	} else {
		// We think NO is underpriced (YES is overpriced)
		noMarketProb := decimal.NewFromInt(1).Sub(marketProb)
		noForecastProb := decimal.NewFromInt(1).Sub(forecastProb)
		edge = noForecastProb.Sub(noMarketProb).Div(noMarketProb).Mul(decimal.NewFromInt(10000))
		side = "NO"
	}

	signal.EdgeBps = edge
	signal.Side = side

	// Determine signal strength based on edge and confidence
	minEdge := decimal.NewFromInt(int64(minEdgeBps))

	if edge.GreaterThan(minEdge) {
		// Strong enough edge
		signal.Signal = SignalBuy

		// Strength is a function of edge and confidence
		// Normalized edge (edge / 100) * confidence
		normalizedEdge := edge.Div(decimal.NewFromInt(100))
		if normalizedEdge.GreaterThan(decimal.NewFromInt(1)) {
			normalizedEdge = decimal.NewFromInt(1)
		}
		signal.Strength = normalizedEdge.Mul(forecast.Confidence)

		signal.Reasoning = fmt.Sprintf(
			"Forecast: %.1f%% vs Market: %.1f%%. Edge: %.0f bps on %s. Confidence: %.0f%%. Disagreement: %.1f%%",
			forecastProb.Mul(decimal.NewFromInt(100)).InexactFloat64(),
			marketProb.Mul(decimal.NewFromInt(100)).InexactFloat64(),
			edge.InexactFloat64(),
			side,
			forecast.Confidence.Mul(decimal.NewFromInt(100)).InexactFloat64(),
			forecast.Disagreement.Mul(decimal.NewFromInt(100)).InexactFloat64(),
		)
	} else {
		signal.Reasoning = fmt.Sprintf(
			"Edge %.0f bps below threshold %d bps. Forecast: %.1f%% vs Market: %.1f%%",
			edge.InexactFloat64(),
			minEdgeBps,
			forecastProb.Mul(decimal.NewFromInt(100)).InexactFloat64(),
			marketProb.Mul(decimal.NewFromInt(100)).InexactFloat64(),
		)
	}

	return signal
}

// RankSignals ranks trading signals by expected value.
func RankSignals(signals []*TradingSignal) []*TradingSignal {
	// Sort by edge * strength (expected value proxy)
	sort.Slice(signals, func(i, j int) bool {
		evI := signals[i].EdgeBps.Mul(signals[i].Strength)
		evJ := signals[j].EdgeBps.Mul(signals[j].Strength)
		return evI.GreaterThan(evJ)
	})
	return signals
}
