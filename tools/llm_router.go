package tools

import (
	"fmt"
	"os"
	"time"
)

// === LLM Router: Intelligent Model Selection ===

// ModelTier represents different quality/speed tiers
type ModelTier string

const (
	TierLocal     ModelTier = "local"     // Local models via Ollama (FREE, offline)
	TierFree      ModelTier = "free"      // Free cloud models for testing
	TierSuperFast ModelTier = "superfast" // Ultra-fast (0.2-1s)
	TierFast      ModelTier = "fast"      // Fast (1-3s)
	TierBalanced  ModelTier = "balanced"  // Good balance (3-6s)
	TierReasoning ModelTier = "reasoning" // Deep reasoning (2-5s)
	TierCoding    ModelTier = "coding"    // Specialized for code
	TierElite     ModelTier = "elite"     // Highest quality (10-15s)
	TierVision    ModelTier = "vision"    // Multimodal vision
)

// ModelPreset contains curated model configurations
type ModelPreset struct {
	Name        string
	Provider    string
	Model       string
	BaseURL     string
	Description string
	Tier        ModelTier
	AvgLatency  time.Duration
	CostPer1k   float64 // USD per 1k tokens (avg prompt+completion)
	ContextSize int     // Context window in tokens
}

// ModelRouter helps select the best model for a task
type ModelRouter struct {
	presets map[ModelTier][]ModelPreset
	apiKeys map[string]string
}

// NewModelRouter creates a router with curated presets
func NewModelRouter() *ModelRouter {
	router := &ModelRouter{
		presets: make(map[ModelTier][]ModelPreset),
		apiKeys: map[string]string{
			"cerebras":   os.Getenv("CEREBRAS_API_KEY"),
			"openrouter": os.Getenv("OPENROUTER_API_KEY"),
			"anthropic":  os.Getenv("ANTHROPIC_API_KEY"),
			"kimi":       os.Getenv("KIMI_API_KEY"),
			"deepseek":   os.Getenv("DEEPSEEK_API_KEY"),
		},
	}

	router.initPresets()
	return router
}

func (r *ModelRouter) initPresets() {
	// LOCAL TIER - Ollama models (FREE, offline, private)
	ollamaURL := os.Getenv("OLLAMA_URL")
	if ollamaURL == "" {
		ollamaURL = "http://localhost:11434"
	}

	r.presets[TierLocal] = []ModelPreset{
		{
			Name:        "Ollama Qwen3 8B",
			Provider:    "ollama",
			Model:       "qwen3:8b",
			BaseURL:     ollamaURL,
			Description: "Local Qwen3 8B - great all-rounder",
			Tier:        TierLocal,
			AvgLatency:  3 * time.Second,
			CostPer1k:   0.0,
			ContextSize: 32000,
		},
		{
			Name:        "Ollama DeepSeek R1 14B",
			Provider:    "ollama",
			Model:       "deepseek-r1:14b",
			BaseURL:     ollamaURL,
			Description: "Local DeepSeek R1 reasoning model",
			Tier:        TierLocal,
			AvgLatency:  5 * time.Second,
			CostPer1k:   0.0,
			ContextSize: 32000,
		},
		{
			Name:        "Ollama Gemma2 27B",
			Provider:    "ollama",
			Model:       "gemma2:27b-instruct-q4_K_M",
			BaseURL:     ollamaURL,
			Description: "Local Gemma2 27B - high quality",
			Tier:        TierLocal,
			AvgLatency:  8 * time.Second,
			CostPer1k:   0.0,
			ContextSize: 8000,
		},
		{
			Name:        "Ollama Mistral Small 24B",
			Provider:    "ollama",
			Model:       "mistral-small:24b",
			BaseURL:     ollamaURL,
			Description: "Local Mistral Small - fast & capable",
			Tier:        TierLocal,
			AvgLatency:  6 * time.Second,
			CostPer1k:   0.0,
			ContextSize: 32000,
		},
		{
			Name:        "Ollama Llama3.2 3B",
			Provider:    "ollama",
			Model:       "llama3.2:3b",
			BaseURL:     ollamaURL,
			Description: "Local Llama3.2 3B - ultra fast",
			Tier:        TierLocal,
			AvgLatency:  1 * time.Second,
			CostPer1k:   0.0,
			ContextSize: 128000,
		},
		{
			Name:        "Ollama Qwen2.5 1.5B",
			Provider:    "ollama",
			Model:       "qwen2.5:1.5b",
			BaseURL:     ollamaURL,
			Description: "Local Qwen2.5 1.5B - tiny & instant",
			Tier:        TierLocal,
			AvgLatency:  500 * time.Millisecond,
			CostPer1k:   0.0,
			ContextSize: 32000,
		},
	}

	// LOCAL VISION TIER - added to TierVision later, but also here for convenience
	// Vision models are also in TierVision

	// FREE TIER - Testing & Development
	r.presets[TierFree] = []ModelPreset{
		{
			Name:        "Qwen3 Coder Free",
			Provider:    "openai",
			Model:       "qwen/qwen3-coder:free",
			BaseURL:     "https://openrouter.ai/api/v1",
			Description: "Free coding model, great for testing",
			Tier:        TierFree,
			AvgLatency:  6 * time.Second,
			CostPer1k:   0.0,
			ContextSize: 32000,
		},
		{
			Name:        "Grok 4.1 Fast Free",
			Provider:    "openai",
			Model:       "x-ai/grok-4.1-fast:free",
			BaseURL:     "https://openrouter.ai/api/v1",
			Description: "Free tier Grok, 2M context",
			Tier:        TierFree,
			AvgLatency:  8 * time.Second,
			CostPer1k:   0.0,
			ContextSize: 2000000,
		},
	}

	// SUPER FAST TIER - Cerebras (0.2-1s)
	r.presets[TierSuperFast] = []ModelPreset{
		{
			Name:        "Cerebras Llama 3.3 70B",
			Provider:    "openai",
			Model:       "llama-3.3-70b",
			BaseURL:     "https://api.cerebras.ai/v1",
			Description: "FASTEST model on the planet (0.2s)",
			Tier:        TierSuperFast,
			AvgLatency:  200 * time.Millisecond,
			CostPer1k:   0.002,
			ContextSize: 131000,
		},
		{
			Name:        "Cerebras Qwen 3 32B",
			Provider:    "openai",
			Model:       "qwen-3-32b",
			BaseURL:     "https://api.cerebras.ai/v1",
			Description: "Ultra-fast multilingual (0.4s)",
			Tier:        TierSuperFast,
			AvgLatency:  400 * time.Millisecond,
			CostPer1k:   0.002,
			ContextSize: 32000,
		},
		{
			Name:        "Cerebras Llama 3.1 8B",
			Provider:    "openai",
			Model:       "llama3.1-8b",
			BaseURL:     "https://api.cerebras.ai/v1",
			Description: "Tiny but blazing fast (0.1s)",
			Tier:        TierSuperFast,
			AvgLatency:  100 * time.Millisecond,
			CostPer1k:   0.001,
			ContextSize: 131000,
		},
	}

	// FAST TIER - (1-3s)
	r.presets[TierFast] = []ModelPreset{
		{
			Name:        "GPT-5.1",
			Provider:    "openai",
			Model:       "openai/gpt-5.1",
			BaseURL:     "https://openrouter.ai/api/v1",
			Description: "Latest OpenAI flagship, very fast",
			Tier:        TierFast,
			AvgLatency:  1 * time.Second,
			CostPer1k:   0.011,
			ContextSize: 400000,
		},
		{
			Name:        "Cerebras GLM 4.6",
			Provider:    "openai",
			Model:       "zai-glm-4.6",
			BaseURL:     "https://api.cerebras.ai/v1",
			Description: "Chinese + English with reasoning (1.6s)",
			Tier:        TierFast,
			AvgLatency:  1600 * time.Millisecond,
			CostPer1k:   0.002,
			ContextSize: 128000,
		},
		{
			Name:        "o3-mini-high",
			Provider:    "openai",
			Model:       "openai/o3-mini-high",
			BaseURL:     "https://openrouter.ai/api/v1",
			Description: "OpenAI o3-mini-high (2025), fast + cheap",
			Tier:        TierFast,
			AvgLatency:  2 * time.Second,
			CostPer1k:   0.0044,
			ContextSize: 200000,
		},
	}

	// BALANCED TIER - (3-6s)
	r.presets[TierBalanced] = []ModelPreset{
		{
			Name:        "DeepSeek V3 (Direct)",
			Provider:    "deepseek",
			Model:       "deepseek-chat",
			BaseURL:     "https://api.deepseek.com/v1",
			Description: "Excellent value, direct API, 64k context",
			Tier:        TierBalanced,
			AvgLatency:  2 * time.Second,
			CostPer1k:   0.00069, // $0.27/M input + $1.10/M output avg
			ContextSize: 64000,
		},
		{
			Name:        "o4-mini",
			Provider:    "openai",
			Model:       "openai/o4-mini",
			BaseURL:     "https://openrouter.ai/api/v1",
			Description: "Fast reasoning model, good balance",
			Tier:        TierBalanced,
			AvgLatency:  3 * time.Second,
			CostPer1k:   0.005,
			ContextSize: 200000,
		},
		{
			Name:        "Kimi K2 128k",
			Provider:    "openai",
			Model:       "moonshot-v1-128k",
			BaseURL:     "https://api.moonshot.cn/v1",
			Description: "Kimi K2 long-context, fast Chinese/English",
			Tier:        TierBalanced,
			AvgLatency:  6 * time.Second,
			CostPer1k:   0.008,
			ContextSize: 128000,
		},
		{
			Name:        "Qwen3 Max",
			Provider:    "openai",
			Model:       "qwen/qwen3-max",
			BaseURL:     "https://openrouter.ai/api/v1",
			Description: "Alibaba's best, multilingual",
			Tier:        TierBalanced,
			AvgLatency:  5 * time.Second,
			CostPer1k:   0.007,
			ContextSize: 256000,
		},
	}

	// REASONING TIER - Deep thinking models
	r.presets[TierReasoning] = []ModelPreset{
		{
			Name:        "DeepSeek R1 (Direct)",
			Provider:    "deepseek",
			Model:       "deepseek-reasoner",
			BaseURL:     "https://api.deepseek.com/v1",
			Description: "97.3% MATH-500, best reasoning model",
			Tier:        TierReasoning,
			AvgLatency:  3 * time.Second,
			CostPer1k:   0.00219, // $0.55/M input + $2.19/M output avg
			ContextSize: 64000,
		},
		{
			Name:        "o3-pro",
			Provider:    "openai",
			Model:       "openai/o3-pro",
			BaseURL:     "https://openrouter.ai/api/v1",
			Description: "Universe-scale complexity reasoning",
			Tier:        TierReasoning,
			AvgLatency:  8 * time.Second,
			CostPer1k:   0.100,
			ContextSize: 200000,
		},
		{
			Name:        "DeepSeek V3 (Cogito)",
			Provider:    "openai",
			Model:       "deepcogito/cogito-v2.1-671b",
			BaseURL:     "https://openrouter.ai/api/v1",
			Description: "671B MoE, GPT-4 level reasoning (via OpenRouter)",
			Tier:        TierReasoning,
			AvgLatency:  12 * time.Second,
			CostPer1k:   0.00125,
			ContextSize: 128000,
		},
	}

	// CODING TIER - Specialized for code
	r.presets[TierCoding] = []ModelPreset{
		{
			Name:        "DeepSeek Coder (Direct)",
			Provider:    "deepseek",
			Model:       "deepseek-coder",
			BaseURL:     "https://api.deepseek.com/v1",
			Description: "Excellent coding model, direct API",
			Tier:        TierCoding,
			AvgLatency:  2 * time.Second,
			CostPer1k:   0.00069, // Same pricing as deepseek-chat
			ContextSize: 64000,
		},
		{
			Name:        "GPT-5.1 Codex",
			Provider:    "openai",
			Model:       "openai/gpt-5.1-codex",
			BaseURL:     "https://openrouter.ai/api/v1",
			Description: "Best coding model (1.4s)",
			Tier:        TierCoding,
			AvgLatency:  1400 * time.Millisecond,
			CostPer1k:   0.011,
			ContextSize: 400000,
		},
		{
			Name:        "Qwen3 Coder Free",
			Provider:    "openai",
			Model:       "qwen/qwen3-coder:free",
			BaseURL:     "https://openrouter.ai/api/v1",
			Description: "Free coding model for testing",
			Tier:        TierCoding,
			AvgLatency:  6 * time.Second,
			CostPer1k:   0.0,
			ContextSize: 32000,
		},
	}

	// ELITE TIER - Highest quality models
	r.presets[TierElite] = []ModelPreset{
		{
			Name:        "Claude Sonnet 4.5",
			Provider:    "openai",
			Model:       "anthropic/claude-sonnet-4.5",
			BaseURL:     "https://openrouter.ai/api/v1",
			Description: "Claude Sonnet 4.5 via OpenRouter",
			Tier:        TierElite,
			AvgLatency:  8 * time.Second,
			CostPer1k:   0.015,
			ContextSize: 200000,
		},
		{
			Name:        "Claude Opus 4.5",
			Provider:    "openai",
			Model:       "anthropic/claude-opus-4.5",
			BaseURL:     "https://openrouter.ai/api/v1",
			Description: "Latest Claude Opus 4.5 via OpenRouter",
			Tier:        TierElite,
			AvgLatency:  10 * time.Second,
			CostPer1k:   0.025,
			ContextSize: 200000,
		},
		{
			Name:        "Claude Opus 4.1",
			Provider:    "openai",
			Model:       "anthropic/claude-opus-4.1",
			BaseURL:     "https://openrouter.ai/api/v1",
			Description: "Anthropic's best, top-tier reasoning (4.1)",
			Tier:        TierElite,
			AvgLatency:  12 * time.Second,
			CostPer1k:   0.105,
			ContextSize: 200000,
		},
		{
			Name:        "GPT-4o Extended",
			Provider:    "openai",
			Model:       "openai/gpt-4o",
			BaseURL:     "https://openrouter.ai/api/v1",
			Description: "OpenAI's previous flagship, proven quality",
			Tier:        TierElite,
			AvgLatency:  13 * time.Second,
			CostPer1k:   0.024,
			ContextSize: 128000,
		},
	}

	// VISION TIER - Multimodal models
	r.presets[TierVision] = []ModelPreset{
		{
			Name:        "Ollama Qwen3-VL 2B",
			Provider:    "ollama",
			Model:       "qwen3-vl:2b",
			BaseURL:     ollamaURL,
			Description: "Local vision model - FREE & private",
			Tier:        TierVision,
			AvgLatency:  3 * time.Second,
			CostPer1k:   0.0,
			ContextSize: 32000,
		},
		{
			Name:        "Ollama Llama3.2 Vision 11B",
			Provider:    "ollama",
			Model:       "llama3.2-vision:11b",
			BaseURL:     ollamaURL,
			Description: "Local Llama vision - high quality",
			Tier:        TierVision,
			AvgLatency:  8 * time.Second,
			CostPer1k:   0.0,
			ContextSize: 128000,
		},
		{
			Name:        "Gemini 2.0 Flash",
			Provider:    "openai",
			Model:       "google/gemini-2.0-flash-exp:free",
			BaseURL:     "https://openrouter.ai/api/v1",
			Description: "Google's latest vision model (FREE)",
			Tier:        TierVision,
			AvgLatency:  8 * time.Second,
			CostPer1k:   0.0,
			ContextSize: 1000000,
		},
		{
			Name:        "GPT-4o Vision",
			Provider:    "openai",
			Model:       "openai/gpt-4o",
			BaseURL:     "https://openrouter.ai/api/v1",
			Description: "OpenAI multimodal, strong vision",
			Tier:        TierVision,
			AvgLatency:  13 * time.Second,
			CostPer1k:   0.024,
			ContextSize: 128000,
		},
	}
}

// GetConfig returns an LLMConfig for the specified tier and index
func (r *ModelRouter) GetConfig(tier ModelTier, index int) (LLMConfig, error) {
	presets, ok := r.presets[tier]
	if !ok {
		return LLMConfig{}, fmt.Errorf("unknown tier: %s", tier)
	}

	if index < 0 || index >= len(presets) {
		return LLMConfig{}, fmt.Errorf("index %d out of range for tier %s (has %d models)", index, tier, len(presets))
	}

	preset := presets[index]
	apiKey := r.apiKeys[preset.BaseURL]
	if apiKey == "" {
		// Try to get from preset base URL
		switch {
		case preset.Provider == "ollama":
			apiKey = "ollama" // Ollama doesn't need a key, but set something
		case preset.BaseURL == "https://api.cerebras.ai/v1":
			apiKey = r.apiKeys["cerebras"]
		case preset.BaseURL == "https://openrouter.ai/api/v1":
			apiKey = r.apiKeys["openrouter"]
		case preset.BaseURL == "https://api.anthropic.com/v1":
			apiKey = r.apiKeys["anthropic"]
		case preset.BaseURL == "https://api.moonshot.cn/v1":
			apiKey = r.apiKeys["kimi"]
		case preset.BaseURL == "https://api.deepseek.com/v1":
			apiKey = r.apiKeys["deepseek"]
		}
	}

	return LLMConfig{
		Provider:    preset.Provider,
		Model:       preset.Model,
		APIKey:      apiKey,
		BaseURL:     preset.BaseURL,
		MaxTokens:   4096,
		Temperature: 0.7,
		Timeout:     60 * time.Second,
	}, nil
}

// GetBestFor returns the best model for a specific use case
func (r *ModelRouter) GetBestFor(useCase string) (LLMConfig, error) {
	switch useCase {
	case "local", "ollama", "offline", "private":
		return r.GetConfig(TierLocal, 0) // Ollama Qwen3 8B

	case "local-fast", "ollama-fast":
		return r.GetConfig(TierLocal, 4) // Ollama Llama3.2 3B

	case "local-reasoning", "ollama-reasoning":
		return r.GetConfig(TierLocal, 1) // Ollama DeepSeek R1 14B

	case "local-vision", "ollama-vision":
		return r.GetConfig(TierVision, 0) // Ollama Qwen3-VL 2B

	case "speed", "fast", "quick":
		return r.GetConfig(TierSuperFast, 0) // Cerebras Llama 3.3 70B

	case "coding", "code", "programming":
		return r.GetConfig(TierCoding, 0) // DeepSeek Coder

	case "reasoning", "think", "complex":
		return r.GetConfig(TierReasoning, 0) // DeepSeek R1

	case "quality", "best", "elite":
		return r.GetConfig(TierElite, 0) // Claude Sonnet 4.5

	case "vision", "image", "multimodal":
		return r.GetConfig(TierVision, 0) // Ollama Qwen3-VL (local first)

	case "free", "test", "testing":
		return r.GetConfig(TierFree, 0) // Qwen3 Coder Free

	case "balanced", "default":
		return r.GetConfig(TierBalanced, 0) // DeepSeek V3

	case "chinese", "multilingual":
		return r.GetConfig(TierFast, 1) // Cerebras GLM 4.6

	default:
		// Default to local if available, else superfast
		return r.GetConfig(TierLocal, 0)
	}
}

// GetPreset returns the ModelPreset for a tier and index
func (r *ModelRouter) GetPreset(tier ModelTier, index int) (ModelPreset, error) {
	presets, ok := r.presets[tier]
	if !ok {
		return ModelPreset{}, fmt.Errorf("unknown tier: %s", tier)
	}

	if index < 0 || index >= len(presets) {
		return ModelPreset{}, fmt.Errorf("index %d out of range for tier %s (has %d models)", index, tier, len(presets))
	}

	return presets[index], nil
}

// GetPresetByName finds a preset by name
func (r *ModelRouter) GetPresetByName(name string) (ModelPreset, error) {
	for _, presets := range r.presets {
		for _, preset := range presets {
			if preset.Name == name {
				return preset, nil
			}
		}
	}
	return ModelPreset{}, fmt.Errorf("model not found: %s", name)
}

// ListTier returns all models in a tier
func (r *ModelRouter) ListTier(tier ModelTier) []ModelPreset {
	return r.presets[tier]
}

// ListAll returns all available presets organized by tier
func (r *ModelRouter) ListAll() map[ModelTier][]ModelPreset {
	return r.presets
}

// GetConfigByName finds a model by name across all tiers
func (r *ModelRouter) GetConfigByName(name string) (LLMConfig, error) {
	for _, presets := range r.presets {
		for _, preset := range presets {
			if preset.Name == name {
				apiKey := r.apiKeys[preset.BaseURL]
				if apiKey == "" {
					switch {
					case preset.Provider == "ollama":
						apiKey = "ollama"
					case preset.BaseURL == "https://api.cerebras.ai/v1":
						apiKey = r.apiKeys["cerebras"]
					case preset.BaseURL == "https://openrouter.ai/api/v1":
						apiKey = r.apiKeys["openrouter"]
					case preset.BaseURL == "https://api.anthropic.com/v1":
						apiKey = r.apiKeys["anthropic"]
					case preset.BaseURL == "https://api.deepseek.com/v1":
						apiKey = r.apiKeys["deepseek"]
					}
				}

				return LLMConfig{
					Provider:    preset.Provider,
					Model:       preset.Model,
					APIKey:      apiKey,
					BaseURL:     preset.BaseURL,
					MaxTokens:   4096,
					Temperature: 0.7,
					Timeout:     60 * time.Second,
				}, nil
			}
		}
	}
	return LLMConfig{}, fmt.Errorf("model not found: %s", name)
}

// Convenience functions for common use cases

func (r *ModelRouter) LocalModel() (LLMConfig, error) {
	return r.GetConfig(TierLocal, 0) // Ollama Qwen3 8B
}

func (r *ModelRouter) LocalFastModel() (LLMConfig, error) {
	return r.GetConfig(TierLocal, 4) // Ollama Llama3.2 3B
}

func (r *ModelRouter) LocalVisionModel() (LLMConfig, error) {
	return r.GetConfig(TierVision, 0) // Ollama Qwen3-VL
}

func (r *ModelRouter) FastestModel() (LLMConfig, error) {
	return r.GetConfig(TierSuperFast, 0)
}

func (r *ModelRouter) CodingModel() (LLMConfig, error) {
	return r.GetConfig(TierCoding, 0)
}

func (r *ModelRouter) EliteModel() (LLMConfig, error) {
	return r.GetConfig(TierElite, 0)
}

func (r *ModelRouter) VisionModel() (LLMConfig, error) {
	return r.GetConfig(TierVision, 0)
}

func (r *ModelRouter) FreeModel() (LLMConfig, error) {
	return r.GetConfig(TierFree, 0)
}

func (r *ModelRouter) ReasoningModel() (LLMConfig, error) {
	return r.GetConfig(TierReasoning, 1) // DeepSeek V3
}

func (r *ModelRouter) BalancedModel() (LLMConfig, error) {
	return r.GetConfig(TierBalanced, 0)
}
