// polymarket-agentd is the Polymarket trading agent daemon.
// It runs a continuous trading workflow with LLM-based forecasting.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/phenomenon0/polymarket-agents/pkg/polymarket/book"
	"github.com/phenomenon0/polymarket-agents/pkg/polymarket/clob"
	"github.com/phenomenon0/polymarket-agents/pkg/polymarket/gamma"
	"github.com/phenomenon0/polymarket-agents/pkg/trader/agents"
	"github.com/phenomenon0/polymarket-agents/pkg/trader/metrics"
	"github.com/phenomenon0/polymarket-agents/pkg/trader/orchestrator"
	"github.com/phenomenon0/polymarket-agents/pkg/trader/paper"
	"github.com/phenomenon0/polymarket-agents/pkg/trader/policy"
	"github.com/phenomenon0/polymarket-agents/pkg/trader/streaming"
	"github.com/phenomenon0/polymarket-agents/tools"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/shopspring/decimal"
)

var (
	// Flags
	paperMode  = flag.Bool("paper", true, "Run in paper trading mode")
	httpAddr   = flag.String("http", ":8080", "HTTP server address for status API")
	privateKey = flag.String("key", "", "Private key for live trading (or POLYMARKET_PRIVATE_KEY env)")
	minEdgeBps = flag.Int("min-edge", 100, "Minimum edge in basis points")
	maxMarkets = flag.Int("max-markets", 20, "Maximum markets to track")
	initialBal = flag.Float64("balance", 10000, "Initial paper trading balance")
	verbose    = flag.Bool("verbose", false, "Verbose logging")
	llmPreset  = flag.String("llm-preset", "balanced", "LLM preset: elite, balanced, cheap, local, fast")
	noLLM      = flag.Bool("no-llm", false, "Disable LLM forecasting (signals will not be generated)")
)

func main() {
	flag.Parse()

	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	log.Println("Starting Polymarket Trading Agent")

	// Context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Initialize components
	agent, err := newAgent()
	if err != nil {
		log.Fatalf("Failed to initialize agent: %v", err)
	}

	// Set up callbacks
	agent.orch.OnStageComplete(func(result *orchestrator.StageResult) {
		if *verbose || !result.Success {
			log.Printf("[%s] %s (%.2fms)", result.Stage, statusStr(result.Success), float64(result.Duration.Microseconds())/1000)
			if result.Error != "" {
				log.Printf("  Error: %s", result.Error)
			}
		}
	})

	agent.orch.OnSignal(func(signal *agents.TradingSignal) {
		log.Printf("[SIGNAL] %s %s @ %.2f%% (edge: %.0f bps, strength: %.2f)",
			signal.Signal, signal.Side,
			signal.CurrentPrice.Mul(decimal.NewFromInt(100)).InexactFloat64(),
			signal.EdgeBps.InexactFloat64(),
			signal.Strength.InexactFloat64())
		log.Printf("  %s", signal.Reasoning)

		// Broadcast to WebSocket clients
		agent.streamHub.BroadcastSignal(signal)
	})

	agent.orch.OnError(func(err error) {
		log.Printf("[ERROR] %v", err)

		// Broadcast to WebSocket clients
		agent.streamHub.BroadcastError(err, "orchestrator")
	})

	// Start HTTP server
	go agent.startHTTP()

	// Start orchestrator
	if err := agent.orch.Start(ctx); err != nil {
		log.Fatalf("Failed to start orchestrator: %v", err)
	}

	log.Printf("Agent running (paper=%v, http=%s)", *paperMode, *httpAddr)
	log.Printf("WebSocket streaming available at ws://%s/ws", *httpAddr)
	log.Println("Press Ctrl+C to stop")

	// Wait for signal
	<-sigCh
	log.Println("Shutting down...")

	// Graceful shutdown
	agent.orch.Stop()
	cancel()

	// Print final stats
	if agent.paperEngine != nil {
		stats := agent.paperEngine.GetStats()
		log.Printf("Final Stats: PnL=$%.2f, Trades=%d, WinRate=%.1f%%",
			stats.TotalPnL.InexactFloat64(),
			stats.TotalTrades,
			stats.WinRate.Mul(decimal.NewFromInt(100)).InexactFloat64())
	}

	log.Println("Goodbye!")
}

type tradingAgent struct {
	gammaClient  *gamma.Client
	clobClient   *clob.Client
	forecaster   *agents.Forecaster
	policyEngine *policy.PolicyEngine
	paperEngine  *paper.Engine
	orch         *orchestrator.Orchestrator
	metrics      *metrics.TradingMetrics
	streamHub    *streaming.Hub
}

func newAgent() (*tradingAgent, error) {
	agent := &tradingAgent{
		metrics:   metrics.NewTradingMetrics(),
		streamHub: streaming.NewHub(),
	}

	// Start streaming hub
	go agent.streamHub.Run()

	// Initialize Gamma client (always needed)
	agent.gammaClient = gamma.NewClient()

	// Initialize CLOB client
	key := *privateKey
	if key == "" {
		key = os.Getenv("POLYMARKET_PRIVATE_KEY")
	}

	if key != "" {
		var err error
		agent.clobClient, err = clob.NewClient(key)
		if err != nil {
			return nil, fmt.Errorf("failed to create CLOB client: %w", err)
		}
		log.Printf("CLOB client initialized (address: %s)", agent.clobClient.Address())
	} else {
		log.Println("No private key provided - CLOB client in read-only mode")
		// Create a dummy client for read-only operations
		dummyKey := "0x0000000000000000000000000000000000000000000000000000000000000001"
		agent.clobClient, _ = clob.NewClient(dummyKey)
	}

	// Initialize policy engine
	limits := policy.DefaultRiskLimits()
	if *paperMode {
		limits = policy.TightRiskLimits() // Tighter limits for paper trading
	}
	agent.policyEngine = policy.NewPolicyEngine(limits)

	// Initialize paper trading engine
	if *paperMode {
		paperConfig := paper.DefaultSimulationConfig()
		paperConfig.InitialBalance = decimal.NewFromFloat(*initialBal)

		// Create a price provider that uses the CLOB client
		provider := &clobPriceProvider{client: agent.clobClient}
		agent.paperEngine = paper.NewEngine(paperConfig, provider)

		agent.paperEngine.OnTrade(func(trade *paper.Trade) {
			log.Printf("[TRADE] %s %s @ %s (size: %s)",
				trade.Side, trade.TokenID, trade.Price, trade.Size)

			// Broadcast to WebSocket clients
			agent.streamHub.BroadcastTrade(trade)
		})
	}

	// Initialize forecaster
	if *noLLM {
		agent.forecaster = agents.NewForecaster(nil)
		log.Println("Note: Forecaster initialized without LLM clients - signals will not be generated")
	} else {
		// Create model router and forecaster
		router := tools.NewModelRouter()
		preset := parsePreset(*llmPreset)

		forecaster, err := agents.CreateForecasterWithPreset(router, preset)
		if err != nil {
			log.Printf("Warning: Failed to create LLM forecaster: %v", err)
			log.Println("Falling back to no-LLM mode")
			agent.forecaster = agents.NewForecaster(nil)
		} else {
			agent.forecaster = forecaster
			log.Printf("Forecaster initialized with preset: %s", strings.ToUpper(*llmPreset))
		}
	}

	// Initialize orchestrator
	orchConfig := orchestrator.DefaultWorkflowConfig()
	orchConfig.MinEdgeBps = *minEdgeBps
	orchConfig.MaxMarkets = *maxMarkets
	orchConfig.UsePaperTrade = *paperMode
	orchConfig.MaxOrderSize = decimal.NewFromInt(100)

	agent.orch = orchestrator.NewOrchestrator(
		orchConfig,
		agent.gammaClient,
		agent.clobClient,
		agent.forecaster,
		agent.policyEngine,
		agent.paperEngine,
	)

	return agent, nil
}

func (a *tradingAgent) startHTTP() {
	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Status endpoint
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		status := a.orch.GetStatus()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	})

	// Markets endpoint
	mux.HandleFunc("/markets", func(w http.ResponseWriter, r *http.Request) {
		markets := a.orch.GetActiveMarkets()
		w.Header().Set("Content-Type", "application/json")

		summaries := make([]map[string]interface{}, len(markets))
		for i, m := range markets {
			summaries[i] = map[string]interface{}{
				"id":        m.ID,
				"question":  m.Question,
				"yes_price": m.YesPrice(),
				"volume":    m.Volume.Float64(),
			}
		}
		json.NewEncoder(w).Encode(summaries)
	})

	// Signals endpoint
	mux.HandleFunc("/signals", func(w http.ResponseWriter, r *http.Request) {
		signals := a.orch.GetSignals()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(signals)
	})

	// Account endpoint (paper trading)
	mux.HandleFunc("/account", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if a.paperEngine != nil {
			json.NewEncoder(w).Encode(a.paperEngine.GetAccount())
		} else {
			json.NewEncoder(w).Encode(map[string]string{"error": "not in paper mode"})
		}
	})

	// Stats endpoint
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if a.paperEngine != nil {
			json.NewEncoder(w).Encode(a.paperEngine.GetStats())
		} else {
			json.NewEncoder(w).Encode(map[string]string{"error": "not in paper mode"})
		}
	})

	// Policy endpoint
	mux.HandleFunc("/policy", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(a.policyEngine.Status())
	})

	// Prometheus metrics endpoint
	mux.Handle("/metrics", promhttp.HandlerFor(a.metrics.Registry(), promhttp.HandlerOpts{}))

	// WebSocket streaming endpoint
	mux.HandleFunc("/ws", a.streamHub.ServeWS)

	server := &http.Server{
		Addr:         *httpAddr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Printf("HTTP server listening on %s", *httpAddr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Printf("HTTP server error: %v", err)
	}
}

func statusStr(success bool) string {
	if success {
		return "OK"
	}
	return "FAILED"
}

func parsePreset(s string) agents.ForecasterPreset {
	switch strings.ToLower(s) {
	case "elite":
		return agents.PresetElite
	case "balanced":
		return agents.PresetBalanced
	case "cheap":
		return agents.PresetCheap
	case "local":
		return agents.PresetLocal
	case "fast":
		return agents.PresetFast
	default:
		return agents.PresetBalanced
	}
}

// clobPriceProvider implements paper.PriceProvider using the CLOB client.
type clobPriceProvider struct {
	client *clob.Client
}

func (p *clobPriceProvider) GetMidPrice(ctx context.Context, tokenID string) (decimal.Decimal, error) {
	mid, err := p.client.GetMidpoint(ctx, tokenID)
	if err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromString(mid)
}

func (p *clobPriceProvider) GetOrderBook(ctx context.Context, tokenID string) (*book.OrderBook, error) {
	summary, err := p.client.GetOrderBook(ctx, tokenID)
	if err != nil {
		return nil, err
	}

	ob := book.NewOrderBook(tokenID, summary.Market)

	bids := make([]book.PriceLevel, len(summary.Bids))
	for i, b := range summary.Bids {
		price, _ := decimal.NewFromString(b.Price)
		size, _ := decimal.NewFromString(b.Size)
		bids[i] = book.PriceLevel{Price: price, Size: size}
	}
	ob.SetBids(bids)

	asks := make([]book.PriceLevel, len(summary.Asks))
	for i, a := range summary.Asks {
		price, _ := decimal.NewFromString(a.Price)
		size, _ := decimal.NewFromString(a.Size)
		asks[i] = book.PriceLevel{Price: price, Size: size}
	}
	ob.SetAsks(asks)

	return ob, nil
}
