package gamma

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"golang.org/x/time/rate"
)

const (
	// DefaultBaseURL is the Gamma API base URL
	DefaultBaseURL = "https://gamma-api.polymarket.com"

	// Rate limits (from Polymarket docs)
	defaultRateLimit = 10.0 // requests per second
	defaultBurst     = 5
)

// Client is a Gamma API client.
type Client struct {
	baseURL    string
	httpClient *http.Client
	limiter    *rate.Limiter
}

// ClientOption configures the client.
type ClientOption func(*Client)

// WithBaseURL sets a custom base URL.
func WithBaseURL(url string) ClientOption {
	return func(c *Client) {
		c.baseURL = url
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = client
	}
}

// WithRateLimit sets custom rate limiting.
func WithRateLimit(rps float64, burst int) ClientOption {
	return func(c *Client) {
		c.limiter = rate.NewLimiter(rate.Limit(rps), burst)
	}
}

// NewClient creates a new Gamma API client.
func NewClient(opts ...ClientOption) *Client {
	c := &Client{
		baseURL: DefaultBaseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        20,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		limiter: rate.NewLimiter(rate.Limit(defaultRateLimit), defaultBurst),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// ListEvents fetches events from the Gamma API.
func (c *Client) ListEvents(ctx context.Context, filter *EventsFilter) ([]Event, error) {
	params := url.Values{}
	if filter != nil {
		if filter.Active != nil {
			params.Set("active", strconv.FormatBool(*filter.Active))
		}
		if filter.Closed != nil {
			params.Set("closed", strconv.FormatBool(*filter.Closed))
		}
		if filter.Archived != nil {
			params.Set("archived", strconv.FormatBool(*filter.Archived))
		}
		if filter.Slug != "" {
			params.Set("slug", filter.Slug)
		}
		if filter.Tag != "" {
			params.Set("tag", filter.Tag)
		}
		if filter.TagID != "" {
			params.Set("tag_id", filter.TagID)
		}
		if filter.StartDate != "" {
			params.Set("start_date_min", filter.StartDate)
		}
		if filter.EndDate != "" {
			params.Set("end_date_max", filter.EndDate)
		}
		if filter.Limit > 0 {
			params.Set("limit", strconv.Itoa(filter.Limit))
		}
		if filter.Offset > 0 {
			params.Set("offset", strconv.Itoa(filter.Offset))
		}
		if filter.Order != "" {
			params.Set("order", filter.Order)
		}
		if filter.SortBy != "" {
			params.Set("sort_by", filter.SortBy)
		}
	}

	var events []Event
	if err := c.get(ctx, "/events", params, &events); err != nil {
		return nil, err
	}
	return events, nil
}

// GetEvent fetches a single event by ID.
func (c *Client) GetEvent(ctx context.Context, id string) (*Event, error) {
	var event Event
	if err := c.get(ctx, "/events/"+id, nil, &event); err != nil {
		return nil, err
	}
	return &event, nil
}

// GetEventBySlug fetches an event by its slug.
func (c *Client) GetEventBySlug(ctx context.Context, slug string) (*Event, error) {
	events, err := c.ListEvents(ctx, &EventsFilter{Slug: slug, Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("event not found: %s", slug)
	}
	return &events[0], nil
}

// ListMarkets fetches markets from the Gamma API.
func (c *Client) ListMarkets(ctx context.Context, filter *MarketsFilter) ([]Market, error) {
	params := url.Values{}
	if filter != nil {
		if filter.Active != nil {
			params.Set("active", strconv.FormatBool(*filter.Active))
		}
		if filter.Closed != nil {
			params.Set("closed", strconv.FormatBool(*filter.Closed))
		}
		if filter.ClobTokenIDs != "" {
			params.Set("clob_token_ids", filter.ClobTokenIDs)
		}
		if filter.ConditionID != "" {
			params.Set("condition_id", filter.ConditionID)
		}
		if filter.Slug != "" {
			params.Set("slug", filter.Slug)
		}
		if filter.EventID != "" {
			params.Set("event_id", filter.EventID)
		}
		if filter.Limit > 0 {
			params.Set("limit", strconv.Itoa(filter.Limit))
		}
		if filter.Offset > 0 {
			params.Set("offset", strconv.Itoa(filter.Offset))
		}
	}

	var markets []Market
	if err := c.get(ctx, "/markets", params, &markets); err != nil {
		return nil, err
	}
	return markets, nil
}

// GetMarket fetches a single market by condition ID.
func (c *Client) GetMarket(ctx context.Context, conditionID string) (*Market, error) {
	var market Market
	if err := c.get(ctx, "/markets/"+conditionID, nil, &market); err != nil {
		return nil, err
	}
	return &market, nil
}

// GetMarketByTokenID fetches a market by one of its CLOB token IDs.
func (c *Client) GetMarketByTokenID(ctx context.Context, tokenID string) (*Market, error) {
	markets, err := c.ListMarkets(ctx, &MarketsFilter{ClobTokenIDs: tokenID, Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(markets) == 0 {
		return nil, fmt.Errorf("market not found for token: %s", tokenID)
	}
	return &markets[0], nil
}

// ListTradeableEvents fetches all events that can be traded on.
func (c *Client) ListTradeableEvents(ctx context.Context, limit, offset int) ([]Event, error) {
	active := true
	closed := false
	archived := false
	return c.ListEvents(ctx, &EventsFilter{
		Active:   &active,
		Closed:   &closed,
		Archived: &archived,
		Limit:    limit,
		Offset:   offset,
	})
}

// ListTradeableMarkets fetches all markets that can be traded on.
func (c *Client) ListTradeableMarkets(ctx context.Context, limit, offset int) ([]Market, error) {
	active := true
	closed := false
	return c.ListMarkets(ctx, &MarketsFilter{
		Active: &active,
		Closed: &closed,
		Limit:  limit,
		Offset: offset,
	})
}

// ListAllTradeableEvents fetches all tradeable events using pagination.
func (c *Client) ListAllTradeableEvents(ctx context.Context) ([]Event, error) {
	var allEvents []Event
	limit := 100
	offset := 0

	for {
		events, err := c.ListTradeableEvents(ctx, limit, offset)
		if err != nil {
			return nil, err
		}

		allEvents = append(allEvents, events...)

		if len(events) < limit {
			break
		}
		offset += limit
	}

	return allEvents, nil
}

// ListAllTradeableMarkets fetches all tradeable markets using pagination.
func (c *Client) ListAllTradeableMarkets(ctx context.Context) ([]Market, error) {
	var allMarkets []Market
	limit := 100
	offset := 0

	for {
		markets, err := c.ListTradeableMarkets(ctx, limit, offset)
		if err != nil {
			return nil, err
		}

		allMarkets = append(allMarkets, markets...)

		if len(markets) < limit {
			break
		}
		offset += limit
	}

	return allMarkets, nil
}

// get performs a GET request with rate limiting.
func (c *Client) get(ctx context.Context, path string, params url.Values, result interface{}) error {
	// Wait for rate limiter
	if err := c.limiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limiter: %w", err)
	}

	// Build URL
	u := c.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	// Check status
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("api error %d: %s", resp.StatusCode, string(body))
	}

	// Decode response
	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}
