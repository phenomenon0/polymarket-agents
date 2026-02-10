package tools

import (
	"github.com/phenomenon0/polymarket-agents/core"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// === LLM Tool Configuration ===

type LLMConfig struct {
	Provider    string // "openai", "anthropic", "ollama", "deepseek", "openrouter"
	Model       string
	Tier        string // optional router tier metadata
	Preset      string // optional preset/name from router
	APIKey      string
	BaseURL     string
	MaxTokens   int
	Temperature float64
	Timeout     time.Duration
	RetryPolicy RetryPolicy
}

type RetryPolicy struct {
	MaxRetries int
	Backoff    time.Duration
}

type CostTracker struct {
	TotalTokens      int64
	PromptTokens     int64
	CompletionTokens int64
	EstimatedCostUSD float64
	lastCost         float64
}

// Rough rate table (USD per token) for December 2025 SOTA models; fallback uses heuristics.
var modelRates = []struct {
	match       string
	inputRate   float64
	outputRate  float64
	matchPrefix bool
}{
	// ===== DECEMBER 2025 SOTA MODELS =====

	// Claude Opus 4.5 (Nov 24, 2025) - 80.9% SWE-bench Verified ⭐
	{"claude-opus-4.5", 0.000003, 0.000015, false},
	{"claude-sonnet-4.5", 0.000003, 0.000015, false},

	// GPT-5.1 (Nov 13, 2025) - 2-3x faster than GPT-5
	{"gpt-5.1", 0.00000125, 0.000010, true},

	// Gemini 3 Pro (Nov 18, 2025) - 1501 LMArena Elo ⭐
	{"gemini-3-pro", 0.000002, 0.000012, true},

	// DeepSeek R1 (Jan 2025) - 97.3% MATH-500 ⭐
	{"deepseek-r1", 0.0000004, 0.00000175, false},

	// DeepSeek V3.2 (Dec 2025) - 96% AIME, insane value ⭐
	{"deepseek-v3.2", 0.00000028, 0.00000041, true},
	{"deepseek-v3", 0.00000015, 0.00000075, true},

	// Gemini 2.5 Flash-Lite - Ultra-cheap, 1M context
	{"gemini-2.5-flash-lite", 0.0000001, 0.0000004, false},

	// Gemini 2.5 Pro - Excellent multimodal
	{"gemini-2.5-pro", 0.00000125, 0.000010, false},

	// ===== LEGACY MODELS (kept for compatibility) =====

	// GPT-5 (Aug 2025)
	{"gpt-5", 0.000002, 0.000008, true},

	// GPT-4o series
	{"gpt-4o-mini", 0.000000150, 0.000000600, false},
	{"gpt-4o", 0.0000050, 0.0000150, true},
	{"gpt-4.1", 0.0000300, 0.0000600, true},
	{"gpt-4", 0.0000300, 0.0000600, true},

	// O1/O3 reasoning series
	{"o3-", 0.0000300, 0.0000600, true},

	// Claude legacy
	{"claude-3.5", 0.0000030, 0.0000150, true},
	{"claude-3-opus", 0.0000150, 0.0000750, false},
	{"claude-3-sonnet", 0.0000030, 0.0000150, false},
	{"claude-3-haiku", 0.00000025, 0.00000125, false},

	// DeepSeek legacy
	{"deepseek", 0.00000014, 0.00000028, true},

	// Open source models
	{"llama-3.1", 0.00000050, 0.00000070, true},
	{"llama-3", 0.00000060, 0.00000080, true},
	{"llama3", 0.00000060, 0.00000080, true},
	{"qwen3", 0.00000050, 0.00000070, true},
	{"glm", 0.00000100, 0.00000200, true},
	{"moonshot", 0.0000020, 0.0000060, true},
	{"grok-4.1", 0.00000050, 0.00000050, true},
}

func rateForModel(model string) (float64, float64, bool) {
	lower := strings.ToLower(model)
	for _, r := range modelRates {
		if r.matchPrefix {
			if strings.HasPrefix(lower, strings.ToLower(r.match)) {
				return r.inputRate, r.outputRate, true
			}
		} else if strings.Contains(lower, strings.ToLower(r.match)) {
			return r.inputRate, r.outputRate, true
		}
	}
	return 0, 0, false
}

func calculateCost(model string, promptTokens, completionTokens int) float64 {
	// Prefer explicit rates
	if in, out, ok := rateForModel(model); ok {
		return (float64(promptTokens) * in) + (float64(completionTokens) * out)
	}

	// Fallback heuristic: cheap default with GPT-4 premium uplift
	rateInput := 0.000005  // $5 per 1M tokens
	rateOutput := 0.000015 // $15 per 1M tokens

	if strings.Contains(strings.ToLower(model), "gpt-4") {
		rateInput = 0.00003
		rateOutput = 0.00006
	}

	return (float64(promptTokens) * rateInput) + (float64(completionTokens) * rateOutput)
}

func (c *CostTracker) AddUsage(prompt, completion int, model string) {
	c.PromptTokens += int64(prompt)
	c.CompletionTokens += int64(completion)
	c.TotalTokens += int64(prompt + completion)
	cost := calculateCost(model, prompt, completion)
	c.EstimatedCostUSD += cost
	c.lastCost = cost
}

func (c *CostTracker) LastCost() float64 {
	return c.lastCost
}

var DefaultOpenAIConfig = LLMConfig{
	Provider:    "openai",
	Model:       "gpt-4o-mini",
	BaseURL:     "https://api.openai.com/v1",
	MaxTokens:   4096,
	Temperature: 0.7,
	Timeout:     60 * time.Second,
}

var DefaultAnthropicConfig = LLMConfig{
	Provider:    "anthropic",
	Model:       "claude-sonnet-4-20250514",
	BaseURL:     "https://api.anthropic.com/v1",
	MaxTokens:   4096,
	Temperature: 0.7,
	Timeout:     60 * time.Second,
}

var DefaultOllamaConfig = LLMConfig{
	Provider:    "ollama",
	Model:       "llama3.2",
	BaseURL:     "http://localhost:11434",
	MaxTokens:   4096,
	Temperature: 0.7,
	Timeout:     120 * time.Second,
}

var DefaultOpenRouterConfig = LLMConfig{
	Provider:    "openrouter",
	Model:       "gemini-2.5-flash-lite", // Gemini 2.5 Flash-Lite - Ultra-fast & cheapest ($0.25/M)
	BaseURL:     "https://openrouter.ai/api/v1",
	MaxTokens:   4096,
	Temperature: 0.7,
	Timeout:     60 * time.Second,
}

// === LLM Request/Response ===

type LLMMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type LLMRequest struct {
	Messages    []LLMMessage `json:"messages"`
	System      string       `json:"system,omitempty"`
	MaxTokens   int          `json:"max_tokens,omitempty"`
	Temperature float64      `json:"temperature,omitempty"`
}

type LLMResponse struct {
	Content      string `json:"content"`
	Model        string `json:"model"`
	FinishReason string `json:"finish_reason"`
	Usage        struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// === LLM Tool ===

type LLMTool struct {
	config      LLMConfig
	client      *http.Client
	costTracker *CostTracker
}

func (t *LLMTool) parseRequest(input any) (*LLMRequest, error) {
	var req LLMRequest
	switch input := input.(type) {
	case LLMRequest:
		req = input
	case *LLMRequest:
		req = *input
	case string:
		req = LLMRequest{
			Messages: []LLMMessage{{Role: "user", Content: input}},
		}
	case map[string]any:
		data, _ := json.Marshal(input)
		if err := json.Unmarshal(data, &req); err != nil {
			return nil, fmt.Errorf("invalid LLM request input: %v", err)
		}
	default:
		return nil, fmt.Errorf("unsupported input type: %T", input)
	}
	return &req, nil
}

func (t *LLMTool) applyDefaults(req *LLMRequest) {
	if req.MaxTokens == 0 {
		req.MaxTokens = t.config.MaxTokens
	}
	if req.Temperature == 0 {
		req.Temperature = t.config.Temperature
	}
}

func (t *LLMTool) normalizeRequest(ctx *core.ToolContext) (*LLMRequest, *core.ToolExecResult) {
	req, err := t.parseRequest(ctx.Request.ToolReq.Input)
	if err != nil {
		return nil, &core.ToolExecResult{
			Status: core.ToolFailed,
			Error:  err.Error(),
		}
	}

	t.applyDefaults(req)
	return req, nil
}

func NewLLMTool(config LLMConfig) *LLMTool {
	// Create HTTP client with connection pooling for better performance
	transport := &http.Transport{
		MaxIdleConns:        20,               // Max idle connections across all hosts
		MaxIdleConnsPerHost: 10,               // Max idle connections per host (LLM APIs)
		MaxConnsPerHost:     20,               // Max total connections per host
		IdleConnTimeout:     90 * time.Second, // How long idle connections stay open
		DisableKeepAlives:   false,            // Enable connection reuse
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second, // Connection timeout (generous for LLM APIs)
			KeepAlive: 30 * time.Second, // TCP keepalive
		}).DialContext,
		ForceAttemptHTTP2:     true,              // Try HTTP/2
		TLSHandshakeTimeout:   15 * time.Second,  // TLS handshake timeout
		ResponseHeaderTimeout: 120 * time.Second, // Waiting for response headers (LLMs can be slow)
	}

	return &LLMTool{
		config: config,
		client: &http.Client{
			Transport: transport,
			Timeout:   config.Timeout,
		},
		costTracker: &CostTracker{},
	}
}

func (t *LLMTool) Cost() *CostTracker {
	return t.costTracker
}

func (t *LLMTool) Name() string {
	return "llm"
}

// EstimateCost provides a preflight cost estimate (tokens + USD) for budgeting.
func (t *LLMTool) EstimateCost(input any) (float64, int, int, bool) {
	req, err := t.parseRequest(input)
	if err != nil {
		return 0, 0, 0, false
	}
	t.applyDefaults(req)

	promptTokens := estimatePromptTokens(req)
	// Completion tokens unknown pre-call; use MaxTokens as an upper bound.
	completionTokens := req.MaxTokens
	if completionTokens == 0 {
		completionTokens = t.config.MaxTokens
	}

	cost := calculateCost(t.config.Model, promptTokens, completionTokens)
	return cost, promptTokens, completionTokens, true
}

// ExecuteStream uses provider-native streaming when available; otherwise falls back to chunking the final response.
func (t *LLMTool) ExecuteStream(ctx *core.ToolContext) (<-chan *core.ToolChunk, <-chan *core.ToolExecResult) {
	chunkChan := make(chan *core.ToolChunk, 8)
	resultChan := make(chan *core.ToolExecResult, 1)

	req, errRes := t.normalizeRequest(ctx)
	if errRes != nil {
		defer close(chunkChan)
		resultChan <- errRes
		close(resultChan)
		return chunkChan, resultChan
	}

	switch t.config.Provider {
	case "openai":
		go t.streamOpenAI(ctx, req, chunkChan, resultChan)
	case "anthropic":
		go t.streamAnthropic(ctx, req, chunkChan, resultChan)
	case "openrouter":
		go t.streamOpenAI(ctx, req, chunkChan, resultChan) // OpenRouter is OpenAI-compatible
	case "deepseek":
		go t.streamOpenAI(ctx, req, chunkChan, resultChan) // DeepSeek is OpenAI-compatible
	default:
		// Fallback: non-streaming call then chunk locally
		go func() {
			defer close(chunkChan)
			defer close(resultChan)
			result := t.Execute(ctx)
			if result != nil && result.Status == core.ToolComplete {
				if resp, ok := result.Output.(*LLMResponse); ok && resp != nil {
					chunks := splitLLMContent(resp.Content, 160)
					for i, part := range chunks {
						select {
						case chunkChan <- &core.ToolChunk{Index: i, Data: part}:
						case <-ctx.Ctx.Done():
							return
						}
					}
					select {
					case chunkChan <- &core.ToolChunk{Index: len(chunks), IsFinal: true}:
					case <-ctx.Ctx.Done():
					}
				}
			}
			resultChan <- result
		}()
	}

	return chunkChan, resultChan
}

func (t *LLMTool) Execute(ctx *core.ToolContext) *core.ToolExecResult {
	req, errRes := t.normalizeRequest(ctx)
	if errRes != nil {
		return errRes
	}

	// Execute based on provider
	var resp *LLMResponse
	var err error

	// Retry loop
	maxRetries := t.config.RetryPolicy.MaxRetries
	if maxRetries == 0 {
		maxRetries = 1
	}

	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			time.Sleep(t.config.RetryPolicy.Backoff * time.Duration(i))
		}

		switch t.config.Provider {
		case "openai":
			resp, err = t.callOpenAI(ctx, req)
		case "anthropic":
			resp, err = t.callAnthropic(ctx, req)
		case "ollama":
			resp, err = t.callOllama(ctx, req)
		case "openrouter":
			resp, err = t.callOpenAI(ctx, req) // OpenAI-compatible
		case "deepseek":
			resp, err = t.callOpenAI(ctx, req) // DeepSeek is OpenAI-compatible
		default:
			return &core.ToolExecResult{
				Status: core.ToolFailed,
				Error:  fmt.Sprintf("unknown provider: %s", t.config.Provider),
			}
		}

		if err == nil {
			break
		}

		// Check if context cancelled, don't retry
		select {
		case <-ctx.Ctx.Done():
			return &core.ToolExecResult{
				Status: core.ToolCanceled,
				Error:  "request cancelled",
			}
		default:
		}

		// If error is not temporary (e.g. 401), don't retry?
		// For now, retry all errors except context cancellation
	}

	if err != nil {
		return &core.ToolExecResult{
			Status: core.ToolFailed,
			Error:  err.Error(),
		}
	}

	// Track cost
	t.costTracker.AddUsage(resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Model)

	callCost := t.costTracker.LastCost()
	return &core.ToolExecResult{
		Status: core.ToolComplete,
		Output: resp,
		Metadata: map[string]any{
			"cost":              callCost,
			"prompt_tokens":     resp.Usage.PromptTokens,
			"completion_tokens": resp.Usage.CompletionTokens,
			"total_tokens":      resp.Usage.TotalTokens,
			"model":             resp.Model,
			"provider":          t.config.Provider,
			"tier":              t.config.Tier,
			"preset":            t.config.Preset,
			"estimated":         false,
		},
	}
}

// === Provider Implementations ===

func (t *LLMTool) callOpenAI(ctx *core.ToolContext, req *LLMRequest) (*LLMResponse, error) {
	// Build OpenAI request
	openaiReq := map[string]any{
		"model":    t.config.Model,
		"messages": req.Messages,
	}

	// GPT-5 models and reasoning models have special requirements
	isReasoningModel := strings.HasPrefix(t.config.Model, "gpt-5") || strings.HasPrefix(t.config.Model, "o1") || strings.HasPrefix(t.config.Model, "o3")

	if isReasoningModel {
		// Use max_completion_tokens instead of max_tokens
		openaiReq["max_completion_tokens"] = req.MaxTokens
		// Reasoning models only support temperature=1 (default), so don't send it
	} else {
		openaiReq["max_tokens"] = req.MaxTokens
		openaiReq["temperature"] = req.Temperature
	}

	body, _ := json.Marshal(openaiReq)

	httpReq, err := http.NewRequestWithContext(ctx.Ctx, "POST",
		t.config.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+t.config.APIKey)

	// OpenRouter-specific headers
	if t.config.Provider == "openrouter" {
		httpReq.Header.Set("HTTP-Referer", "https://github.com/phenomenon0/polymarket-agents")
		httpReq.Header.Set("X-Title", "AgentScope Enhanced Demo")
	}

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OpenAI API error %d: %s", resp.StatusCode, string(body))
	}

	var openaiResp struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				Reasoning string `json:"reasoning"` // For models like GLM that use reasoning field
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
		Model string `json:"model"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&openaiResp); err != nil {
		return nil, err
	}

	if len(openaiResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	// CRITICAL FIX: Some models (like GLM) use "reasoning" field instead of "content"
	// Prioritize content, but fall back to reasoning if content is empty
	content := openaiResp.Choices[0].Message.Content
	if content == "" && openaiResp.Choices[0].Message.Reasoning != "" {
		content = openaiResp.Choices[0].Message.Reasoning
	}

	return &LLMResponse{
		Content:      content,
		Model:        openaiResp.Model,
		FinishReason: openaiResp.Choices[0].FinishReason,
		Usage:        openaiResp.Usage,
	}, nil
}

func (t *LLMTool) callAnthropic(ctx *core.ToolContext, req *LLMRequest) (*LLMResponse, error) {
	// Build Anthropic request
	anthropicReq := map[string]any{
		"model":      t.config.Model,
		"max_tokens": req.MaxTokens,
		"messages":   req.Messages,
	}

	if req.System != "" {
		anthropicReq["system"] = req.System
	}

	body, _ := json.Marshal(anthropicReq)

	httpReq, err := http.NewRequestWithContext(ctx.Ctx, "POST",
		t.config.BaseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", t.config.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Anthropic API error %d: %s", resp.StatusCode, string(body))
	}

	var anthropicResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Model      string `json:"model"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&anthropicResp); err != nil {
		return nil, err
	}

	content := ""
	for _, c := range anthropicResp.Content {
		if c.Type == "text" {
			content += c.Text
		}
	}

	return &LLMResponse{
		Content:      content,
		Model:        anthropicResp.Model,
		FinishReason: anthropicResp.StopReason,
		Usage: struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		}{
			PromptTokens:     anthropicResp.Usage.InputTokens,
			CompletionTokens: anthropicResp.Usage.OutputTokens,
			TotalTokens:      anthropicResp.Usage.InputTokens + anthropicResp.Usage.OutputTokens,
		},
	}, nil
}

func (t *LLMTool) callOllama(ctx *core.ToolContext, req *LLMRequest) (*LLMResponse, error) {
	// Build Ollama request (uses OpenAI-compatible endpoint)
	ollamaReq := map[string]any{
		"model":    t.config.Model,
		"messages": req.Messages,
		"stream":   false,
		"options": map[string]any{
			"temperature": req.Temperature,
			"num_predict": req.MaxTokens,
		},
	}

	body, _ := json.Marshal(ollamaReq)

	httpReq, err := http.NewRequestWithContext(ctx.Ctx, "POST",
		t.config.BaseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Ollama API error %d: %s", resp.StatusCode, string(body))
	}

	var ollamaResp struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Model string `json:"model"`
		Done  bool   `json:"done"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return nil, err
	}

	finishReason := "stop"
	if !ollamaResp.Done {
		finishReason = "length"
	}

	return &LLMResponse{
		Content:      ollamaResp.Message.Content,
		Model:        ollamaResp.Model,
		FinishReason: finishReason,
	}, nil
}

// streamOpenAI streams SSE tokens from OpenAI chat completions.
func (t *LLMTool) streamOpenAI(ctx *core.ToolContext, req *LLMRequest, chunkChan chan<- *core.ToolChunk, resultChan chan<- *core.ToolExecResult) {
	defer close(chunkChan)
	defer close(resultChan)

	openaiReq := map[string]any{
		"model":       t.config.Model,
		"messages":    req.Messages,
		"max_tokens":  req.MaxTokens,
		"temperature": req.Temperature,
		"stream":      true,
		"stream_options": map[string]any{
			"include_usage": true,
		},
	}

	body, _ := json.Marshal(openaiReq)
	httpReq, err := http.NewRequestWithContext(ctx.Ctx, "POST",
		t.config.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		resultChan <- &core.ToolExecResult{Status: core.ToolFailed, Error: err.Error()}
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+t.config.APIKey)

	// OpenRouter-specific headers
	if t.config.Provider == "openrouter" {
		httpReq.Header.Set("HTTP-Referer", "https://github.com/phenomenon0/polymarket-agents")
		httpReq.Header.Set("X-Title", "AgentScope Enhanced Demo")
	}

	resp, err := t.client.Do(httpReq)
	if err != nil {
		resultChan <- &core.ToolExecResult{Status: core.ToolFailed, Error: err.Error()}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		resultChan <- &core.ToolExecResult{
			Status: core.ToolFailed,
			Error:  fmt.Sprintf("OpenAI API error %d: %s", resp.StatusCode, string(body)),
		}
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1024), 1024*1024)

	var content strings.Builder
	finishReason := ""
	model := ""
	index := 0
	promptTokens := 0
	completionTokens := 0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}

		var chunk struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Model   string `json:"model"`
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			} `json:"usage"`
		}

		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if chunk.Model != "" {
			model = chunk.Model
		}

		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				content.WriteString(choice.Delta.Content)
				select {
				case chunkChan <- &core.ToolChunk{Index: index, Data: choice.Delta.Content}:
					index++
				case <-ctx.Ctx.Done():
					return
				}
			}
			if choice.FinishReason != "" {
				finishReason = choice.FinishReason
			}
		}

		if chunk.Usage.TotalTokens > 0 {
			promptTokens = chunk.Usage.PromptTokens
			completionTokens = chunk.Usage.CompletionTokens
		}
	}

	if err := scanner.Err(); err != nil {
		resultChan <- &core.ToolExecResult{Status: core.ToolFailed, Error: err.Error()}
		return
	}

	// Final chunk marker
	select {
	case chunkChan <- &core.ToolChunk{Index: index, IsFinal: true}:
	case <-ctx.Ctx.Done():
		return
	}

	respObj := &LLMResponse{
		Content:      content.String(),
		Model:        model,
		FinishReason: finishReason,
	}
	if respObj.Model == "" {
		respObj.Model = t.config.Model
	}

	if promptTokens == 0 && completionTokens == 0 {
		promptTokens = estimatePromptTokens(req)
		completionTokens = estimateTokens(respObj.Content)
	}

	t.costTracker.AddUsage(promptTokens, completionTokens, respObj.Model)

	resultChan <- &core.ToolExecResult{
		Status: core.ToolComplete,
		Output: respObj,
		Metadata: map[string]any{
			"cost":              t.costTracker.LastCost(),
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      promptTokens + completionTokens,
			"model":             respObj.Model,
			"provider":          t.config.Provider,
			"tier":              t.config.Tier,
			"preset":            t.config.Preset,
			"estimated":         promptTokens == 0 || completionTokens == 0,
		},
	}
}

// streamAnthropic streams SSE tokens from Anthropic /messages.
func (t *LLMTool) streamAnthropic(ctx *core.ToolContext, req *LLMRequest, chunkChan chan<- *core.ToolChunk, resultChan chan<- *core.ToolExecResult) {
	defer close(chunkChan)
	defer close(resultChan)

	anthropicReq := map[string]any{
		"model":      t.config.Model,
		"max_tokens": req.MaxTokens,
		"messages":   req.Messages,
		"stream":     true,
	}
	if req.System != "" {
		anthropicReq["system"] = req.System
	}

	body, _ := json.Marshal(anthropicReq)

	httpReq, err := http.NewRequestWithContext(ctx.Ctx, "POST",
		t.config.BaseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		resultChan <- &core.ToolExecResult{Status: core.ToolFailed, Error: err.Error()}
		return
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", t.config.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := t.client.Do(httpReq)
	if err != nil {
		resultChan <- &core.ToolExecResult{Status: core.ToolFailed, Error: err.Error()}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		resultChan <- &core.ToolExecResult{
			Status: core.ToolFailed,
			Error:  fmt.Sprintf("Anthropic API error %d: %s", resp.StatusCode, string(body)),
		}
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1024), 1024*1024)

	var content strings.Builder
	model := ""
	finishReason := ""
	index := 0
	done := false
	promptTokens := 0
	completionTokens := 0

	for scanner.Scan() {
		if done {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}

		var evt map[string]any
		if err := json.Unmarshal([]byte(payload), &evt); err != nil {
			continue
		}

		evtType, _ := evt["type"].(string)
		switch evtType {
		case "message_start":
			if msg, ok := evt["message"].(map[string]any); ok {
				if m, ok := msg["model"].(string); ok {
					model = m
				}
			}
		case "content_block_delta":
			// delta.text
			if delta, ok := evt["delta"].(map[string]any); ok {
				if text, ok := delta["text"].(string); ok && text != "" {
					content.WriteString(text)
					select {
					case chunkChan <- &core.ToolChunk{Index: index, Data: text}:
						index++
					case <-ctx.Ctx.Done():
						return
					}
				}
			}
		case "message_delta":
			if stop, ok := evt["stop_reason"].(string); ok {
				finishReason = stop
			}
			if usage, ok := evt["usage"].(map[string]any); ok {
				if v, ok := usage["input_tokens"].(float64); ok {
					promptTokens = int(v)
				}
				if v, ok := usage["output_tokens"].(float64); ok {
					completionTokens = int(v)
				}
			}
		case "message_stop":
			if finishReason == "" {
				finishReason = "stop"
			}
			if usage, ok := evt["usage"].(map[string]any); ok {
				if v, ok := usage["input_tokens"].(float64); ok {
					promptTokens = int(v)
				}
				if v, ok := usage["output_tokens"].(float64); ok {
					completionTokens = int(v)
				}
			}
			done = true
		}
	}

	if err := scanner.Err(); err != nil {
		resultChan <- &core.ToolExecResult{Status: core.ToolFailed, Error: err.Error()}
		return
	}

	select {
	case chunkChan <- &core.ToolChunk{Index: index, IsFinal: true}:
	case <-ctx.Ctx.Done():
		return
	}

	respObj := &LLMResponse{
		Content:      content.String(),
		Model:        model,
		FinishReason: finishReason,
	}
	if respObj.Model == "" {
		respObj.Model = t.config.Model
	}

	if promptTokens == 0 && completionTokens == 0 {
		promptTokens = estimatePromptTokens(req)
		completionTokens = estimateTokens(respObj.Content)
	}
	t.costTracker.AddUsage(promptTokens, completionTokens, t.config.Model)

	resultChan <- &core.ToolExecResult{
		Status: core.ToolComplete,
		Output: respObj,
		Metadata: map[string]any{
			"cost":              t.costTracker.LastCost(),
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      promptTokens + completionTokens,
			"model":             respObj.Model,
			"provider":          t.config.Provider,
			"tier":              t.config.Tier,
			"preset":            t.config.Preset,
			"estimated":         promptTokens == 0 || completionTokens == 0,
		},
	}
}

// splitLLMContent breaks a response into roughly sized chunks for streaming.
func splitLLMContent(content string, maxLen int) []string {
	if maxLen <= 0 {
		maxLen = 160
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	words := strings.Fields(content)
	var chunks []string
	var buf strings.Builder

	for _, w := range words {
		if buf.Len()+len(w)+1 > maxLen {
			chunks = append(chunks, buf.String())
			buf.Reset()
		}
		if buf.Len() > 0 {
			buf.WriteString(" ")
		}
		buf.WriteString(w)
	}

	if buf.Len() > 0 {
		chunks = append(chunks, buf.String())
	}
	return chunks
}

func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	// Very rough heuristic: ~4 characters per token for mixed English.
	return (len(text) + 3) / 4
}

func estimatePromptTokens(req *LLMRequest) int {
	if req == nil {
		return 0
	}
	total := estimateTokens(req.System)
	for _, msg := range req.Messages {
		total += estimateTokens(msg.Content)
	}
	return total
}

func (t *LLMTool) callMock(ctx *core.ToolContext, req *LLMRequest) (*LLMResponse, error) {
	// Simple mock that echoes input or returns a fixed JSON if it detects a schema prompt
	content := "Mock response to: " + req.Messages[len(req.Messages)-1].Content

	// If system prompt asks for JSON, return dummy JSON
	if strings.Contains(req.System, "valid JSON") {
		content = "```json\n{\"mock_key\": \"mock_value\"}\n```"
	}

	return &LLMResponse{
		Content:      content,
		Model:        "mock-model",
		FinishReason: "stop",
		Usage: struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		}{
			PromptTokens:     10,
			CompletionTokens: 10,
			TotalTokens:      20,
		},
	}, nil
}
