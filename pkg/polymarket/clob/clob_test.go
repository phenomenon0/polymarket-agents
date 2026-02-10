package clob

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// Test private key (DO NOT use in production!)
const testPrivateKey = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

func TestNewClient(t *testing.T) {
	client, err := NewClient(testPrivateKey)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	// Hardhat/Anvil account 0 address
	expected := "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"
	if !strings.EqualFold(client.Address(), expected) {
		t.Errorf("Wrong address: got %s, want %s", client.Address(), expected)
	}

	if client.Funder() != client.Address() {
		t.Error("Funder should default to wallet address")
	}

	if client.HasCredentials() {
		t.Error("Should not have credentials initially")
	}
}

func TestNewClientWithOptions(t *testing.T) {
	customClient := &http.Client{Timeout: 60 * time.Second}
	creds := &APICredentials{
		APIKey:     "test-key",
		Secret:     "test-secret",
		Passphrase: "test-passphrase",
	}

	client, err := NewClient(testPrivateKey,
		WithCLOBBaseURL("https://custom.clob.com"),
		WithChainID(80001), // Mumbai
		WithCredentials(creds),
		WithSignatureType(1),
		WithFunder("0x1234567890123456789012345678901234567890"),
		WithCLOBHTTPClient(customClient),
	)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	if client.baseURL != "https://custom.clob.com" {
		t.Errorf("Wrong base URL: %s", client.baseURL)
	}

	if client.chainID != 80001 {
		t.Errorf("Wrong chain ID: %d", client.chainID)
	}

	if !client.HasCredentials() {
		t.Error("Should have credentials")
	}

	if client.sigType != 1 {
		t.Errorf("Wrong signature type: %d", client.sigType)
	}

	if client.funder != "0x1234567890123456789012345678901234567890" {
		t.Errorf("Wrong funder: %s", client.funder)
	}
}

func TestNewClientInvalidKey(t *testing.T) {
	_, err := NewClient("invalid-key")
	if err == nil {
		t.Error("Expected error for invalid key")
	}
}

func TestGetOrderBook(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/book" {
			t.Errorf("Expected path /book, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("token_id") != "token123" {
			t.Errorf("Expected token_id=token123, got %s", r.URL.Query().Get("token_id"))
		}

		book := OrderBookSummary{
			Market:    "0xabc",
			TokenID:   "token123",
			Timestamp: "1234567890",
			Bids: []PriceLevel{
				{Price: "0.50", Size: "100"},
				{Price: "0.49", Size: "200"},
			},
			Asks: []PriceLevel{
				{Price: "0.51", Size: "150"},
				{Price: "0.52", Size: "250"},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(book)
	}))
	defer server.Close()

	client, _ := NewClient(testPrivateKey, WithCLOBBaseURL(server.URL))

	book, err := client.GetOrderBook(context.Background(), "token123")
	if err != nil {
		t.Fatalf("GetOrderBook failed: %v", err)
	}

	if book.TokenID != "token123" {
		t.Errorf("Wrong token ID: %s", book.TokenID)
	}

	if len(book.Bids) != 2 {
		t.Errorf("Expected 2 bids, got %d", len(book.Bids))
	}

	if len(book.Asks) != 2 {
		t.Errorf("Expected 2 asks, got %d", len(book.Asks))
	}

	if book.Bids[0].Price != "0.50" {
		t.Errorf("Wrong best bid: %s", book.Bids[0].Price)
	}

	if book.Asks[0].Price != "0.51" {
		t.Errorf("Wrong best ask: %s", book.Asks[0].Price)
	}
}

func TestGetPrice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/price" {
			t.Errorf("Expected path /price, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"price": "0.55"})
	}))
	defer server.Close()

	client, _ := NewClient(testPrivateKey, WithCLOBBaseURL(server.URL))

	price, err := client.GetPrice(context.Background(), "token123")
	if err != nil {
		t.Fatalf("GetPrice failed: %v", err)
	}

	if price != "0.55" {
		t.Errorf("Wrong price: %s", price)
	}
}

func TestGetMidpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"mid": "0.505"})
	}))
	defer server.Close()

	client, _ := NewClient(testPrivateKey, WithCLOBBaseURL(server.URL))

	mid, err := client.GetMidpoint(context.Background(), "token123")
	if err != nil {
		t.Fatalf("GetMidpoint failed: %v", err)
	}

	if mid != "0.505" {
		t.Errorf("Wrong midpoint: %s", mid)
	}
}

func TestGetSpread(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"bid": "0.50", "ask": "0.51"})
	}))
	defer server.Close()

	client, _ := NewClient(testPrivateKey, WithCLOBBaseURL(server.URL))

	bid, ask, err := client.GetSpread(context.Background(), "token123")
	if err != nil {
		t.Fatalf("GetSpread failed: %v", err)
	}

	if bid != "0.50" {
		t.Errorf("Wrong bid: %s", bid)
	}

	if ask != "0.51" {
		t.Errorf("Wrong ask: %s", ask)
	}
}

func TestGetMarket(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/markets/0xabc123" {
			t.Errorf("Expected path /markets/0xabc123, got %s", r.URL.Path)
		}

		market := MarketInfo{
			ConditionID:      "0xabc123",
			QuestionID:       "0xdef456",
			Description:      "Test market",
			Active:           true,
			AcceptingOrders:  true,
			MinimumOrderSize: "5",
			MinimumTickSize:  "0.01",
			Tokens: []Token{
				{TokenID: "token1", Outcome: "Yes", Price: "0.65"},
				{TokenID: "token2", Outcome: "No", Price: "0.35"},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(market)
	}))
	defer server.Close()

	client, _ := NewClient(testPrivateKey, WithCLOBBaseURL(server.URL))

	market, err := client.GetMarket(context.Background(), "0xabc123")
	if err != nil {
		t.Fatalf("GetMarket failed: %v", err)
	}

	if market.ConditionID != "0xabc123" {
		t.Errorf("Wrong condition ID: %s", market.ConditionID)
	}

	if !market.Active {
		t.Error("Market should be active")
	}

	if len(market.Tokens) != 2 {
		t.Errorf("Expected 2 tokens, got %d", len(market.Tokens))
	}
}

func TestGetOpenOrdersNoCredentials(t *testing.T) {
	client, _ := NewClient(testPrivateKey)

	_, err := client.GetOpenOrders(context.Background())
	if err == nil {
		t.Error("Expected error without credentials")
	}

	if !strings.Contains(err.Error(), "L2 credentials required") {
		t.Errorf("Wrong error message: %s", err.Error())
	}
}

func TestGetOpenOrders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify L2 auth headers
		if r.Header.Get("POLY_API_KEY") == "" {
			t.Error("Missing POLY_API_KEY header")
		}
		if r.Header.Get("POLY_SIGNATURE") == "" {
			t.Error("Missing POLY_SIGNATURE header")
		}
		if r.Header.Get("POLY_TIMESTAMP") == "" {
			t.Error("Missing POLY_TIMESTAMP header")
		}
		if r.Header.Get("POLY_PASSPHRASE") == "" {
			t.Error("Missing POLY_PASSPHRASE header")
		}

		orders := []Order{
			{
				ID:        "order1",
				Status:    OrderStatusLive,
				TokenID:   "token123",
				Side:      OrderSideBuy,
				Price:     "0.50",
				Size:      "100",
				CreatedAt: time.Now(),
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(orders)
	}))
	defer server.Close()

	creds := &APICredentials{
		APIKey:     "test-key",
		Secret:     "dGVzdC1zZWNyZXQ=", // base64("test-secret")
		Passphrase: "test-pass",
	}

	client, _ := NewClient(testPrivateKey,
		WithCLOBBaseURL(server.URL),
		WithCredentials(creds),
	)

	orders, err := client.GetOpenOrders(context.Background())
	if err != nil {
		t.Fatalf("GetOpenOrders failed: %v", err)
	}

	if len(orders) != 1 {
		t.Errorf("Expected 1 order, got %d", len(orders))
	}

	if orders[0].ID != "order1" {
		t.Errorf("Wrong order ID: %s", orders[0].ID)
	}
}

func TestGetTrades(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		trades := []Trade{
			{
				ID:      "trade1",
				TokenID: "token123",
				Side:    OrderSideBuy,
				Price:   "0.52",
				Size:    "50",
				Status:  "MATCHED",
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(trades)
	}))
	defer server.Close()

	creds := &APICredentials{
		APIKey:     "test-key",
		Secret:     "dGVzdC1zZWNyZXQ=",
		Passphrase: "test-pass",
	}

	client, _ := NewClient(testPrivateKey,
		WithCLOBBaseURL(server.URL),
		WithCredentials(creds),
	)

	trades, err := client.GetTrades(context.Background())
	if err != nil {
		t.Fatalf("GetTrades failed: %v", err)
	}

	if len(trades) != 1 {
		t.Errorf("Expected 1 trade, got %d", len(trades))
	}

	if trades[0].ID != "trade1" {
		t.Errorf("Wrong trade ID: %s", trades[0].ID)
	}
}

func TestBuildOrder(t *testing.T) {
	client, _ := NewClient(testPrivateKey)

	args := &OrderArgs{
		TokenID:   "12345",
		Side:      OrderSideBuy,
		Price:     0.50,
		Size:      100.0,
		OrderType: OrderTypeGTC,
	}

	order, err := client.BuildOrder(args, "0.01", false)
	if err != nil {
		t.Fatalf("BuildOrder failed: %v", err)
	}

	if order.TokenID != "12345" {
		t.Errorf("Wrong token ID: %s", order.TokenID)
	}

	if order.Side != "BUY" {
		t.Errorf("Wrong side: %s", order.Side)
	}

	if order.Salt == "" {
		t.Error("Salt should not be empty")
	}

	if order.Maker != client.Address() {
		t.Errorf("Wrong maker: %s", order.Maker)
	}

	if order.Signer != client.Address() {
		t.Errorf("Wrong signer: %s", order.Signer)
	}

	if order.Expiration != "0" {
		t.Errorf("Wrong expiration: %s", order.Expiration)
	}

	// Verify amounts (USDC has 6 decimals)
	// Buying 100 tokens at $0.50 = $50 = 50,000,000 micro-USDC
	if order.MakerAmount != "50000000" {
		t.Errorf("Wrong maker amount: %s (expected 50000000)", order.MakerAmount)
	}

	// Taker receives 100 tokens = 100,000,000 units
	if order.TakerAmount != "100000000" {
		t.Errorf("Wrong taker amount: %s (expected 100000000)", order.TakerAmount)
	}
}

func TestBuildOrderSell(t *testing.T) {
	client, _ := NewClient(testPrivateKey)

	args := &OrderArgs{
		TokenID:   "12345",
		Side:      OrderSideSell,
		Price:     0.60,
		Size:      50.0,
		OrderType: OrderTypeGTC,
	}

	order, err := client.BuildOrder(args, "0.01", false)
	if err != nil {
		t.Fatalf("BuildOrder failed: %v", err)
	}

	if order.Side != "SELL" {
		t.Errorf("Wrong side: %s", order.Side)
	}

	// Selling 50 tokens = 50,000,000 units
	if order.MakerAmount != "50000000" {
		t.Errorf("Wrong maker amount: %s (expected 50000000)", order.MakerAmount)
	}

	// Receives 50 * $0.60 = $30 = 30,000,000 micro-USDC
	if order.TakerAmount != "30000000" {
		t.Errorf("Wrong taker amount: %s (expected 30000000)", order.TakerAmount)
	}
}

func TestBuildOrderWithExpiration(t *testing.T) {
	client, _ := NewClient(testPrivateKey)

	expiration := time.Now().Add(24 * time.Hour).Unix()

	args := &OrderArgs{
		TokenID:    "12345",
		Side:       OrderSideBuy,
		Price:      0.50,
		Size:       100.0,
		OrderType:  OrderTypeGTD,
		Expiration: expiration,
	}

	order, err := client.BuildOrder(args, "0.01", false)
	if err != nil {
		t.Fatalf("BuildOrder failed: %v", err)
	}

	if order.Expiration == "0" {
		t.Error("Expiration should be set")
	}
}

func TestSignOrder(t *testing.T) {
	client, _ := NewClient(testPrivateKey)

	order := &OrderPayload{
		Salt:          "123456789",
		Maker:         client.Address(),
		Signer:        client.Address(),
		Taker:         "0x0000000000000000000000000000000000000000",
		TokenID:       "12345",
		MakerAmount:   "50000000",
		TakerAmount:   "100000000",
		Expiration:    "0",
		Nonce:         "0",
		FeeRateBps:    "0",
		Side:          "BUY",
		SignatureType: 0,
	}

	signature, err := client.SignOrder(order, false)
	if err != nil {
		t.Fatalf("SignOrder failed: %v", err)
	}

	if signature == "" {
		t.Error("Signature should not be empty")
	}

	// EIP-712 signatures are 65 bytes (130 hex chars + 0x prefix)
	if !strings.HasPrefix(signature, "0x") {
		t.Error("Signature should have 0x prefix")
	}

	if len(signature) != 132 {
		t.Errorf("Wrong signature length: %d (expected 132)", len(signature))
	}
}

func TestPostOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/order" {
			t.Errorf("Expected path /order, got %s", r.URL.Path)
		}

		var order SignedOrder
		if err := json.NewDecoder(r.Body).Decode(&order); err != nil {
			t.Errorf("Failed to decode order: %v", err)
		}

		if order.Signature == "" {
			t.Error("Signature should not be empty")
		}

		resp := PostOrderResponse{
			OrderID: "new-order-123",
			Success: true,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	creds := &APICredentials{
		APIKey:     "test-key",
		Secret:     "dGVzdC1zZWNyZXQ=",
		Passphrase: "test-pass",
	}

	client, _ := NewClient(testPrivateKey,
		WithCLOBBaseURL(server.URL),
		WithCredentials(creds),
	)

	order := &SignedOrder{
		Order: OrderPayload{
			Salt:        "123456789",
			Maker:       client.Address(),
			Signer:      client.Address(),
			Taker:       "0x0000000000000000000000000000000000000000",
			TokenID:     "12345",
			MakerAmount: "50000000",
			TakerAmount: "100000000",
			Expiration:  "0",
			Nonce:       "0",
			FeeRateBps:  "0",
			Side:        "BUY",
		},
		Signature: "0x" + strings.Repeat("ab", 65),
		Owner:     client.Address(),
		OrderType: OrderTypeGTC,
	}

	resp, err := client.PostOrder(context.Background(), order)
	if err != nil {
		t.Fatalf("PostOrder failed: %v", err)
	}

	if !resp.Success {
		t.Error("Order should be successful")
	}

	if resp.OrderID != "new-order-123" {
		t.Errorf("Wrong order ID: %s", resp.OrderID)
	}
}

func TestCancelOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("Expected DELETE, got %s", r.Method)
		}

		resp := CancelOrderResponse{
			Canceled:    []string{"order-123"},
			NotCanceled: nil,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	creds := &APICredentials{
		APIKey:     "test-key",
		Secret:     "dGVzdC1zZWNyZXQ=",
		Passphrase: "test-pass",
	}

	client, _ := NewClient(testPrivateKey,
		WithCLOBBaseURL(server.URL),
		WithCredentials(creds),
	)

	err := client.CancelOrder(context.Background(), "order-123")
	if err != nil {
		t.Fatalf("CancelOrder failed: %v", err)
	}
}

func TestCancelOrderPartialFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := CancelOrderResponse{
			Canceled: []string{"order-1"},
			NotCanceled: []CancelFailure{
				{OrderID: "order-2", Reason: "already filled"},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	creds := &APICredentials{
		APIKey:     "test-key",
		Secret:     "dGVzdC1zZWNyZXQ=",
		Passphrase: "test-pass",
	}

	client, _ := NewClient(testPrivateKey,
		WithCLOBBaseURL(server.URL),
		WithCredentials(creds),
	)

	err := client.CancelOrders(context.Background(), []string{"order-1", "order-2"})
	if err == nil {
		t.Error("Expected error for partial failure")
	}

	if !strings.Contains(err.Error(), "not canceled") {
		t.Errorf("Wrong error message: %s", err.Error())
	}
}

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "invalid token_id"}`))
	}))
	defer server.Close()

	client, _ := NewClient(testPrivateKey, WithCLOBBaseURL(server.URL))

	_, err := client.GetOrderBook(context.Background(), "invalid")
	if err == nil {
		t.Error("Expected error for bad request")
	}

	if !strings.Contains(err.Error(), "400") {
		t.Errorf("Error should contain status code: %s", err.Error())
	}
}

// --- Integration Tests ---

func TestIntegrationGetOrderBook(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client, err := NewClient(testPrivateKey)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Use a known active market token ID
	// This token ID may need to be updated if the market becomes inactive
	tokenID := os.Getenv("POLYMARKET_TEST_TOKEN_ID")
	if tokenID == "" {
		t.Skip("POLYMARKET_TEST_TOKEN_ID not set")
	}

	book, err := client.GetOrderBook(ctx, tokenID)
	if err != nil {
		t.Fatalf("GetOrderBook failed: %v", err)
	}

	t.Logf("OrderBook for %s:", tokenID)
	t.Logf("  Bids: %d levels", len(book.Bids))
	t.Logf("  Asks: %d levels", len(book.Asks))
	if len(book.Bids) > 0 {
		t.Logf("  Best bid: %s @ %s", book.Bids[0].Size, book.Bids[0].Price)
	}
	if len(book.Asks) > 0 {
		t.Logf("  Best ask: %s @ %s", book.Asks[0].Size, book.Asks[0].Price)
	}
}

func TestIntegrationGetMarketInfo(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client, err := NewClient(testPrivateKey)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conditionID := os.Getenv("POLYMARKET_TEST_CONDITION_ID")
	if conditionID == "" {
		t.Skip("POLYMARKET_TEST_CONDITION_ID not set")
	}

	market, err := client.GetMarket(ctx, conditionID)
	if err != nil {
		t.Fatalf("GetMarket failed: %v", err)
	}

	t.Logf("Market: %s", market.Description)
	t.Logf("  Active: %v", market.Active)
	t.Logf("  Accepting Orders: %v", market.AcceptingOrders)
	t.Logf("  Tokens: %d", len(market.Tokens))
	for _, tok := range market.Tokens {
		t.Logf("    %s: %s @ %s", tok.Outcome, tok.TokenID, tok.Price)
	}
}

func TestIntegrationCreateAPIKey(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	privateKey := os.Getenv("POLYMARKET_TEST_PRIVATE_KEY")
	if privateKey == "" {
		t.Skip("POLYMARKET_TEST_PRIVATE_KEY not set")
	}

	client, err := NewClient(privateKey)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	creds, err := client.CreateOrDeriveAPIKey(ctx)
	if err != nil {
		t.Fatalf("CreateOrDeriveAPIKey failed: %v", err)
	}

	t.Logf("API Key: %s", creds.APIKey[:10]+"...")
	t.Logf("Has Secret: %v", creds.Secret != "")
	t.Logf("Has Passphrase: %v", creds.Passphrase != "")

	if !client.HasCredentials() {
		t.Error("Client should have credentials after CreateOrDeriveAPIKey")
	}
}
