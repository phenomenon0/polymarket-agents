// Package polymarket provides Agent-GO tools for interacting with Polymarket APIs.
// These tools are designed for LLM agents to research and analyze prediction markets.
package polymarket

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/phenomenon0/polymarket-agents/core"
	"github.com/phenomenon0/polymarket-agents/pkg/polymarket/gamma"
)

// === Gamma API Tools (Market Research) ===

// SearchMarketsTool searches for markets matching a query.
type SearchMarketsTool struct {
	client *gamma.Client
}

type SearchMarketsInput struct {
	Query     string  `json:"query"`      // Search query (filters by question text)
	Active    *bool   `json:"active"`     // Filter by active status
	Limit     int     `json:"limit"`      // Max results (default 20)
	MinVolume float64 `json:"min_volume"` // Minimum volume filter
}

type SearchMarketsOutput struct {
	Markets []MarketSummary `json:"markets"`
	Count   int             `json:"count"`
}

type MarketSummary struct {
	ID         string  `json:"id"`
	Question   string  `json:"question"`
	YesPrice   float64 `json:"yes_price"`
	NoPrice    float64 `json:"no_price"`
	Volume     float64 `json:"volume"`
	Liquidity  float64 `json:"liquidity"`
	Active     bool    `json:"active"`
	EventSlug  string  `json:"event_slug"`
	EndDate    string  `json:"end_date"`
	YesTokenID string  `json:"yes_token_id"`
	NoTokenID  string  `json:"no_token_id"`
}

func NewSearchMarketsTool(client *gamma.Client) *SearchMarketsTool {
	return &SearchMarketsTool{client: client}
}

func (t *SearchMarketsTool) Name() string {
	return "polymarket_search_markets"
}

func (t *SearchMarketsTool) InputSchema() []byte {
	return []byte(`{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "Search query for market questions"},
			"active": {"type": "boolean", "description": "Filter to only active markets"},
			"limit": {"type": "integer", "description": "Maximum number of results (default 20)", "maximum": 100},
			"min_volume": {"type": "number", "description": "Minimum trading volume filter"}
		}
	}`)
}

func (t *SearchMarketsTool) OutputSchema() []byte {
	return []byte(`{
		"type": "object",
		"properties": {
			"markets": {"type": "array", "items": {"type": "object"}},
			"count": {"type": "integer"}
		}
	}`)
}

func (t *SearchMarketsTool) Execute(tc *core.ToolContext) *core.ToolExecResult {
	var input SearchMarketsInput
	if err := parseInput(tc.Request, &input); err != nil {
		return errorResult(err)
	}

	if input.Limit == 0 {
		input.Limit = 20
	}

	ctx, cancel := context.WithTimeout(tc.Ctx, 30*time.Second)
	defer cancel()

	filter := &gamma.MarketsFilter{
		Active: input.Active,
		Limit:  input.Limit * 3, // Fetch more to filter client-side
	}

	markets, err := t.client.ListMarkets(ctx, filter)
	if err != nil {
		return errorResult(fmt.Errorf("list markets failed: %w", err))
	}

	// Filter by query and volume
	query := strings.ToLower(input.Query)
	summaries := make([]MarketSummary, 0, len(markets))
	for _, m := range markets {
		// Filter by query if provided
		if query != "" && !strings.Contains(strings.ToLower(m.Question), query) {
			continue
		}

		// Filter by volume
		if input.MinVolume > 0 && m.Volume.Float64() < input.MinVolume {
			continue
		}

		summaries = append(summaries, MarketSummary{
			ID:         m.ID,
			Question:   m.Question,
			YesPrice:   m.YesPrice(),
			NoPrice:    m.NoPrice(),
			Volume:     m.Volume.Float64(),
			Liquidity:  m.Liquidity.Float64(),
			Active:     m.Active,
			EventSlug:  m.Slug,
			EndDate:    m.EndDate.Format(time.RFC3339),
			YesTokenID: m.YesTokenID(),
			NoTokenID:  m.NoTokenID(),
		})

		if len(summaries) >= input.Limit {
			break
		}
	}

	return &core.ToolExecResult{
		Status: core.ToolComplete,
		Output: SearchMarketsOutput{
			Markets: summaries,
			Count:   len(summaries),
		},
	}
}

// GetMarketTool retrieves detailed information about a specific market.
type GetMarketTool struct {
	client *gamma.Client
}

type GetMarketInput struct {
	MarketID string `json:"market_id"` // Market ID or condition ID
	TokenID  string `json:"token_id"`  // Or lookup by token ID
}

type GetMarketOutput struct {
	ID          string   `json:"id"`
	Question    string   `json:"question"`
	Description string   `json:"description"`
	YesPrice    float64  `json:"yes_price"`
	NoPrice     float64  `json:"no_price"`
	Volume      float64  `json:"volume"`
	Liquidity   float64  `json:"liquidity"`
	Active      bool     `json:"active"`
	Closed      bool     `json:"closed"`
	EndDate     string   `json:"end_date"`
	YesTokenID  string   `json:"yes_token_id"`
	NoTokenID   string   `json:"no_token_id"`
	ConditionID string   `json:"condition_id"`
	Tags        []string `json:"tags"`
}

func NewGetMarketTool(client *gamma.Client) *GetMarketTool {
	return &GetMarketTool{client: client}
}

func (t *GetMarketTool) Name() string {
	return "polymarket_get_market"
}

func (t *GetMarketTool) InputSchema() []byte {
	return []byte(`{
		"type": "object",
		"properties": {
			"market_id": {"type": "string", "description": "Market ID to retrieve"},
			"token_id": {"type": "string", "description": "Alternatively, lookup by token ID"}
		}
	}`)
}

func (t *GetMarketTool) OutputSchema() []byte {
	return []byte(`{"type": "object"}`)
}

func (t *GetMarketTool) Execute(tc *core.ToolContext) *core.ToolExecResult {
	var input GetMarketInput
	if err := parseInput(tc.Request, &input); err != nil {
		return errorResult(err)
	}

	if input.MarketID == "" && input.TokenID == "" {
		return errorResult(fmt.Errorf("either market_id or token_id is required"))
	}

	ctx, cancel := context.WithTimeout(tc.Ctx, 30*time.Second)
	defer cancel()

	var market *gamma.Market
	var err error

	if input.TokenID != "" {
		market, err = t.client.GetMarketByTokenID(ctx, input.TokenID)
	} else {
		market, err = t.client.GetMarket(ctx, input.MarketID)
	}

	if err != nil {
		return errorResult(fmt.Errorf("get market failed: %w", err))
	}

	tags := make([]string, 0)
	for _, tag := range market.Tags {
		tags = append(tags, tag.Label)
	}

	return &core.ToolExecResult{
		Status: core.ToolComplete,
		Output: GetMarketOutput{
			ID:          market.ID,
			Question:    market.Question,
			Description: market.Description,
			YesPrice:    market.YesPrice(),
			NoPrice:     market.NoPrice(),
			Volume:      market.Volume.Float64(),
			Liquidity:   market.Liquidity.Float64(),
			Active:      market.Active,
			Closed:      market.Closed,
			EndDate:     market.EndDate.Format(time.RFC3339),
			YesTokenID:  market.YesTokenID(),
			NoTokenID:   market.NoTokenID(),
			ConditionID: market.ConditionID,
			Tags:        tags,
		},
	}
}

// ListEventsTool lists prediction market events.
type ListEventsTool struct {
	client *gamma.Client
}

type ListEventsInput struct {
	Active *bool  `json:"active"` // Filter by active status
	Limit  int    `json:"limit"`  // Max results (default 20)
	Offset int    `json:"offset"` // Pagination offset
	Tag    string `json:"tag"`    // Filter by tag
	Order  string `json:"order"`  // "asc" or "desc"
}

type ListEventsOutput struct {
	Events []EventSummary `json:"events"`
	Count  int            `json:"count"`
}

type EventSummary struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Slug        string          `json:"slug"`
	Volume      float64         `json:"volume"`
	Liquidity   float64         `json:"liquidity"`
	Active      bool            `json:"active"`
	Closed      bool            `json:"closed"`
	StartDate   string          `json:"start_date"`
	EndDate     string          `json:"end_date"`
	Markets     []MarketSummary `json:"markets,omitempty"`
	MarketCount int             `json:"market_count"`
}

func NewListEventsTool(client *gamma.Client) *ListEventsTool {
	return &ListEventsTool{client: client}
}

func (t *ListEventsTool) Name() string {
	return "polymarket_list_events"
}

func (t *ListEventsTool) InputSchema() []byte {
	return []byte(`{
		"type": "object",
		"properties": {
			"active": {"type": "boolean", "description": "Filter to only active events"},
			"limit": {"type": "integer", "description": "Maximum number of results", "maximum": 100},
			"offset": {"type": "integer", "description": "Pagination offset"},
			"tag": {"type": "string", "description": "Filter by tag (e.g., 'politics', 'sports')"},
			"order": {"type": "string", "description": "Sort order: 'asc' or 'desc'"}
		}
	}`)
}

func (t *ListEventsTool) OutputSchema() []byte {
	return []byte(`{
		"type": "object",
		"properties": {
			"events": {"type": "array"},
			"count": {"type": "integer"}
		}
	}`)
}

func (t *ListEventsTool) Execute(tc *core.ToolContext) *core.ToolExecResult {
	var input ListEventsInput
	if err := parseInput(tc.Request, &input); err != nil {
		return errorResult(err)
	}

	if input.Limit == 0 {
		input.Limit = 20
	}

	ctx, cancel := context.WithTimeout(tc.Ctx, 30*time.Second)
	defer cancel()

	filter := &gamma.EventsFilter{
		Active: input.Active,
		Limit:  input.Limit,
		Offset: input.Offset,
		Tag:    input.Tag,
		Order:  input.Order,
	}

	events, err := t.client.ListEvents(ctx, filter)
	if err != nil {
		return errorResult(fmt.Errorf("list events failed: %w", err))
	}

	summaries := make([]EventSummary, 0, len(events))
	for _, e := range events {
		summaries = append(summaries, EventSummary{
			ID:          e.ID,
			Title:       e.Title,
			Slug:        e.Slug,
			Volume:      e.Volume.Float64(),
			Liquidity:   e.Liquidity.Float64(),
			Active:      e.Active,
			Closed:      e.Closed,
			StartDate:   e.StartDate.Format(time.RFC3339),
			EndDate:     e.EndDate.Format(time.RFC3339),
			MarketCount: len(e.Markets),
		})
	}

	return &core.ToolExecResult{
		Status: core.ToolComplete,
		Output: ListEventsOutput{
			Events: summaries,
			Count:  len(summaries),
		},
	}
}

// GetEventTool retrieves detailed information about a specific event.
type GetEventTool struct {
	client *gamma.Client
}

type GetEventInput struct {
	EventID string `json:"event_id"` // Event ID
	Slug    string `json:"slug"`     // Or lookup by slug
}

func NewGetEventTool(client *gamma.Client) *GetEventTool {
	return &GetEventTool{client: client}
}

func (t *GetEventTool) Name() string {
	return "polymarket_get_event"
}

func (t *GetEventTool) InputSchema() []byte {
	return []byte(`{
		"type": "object",
		"properties": {
			"event_id": {"type": "string", "description": "Event ID to retrieve"},
			"slug": {"type": "string", "description": "Alternatively, lookup by event slug"}
		}
	}`)
}

func (t *GetEventTool) OutputSchema() []byte {
	return []byte(`{"type": "object"}`)
}

func (t *GetEventTool) Execute(tc *core.ToolContext) *core.ToolExecResult {
	var input GetEventInput
	if err := parseInput(tc.Request, &input); err != nil {
		return errorResult(err)
	}

	if input.EventID == "" && input.Slug == "" {
		return errorResult(fmt.Errorf("either event_id or slug is required"))
	}

	ctx, cancel := context.WithTimeout(tc.Ctx, 30*time.Second)
	defer cancel()

	var event *gamma.Event
	var err error

	if input.Slug != "" {
		event, err = t.client.GetEventBySlug(ctx, input.Slug)
	} else {
		event, err = t.client.GetEvent(ctx, input.EventID)
	}

	if err != nil {
		return errorResult(fmt.Errorf("get event failed: %w", err))
	}

	// Convert markets
	markets := make([]MarketSummary, 0, len(event.Markets))
	for _, m := range event.Markets {
		markets = append(markets, MarketSummary{
			ID:         m.ID,
			Question:   m.Question,
			YesPrice:   m.YesPrice(),
			NoPrice:    m.NoPrice(),
			Volume:     m.Volume.Float64(),
			Liquidity:  m.Liquidity.Float64(),
			Active:     m.Active,
			YesTokenID: m.YesTokenID(),
			NoTokenID:  m.NoTokenID(),
		})
	}

	return &core.ToolExecResult{
		Status: core.ToolComplete,
		Output: EventSummary{
			ID:          event.ID,
			Title:       event.Title,
			Slug:        event.Slug,
			Volume:      event.Volume.Float64(),
			Liquidity:   event.Liquidity.Float64(),
			Active:      event.Active,
			Closed:      event.Closed,
			StartDate:   event.StartDate.Format(time.RFC3339),
			EndDate:     event.EndDate.Format(time.RFC3339),
			Markets:     markets,
			MarketCount: len(markets),
		},
	}
}

// === Helper Functions ===

func parseInput(msg *core.Message, v interface{}) error {
	if msg == nil || msg.ToolReq == nil {
		return fmt.Errorf("no tool request")
	}

	// Try InputRaw first
	if len(msg.ToolReq.InputRaw) > 0 {
		return json.Unmarshal(msg.ToolReq.InputRaw, v)
	}

	// Fall back to Input
	if msg.ToolReq.Input != nil {
		data, err := json.Marshal(msg.ToolReq.Input)
		if err != nil {
			return err
		}
		return json.Unmarshal(data, v)
	}

	return nil
}

func errorResult(err error) *core.ToolExecResult {
	return &core.ToolExecResult{
		Status: core.ToolFailed,
		Error:  err.Error(),
	}
}

// RegisterGammaTools registers all Gamma API tools with the registry.
func RegisterGammaTools(registry *core.ToolRegistry, client *gamma.Client) {
	// All Gamma tools are read-only and safe
	policy := core.ToolPolicy{
		MaxRetries:      3,
		BaseBackoff:     100 * time.Millisecond,
		MaxBackoff:      5 * time.Second,
		Retriable:       true,
		DefaultTimeout:  30 * time.Second,
		RateLimitPerSec: 5.0,
		Burst:           10,
		LimitKey:        "polymarket-gamma",
	}

	registry.Register(NewSearchMarketsTool(client), policy, nil)
	registry.Register(NewGetMarketTool(client), policy, nil)
	registry.Register(NewListEventsTool(client), policy, nil)
	registry.Register(NewGetEventTool(client), policy, nil)
}
