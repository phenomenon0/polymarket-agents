package gamma

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestListEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/events" {
			t.Errorf("Expected path /events, got %s", r.URL.Path)
		}

		events := []Event{
			{
				ID:     "1",
				Title:  "Test Event",
				Active: true,
				Slug:   "test-event",
			},
			{
				ID:     "2",
				Title:  "Another Event",
				Active: true,
				Slug:   "another-event",
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(events)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))

	events, err := client.ListEvents(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListEvents failed: %v", err)
	}

	if len(events) != 2 {
		t.Errorf("Expected 2 events, got %d", len(events))
	}

	if events[0].Title != "Test Event" {
		t.Errorf("Wrong title: got %s", events[0].Title)
	}
}

func TestListEventsWithFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("active") != "true" {
			t.Errorf("Expected active=true, got %s", query.Get("active"))
		}
		if query.Get("limit") != "10" {
			t.Errorf("Expected limit=10, got %s", query.Get("limit"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]Event{})
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))

	active := true
	_, err := client.ListEvents(context.Background(), &EventsFilter{
		Active: &active,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("ListEvents failed: %v", err)
	}
}

func TestListMarkets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/markets" {
			t.Errorf("Expected path /markets, got %s", r.URL.Path)
		}

		markets := []Market{
			{
				ID:               "1",
				Question:         "Will X happen?",
				Active:           true,
				ClobTokenIDsRaw:  `["token1", "token2"]`,
				OutcomePricesRaw: `["0.65", "0.35"]`,
				OutcomesRaw:      `["Yes", "No"]`,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(markets)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))

	markets, err := client.ListMarkets(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListMarkets failed: %v", err)
	}

	if len(markets) != 1 {
		t.Errorf("Expected 1 market, got %d", len(markets))
	}

	if markets[0].Question != "Will X happen?" {
		t.Errorf("Wrong question: got %s", markets[0].Question)
	}

	if markets[0].YesTokenID() != "token1" {
		t.Errorf("Wrong YES token: got %s", markets[0].YesTokenID())
	}

	if markets[0].NoTokenID() != "token2" {
		t.Errorf("Wrong NO token: got %s", markets[0].NoTokenID())
	}
}

func TestGetEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/events/123" {
			t.Errorf("Expected path /events/123, got %s", r.URL.Path)
		}

		event := Event{
			ID:    "123",
			Title: "Single Event",
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(event)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))

	event, err := client.GetEvent(context.Background(), "123")
	if err != nil {
		t.Fatalf("GetEvent failed: %v", err)
	}

	if event.ID != "123" {
		t.Errorf("Wrong ID: got %s", event.ID)
	}
}

func TestGetMarketByTokenID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("clob_token_ids") != "token123" {
			t.Errorf("Expected clob_token_ids=token123, got %s", query.Get("clob_token_ids"))
		}

		markets := []Market{
			{
				ID:              "1",
				ClobTokenIDsRaw: `["token123", "token456"]`,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(markets)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))

	market, err := client.GetMarketByTokenID(context.Background(), "token123")
	if err != nil {
		t.Fatalf("GetMarketByTokenID failed: %v", err)
	}

	if market.ID != "1" {
		t.Errorf("Wrong ID: got %s", market.ID)
	}
}

func TestMarketMethods(t *testing.T) {
	market := Market{
		ClobTokenIDsRaw:  `["yes-token", "no-token"]`,
		OutcomePricesRaw: `["0.65", "0.35"]`,
		OutcomesRaw:      `["Yes", "No"]`,
		Active:           true,
		AcceptingOrders:  true,
	}

	if market.YesTokenID() != "yes-token" {
		t.Errorf("YesTokenID wrong: %s", market.YesTokenID())
	}

	if market.NoTokenID() != "no-token" {
		t.Errorf("NoTokenID wrong: %s", market.NoTokenID())
	}

	if market.YesPrice() != 0.65 {
		t.Errorf("YesPrice wrong: %f", market.YesPrice())
	}

	if market.NoPrice() != 0.35 {
		t.Errorf("NoPrice wrong: %f", market.NoPrice())
	}

	if !market.IsTradeable() {
		t.Error("Market should be tradeable")
	}
}

func TestEventMethods(t *testing.T) {
	event := Event{
		Active:     true,
		Closed:     false,
		Archived:   false,
		Restricted: false,
	}

	if !event.IsTradeable() {
		t.Error("Event should be tradeable")
	}

	event.Restricted = true
	if event.IsTradeable() {
		t.Error("Restricted event should not be tradeable")
	}
}

func TestClientWithOptions(t *testing.T) {
	customClient := &http.Client{Timeout: 60 * time.Second}

	client := NewClient(
		WithBaseURL("https://custom.api.com"),
		WithHTTPClient(customClient),
		WithRateLimit(5.0, 2),
	)

	if client.baseURL != "https://custom.api.com" {
		t.Errorf("Wrong base URL: %s", client.baseURL)
	}

	if client.httpClient != customClient {
		t.Error("Custom HTTP client not set")
	}
}

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Bad Request"))
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))

	_, err := client.ListEvents(context.Background(), nil)
	if err == nil {
		t.Error("Expected error for bad request")
	}
}

// Integration test - only run with POLYMARKET_TEST_API=1
func TestIntegrationListEvents(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client := NewClient()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	events, err := client.ListTradeableEvents(ctx, 5, 0)
	if err != nil {
		t.Fatalf("ListTradeableEvents failed: %v", err)
	}

	t.Logf("Found %d tradeable events", len(events))
	for _, e := range events {
		t.Logf("  - %s: %s (volume: %.2f)", e.ID, e.Title, e.Volume.Float64())
	}
}

// Integration test for markets
func TestIntegrationListMarkets(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client := NewClient()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	markets, err := client.ListTradeableMarkets(ctx, 5, 0)
	if err != nil {
		t.Fatalf("ListTradeableMarkets failed: %v", err)
	}

	t.Logf("Found %d tradeable markets", len(markets))
	for _, m := range markets {
		prices := m.OutcomePrices()
		yesPrice, noPrice := "N/A", "N/A"
		if len(prices) > 0 {
			yesPrice = prices[0]
		}
		if len(prices) > 1 {
			noPrice = prices[1]
		}
		t.Logf("  - %s: %s (YES: %s, NO: %s)", m.ID, m.Question, yesPrice, noPrice)
	}
}
