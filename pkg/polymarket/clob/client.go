package clob

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/phenomenon0/polymarket-agents/pkg/eth"

	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/time/rate"
)

// Client is a CLOB API client.
type Client struct {
	baseURL    string
	chainID    int
	wallet     *eth.Wallet
	eip712     *eth.EIP712Signer
	hmac       *eth.HMACSigner
	creds      *APICredentials
	httpClient *http.Client
	limiter    *rate.Limiter
	sigType    int    // 0=EOA, 1=PolyProxy, 2=GnosisSafe
	funder     string // Funder address (for proxy wallets)
}

// ClientOption configures the client.
type ClientOption func(*Client)

// WithCLOBBaseURL sets a custom base URL.
func WithCLOBBaseURL(url string) ClientOption {
	return func(c *Client) {
		c.baseURL = url
	}
}

// WithChainID sets the chain ID.
func WithChainID(chainID int) ClientOption {
	return func(c *Client) {
		c.chainID = chainID
	}
}

// WithCredentials sets L2 API credentials.
func WithCredentials(creds *APICredentials) ClientOption {
	return func(c *Client) {
		c.creds = creds
		c.hmac = eth.NewHMACSigner(&eth.APICredentials{
			APIKey:     creds.APIKey,
			Secret:     creds.Secret,
			Passphrase: creds.Passphrase,
		})
	}
}

// WithSignatureType sets the signature type.
// 0=EOA, 1=PolyProxy, 2=GnosisSafe
func WithSignatureType(sigType int) ClientOption {
	return func(c *Client) {
		c.sigType = sigType
	}
}

// WithFunder sets the funder address (for proxy wallets).
func WithFunder(funder string) ClientOption {
	return func(c *Client) {
		c.funder = funder
	}
}

// WithCLOBHTTPClient sets a custom HTTP client.
func WithCLOBHTTPClient(client *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = client
	}
}

// NewClient creates a new CLOB API client.
func NewClient(privateKey string, opts ...ClientOption) (*Client, error) {
	wallet, err := eth.NewWallet(privateKey)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	c := &Client{
		baseURL: DefaultBaseURL,
		chainID: ChainIDPolygon,
		wallet:  wallet,
		eip712:  eth.NewEIP712Signer(wallet),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        20,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		limiter: rate.NewLimiter(rate.Limit(10), 5),
		sigType: 0, // EOA by default
	}

	for _, opt := range opts {
		opt(c)
	}

	// Default funder to wallet address
	if c.funder == "" {
		c.funder = wallet.AddressHex()
	}

	return c, nil
}

// NewPublicClient creates a CLOB client for public (unauthenticated) operations only.
// Use this for reading orderbooks, prices, and market data without needing a wallet.
func NewPublicClient(opts ...ClientOption) *Client {
	c := &Client{
		baseURL: DefaultBaseURL,
		chainID: ChainIDPolygon,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        20,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		limiter: rate.NewLimiter(rate.Limit(10), 5),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Address returns the wallet address.
func (c *Client) Address() string {
	return c.wallet.AddressHex()
}

// Funder returns the funder address.
func (c *Client) Funder() string {
	return c.funder
}

// HasCredentials returns true if L2 credentials are set.
func (c *Client) HasCredentials() bool {
	return c.creds != nil
}

// --- L1 Authentication Methods ---

// CreateOrDeriveAPIKey creates or derives L2 API credentials.
// This uses L1 (EIP-712) authentication.
func (c *Client) CreateOrDeriveAPIKey(ctx context.Context) (*APICredentials, error) {
	// Try to derive first
	creds, err := c.DeriveAPIKey(ctx, 0)
	if err == nil {
		return creds, nil
	}

	// If derive fails, create new
	return c.CreateAPIKey(ctx, 0)
}

// CreateAPIKey creates new L2 API credentials.
func (c *Client) CreateAPIKey(ctx context.Context, nonce int64) (*APICredentials, error) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonceBI := big.NewInt(nonce)

	signature, err := c.eip712.SignClobAuth(int64(c.chainID), timestamp, nonceBI)
	if err != nil {
		return nil, fmt.Errorf("sign failed: %w", err)
	}

	headers := eth.L1AuthHeaders(c.wallet.AddressHex(), signature, timestamp, nonce)

	var creds APICredentials
	if err := c.post(ctx, "/auth/api-key", headers, nil, &creds); err != nil {
		return nil, err
	}

	// Store credentials
	c.creds = &creds
	c.hmac = eth.NewHMACSigner(&eth.APICredentials{
		APIKey:     creds.APIKey,
		Secret:     creds.Secret,
		Passphrase: creds.Passphrase,
	})

	return &creds, nil
}

// DeriveAPIKey derives existing L2 API credentials.
func (c *Client) DeriveAPIKey(ctx context.Context, nonce int64) (*APICredentials, error) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonceBI := big.NewInt(nonce)

	signature, err := c.eip712.SignClobAuth(int64(c.chainID), timestamp, nonceBI)
	if err != nil {
		return nil, fmt.Errorf("sign failed: %w", err)
	}

	headers := eth.L1AuthHeaders(c.wallet.AddressHex(), signature, timestamp, nonce)

	var creds APICredentials
	if err := c.get(ctx, "/auth/derive-api-key", headers, nil, &creds); err != nil {
		return nil, err
	}

	// Store credentials
	c.creds = &creds
	c.hmac = eth.NewHMACSigner(&eth.APICredentials{
		APIKey:     creds.APIKey,
		Secret:     creds.Secret,
		Passphrase: creds.Passphrase,
	})

	return &creds, nil
}

// --- Public Methods (no auth required) ---

// GetOrderBook fetches the orderbook for a token.
func (c *Client) GetOrderBook(ctx context.Context, tokenID string) (*OrderBookSummary, error) {
	params := url.Values{}
	params.Set("token_id", tokenID)

	var book OrderBookSummary
	if err := c.get(ctx, "/book", nil, params, &book); err != nil {
		return nil, err
	}
	return &book, nil
}

// GetPrice fetches the current price for a token.
func (c *Client) GetPrice(ctx context.Context, tokenID string) (string, error) {
	params := url.Values{}
	params.Set("token_id", tokenID)

	var result struct {
		Price string `json:"price"`
	}
	if err := c.get(ctx, "/price", nil, params, &result); err != nil {
		return "", err
	}
	return result.Price, nil
}

// GetMidpoint fetches the midpoint price for a token.
func (c *Client) GetMidpoint(ctx context.Context, tokenID string) (string, error) {
	params := url.Values{}
	params.Set("token_id", tokenID)

	var result struct {
		Mid string `json:"mid"`
	}
	if err := c.get(ctx, "/midpoint", nil, params, &result); err != nil {
		return "", err
	}
	return result.Mid, nil
}

// GetSpread fetches the bid-ask spread for a token.
func (c *Client) GetSpread(ctx context.Context, tokenID string) (bid, ask string, err error) {
	params := url.Values{}
	params.Set("token_id", tokenID)

	var result struct {
		Bid string `json:"bid"`
		Ask string `json:"ask"`
	}
	if err := c.get(ctx, "/spread", nil, params, &result); err != nil {
		return "", "", err
	}
	return result.Bid, result.Ask, nil
}

// PriceHistoryPoint represents a single point in price history.
type PriceHistoryPoint struct {
	Timestamp int64   `json:"t"` // Unix timestamp (seconds)
	Price     float64 `json:"p"` // Price at that time
}

// PriceHistoryResponse is the response from prices-history endpoint.
type PriceHistoryResponse struct {
	History []PriceHistoryPoint `json:"history"`
}

// GetPriceHistory fetches historical prices for a token.
// interval: time resolution - "1m", "5m", "1h", "1d"
// fidelity: minimum granularity in minutes (e.g., 1, 5, 60)
// startTs, endTs: Unix timestamps in seconds (0 = no limit)
func (c *Client) GetPriceHistory(ctx context.Context, tokenID string, startTs, endTs int64, fidelity int) ([]PriceHistoryPoint, error) {
	params := url.Values{}
	params.Set("market", tokenID)
	if startTs > 0 {
		params.Set("startTs", strconv.FormatInt(startTs, 10))
	}
	if endTs > 0 {
		params.Set("endTs", strconv.FormatInt(endTs, 10))
	}
	if fidelity > 0 {
		params.Set("fidelity", strconv.Itoa(fidelity))
	}

	var result PriceHistoryResponse
	if err := c.get(ctx, "/prices-history", nil, params, &result); err != nil {
		return nil, err
	}
	return result.History, nil
}

// GetMarket fetches market info by condition ID.
func (c *Client) GetMarket(ctx context.Context, conditionID string) (*MarketInfo, error) {
	var market MarketInfo
	if err := c.get(ctx, "/markets/"+conditionID, nil, nil, &market); err != nil {
		return nil, err
	}
	return &market, nil
}

// --- L2 Authenticated Methods ---

// GetOpenOrders fetches open orders for the authenticated user.
func (c *Client) GetOpenOrders(ctx context.Context) ([]Order, error) {
	if !c.HasCredentials() {
		return nil, fmt.Errorf("L2 credentials required")
	}

	headers, err := c.l2Headers("GET", "/orders", nil)
	if err != nil {
		return nil, err
	}

	var orders []Order
	if err := c.get(ctx, "/orders", headers, nil, &orders); err != nil {
		return nil, err
	}
	return orders, nil
}

// GetOrder fetches a specific order by ID.
func (c *Client) GetOrder(ctx context.Context, orderID string) (*Order, error) {
	if !c.HasCredentials() {
		return nil, fmt.Errorf("L2 credentials required")
	}

	path := "/orders/" + orderID
	headers, err := c.l2Headers("GET", path, nil)
	if err != nil {
		return nil, err
	}

	var order Order
	if err := c.get(ctx, path, headers, nil, &order); err != nil {
		return nil, err
	}
	return &order, nil
}

// GetTrades fetches trades for the authenticated user.
func (c *Client) GetTrades(ctx context.Context) ([]Trade, error) {
	if !c.HasCredentials() {
		return nil, fmt.Errorf("L2 credentials required")
	}

	headers, err := c.l2Headers("GET", "/trades", nil)
	if err != nil {
		return nil, err
	}

	var trades []Trade
	if err := c.get(ctx, "/trades", headers, nil, &trades); err != nil {
		return nil, err
	}
	return trades, nil
}

// PostOrder submits a signed order.
func (c *Client) PostOrder(ctx context.Context, order *SignedOrder) (*PostOrderResponse, error) {
	if !c.HasCredentials() {
		return nil, fmt.Errorf("L2 credentials required")
	}

	body, err := json.Marshal(order)
	if err != nil {
		return nil, err
	}

	headers, err := c.l2Headers("POST", "/order", body)
	if err != nil {
		return nil, err
	}

	var resp PostOrderResponse
	if err := c.post(ctx, "/order", headers, body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CancelOrder cancels an order by ID.
func (c *Client) CancelOrder(ctx context.Context, orderID string) error {
	return c.CancelOrders(ctx, []string{orderID})
}

// CancelOrders cancels multiple orders.
func (c *Client) CancelOrders(ctx context.Context, orderIDs []string) error {
	if !c.HasCredentials() {
		return fmt.Errorf("L2 credentials required")
	}

	body, err := json.Marshal(orderIDs)
	if err != nil {
		return err
	}

	headers, err := c.l2Headers("DELETE", "/orders", body)
	if err != nil {
		return err
	}

	var resp CancelOrderResponse
	if err := c.delete(ctx, "/orders", headers, body, &resp); err != nil {
		return err
	}

	if len(resp.NotCanceled) > 0 {
		return fmt.Errorf("some orders not canceled: %v", resp.NotCanceled)
	}

	return nil
}

// CancelAllOrders cancels all open orders.
func (c *Client) CancelAllOrders(ctx context.Context) error {
	if !c.HasCredentials() {
		return fmt.Errorf("L2 credentials required")
	}

	headers, err := c.l2Headers("DELETE", "/orders/all", nil)
	if err != nil {
		return err
	}

	return c.delete(ctx, "/orders/all", headers, nil, nil)
}

// --- Order Building ---

// BuildOrder creates an order payload from args.
func (c *Client) BuildOrder(args *OrderArgs, tickSize string, negRisk bool) (*OrderPayload, error) {
	// Generate random salt
	salt, err := generateSalt()
	if err != nil {
		return nil, err
	}

	// Calculate amounts based on side
	price := strconv.FormatFloat(args.Price, 'f', -1, 64)
	size := strconv.FormatFloat(args.Size, 'f', -1, 64)

	var makerAmount, takerAmount string
	if args.Side == OrderSideBuy {
		// Buying: maker pays USDC (price * size), receives tokens (size)
		makerAmount = strconv.FormatFloat(args.Price*args.Size*1e6, 'f', 0, 64) // USDC has 6 decimals
		takerAmount = strconv.FormatFloat(args.Size*1e6, 'f', 0, 64)
	} else {
		// Selling: maker pays tokens (size), receives USDC (price * size)
		makerAmount = strconv.FormatFloat(args.Size*1e6, 'f', 0, 64)
		takerAmount = strconv.FormatFloat(args.Price*args.Size*1e6, 'f', 0, 64)
	}

	// Default expiration to 0 (never expires)
	expiration := "0"
	if args.Expiration > 0 {
		expiration = strconv.FormatInt(args.Expiration, 10)
	}

	// Taker is zero address (anyone can fill)
	taker := "0x0000000000000000000000000000000000000000"

	order := &OrderPayload{
		Salt:          salt,
		Maker:         c.funder,
		Signer:        c.wallet.AddressHex(),
		Taker:         taker,
		TokenID:       args.TokenID,
		MakerAmount:   makerAmount,
		TakerAmount:   takerAmount,
		Expiration:    expiration,
		Nonce:         "0",
		FeeRateBps:    "0",
		Side:          string(args.Side),
		SignatureType: c.sigType,
	}

	_ = price
	_ = size

	return order, nil
}

// SignOrder signs an order payload.
func (c *Client) SignOrder(order *OrderPayload, negRisk bool) (string, error) {
	// Determine exchange address based on negRisk
	exchangeAddr := eth.CTFExchangeAddress
	if negRisk {
		exchangeAddr = eth.NegRiskCTFExchangeAddress
	}

	// Convert to eth.OrderData
	salt, _ := new(big.Int).SetString(order.Salt, 10)
	tokenID, _ := new(big.Int).SetString(order.TokenID, 10)
	makerAmount, _ := new(big.Int).SetString(order.MakerAmount, 10)
	takerAmount, _ := new(big.Int).SetString(order.TakerAmount, 10)
	expiration, _ := new(big.Int).SetString(order.Expiration, 10)
	nonce, _ := new(big.Int).SetString(order.Nonce, 10)
	feeRateBps, _ := new(big.Int).SetString(order.FeeRateBps, 10)

	var side uint8
	if order.Side == "SELL" {
		side = 1
	}

	orderData := &eth.OrderData{
		Salt:          salt,
		Maker:         common.HexToAddress(order.Maker),
		Signer:        common.HexToAddress(order.Signer),
		Taker:         common.HexToAddress(order.Taker),
		TokenID:       tokenID,
		MakerAmount:   makerAmount,
		TakerAmount:   takerAmount,
		Expiration:    expiration,
		Nonce:         nonce,
		FeeRateBps:    feeRateBps,
		Side:          side,
		SignatureType: uint8(order.SignatureType),
	}

	return c.eip712.SignOrder(int64(c.chainID), exchangeAddr, orderData)
}

// CreateAndPostOrder builds, signs, and posts an order.
func (c *Client) CreateAndPostOrder(ctx context.Context, args *OrderArgs, tickSize string, negRisk bool) (*PostOrderResponse, error) {
	// Build order
	order, err := c.BuildOrder(args, tickSize, negRisk)
	if err != nil {
		return nil, fmt.Errorf("build order: %w", err)
	}

	// Sign order
	signature, err := c.SignOrder(order, negRisk)
	if err != nil {
		return nil, fmt.Errorf("sign order: %w", err)
	}

	orderType := args.OrderType
	if orderType == "" {
		orderType = OrderTypeGTC
	}

	signedOrder := &SignedOrder{
		Order:     *order,
		Signature: signature,
		Owner:     c.funder,
		OrderType: orderType,
	}

	// Post order
	return c.PostOrder(ctx, signedOrder)
}

// --- Internal helpers ---

func (c *Client) l2Headers(method, path string, body []byte) (map[string]string, error) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	return c.hmac.SignRequest(timestamp, method, path, body, c.funder)
}

func (c *Client) get(ctx context.Context, path string, headers map[string]string, params url.Values, result interface{}) error {
	if err := c.limiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limiter: %w", err)
	}

	u := c.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("api error %d: %s", resp.StatusCode, string(body))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	return nil
}

func (c *Client) post(ctx context.Context, path string, headers map[string]string, body []byte, result interface{}) error {
	if err := c.limiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limiter: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("api error %d: %s", resp.StatusCode, string(body))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	return nil
}

func (c *Client) delete(ctx context.Context, path string, headers map[string]string, body []byte, result interface{}) error {
	if err := c.limiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limiter: %w", err)
	}

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, "DELETE", c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("api error %d: %s", resp.StatusCode, string(body))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	return nil
}

func generateSalt() (string, error) {
	max := new(big.Int).Lsh(big.NewInt(1), 128) // 2^128
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return n.String(), nil
}
