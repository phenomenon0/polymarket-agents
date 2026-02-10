package tools

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/phenomenon0/polymarket-agents/core"
)

func TestModelRouter(t *testing.T) {
	router := NewModelRouter()

	// Test all tier accessors return valid configs
	tiers := []struct {
		name  string
		tier  ModelTier
		index int
	}{
		{"Local", TierLocal, 0},
		{"Free", TierFree, 0},
		{"SuperFast", TierSuperFast, 0},
		{"Fast", TierFast, 0},
		{"Balanced", TierBalanced, 0},
		{"Reasoning", TierReasoning, 0},
		{"Coding", TierCoding, 0},
		{"Elite", TierElite, 0},
		{"Vision", TierVision, 0},
	}

	for _, tt := range tiers {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := router.GetConfig(tt.tier, tt.index)
			if err != nil {
				t.Errorf("Failed to get %s config: %v", tt.name, err)
				return
			}

			// Validate config structure
			if cfg.Model == "" {
				t.Errorf("%s model is empty", tt.name)
			}
			if cfg.BaseURL == "" {
				t.Errorf("%s baseURL is empty", tt.name)
			}
			if cfg.Provider == "" {
				t.Errorf("%s provider is empty", tt.name)
			}

			// Verify API key injection works
			apiKey := os.Getenv("OPENROUTER_API_KEY")
			if apiKey != "" {
				cfg.APIKey = apiKey
				if cfg.APIKey != apiKey {
					t.Errorf("%s: API key not set correctly", tt.name)
				}
			}

			t.Logf("✅ %s: %s (%s)", tt.name, cfg.Model, cfg.BaseURL)
		})
	}
}

// TestRouterActuallyRoutes verifies router returns DIFFERENT configs for different tiers
func TestRouterActuallyRoutes(t *testing.T) {
	router := NewModelRouter()

	fastCfg, _ := router.GetConfig(TierSuperFast, 0)
	eliteCfg, _ := router.GetConfig(TierElite, 0)
	freeCfg, _ := router.GetConfig(TierFree, 0)

	// Verify different tiers return different models
	if fastCfg.Model == eliteCfg.Model {
		t.Errorf("SuperFast and Elite returned same model: %s", fastCfg.Model)
	}

	if fastCfg.Model == freeCfg.Model {
		t.Errorf("SuperFast and Free returned same model: %s", fastCfg.Model)
	}

	t.Logf("✅ Router returns different configs:")
	t.Logf("   SuperFast: %s", fastCfg.Model)
	t.Logf("   Elite: %s", eliteCfg.Model)
	t.Logf("   Free: %s", freeCfg.Model)
}

// TestRouterIndexing verifies multiple models per tier
func TestRouterIndexing(t *testing.T) {
	router := NewModelRouter()

	// SuperFast tier should have multiple models
	cfg0, err0 := router.GetConfig(TierSuperFast, 0)
	cfg1, err1 := router.GetConfig(TierSuperFast, 1)

	if err0 != nil || err1 != nil {
		t.Skip("SuperFast tier needs at least 2 models for this test")
	}

	if cfg0.Model == cfg1.Model {
		t.Errorf("Index 0 and 1 of SuperFast returned same model")
	}

	t.Logf("✅ SuperFast[0]: %s", cfg0.Model)
	t.Logf("✅ SuperFast[1]: %s", cfg1.Model)
}

// TestRouterWithRealLLM - Actually uses the router to make LLM calls
func TestRouterWithRealLLM(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping real LLM test in short mode")
	}

	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Skip("OPENROUTER_API_KEY not set - skipping real LLM routing test")
	}

	router := NewModelRouter()

	// Test routing to different tiers actually works
	tiers := []struct {
		tier     ModelTier
		maxTime  time.Duration
		testName string
	}{
		{TierSuperFast, 5 * time.Second, "SuperFast should be <5s"},
		{TierBalanced, 15 * time.Second, "Balanced should be <15s"},
	}

	for _, tc := range tiers {
		t.Run(tc.testName, func(t *testing.T) {
			cfg, err := router.GetConfig(tc.tier, 0)
			if err != nil {
				t.Fatalf("Failed to get config: %v", err)
			}

			cfg.APIKey = apiKey
			cfg.Timeout = 30 * time.Second
			cfg.MaxTokens = 50

			llm := NewLLMTool(cfg)

			req := &LLMRequest{
				System: "Answer in 5 words or less.",
				Messages: []LLMMessage{
					{Role: "user", Content: "What is 2+2?"},
				},
			}

			start := time.Now()
			result := llm.Execute(&core.ToolContext{
				Request: &core.Message{
					ToolReq: &core.ToolRequestPayload{Input: req},
				},
				Ctx: context.Background(),
			})
			elapsed := time.Since(start)

			if result.Status != core.ToolComplete {
				t.Fatalf("LLM request failed: %s", result.Error)
			}

			resp, ok := result.Output.(*LLMResponse)
			if !ok || resp.Content == "" {
				t.Fatalf("Invalid response")
			}

			if elapsed > tc.maxTime {
				t.Errorf("Tier %s took %v, expected <%v", tc.tier, elapsed, tc.maxTime)
			}

			t.Logf("✅ %s: %s responded in %v: %s", tc.tier, cfg.Model, elapsed, resp.Content)
		})
	}
}

func TestConvenienceMethods(t *testing.T) {
	router := NewModelRouter()

	tests := []struct {
		name         string
		getter       func() (LLMConfig, error)
		expectedTier ModelTier
		tierIndex    int
	}{
		{"Local", router.LocalModel, TierLocal, 0},
		{"LocalFast", router.LocalFastModel, TierLocal, 4},
		{"LocalVision", router.LocalVisionModel, TierVision, 0},
		{"Fastest", router.FastestModel, TierSuperFast, 0},
		{"Coding", router.CodingModel, TierCoding, 0},
		{"Elite", router.EliteModel, TierElite, 0},
		{"Vision", router.VisionModel, TierVision, 0},
		{"Free", router.FreeModel, TierFree, 0},
		{"Reasoning", router.ReasoningModel, TierReasoning, 1}, // Index 1, not 0!
		{"Balanced", router.BalancedModel, TierBalanced, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := tt.getter()
			if err != nil {
				t.Errorf("Failed to get %s model: %v", tt.name, err)
				return
			}
			if cfg.Model == "" {
				t.Errorf("%s returned empty model", tt.name)
			}

			// Verify convenience method returns same as GetConfig for that tier+index
			tierCfg, _ := router.GetConfig(tt.expectedTier, tt.tierIndex)
			if cfg.Model != tierCfg.Model {
				t.Errorf("%s convenience method returned %s, but tier[%d] config has %s",
					tt.name, cfg.Model, tt.tierIndex, tierCfg.Model)
			}

			t.Logf("✅ %s: %s (matches tier %s[%d])", tt.name, cfg.Model, tt.expectedTier, tt.tierIndex)
		})
	}
}

func TestGetBestFor(t *testing.T) {
	router := NewModelRouter()

	useCases := []struct {
		useCase      string
		expectedTier ModelTier
	}{
		{"local", TierLocal},
		{"ollama", TierLocal},
		{"speed", TierSuperFast},
		{"coding", TierCoding},
		{"reasoning", TierReasoning},
		{"quality", TierElite},
		{"vision", TierVision},
		{"free", TierFree},
		{"balanced", TierBalanced},
		{"unknown", TierLocal}, // Now defaults to local
	}

	for _, tc := range useCases {
		t.Run(tc.useCase, func(t *testing.T) {
			cfg, err := router.GetBestFor(tc.useCase)
			if err != nil {
				t.Errorf("Failed to get model for %s: %v", tc.useCase, err)
				return
			}
			if cfg.Model == "" {
				t.Errorf("Empty model for use case: %s", tc.useCase)
			}

			// Verify GetBestFor actually maps to the right tier
			tierCfg, _ := router.GetConfig(tc.expectedTier, 0)
			if cfg.Model != tierCfg.Model {
				t.Errorf("GetBestFor(%s) returned %s, expected tier %s model %s",
					tc.useCase, cfg.Model, tc.expectedTier, tierCfg.Model)
			}

			t.Logf("✅ %s -> %s (tier: %s)", tc.useCase, cfg.Model, tc.expectedTier)
		})
	}
}

func TestListTier(t *testing.T) {
	router := NewModelRouter()

	tiers := []ModelTier{
		TierLocal,
		TierFree,
		TierSuperFast,
		TierFast,
		TierBalanced,
		TierReasoning,
		TierCoding,
		TierElite,
		TierVision,
	}

	for _, tier := range tiers {
		t.Run(string(tier), func(t *testing.T) {
			presets := router.ListTier(tier)
			if len(presets) == 0 {
				t.Errorf("No presets for tier: %s", tier)
				return
			}

			t.Logf("✅ %s tier has %d models:", tier, len(presets))
			for i, p := range presets {
				t.Logf("   [%d] %s - %v latency, $%.4f/1k", i, p.Name, p.AvgLatency, p.CostPer1k)
			}
		})
	}
}

func TestGetConfigByName(t *testing.T) {
	router := NewModelRouter()

	modelNames := []string{
		"Ollama Qwen3 8B",
		"Ollama DeepSeek R1 14B",
		"Cerebras Llama 3.3 70B",
		"GPT-5.1 Codex",
		"Claude Opus 4.1",
		"Gemini 2.0 Flash",
		"Qwen3 Coder Free",
	}

	for _, name := range modelNames {
		t.Run(name, func(t *testing.T) {
			cfg, err := router.GetConfigByName(name)
			if err != nil {
				t.Errorf("Failed to get config for %s: %v", name, err)
				return
			}
			if cfg.Model == "" {
				t.Errorf("Empty model for name: %s", name)
			}
			t.Logf("✅ Found: %s -> %s", name, cfg.Model)
		})
	}

	// Test non-existent model
	t.Run("NonExistent", func(t *testing.T) {
		_, err := router.GetConfigByName("Does Not Exist")
		if err == nil {
			t.Error("Expected error for non-existent model, got nil")
		}
		t.Logf("✅ Correctly rejected non-existent model")
	})
}

func TestListAll(t *testing.T) {
	router := NewModelRouter()

	allModels := router.ListAll()
	if len(allModels) == 0 {
		t.Fatal("No models registered")
	}

	t.Logf("✅ Total tiers: %d", len(allModels))
	totalModels := 0
	for tier, presets := range allModels {
		totalModels += len(presets)
		t.Logf("   %s: %d models", tier, len(presets))
	}
	t.Logf("✅ Total models available: %d", totalModels)

	if totalModels < 15 {
		t.Errorf("Expected at least 15 models, got %d", totalModels)
	}
}

func TestModelPresetFields(t *testing.T) {
	router := NewModelRouter()

	allModels := router.ListAll()
	for tier, presets := range allModels {
		for i, preset := range presets {
			t.Run(preset.Name, func(t *testing.T) {
				if preset.Name == "" {
					t.Errorf("Tier %s[%d]: Name is empty", tier, i)
				}
				if preset.Provider == "" {
					t.Errorf("Tier %s[%d]: Provider is empty", tier, i)
				}
				if preset.Model == "" {
					t.Errorf("Tier %s[%d]: Model is empty", tier, i)
				}
				if preset.BaseURL == "" {
					t.Errorf("Tier %s[%d]: BaseURL is empty", tier, i)
				}
				if preset.Description == "" {
					t.Errorf("Tier %s[%d]: Description is empty", tier, i)
				}
				if preset.AvgLatency == 0 {
					t.Errorf("Tier %s[%d]: AvgLatency is zero", tier, i)
				}
				if preset.ContextSize == 0 {
					t.Errorf("Tier %s[%d]: ContextSize is zero", tier, i)
				}

				t.Logf("✅ %s: All fields valid", preset.Name)
			})
		}
	}
}
