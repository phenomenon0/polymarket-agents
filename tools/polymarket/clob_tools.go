package polymarket

import (
	"context"
	"fmt"
	"time"

	"github.com/phenomenon0/polymarket-agents/core"
	"github.com/phenomenon0/polymarket-agents/pkg/polymarket/book"
	"github.com/phenomenon0/polymarket-agents/pkg/polymarket/clob"

	"github.com/shopspring/decimal"
)

// Risk Classifications for CLOB tools
const (
	RiskClassReadOnly      = "read-only" // No auth required, safe
	RiskClassAuthenticated = "auth"      // Requires L2 credentials
	RiskClassTrading       = "trading"   // Modifies positions
	RiskClassHighRisk      = "high-risk" // Large position changes
)

// === Read-Only CLOB Tools ===

// GetOrderBookTool fetches the current orderbook for a token.
type GetOrderBookTool struct {
	client *clob.Client
}

type GetOrderBookInput struct {
	TokenID string `json:"token_id"` // Token ID (YES or NO outcome)
}

type GetOrderBookOutput struct {
	TokenID   string      `json:"token_id"`
	BestBid   *PriceSize  `json:"best_bid,omitempty"`
	BestAsk   *PriceSize  `json:"best_ask,omitempty"`
	Midpoint  string      `json:"midpoint"`
	Spread    string      `json:"spread"`
	SpreadBps string      `json:"spread_bps"`
	BidDepth  int         `json:"bid_depth"`
	AskDepth  int         `json:"ask_depth"`
	Bids      []PriceSize `json:"bids"`
	Asks      []PriceSize `json:"asks"`
}

type PriceSize struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

func NewGetOrderBookTool(client *clob.Client) *GetOrderBookTool {
	return &GetOrderBookTool{client: client}
}

func (t *GetOrderBookTool) Name() string {
	return "polymarket_get_orderbook"
}

func (t *GetOrderBookTool) InputSchema() []byte {
	return []byte(`{
		"type": "object",
		"required": ["token_id"],
		"properties": {
			"token_id": {"type": "string", "description": "Token ID for the outcome to fetch orderbook"}
		}
	}`)
}

func (t *GetOrderBookTool) OutputSchema() []byte {
	return []byte(`{"type": "object"}`)
}

func (t *GetOrderBookTool) Execute(tc *core.ToolContext) *core.ToolExecResult {
	var input GetOrderBookInput
	if err := parseInput(tc.Request, &input); err != nil {
		return errorResult(err)
	}

	if input.TokenID == "" {
		return errorResult(fmt.Errorf("token_id is required"))
	}

	ctx, cancel := context.WithTimeout(tc.Ctx, 30*time.Second)
	defer cancel()

	bookSummary, err := t.client.GetOrderBook(ctx, input.TokenID)
	if err != nil {
		return errorResult(fmt.Errorf("get orderbook failed: %w", err))
	}

	// Convert to our orderbook for calculations
	ob := book.NewOrderBook(input.TokenID, bookSummary.Market)

	bids := make([]book.PriceLevel, len(bookSummary.Bids))
	for i, b := range bookSummary.Bids {
		price, _ := decimal.NewFromString(b.Price)
		size, _ := decimal.NewFromString(b.Size)
		bids[i] = book.PriceLevel{Price: price, Size: size}
	}
	ob.SetBids(bids)

	asks := make([]book.PriceLevel, len(bookSummary.Asks))
	for i, a := range bookSummary.Asks {
		price, _ := decimal.NewFromString(a.Price)
		size, _ := decimal.NewFromString(a.Size)
		asks[i] = book.PriceLevel{Price: price, Size: size}
	}
	ob.SetAsks(asks)

	// Build output
	output := GetOrderBookOutput{
		TokenID:   input.TokenID,
		Midpoint:  ob.Midpoint().String(),
		Spread:    ob.Spread().String(),
		SpreadBps: ob.SpreadBps().StringFixed(2),
		BidDepth:  ob.BidDepth(),
		AskDepth:  ob.AskDepth(),
	}

	// Best bid/ask
	if bestBidPrice, bestBidSize := ob.BestBid(); !bestBidPrice.IsZero() {
		output.BestBid = &PriceSize{Price: bestBidPrice.String(), Size: bestBidSize.String()}
	}
	if bestAskPrice, bestAskSize := ob.BestAsk(); !bestAskPrice.IsZero() {
		output.BestAsk = &PriceSize{Price: bestAskPrice.String(), Size: bestAskSize.String()}
	}

	// Top 10 levels
	output.Bids = make([]PriceSize, 0, 10)
	for i, b := range bookSummary.Bids {
		if i >= 10 {
			break
		}
		output.Bids = append(output.Bids, PriceSize{Price: b.Price, Size: b.Size})
	}

	output.Asks = make([]PriceSize, 0, 10)
	for i, a := range bookSummary.Asks {
		if i >= 10 {
			break
		}
		output.Asks = append(output.Asks, PriceSize{Price: a.Price, Size: a.Size})
	}

	return &core.ToolExecResult{
		Status: core.ToolComplete,
		Output: output,
	}
}

// GetMarketInfoTool fetches market information from CLOB.
type GetMarketInfoTool struct {
	client *clob.Client
}

type GetMarketInfoInput struct {
	ConditionID string `json:"condition_id"` // Market condition ID
}

type GetMarketInfoOutput struct {
	ConditionID      string      `json:"condition_id"`
	Description      string      `json:"description"`
	Active           bool        `json:"active"`
	AcceptingOrders  bool        `json:"accepting_orders"`
	MinimumOrderSize string      `json:"minimum_order_size"`
	MinimumTickSize  string      `json:"minimum_tick_size"`
	NegRisk          bool        `json:"neg_risk"`
	Tokens           []TokenInfo `json:"tokens"`
}

type TokenInfo struct {
	TokenID string `json:"token_id"`
	Outcome string `json:"outcome"`
	Price   string `json:"price"`
}

func NewGetMarketInfoTool(client *clob.Client) *GetMarketInfoTool {
	return &GetMarketInfoTool{client: client}
}

func (t *GetMarketInfoTool) Name() string {
	return "polymarket_get_market_info"
}

func (t *GetMarketInfoTool) InputSchema() []byte {
	return []byte(`{
		"type": "object",
		"required": ["condition_id"],
		"properties": {
			"condition_id": {"type": "string", "description": "Market condition ID"}
		}
	}`)
}

func (t *GetMarketInfoTool) OutputSchema() []byte {
	return []byte(`{"type": "object"}`)
}

func (t *GetMarketInfoTool) Execute(tc *core.ToolContext) *core.ToolExecResult {
	var input GetMarketInfoInput
	if err := parseInput(tc.Request, &input); err != nil {
		return errorResult(err)
	}

	if input.ConditionID == "" {
		return errorResult(fmt.Errorf("condition_id is required"))
	}

	ctx, cancel := context.WithTimeout(tc.Ctx, 30*time.Second)
	defer cancel()

	market, err := t.client.GetMarket(ctx, input.ConditionID)
	if err != nil {
		return errorResult(fmt.Errorf("get market failed: %w", err))
	}

	tokens := make([]TokenInfo, len(market.Tokens))
	for i, t := range market.Tokens {
		tokens[i] = TokenInfo{
			TokenID: t.TokenID,
			Outcome: t.Outcome,
			Price:   t.Price,
		}
	}

	return &core.ToolExecResult{
		Status: core.ToolComplete,
		Output: GetMarketInfoOutput{
			ConditionID:      market.ConditionID,
			Description:      market.Description,
			Active:           market.Active,
			AcceptingOrders:  market.AcceptingOrders,
			MinimumOrderSize: market.MinimumOrderSize,
			MinimumTickSize:  market.MinimumTickSize,
			NegRisk:          market.NegRisk,
			Tokens:           tokens,
		},
	}
}

// SimulateTradeTool simulates a trade against the orderbook.
type SimulateTradeTool struct {
	client *clob.Client
}

type SimulateTradeInput struct {
	TokenID string  `json:"token_id"`
	Side    string  `json:"side"` // "BUY" or "SELL"
	Size    float64 `json:"size"` // Amount to trade
}

type SimulateTradeOutput struct {
	TotalSize   string     `json:"total_size"`
	TotalCost   string     `json:"total_cost"`
	AvgPrice    string     `json:"avg_price"`
	PriceImpact string     `json:"price_impact_percent"`
	Unfilled    string     `json:"unfilled"`
	Fills       []FillInfo `json:"fills"`
	Feasible    bool       `json:"feasible"`
}

type FillInfo struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

func NewSimulateTradeTool(client *clob.Client) *SimulateTradeTool {
	return &SimulateTradeTool{client: client}
}

func (t *SimulateTradeTool) Name() string {
	return "polymarket_simulate_trade"
}

func (t *SimulateTradeTool) InputSchema() []byte {
	return []byte(`{
		"type": "object",
		"required": ["token_id", "side", "size"],
		"properties": {
			"token_id": {"type": "string", "description": "Token ID to trade"},
			"side": {"type": "string", "enum": ["BUY", "SELL"], "description": "Trade side"},
			"size": {"type": "number", "description": "Amount to trade"}
		}
	}`)
}

func (t *SimulateTradeTool) OutputSchema() []byte {
	return []byte(`{"type": "object"}`)
}

func (t *SimulateTradeTool) Execute(tc *core.ToolContext) *core.ToolExecResult {
	var input SimulateTradeInput
	if err := parseInput(tc.Request, &input); err != nil {
		return errorResult(err)
	}

	if input.TokenID == "" || input.Side == "" || input.Size <= 0 {
		return errorResult(fmt.Errorf("token_id, side, and positive size are required"))
	}

	ctx, cancel := context.WithTimeout(tc.Ctx, 30*time.Second)
	defer cancel()

	bookSummary, err := t.client.GetOrderBook(ctx, input.TokenID)
	if err != nil {
		return errorResult(fmt.Errorf("get orderbook failed: %w", err))
	}

	// Build orderbook
	ob := book.NewOrderBook(input.TokenID, bookSummary.Market)

	bids := make([]book.PriceLevel, len(bookSummary.Bids))
	for i, b := range bookSummary.Bids {
		price, _ := decimal.NewFromString(b.Price)
		size, _ := decimal.NewFromString(b.Size)
		bids[i] = book.PriceLevel{Price: price, Size: size}
	}
	ob.SetBids(bids)

	asks := make([]book.PriceLevel, len(bookSummary.Asks))
	for i, a := range bookSummary.Asks {
		price, _ := decimal.NewFromString(a.Price)
		size, _ := decimal.NewFromString(a.Size)
		asks[i] = book.PriceLevel{Price: price, Size: size}
	}
	ob.SetAsks(asks)

	// Simulate
	var side book.Side
	if input.Side == "SELL" {
		side = book.SideSell
	} else {
		side = book.SideBuy
	}

	result := ob.SimulateMarketOrder(side, decimal.NewFromFloat(input.Size))

	fills := make([]FillInfo, len(result.Fills))
	for i, f := range result.Fills {
		fills[i] = FillInfo{
			Price: f.Price.String(),
			Size:  f.Size.String(),
		}
	}

	return &core.ToolExecResult{
		Status: core.ToolComplete,
		Output: SimulateTradeOutput{
			TotalSize:   result.TotalSize.String(),
			TotalCost:   result.TotalCost.String(),
			AvgPrice:    result.AvgPrice.String(),
			PriceImpact: result.PriceImpact.StringFixed(4),
			Unfilled:    result.Unfilled.String(),
			Fills:       fills,
			Feasible:    result.Unfilled.IsZero(),
		},
	}
}

// === Authenticated Tools (require L2 credentials) ===

// GetOpenOrdersTool fetches the user's open orders.
type GetOpenOrdersTool struct {
	client *clob.Client
}

type GetOpenOrdersOutput struct {
	Orders []OrderInfo `json:"orders"`
	Count  int         `json:"count"`
}

type OrderInfo struct {
	ID         string `json:"id"`
	TokenID    string `json:"token_id"`
	Side       string `json:"side"`
	Price      string `json:"price"`
	Size       string `json:"size"`
	SizeFilled string `json:"size_filled"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
}

func NewGetOpenOrdersTool(client *clob.Client) *GetOpenOrdersTool {
	return &GetOpenOrdersTool{client: client}
}

func (t *GetOpenOrdersTool) Name() string {
	return "polymarket_get_open_orders"
}

func (t *GetOpenOrdersTool) InputSchema() []byte {
	return []byte(`{"type": "object", "properties": {}}`)
}

func (t *GetOpenOrdersTool) OutputSchema() []byte {
	return []byte(`{"type": "object"}`)
}

func (t *GetOpenOrdersTool) Execute(tc *core.ToolContext) *core.ToolExecResult {
	if !t.client.HasCredentials() {
		return errorResult(fmt.Errorf("L2 credentials required - call polymarket_authenticate first"))
	}

	ctx, cancel := context.WithTimeout(tc.Ctx, 30*time.Second)
	defer cancel()

	orders, err := t.client.GetOpenOrders(ctx)
	if err != nil {
		return errorResult(fmt.Errorf("get open orders failed: %w", err))
	}

	infos := make([]OrderInfo, len(orders))
	for i, o := range orders {
		infos[i] = OrderInfo{
			ID:         o.ID,
			TokenID:    o.TokenID,
			Side:       string(o.Side),
			Price:      o.Price,
			Size:       o.Size,
			SizeFilled: o.SizeFilled,
			Status:     string(o.Status),
			CreatedAt:  o.CreatedAt.Format(time.RFC3339),
		}
	}

	return &core.ToolExecResult{
		Status: core.ToolComplete,
		Output: GetOpenOrdersOutput{
			Orders: infos,
			Count:  len(infos),
		},
	}
}

// GetTradesTool fetches the user's trade history.
type GetTradesTool struct {
	client *clob.Client
}

type GetTradesOutput struct {
	Trades []TradeInfo `json:"trades"`
	Count  int         `json:"count"`
}

type TradeInfo struct {
	ID        string `json:"id"`
	TokenID   string `json:"token_id"`
	Side      string `json:"side"`
	Price     string `json:"price"`
	Size      string `json:"size"`
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

func NewGetTradesTool(client *clob.Client) *GetTradesTool {
	return &GetTradesTool{client: client}
}

func (t *GetTradesTool) Name() string {
	return "polymarket_get_trades"
}

func (t *GetTradesTool) InputSchema() []byte {
	return []byte(`{"type": "object", "properties": {}}`)
}

func (t *GetTradesTool) OutputSchema() []byte {
	return []byte(`{"type": "object"}`)
}

func (t *GetTradesTool) Execute(tc *core.ToolContext) *core.ToolExecResult {
	if !t.client.HasCredentials() {
		return errorResult(fmt.Errorf("L2 credentials required - call polymarket_authenticate first"))
	}

	ctx, cancel := context.WithTimeout(tc.Ctx, 30*time.Second)
	defer cancel()

	trades, err := t.client.GetTrades(ctx)
	if err != nil {
		return errorResult(fmt.Errorf("get trades failed: %w", err))
	}

	infos := make([]TradeInfo, len(trades))
	for i, t := range trades {
		infos[i] = TradeInfo{
			ID:        t.ID,
			TokenID:   t.TokenID,
			Side:      string(t.Side),
			Price:     t.Price,
			Size:      t.Size,
			Status:    t.Status,
			Timestamp: t.MatchTime.Format(time.RFC3339),
		}
	}

	return &core.ToolExecResult{
		Status: core.ToolComplete,
		Output: GetTradesOutput{
			Trades: infos,
			Count:  len(infos),
		},
	}
}

// === Trading Tools (modify positions - HIGH RISK) ===

// PlaceOrderTool places a limit order.
// WARNING: This tool modifies positions and should have strict rate limits.
type PlaceOrderTool struct {
	client *clob.Client
}

type PlaceOrderInput struct {
	TokenID   string  `json:"token_id"`
	Side      string  `json:"side"`                 // "BUY" or "SELL"
	Price     float64 `json:"price"`                // 0.01 to 0.99
	Size      float64 `json:"size"`                 // Amount in tokens
	OrderType string  `json:"order_type,omitempty"` // "GTC", "FOK", "GTD"
	NegRisk   bool    `json:"neg_risk,omitempty"`   // For neg-risk markets
}

type PlaceOrderOutput struct {
	Success bool   `json:"success"`
	OrderID string `json:"order_id,omitempty"`
	Error   string `json:"error,omitempty"`
}

func NewPlaceOrderTool(client *clob.Client) *PlaceOrderTool {
	return &PlaceOrderTool{client: client}
}

func (t *PlaceOrderTool) Name() string {
	return "polymarket_place_order"
}

func (t *PlaceOrderTool) InputSchema() []byte {
	return []byte(`{
		"type": "object",
		"required": ["token_id", "side", "price", "size"],
		"properties": {
			"token_id": {"type": "string", "description": "Token ID to trade"},
			"side": {"type": "string", "enum": ["BUY", "SELL"], "description": "Order side"},
			"price": {"type": "number", "minimum": 0.01, "maximum": 0.99, "description": "Limit price"},
			"size": {"type": "number", "minimum": 0, "description": "Order size in tokens"},
			"order_type": {"type": "string", "enum": ["GTC", "FOK", "GTD"], "description": "Order type (default GTC)"},
			"neg_risk": {"type": "boolean", "description": "Whether this is a neg-risk market"}
		}
	}`)
}

func (t *PlaceOrderTool) OutputSchema() []byte {
	return []byte(`{"type": "object"}`)
}

func (t *PlaceOrderTool) Execute(tc *core.ToolContext) *core.ToolExecResult {
	if !t.client.HasCredentials() {
		return errorResult(fmt.Errorf("L2 credentials required - call polymarket_authenticate first"))
	}

	var input PlaceOrderInput
	if err := parseInput(tc.Request, &input); err != nil {
		return errorResult(err)
	}

	// Validate inputs
	if input.TokenID == "" {
		return errorResult(fmt.Errorf("token_id is required"))
	}
	if input.Side != "BUY" && input.Side != "SELL" {
		return errorResult(fmt.Errorf("side must be BUY or SELL"))
	}
	if input.Price < 0.01 || input.Price > 0.99 {
		return errorResult(fmt.Errorf("price must be between 0.01 and 0.99"))
	}
	if input.Size <= 0 {
		return errorResult(fmt.Errorf("size must be positive"))
	}

	ctx, cancel := context.WithTimeout(tc.Ctx, 30*time.Second)
	defer cancel()

	orderType := clob.OrderTypeGTC
	if input.OrderType != "" {
		orderType = clob.OrderType(input.OrderType)
	}

	var side clob.OrderSide
	if input.Side == "SELL" {
		side = clob.OrderSideSell
	} else {
		side = clob.OrderSideBuy
	}

	args := &clob.OrderArgs{
		TokenID:   input.TokenID,
		Side:      side,
		Price:     input.Price,
		Size:      input.Size,
		OrderType: orderType,
	}

	// Get tick size from market (use default for now)
	tickSize := "0.01"

	resp, err := t.client.CreateAndPostOrder(ctx, args, tickSize, input.NegRisk)
	if err != nil {
		return errorResult(fmt.Errorf("place order failed: %w", err))
	}

	return &core.ToolExecResult{
		Status: core.ToolComplete,
		Output: PlaceOrderOutput{
			Success: resp.Success,
			OrderID: resp.OrderID,
			Error:   resp.ErrorMsg,
		},
	}
}

// CancelOrderTool cancels an open order.
type CancelOrderTool struct {
	client *clob.Client
}

type CancelOrderInput struct {
	OrderID string `json:"order_id"`
}

type CancelOrderOutput struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

func NewCancelOrderTool(client *clob.Client) *CancelOrderTool {
	return &CancelOrderTool{client: client}
}

func (t *CancelOrderTool) Name() string {
	return "polymarket_cancel_order"
}

func (t *CancelOrderTool) InputSchema() []byte {
	return []byte(`{
		"type": "object",
		"required": ["order_id"],
		"properties": {
			"order_id": {"type": "string", "description": "Order ID to cancel"}
		}
	}`)
}

func (t *CancelOrderTool) OutputSchema() []byte {
	return []byte(`{"type": "object"}`)
}

func (t *CancelOrderTool) Execute(tc *core.ToolContext) *core.ToolExecResult {
	if !t.client.HasCredentials() {
		return errorResult(fmt.Errorf("L2 credentials required"))
	}

	var input CancelOrderInput
	if err := parseInput(tc.Request, &input); err != nil {
		return errorResult(err)
	}

	if input.OrderID == "" {
		return errorResult(fmt.Errorf("order_id is required"))
	}

	ctx, cancel := context.WithTimeout(tc.Ctx, 30*time.Second)
	defer cancel()

	err := t.client.CancelOrder(ctx, input.OrderID)
	if err != nil {
		return &core.ToolExecResult{
			Status: core.ToolComplete,
			Output: CancelOrderOutput{
				Success: false,
				Error:   err.Error(),
			},
		}
	}

	return &core.ToolExecResult{
		Status: core.ToolComplete,
		Output: CancelOrderOutput{
			Success: true,
		},
	}
}

// CancelAllOrdersTool cancels all open orders.
type CancelAllOrdersTool struct {
	client *clob.Client
}

func NewCancelAllOrdersTool(client *clob.Client) *CancelAllOrdersTool {
	return &CancelAllOrdersTool{client: client}
}

func (t *CancelAllOrdersTool) Name() string {
	return "polymarket_cancel_all_orders"
}

func (t *CancelAllOrdersTool) InputSchema() []byte {
	return []byte(`{"type": "object", "properties": {}}`)
}

func (t *CancelAllOrdersTool) OutputSchema() []byte {
	return []byte(`{"type": "object"}`)
}

func (t *CancelAllOrdersTool) Execute(tc *core.ToolContext) *core.ToolExecResult {
	if !t.client.HasCredentials() {
		return errorResult(fmt.Errorf("L2 credentials required"))
	}

	ctx, cancel := context.WithTimeout(tc.Ctx, 30*time.Second)
	defer cancel()

	err := t.client.CancelAllOrders(ctx)
	if err != nil {
		return &core.ToolExecResult{
			Status: core.ToolComplete,
			Output: CancelOrderOutput{
				Success: false,
				Error:   err.Error(),
			},
		}
	}

	return &core.ToolExecResult{
		Status: core.ToolComplete,
		Output: CancelOrderOutput{
			Success: true,
		},
	}
}

// === Registration ===

// RegisterCLOBReadOnlyTools registers read-only CLOB tools.
func RegisterCLOBReadOnlyTools(registry *core.ToolRegistry, client *clob.Client) {
	policy := core.ToolPolicy{
		MaxRetries:      3,
		BaseBackoff:     100 * time.Millisecond,
		MaxBackoff:      5 * time.Second,
		Retriable:       true,
		DefaultTimeout:  30 * time.Second,
		RateLimitPerSec: 10.0,
		Burst:           20,
		LimitKey:        "polymarket-clob",
	}

	registry.Register(NewGetOrderBookTool(client), policy, RiskClassReadOnly)
	registry.Register(NewGetMarketInfoTool(client), policy, RiskClassReadOnly)
	registry.Register(NewSimulateTradeTool(client), policy, RiskClassReadOnly)
}

// RegisterCLOBAuthenticatedTools registers authenticated but non-trading tools.
func RegisterCLOBAuthenticatedTools(registry *core.ToolRegistry, client *clob.Client) {
	policy := core.ToolPolicy{
		MaxRetries:      2,
		BaseBackoff:     200 * time.Millisecond,
		MaxBackoff:      5 * time.Second,
		Retriable:       true,
		DefaultTimeout:  30 * time.Second,
		RateLimitPerSec: 5.0,
		Burst:           10,
		LimitKey:        "polymarket-clob-auth",
	}

	registry.Register(NewGetOpenOrdersTool(client), policy, RiskClassAuthenticated)
	registry.Register(NewGetTradesTool(client), policy, RiskClassAuthenticated)
}

// RegisterCLOBTradingTools registers trading tools.
// WARNING: These tools can modify positions and should be used with care.
func RegisterCLOBTradingTools(registry *core.ToolRegistry, client *clob.Client) {
	// Strict rate limiting for trading
	tradingPolicy := core.ToolPolicy{
		MaxRetries:      1, // No retries for order placement
		BaseBackoff:     500 * time.Millisecond,
		MaxBackoff:      5 * time.Second,
		Retriable:       false,
		DefaultTimeout:  30 * time.Second,
		RateLimitPerSec: 1.0, // Only 1 order per second
		Burst:           2,
		LimitKey:        "polymarket-trading",
		BudgetPerDay:    100.0, // Max 100 orders per day
		CostPerCall:     1.0,   // Each order counts as 1
	}

	registry.Register(NewPlaceOrderTool(client), tradingPolicy, RiskClassTrading)

	// Cancel operations can be slightly more frequent
	cancelPolicy := tradingPolicy
	cancelPolicy.RateLimitPerSec = 5.0
	cancelPolicy.Burst = 10
	cancelPolicy.BudgetPerDay = 500.0

	registry.Register(NewCancelOrderTool(client), cancelPolicy, RiskClassTrading)
	registry.Register(NewCancelAllOrdersTool(client), cancelPolicy, RiskClassTrading)
}

// RegisterAllCLOBTools registers all CLOB tools.
func RegisterAllCLOBTools(registry *core.ToolRegistry, client *clob.Client) {
	RegisterCLOBReadOnlyTools(registry, client)
	RegisterCLOBAuthenticatedTools(registry, client)
	RegisterCLOBTradingTools(registry, client)
}
