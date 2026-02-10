# polymarket-agents

Pure Go trading agents for Polymarket prediction markets.

## What This Is

LLM ensemble forecasting, paper trading, CLOB/Gamma API clients, a DAG-based orchestrator, sports analytics with MathShard integration, and a backtesting framework — all in one standalone Go module. Originally extracted from a larger monorepo; has zero external monorepo dependencies.

## Architecture

```
                         cmd/agentd  (trading daemon)
                              │
                         orchestrator  (DAG workflow)
                     ┌────────┼────────────┐
                     │        │            │
                forecaster  policy     paper engine
                (LLM ensemble) (risk)   (simulation)
                     │
                model router  (9 tiers, 30+ models)
                     │
          ┌──────────┼──────────┐
        ollama    deepseek   openrouter ...
          │
          ▼
   ┌──────────────┐   ┌─────────┐   ┌──────────┐
   │ clob client  │   │  gamma  │   │   wss    │
   │ (trading)    │   │ (data)  │   │ (stream) │
   └──────────────┘   └─────────┘   └──────────┘
          │                              │
          ▼                              ▼
   ┌──────────────┐            ┌─────────────────┐
   │  orderbook   │            │  sportsbridge   │
   └──────────────┘            │  (alpha/edge)   │
                               └─────────────────┘
```

## Quick Start

```bash
# Clone
git clone https://github.com/phenomenon0/polymarket-agents.git
cd polymarket-agents

# Build
go build ./...

# Configure
cp .env.example .env
# Edit .env — at minimum set OLLAMA_URL or a cloud LLM key

# Paper trading with local LLM
go run ./cmd/agentd --paper --llm-preset=local

# Backtest demo (no data file needed)
go run ./cmd/backtest

# Backtest with real data
go run ./cmd/backtest --data prices.json --strategy=momentum
```

## Project Structure

```
cmd/agentd/              Trading agent daemon (HTTP API, WebSocket, orchestrator)
cmd/backtest/            Backtesting CLI with multiple strategies
core/                    Minimal framework types (ToolContext, ToolExecResult)
tools/                   LLM tool implementation and model router
tools/polymarket/        Polymarket-specific MCP tool wrappers
pkg/eth/                 Ethereum wallet, EIP-712 signing, HMAC auth
pkg/polymarket/clob/     CLOB API client (trading, orders, WebSocket)
pkg/polymarket/gamma/    Gamma API client (market/event metadata)
pkg/polymarket/book/     Order book management and price levels
pkg/polymarket/sports/   Sports market parsing, MathShard integration
pkg/polymarket/sportsbridge/  Sports alpha bridge (edge calc, Kelly sizing)
pkg/trader/agents/       LLM-based ensemble forecaster
pkg/trader/backtest/     Backtesting engine and strategy implementations
pkg/trader/orchestrator/ DAG-based workflow coordinator
pkg/trader/paper/        Paper trading simulation engine
pkg/trader/policy/       Risk management and position limits
pkg/trader/metrics/      Prometheus metrics collection
pkg/trader/streaming/    WebSocket streaming hub
pkg/wss/                 Generic WebSocket client with auto-reconnect
```

## Trading Agent (`cmd/agentd`)

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-paper` | `true` | Run in paper trading mode |
| `-http` | `:8080` | HTTP server address |
| `-key` | `""` | Private key for live trading (or `POLYMARKET_PRIVATE_KEY` env) |
| `-min-edge` | `100` | Minimum edge in basis points |
| `-max-markets` | `20` | Maximum markets to track |
| `-balance` | `10000` | Initial paper trading balance |
| `-verbose` | `false` | Verbose logging |
| `-llm-preset` | `balanced` | LLM preset: `elite`, `balanced`, `cheap`, `local`, `fast` |
| `-no-llm` | `false` | Disable LLM forecasting |

### HTTP Endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /health` | Health check |
| `GET /status` | Orchestrator status |
| `GET /markets` | Active markets list |
| `GET /signals` | Current trading signals |
| `GET /account` | Paper trading account info |
| `GET /stats` | Trading statistics |
| `GET /policy` | Policy engine status |
| `GET /metrics` | Prometheus metrics |
| `GET /ws` | WebSocket streaming |

### LLM Presets

| Preset | Models Used | Cost |
|--------|-------------|------|
| `local` | Ollama only | Free |
| `cheap` | DeepSeek V3 | ~$0.001/1k tokens |
| `fast` | Cerebras Llama 3.3 70B | ~$0.002/1k tokens |
| `balanced` | DeepSeek V3 + ensemble | ~$0.002/1k tokens |
| `elite` | Claude Sonnet 4.5 + ensemble | ~$0.015/1k tokens |

### Example Invocations

```bash
# Paper trading with local Ollama models (free, offline)
go run ./cmd/agentd --paper --llm-preset=local

# Paper trading with cloud LLM ensemble
go run ./cmd/agentd --paper --llm-preset=balanced --balance=50000

# Live trading (requires funded Polygon wallet)
go run ./cmd/agentd --paper=false --key=0xYOUR_PRIVATE_KEY --llm-preset=elite

# No LLM mode (signals won't be generated, useful for monitoring)
go run ./cmd/agentd --paper --no-llm
```

## Backtester (`cmd/backtest`)

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-data` | `""` | Path to historical data file (JSON or CSV) |
| `-strategy` | `momentum` | Strategy name |
| `-output` | `""` | Output file for results |
| `-balance` | `10000` | Initial balance |
| `-maker-fee` | `0.0` | Maker fee (bps) |
| `-taker-fee` | `0.5` | Taker fee (bps) |
| `-verbose` | `false` | Verbose output |
| `-ma-period` | `10` | Moving average period |
| `-threshold-pct` | `2.0` | % above/below MA to trigger (momentum) |
| `-entry-threshold` | `5.0` | % below MA to buy (mean reversion) |
| `-exit-threshold` | `3.0` | % above entry to sell (mean reversion) |
| `-position-size` | `100` | Position size in dollars |

### Built-in Strategies

| Name | Aliases | Description |
|------|---------|-------------|
| `momentum` | `ma` | Buys above MA, sells below |
| `meanreversion` | `revert` | Buys dips below MA, sells on recovery |
| `buyhold` | `hold` | Buy and hold |
| `forecaster` | `llm` | Simulated LLM forecaster |
| `edge` | — | EMA-based edge strategy |

### Example Invocations

```bash
# Demo with synthetic data (no file needed)
go run ./cmd/backtest

# Momentum strategy on real data
go run ./cmd/backtest --data prices.json --strategy=momentum --ma-period=20

# Mean reversion with custom thresholds
go run ./cmd/backtest --data prices.csv --strategy=meanreversion \
  --entry-threshold=8.0 --exit-threshold=4.0

# Export results
go run ./cmd/backtest --data prices.json --strategy=edge --output=results.json
```

## LLM Model Router

The model router (`tools/llm_router.go`) organizes 30+ models into 9 tiers:

| Tier | Latency | Cost | Example Models |
|------|---------|------|----------------|
| `local` | 1-8s | Free | Ollama Qwen3 8B, DeepSeek R1 14B, Gemma2 27B |
| `free` | 6-8s | Free | Qwen3 Coder Free, Grok 4.1 Fast Free |
| `superfast` | 0.1-0.4s | $0.001-0.002/1k | Cerebras Llama 3.3 70B, Qwen 3 32B |
| `fast` | 1-2s | $0.002-0.011/1k | GPT-5.1, Cerebras GLM 4.6, o3-mini-high |
| `balanced` | 2-6s | $0.001-0.008/1k | DeepSeek V3, o4-mini, Kimi K2, Qwen3 Max |
| `reasoning` | 3-12s | $0.001-0.100/1k | DeepSeek R1, o3-pro, Cogito V2.1 |
| `coding` | 2-6s | $0.000-0.011/1k | DeepSeek Coder, GPT-5.1 Codex |
| `elite` | 8-13s | $0.015-0.105/1k | Claude Sonnet 4.5, Claude Opus 4.5, GPT-4o |
| `vision` | 3-13s | $0.000-0.024/1k | Qwen3-VL 2B (local), Gemini 2.0 Flash (free) |

### Providers and Env Vars

| Provider | Env Var | Notes |
|----------|---------|-------|
| Ollama | `OLLAMA_URL` | Default `http://localhost:11434`, free/offline |
| DeepSeek | `DEEPSEEK_API_KEY` | Best value for cloud |
| Cerebras | `CEREBRAS_API_KEY` | Fastest inference (2000+ tok/s) |
| OpenRouter | `OPENROUTER_API_KEY` | Gateway to 200+ models |
| Anthropic | `ANTHROPIC_API_KEY` | Direct Claude access |
| Kimi / Moonshot | `KIMI_API_KEY` | Chinese/English models |

### Local-Only Setup

```bash
# Install Ollama
brew install ollama   # or visit https://ollama.ai

# Pull a model
ollama pull qwen3:8b

# Run agent with local models only (no API keys needed)
go run ./cmd/agentd --paper --llm-preset=local
```

## Configuration

### Key Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `POLYMARKET_PRIVATE_KEY` | For live trading | Polygon wallet private key |
| `OLLAMA_URL` | No (defaults to localhost:11434) | Ollama server URL |
| `OLLAMA_MODEL` | No (defaults to qwen3:8b) | Default Ollama model |
| `DEEPSEEK_API_KEY` | For cloud LLM | DeepSeek API key |
| `CEREBRAS_API_KEY` | For cloud LLM | Cerebras API key |
| `OPENROUTER_API_KEY` | For cloud LLM | OpenRouter API key |
| `ANTHROPIC_API_KEY` | For cloud LLM | Anthropic API key |
| `KIMI_API_KEY` | For cloud LLM | Kimi/Moonshot API key |

### Default Risk Limits

| Limit | Default | Paper Mode |
|-------|---------|------------|
| Max position size | $1,000 | $100 |
| Max total exposure | $5,000 | $500 |
| Max concentration | 30% | 20% |
| Max open orders | 10 | 5 |
| Max daily loss | $500 | $50 |
| Max daily volume | $10,000 | $1,000 |
| Max order size | $500 | $50 |
| Max slippage | 2% | 1% |
| Cooldown after loss | 15 min | 30 min |

### Default Workflow Timing

| Parameter | Default |
|-----------|---------|
| Discovery interval | 5 minutes |
| Forecast interval | 1 minute |
| Monitor interval | 10 seconds |
| Min market volume | $10,000 |
| Max spread | 500 bps (5%) |

## API Clients

### CLOB Client (Trading)

```go
import "github.com/phenomenon0/polymarket-agents/pkg/polymarket/clob"

// Read-only client (no private key needed)
client, _ := clob.NewClient("0x0000000000000000000000000000000000000000000000000000000000000001")

// Fetch order book
ctx := context.Background()
ob, _ := client.GetOrderBook(ctx, "TOKEN_ID_HERE")
fmt.Println(ob.Bids, ob.Asks)

// Get mid price
mid, _ := client.GetMidpoint(ctx, "TOKEN_ID_HERE")
fmt.Println("Mid:", mid)
```

### Gamma Client (Market Data)

```go
import "github.com/phenomenon0/polymarket-agents/pkg/polymarket/gamma"

client := gamma.NewClient()

// List all active tradeable events
events, _ := client.ListAllTradeableEvents(ctx)
for _, e := range events {
    fmt.Printf("%s — Volume: $%.0f\n", e.Title, e.Volume.Float64())
}

// Get a specific market
market, _ := client.GetMarket(ctx, "CONDITION_ID")
fmt.Printf("YES: %.2f, NO: %.2f\n", market.YesPrice(), market.NoPrice())
```

### WebSocket Client

The `pkg/wss` package provides a generic WebSocket client with auto-reconnect, heartbeat, and subscription management. The CLOB client uses it for real-time order book updates (`pkg/polymarket/clob/wss.go`).

## Sports Analytics

The `pkg/polymarket/sports` and `pkg/polymarket/sportsbridge` packages provide:

- **Market parsing** — Discover and parse Polymarket soccer markets into structured events
- **MathShard integration** — Fetch model probabilities from external sports analytics
- **Edge calculation** — Compare model probabilities to market prices using VWAP
- **Kelly sizing** — Optimal position sizing with capped Kelly criterion
- **Signal generation** — Produce actionable trading signals with risk metadata

Supported leagues: EPL, La Liga, Bundesliga, Ligue 1 (Serie A disabled by default).

## Testing

```bash
# Run all unit tests
go test ./...

# Verbose
go test -v ./...

# Specific package
go test ./pkg/polymarket/clob/
```

Integration tests require Ollama running locally or cloud API keys set in the environment.

## License

Unlicensed — private repository.
