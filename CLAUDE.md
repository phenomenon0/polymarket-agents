# polymarket-agents

## Philosophy

Code is crystallized intention. Understand the mental model before touching anything. Resist premature abstraction — three similar lines of code is better than a premature helper. Keep changes minimal and precise. Every package should justify its existence.

## System Architecture

```
                         cmd/agentd  (HTTP :8080, WS, metrics)
                              │
                     orchestrator (DAG workflow)
              ┌───────────┬───┴───┬──────────┬───────────┐
              │           │       │          │           │
         discovery    forecast  signal    risk-check  execute
         (gamma)     (LLM ens) (edge)    (policy)    (paper/live)
              │           │                              │
              ▼           ▼                              ▼
         gamma API   model router                  clob client
                    (9 tiers, 30+ models)         (orders, book)
                         │                              │
              ┌──────────┼──────────┐              orderbook
            ollama    deepseek   openrouter       (book pkg)
                                                       │
                                                    wss client
                                                  (auto-reconnect)

         sportsbridge ← sports ← mathshard
         (edge, Kelly)   (parser, teams)
```

### Workflow stages (in order):
1. **MarketDiscovery** — Fetch tradeable markets from Gamma
2. **DataCollection** — Fetch order books from CLOB
3. **Forecasting** — LLM ensemble probability estimates
4. **SignalGen** — Compare forecast vs market, calculate edge
5. **RiskCheck** — Validate against policy engine limits
6. **OrderExecution** — Paper or live order placement
7. **Monitoring** — Track positions, update prices

## Package Map

### Commands
- `cmd/agentd/main.go` — Trading daemon entry point. Flags, HTTP routes, orchestrator setup.
- `cmd/backtest/main.go` — Backtesting CLI. Loads data, runs strategies, prints results.
- `cmd/backtest/convert_trades.go` — Trade data conversion utilities.

### Core
- `core/types.go` — Minimal framework shim: `ToolContext`, `ToolExecResult`, `ToolChunk`, `Message`, `ToolPolicy`, `ToolRegistry`. No external deps.

### Tools
- `tools/llm.go` — LLM tool implementation (`LLMConfig`, `LLMTool`).
- `tools/llm_router.go` — Model router with 9 tiers and 30+ presets. Key types: `ModelTier`, `ModelPreset`, `ModelRouter`.
- `tools/polymarket/clob_tools.go` — CLOB tool wrappers for MCP.
- `tools/polymarket/gamma_tools.go` — Gamma tool wrappers for MCP.

### Ethereum
- `pkg/eth/wallet.go` — Private key → address, signing.
- `pkg/eth/eip712.go` — EIP-712 typed data signing for CLOB orders.
- `pkg/eth/hmac.go` — HMAC-SHA256 for L2 API authentication.
- `pkg/eth/constants.go` — Chain IDs, contract addresses.

### Polymarket API Clients
- `pkg/polymarket/clob/client.go` — CLOB client. `NewClient(privateKey)`, `NewPublicClient()`. Methods: `GetOrderBook`, `GetMidpoint`, `PostOrder`, `CancelOrder`, `CreateAndPostOrder`, `GetPriceHistory`. Base URL: `https://clob.polymarket.com`.
- `pkg/polymarket/clob/types.go` — `Order`, `OrderStatus`, `OrderSide`, `OrderType` (GTC/FOK/GTD), `OrderBookSummary`, `PriceLevel`, `Trade`, `Token`, `APICredentials`.
- `pkg/polymarket/clob/wss.go` — CLOB WebSocket for real-time book updates.
- `pkg/polymarket/gamma/client.go` — Gamma client. `NewClient()`. Methods: `ListEvents`, `GetEvent`, `ListMarkets`, `GetMarket`, `ListTradeableEvents`, `ListAllTradeableEvents`. Base URL: `https://gamma-api.polymarket.com`. Rate limit: 10 req/s, burst 5.
- `pkg/polymarket/gamma/types.go` — `Event`, `Market`, `Tag`, `EventsFilter`, `MarketsFilter`. Market helpers: `YesTokenID()`, `NoTokenID()`, `YesPrice()`, `NoPrice()`.
- `pkg/polymarket/book/orderbook.go` — `OrderBook` management, bid/ask levels, mid price.

### Sports Analytics
- `pkg/polymarket/sports/parser.go` — Parse Polymarket markets into structured soccer events.
- `pkg/polymarket/sports/types.go` — `MarketKind` (HOME_WIN, DRAW, AWAY_WIN, BTTS, TOTAL, SPREAD), `SoccerMarketSpec`, `Prediction`, `EdgeResult`. League configs with min edge thresholds.
- `pkg/polymarket/sports/teams.go` — Team name normalization/mapping.
- `pkg/polymarket/sports/mathshard.go` — MathShard API client for probability forecasts. `AlphaProvider` interface.
- `pkg/polymarket/sports/edge.go` — Edge calculation: model prob vs market price.
- `pkg/polymarket/sportsbridge/types.go` — `EventSpec` interface, `Soccer1X2Event`, `Contract`, `Prob3`, `ScoreResult`, `EdgeResult`, `Signal`.
- `pkg/polymarket/sportsbridge/alpha.go` — `AlphaProvider` interface: `Name()`, `CanScore(*Contract)`, `Score(ctx, *Contract)`.
- `pkg/polymarket/sportsbridge/alpha_calibrated.go` — Calibrated probability model.
- `pkg/polymarket/sportsbridge/edge.go` — VWAP-based edge with Kelly sizing.
- `pkg/polymarket/sportsbridge/signaler.go` — Signal generation from alpha + edge.
- `pkg/polymarket/sportsbridge/parser.go` — Market → EventSpec parsing.

### Trader
- `pkg/trader/agents/forecaster.go` — `Forecaster` with `ForecastEnsemble`, `ForecastSingle`, `ForecastWithFallback`, `GenerateSignal`. Types: `LLMClient` interface, `Forecast`, `EnsembleForecast`, `TradingSignal`.
- `pkg/trader/agents/llm_clients.go` — LLM client implementations. `ForecasterPreset` (elite/balanced/cheap/local/fast). `CreateForecasterWithPreset(router, preset)`.
- `pkg/trader/backtest/backtest.go` — `Backtest` engine, `Strategy` interface (`OnTick`, `OnStart`, `OnEnd`), `Config`, `Result`, `PricePoint`, `HistoricalData`.
- `pkg/trader/backtest/strategies.go` — Built-in strategies: `MomentumStrategy`, `MeanReversionStrategy`, `BuyAndHoldStrategy`, `ForecasterStrategy`, `EdgeStrategy`.
- `pkg/trader/orchestrator/orchestrator.go` — `Orchestrator`, `WorkflowConfig`, `StageResult`. Stages: Discovery → DataCollection → Forecasting → SignalGen → RiskCheck → Execution → Monitoring.
- `pkg/trader/paper/engine.go` — `Engine`, `PriceProvider` interface, `SimulationConfig`. Paper trade execution.
- `pkg/trader/paper/types.go` — `Trade`, `Position`, `Account`, `Stats`.
- `pkg/trader/policy/limits.go` — `RiskLimits`, `PolicyEngine`, `DefaultRiskLimits()`, `TightRiskLimits()`.
- `pkg/trader/policy/geoblock.go` — Geographic restriction checks.
- `pkg/trader/metrics/metrics.go` — Prometheus metrics registration.
- `pkg/trader/streaming/hub.go` — WebSocket hub for broadcasting signals, trades, errors.

### WebSocket
- `pkg/wss/client.go` — Generic WebSocket client with auto-reconnect, heartbeat, exponential backoff.
- `pkg/wss/subscription.go` — Subscription management and message routing.

## Key Interfaces

```go
// LLMClient — pkg/trader/agents/forecaster.go
type LLMClient interface {
    Complete(ctx context.Context, prompt string, systemPrompt string) (string, error)
    Provider() LLMProvider
}

// PriceProvider — pkg/trader/paper/engine.go
type PriceProvider interface {
    GetMidPrice(ctx context.Context, tokenID string) (decimal.Decimal, error)
    GetOrderBook(ctx context.Context, tokenID string) (*book.OrderBook, error)
}

// Strategy — pkg/trader/backtest/backtest.go
type Strategy interface {
    OnTick(ctx context.Context, bt *Backtest, point PricePoint)
    OnStart(ctx context.Context, bt *Backtest)
    OnEnd(ctx context.Context, bt *Backtest)
}

// AlphaProvider — pkg/polymarket/sportsbridge/alpha.go
type AlphaProvider interface {
    Name() string
    CanScore(c *Contract) bool
    Score(ctx context.Context, c *Contract) (*ScoreResult, error)
}

// EventSpec — pkg/polymarket/sportsbridge/types.go
type EventSpec interface {
    Category() string
    Key() string
}
```

## Key Commands

```bash
# Build
go build ./...

# Test
go test ./...

# Run paper trading
go run ./cmd/agentd --paper --llm-preset=balanced

# Run backtest demo
go run ./cmd/backtest

# Run backtest with data
go run ./cmd/backtest --data prices.json --strategy=momentum

# Live trading (careful!)
go run ./cmd/agentd --paper=false --key=$POLYMARKET_PRIVATE_KEY --llm-preset=elite
```

## Environment Variables

| Variable | Used By | Description |
|----------|---------|-------------|
| `POLYMARKET_PRIVATE_KEY` | agentd | Polygon wallet private key for live trading |
| `OLLAMA_URL` | llm_router | Ollama server URL (default: `http://localhost:11434`) |
| `OLLAMA_MODEL` | .env config | Default Ollama model (default: `qwen3:8b`) |
| `DEEPSEEK_API_KEY` | llm_router | DeepSeek API key |
| `CEREBRAS_API_KEY` | llm_router | Cerebras API key |
| `OPENROUTER_API_KEY` | llm_router | OpenRouter API key |
| `ANTHROPIC_API_KEY` | llm_router | Anthropic API key |
| `KIMI_API_KEY` | llm_router | Kimi/Moonshot API key |

## File Conventions

- `*_test.go` — Tests, colocated with source
- `types.go` — Type definitions for a package
- `client.go` — API client implementation
- `engine.go` — Core engine logic (paper, backtest)
- `strategies.go` — Strategy implementations

## Dependencies

- `core/` is a minimal shim (`ToolContext`, `ToolExecResult`, etc.) — no external deps
- `pkg/eth/` is the Ethereum signing layer (wallet, EIP-712, HMAC) — depends on `go-ethereum`
- Key external deps: `go-ethereum` (signing), `shopspring/decimal` (math), `gorilla/websocket` (WS), `prometheus/client_golang` (metrics)
- No dependency on any external monorepo

## Known Considerations

- Integration tests require Ollama running locally or cloud API keys set in env
- CLOB trading requires a funded Polygon wallet with USDC
- Sports analytics require MathShard as a data source
- The `core/` package comment still references "Agent-GO" — it's a standalone shim
- Paper mode uses `TightRiskLimits()` (smaller limits for safety)
- Forecaster with `nil` LLM clients initializes but won't generate signals (`--no-llm` mode)
- Gamma API has a 10 req/s rate limit (burst: 5)
