package agents

import (
	"context"
	"fmt"
	"time"

	"github.com/phenomenon0/polymarket-agents/core"
	"github.com/phenomenon0/polymarket-agents/tools"
)

// LLMToolClient wraps the existing LLMTool to implement the LLMClient interface.
type LLMToolClient struct {
	tool     *tools.LLMTool
	config   tools.LLMConfig
	provider LLMProvider
}

// NewLLMToolClient creates an LLMClient from an LLMConfig.
func NewLLMToolClient(config tools.LLMConfig, provider LLMProvider) *LLMToolClient {
	return &LLMToolClient{
		tool:     tools.NewLLMTool(config),
		config:   config,
		provider: provider,
	}
}

// Complete implements LLMClient.Complete.
func (c *LLMToolClient) Complete(ctx context.Context, prompt string, systemPrompt string) (string, error) {
	// Build the request
	req := &tools.LLMRequest{
		Messages: []tools.LLMMessage{
			{Role: "user", Content: prompt},
		},
		System:      systemPrompt,
		MaxTokens:   c.config.MaxTokens,
		Temperature: c.config.Temperature,
	}

	// Create a mock ToolContext
	toolCtx := &core.ToolContext{
		Ctx: ctx,
		Request: &core.Message{
			ToolReq: &core.ToolRequestPayload{
				Input: req,
			},
		},
	}

	// Execute
	result := c.tool.Execute(toolCtx)
	if result.Status != core.ToolComplete {
		return "", fmt.Errorf("LLM call failed: %s", result.Error)
	}

	resp, ok := result.Output.(*tools.LLMResponse)
	if !ok {
		return "", fmt.Errorf("unexpected response type: %T", result.Output)
	}

	return resp.Content, nil
}

// Provider implements LLMClient.Provider.
func (c *LLMToolClient) Provider() LLMProvider {
	return c.provider
}

// Cost returns the cost tracker for this client.
func (c *LLMToolClient) Cost() *tools.CostTracker {
	return c.tool.Cost()
}

// --- Factory functions using the ModelRouter ---

// CreateClientsFromRouter creates LLM clients using the ModelRouter.
func CreateClientsFromRouter(router *tools.ModelRouter) (map[LLMProvider]LLMClient, error) {
	clients := make(map[LLMProvider]LLMClient)

	// Claude - use Elite tier (Claude Sonnet 4.5)
	claudeConfig, err := router.GetConfig(tools.TierElite, 0)
	if err == nil && claudeConfig.APIKey != "" {
		clients[ProviderClaude] = NewLLMToolClient(claudeConfig, ProviderClaude)
	}

	// GPT-4 - use Fast tier (GPT-5.1)
	gptConfig, err := router.GetConfig(tools.TierFast, 0)
	if err == nil && gptConfig.APIKey != "" {
		clients[ProviderGPT4] = NewLLMToolClient(gptConfig, ProviderGPT4)
	}

	// DeepSeek - use Reasoning tier (DeepSeek R1)
	deepseekConfig, err := router.GetConfig(tools.TierReasoning, 0)
	if err == nil && deepseekConfig.APIKey != "" {
		clients[ProviderDeepSeek] = NewLLMToolClient(deepseekConfig, ProviderDeepSeek)
	}

	if len(clients) == 0 {
		return nil, fmt.Errorf("no LLM clients could be created - check API keys")
	}

	return clients, nil
}

// CreateForecasterFromRouter creates a fully configured Forecaster using the ModelRouter.
func CreateForecasterFromRouter(router *tools.ModelRouter) (*Forecaster, error) {
	clients, err := CreateClientsFromRouter(router)
	if err != nil {
		return nil, err
	}

	// Set up weights based on model quality
	weights := map[LLMProvider]float64{
		ProviderClaude:   0.4, // Highest quality
		ProviderGPT4:     0.35,
		ProviderDeepSeek: 0.25, // Best reasoning
	}

	config := &ForecasterConfig{
		Clients:  clients,
		Weights:  weights,
		CacheTTL: 5 * time.Minute,
	}

	return NewForecaster(config), nil
}

// CreateLocalForecaster creates a forecaster using only local Ollama models.
func CreateLocalForecaster(router *tools.ModelRouter) (*Forecaster, error) {
	clients := make(map[LLMProvider]LLMClient)

	// Use local reasoning model
	localConfig, err := router.GetConfig(tools.TierLocal, 1) // DeepSeek R1 14B
	if err != nil {
		// Fall back to Qwen3 8B
		localConfig, err = router.GetConfig(tools.TierLocal, 0)
		if err != nil {
			return nil, fmt.Errorf("no local models available")
		}
	}

	// Use single local model for all providers
	clients[ProviderDeepSeek] = NewLLMToolClient(localConfig, ProviderDeepSeek)

	config := &ForecasterConfig{
		Clients: clients,
		Weights: map[LLMProvider]float64{
			ProviderDeepSeek: 1.0,
		},
		CacheTTL: 10 * time.Minute, // Longer cache for local
	}

	return NewForecaster(config), nil
}

// CreateCheapForecaster creates a forecaster using only free/cheap models.
func CreateCheapForecaster(router *tools.ModelRouter) (*Forecaster, error) {
	clients := make(map[LLMProvider]LLMClient)

	// Try DeepSeek first (very cheap)
	deepseekConfig, err := router.GetConfig(tools.TierBalanced, 0) // DeepSeek V3
	if err == nil && deepseekConfig.APIKey != "" {
		clients[ProviderDeepSeek] = NewLLMToolClient(deepseekConfig, ProviderDeepSeek)
	}

	// Try free tier
	freeConfig, err := router.GetConfig(tools.TierFree, 0)
	if err == nil && freeConfig.APIKey != "" {
		clients[ProviderGPT4] = NewLLMToolClient(freeConfig, ProviderGPT4)
	}

	if len(clients) == 0 {
		// Fall back to local
		return CreateLocalForecaster(router)
	}

	weights := map[LLMProvider]float64{}
	for p := range clients {
		weights[p] = 1.0 / float64(len(clients))
	}

	config := &ForecasterConfig{
		Clients:  clients,
		Weights:  weights,
		CacheTTL: 5 * time.Minute,
	}

	return NewForecaster(config), nil
}

// --- Preset Configurations ---

// ForecasterPreset represents a preconfigured forecaster setup.
type ForecasterPreset string

const (
	PresetElite    ForecasterPreset = "elite"    // Best models, highest cost
	PresetBalanced ForecasterPreset = "balanced" // Good mix of quality and cost
	PresetCheap    ForecasterPreset = "cheap"    // Minimize costs
	PresetLocal    ForecasterPreset = "local"    // Ollama only, free
	PresetFast     ForecasterPreset = "fast"     // Prioritize speed
)

// CreateForecasterWithPreset creates a forecaster with a specific preset.
func CreateForecasterWithPreset(router *tools.ModelRouter, preset ForecasterPreset) (*Forecaster, error) {
	switch preset {
	case PresetElite:
		return CreateForecasterFromRouter(router)

	case PresetBalanced:
		clients := make(map[LLMProvider]LLMClient)

		// DeepSeek for reasoning (cheap but good)
		if cfg, err := router.GetConfig(tools.TierReasoning, 0); err == nil && cfg.APIKey != "" {
			clients[ProviderDeepSeek] = NewLLMToolClient(cfg, ProviderDeepSeek)
		}

		// GPT via fast tier
		if cfg, err := router.GetConfig(tools.TierFast, 2); err == nil && cfg.APIKey != "" { // o3-mini-high
			clients[ProviderGPT4] = NewLLMToolClient(cfg, ProviderGPT4)
		}

		if len(clients) == 0 {
			return CreateCheapForecaster(router)
		}

		return NewForecaster(&ForecasterConfig{
			Clients: clients,
			Weights: map[LLMProvider]float64{
				ProviderDeepSeek: 0.5,
				ProviderGPT4:     0.5,
			},
			CacheTTL: 5 * time.Minute,
		}), nil

	case PresetCheap:
		return CreateCheapForecaster(router)

	case PresetLocal:
		return CreateLocalForecaster(router)

	case PresetFast:
		clients := make(map[LLMProvider]LLMClient)

		// Cerebras for speed
		if cfg, err := router.GetConfig(tools.TierSuperFast, 0); err == nil && cfg.APIKey != "" {
			clients[ProviderGPT4] = NewLLMToolClient(cfg, ProviderGPT4)
		}

		if len(clients) == 0 {
			// Fall back to local fast model
			if cfg, err := router.GetConfig(tools.TierLocal, 4); err == nil { // Llama3.2 3B
				clients[ProviderDeepSeek] = NewLLMToolClient(cfg, ProviderDeepSeek)
			}
		}

		if len(clients) == 0 {
			return nil, fmt.Errorf("no fast models available")
		}

		return NewForecaster(&ForecasterConfig{
			Clients: clients,
			Weights: map[LLMProvider]float64{
				ProviderGPT4:     1.0,
				ProviderDeepSeek: 1.0,
			},
			CacheTTL: 2 * time.Minute,
		}), nil

	default:
		return CreateForecasterFromRouter(router)
	}
}
