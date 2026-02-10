// Package gamma provides a client for the Polymarket Gamma Markets API.
// Gamma is a read-only API for fetching market and event metadata.
package gamma

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// Event represents a Polymarket event (container for multiple markets).
type Event struct {
	ID              string    `json:"id"`
	Ticker          string    `json:"ticker"`
	Slug            string    `json:"slug"`
	Title           string    `json:"title"`
	Description     string    `json:"description"`
	StartDate       time.Time `json:"startDate"`
	EndDate         time.Time `json:"endDate"`
	Active          bool      `json:"active"`
	Closed          bool      `json:"closed"`
	Archived        bool      `json:"archived"`
	New             bool      `json:"new"`
	Featured        bool      `json:"featured"`
	Restricted      bool      `json:"restricted"`
	Liquidity       JSONFloat `json:"liquidity"`
	Volume          JSONFloat `json:"volume"`
	Volume24hr      JSONFloat `json:"volume24hr"`
	OpenInterest    JSONFloat `json:"openInterest"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
	Competitive     JSONFloat `json:"competitive"`
	Markets         []Market  `json:"markets,omitempty"`
	Tags            []Tag     `json:"tags,omitempty"`
	CommentCount    int       `json:"commentCount"`
	NegRisk         bool      `json:"negRisk"`
	NegRiskMarketID string    `json:"negRiskMarketID"`
}

// Market represents a single prediction market.
type Market struct {
	ID                      string    `json:"id"`
	Question                string    `json:"question"`
	ConditionID             string    `json:"conditionId"`
	Slug                    string    `json:"slug"`
	Description             string    `json:"description"`
	StartDate               time.Time `json:"startDate"`
	EndDate                 time.Time `json:"endDate"`
	Active                  bool      `json:"active"`
	Closed                  bool      `json:"closed"`
	Archived                bool      `json:"archived"`
	Funded                  bool      `json:"funded"`
	AcceptingOrders         bool      `json:"acceptingOrders"`
	AcceptingOrderTimestamp string    `json:"acceptingOrderTimestamp"`

	// Token IDs for the YES/NO outcomes (JSON-encoded array)
	ClobTokenIDsRaw string `json:"clobTokenIds"`

	// Outcomes and prices (JSON-encoded arrays)
	OutcomesRaw      string `json:"outcomes"`
	OutcomePricesRaw string `json:"outcomePrices"`

	// Liquidity and volume
	Liquidity    JSONFloat `json:"liquidity"`
	Volume       JSONFloat `json:"volume"`
	Volume24hr   JSONFloat `json:"volume24hr"`
	OpenInterest JSONFloat `json:"openInterest"`
	Spread       JSONFloat `json:"spread"`

	// Order book parameters
	MinimumOrderSize JSONFloat `json:"minimumOrderSize"`
	MinimumTickSize  JSONFloat `json:"minimumTickSize"`

	// Rewards parameters
	RewardsMinSize   JSONFloat `json:"rewardsMinSize"`
	RewardsMaxSpread JSONFloat `json:"rewardsMaxSpread"`
	RewardsDailyRate JSONFloat `json:"rewardsDailyRate"`

	// Resolution
	UmaResolutionStatus string `json:"umaResolutionStatus"`
	UmaBond             string `json:"umaBond"`
	UmaReward           string `json:"umaReward"`
	ResolutionSource    string `json:"resolutionSource"`

	// Timestamps
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// Negative risk
	NegRisk          bool   `json:"negRisk"`
	NegRiskMarketID  string `json:"negRiskMarketID"`
	NegRiskRequestID string `json:"negRiskRequestID"`

	// Event reference
	EventID string `json:"eventID"`

	// Tags and categories
	Tags []Tag `json:"tags,omitempty"`
}

// Tag represents a category tag.
type Tag struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Slug      string `json:"slug"`
	ForceShow bool   `json:"forceShow"`
}

// JSONFloat handles both numeric and string JSON values.
type JSONFloat float64

func (j *JSONFloat) UnmarshalJSON(data []byte) error {
	// Try as number first
	var f float64
	if err := json.Unmarshal(data, &f); err == nil {
		*j = JSONFloat(f)
		return nil
	}

	// Try as string
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	if s == "" {
		*j = 0
		return nil
	}

	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*j = JSONFloat(f)
	return nil
}

func (j JSONFloat) Float64() float64 {
	return float64(j)
}

// EventsResponse is the response from the events endpoint.
type EventsResponse []Event

// MarketsResponse is the response from the markets endpoint.
type MarketsResponse []Market

// EventsFilter contains filter parameters for listing events.
type EventsFilter struct {
	Active    *bool  `url:"active,omitempty"`
	Closed    *bool  `url:"closed,omitempty"`
	Archived  *bool  `url:"archived,omitempty"`
	Slug      string `url:"slug,omitempty"`
	Tag       string `url:"tag,omitempty"`
	TagID     string `url:"tag_id,omitempty"`         // Numeric tag ID (e.g., "82" for EPL)
	StartDate string `url:"start_date_min,omitempty"` // ISO 8601
	EndDate   string `url:"end_date_max,omitempty"`   // ISO 8601
	Limit     int    `url:"limit,omitempty"`
	Offset    int    `url:"offset,omitempty"`
	Order     string `url:"order,omitempty"`   // "asc" or "desc"
	SortBy    string `url:"sort_by,omitempty"` // "volume", "liquidity", etc.
}

// MarketsFilter contains filter parameters for listing markets.
type MarketsFilter struct {
	Active       *bool  `url:"active,omitempty"`
	Closed       *bool  `url:"closed,omitempty"`
	ClobTokenIDs string `url:"clob_token_ids,omitempty"` // Comma-separated
	ConditionID  string `url:"condition_id,omitempty"`
	Slug         string `url:"slug,omitempty"`
	EventID      string `url:"event_id,omitempty"`
	Limit        int    `url:"limit,omitempty"`
	Offset       int    `url:"offset,omitempty"`
}

// BoolPtr returns a pointer to a bool.
func BoolPtr(b bool) *bool {
	return &b
}

// IsTradeable returns true if the event can be traded on.
func (e *Event) IsTradeable() bool {
	return e.Active && !e.Closed && !e.Archived && !e.Restricted
}

// IsTradeable returns true if the market can be traded on.
func (m *Market) IsTradeable() bool {
	return m.Active && !m.Closed && !m.Archived && m.AcceptingOrders
}

// ClobTokenIDs returns the parsed token IDs.
func (m *Market) ClobTokenIDs() []string {
	var ids []string
	if m.ClobTokenIDsRaw == "" {
		return ids
	}
	json.Unmarshal([]byte(m.ClobTokenIDsRaw), &ids)
	return ids
}

// Outcomes returns the parsed outcomes.
func (m *Market) Outcomes() []string {
	var outcomes []string
	if m.OutcomesRaw == "" {
		return outcomes
	}
	json.Unmarshal([]byte(m.OutcomesRaw), &outcomes)
	return outcomes
}

// OutcomePrices returns the parsed outcome prices.
func (m *Market) OutcomePrices() []string {
	var prices []string
	if m.OutcomePricesRaw == "" {
		return prices
	}
	json.Unmarshal([]byte(m.OutcomePricesRaw), &prices)
	return prices
}

// YesTokenID returns the token ID for the YES outcome.
func (m *Market) YesTokenID() string {
	ids := m.ClobTokenIDs()
	if len(ids) > 0 {
		return ids[0]
	}
	return ""
}

// NoTokenID returns the token ID for the NO outcome.
func (m *Market) NoTokenID() string {
	ids := m.ClobTokenIDs()
	if len(ids) > 1 {
		return ids[1]
	}
	return ""
}

// YesPrice returns the current YES price.
func (m *Market) YesPrice() float64 {
	prices := m.OutcomePrices()
	if len(prices) > 0 {
		var price float64
		fmt.Sscanf(prices[0], "%f", &price)
		return price
	}
	return 0
}

// NoPrice returns the current NO price.
func (m *Market) NoPrice() float64 {
	prices := m.OutcomePrices()
	if len(prices) > 1 {
		var price float64
		fmt.Sscanf(prices[1], "%f", &price)
		return price
	}
	return 0
}
