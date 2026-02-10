// polymarket-backtest is a CLI tool for running historical backtests on trading strategies.
package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/phenomenon0/polymarket-agents/pkg/trader/backtest"

	"github.com/shopspring/decimal"
)

var (
	// Input flags
	dataFile   = flag.String("data", "", "Path to historical data file (JSON or CSV)")
	strategy   = flag.String("strategy", "momentum", "Strategy: momentum, meanreversion, buyhold")
	outputFile = flag.String("output", "", "Output file for results (JSON or CSV)")

	// Config flags
	balance  = flag.Float64("balance", 10000, "Initial balance")
	makerFee = flag.Float64("maker-fee", 0.0, "Maker fee in basis points")
	takerFee = flag.Float64("taker-fee", 0.5, "Taker fee in basis points")
	verbose  = flag.Bool("verbose", false, "Verbose output")

	// Strategy-specific flags
	maPeriod       = flag.Int("ma-period", 10, "Moving average period")
	thresholdPct   = flag.Float64("threshold-pct", 2.0, "% above/below MA to trigger (momentum)")
	entryThreshold = flag.Float64("entry-threshold", 5.0, "% below MA to buy (meanreversion)")
	exitThreshold  = flag.Float64("exit-threshold", 3.0, "% above entry to sell (meanreversion)")
	positionSize   = flag.Float64("position-size", 100, "Position size in dollars")
)

func main() {
	flag.Parse()

	if *dataFile == "" {
		// If no data file, generate synthetic data for demo
		log.Println("No data file provided, running demo with synthetic data")
		runDemo()
		return
	}

	// Create backtest
	config := &backtest.Config{
		InitialBalance: decimal.NewFromFloat(*balance),
		MakerFeeBps:    decimal.NewFromFloat(*makerFee),
		TakerFeeBps:    decimal.NewFromFloat(*takerFee),
	}
	bt := backtest.New(config)

	// Load data
	if strings.HasSuffix(*dataFile, ".json") {
		if err := bt.LoadDataFromJSON(*dataFile); err != nil {
			log.Fatalf("Failed to load JSON data: %v", err)
		}
	} else if strings.HasSuffix(*dataFile, ".csv") {
		if err := bt.LoadDataFromCSV(*dataFile); err != nil {
			log.Fatalf("Failed to load CSV data: %v", err)
		}
	} else {
		log.Fatalf("Unknown data file format: %s (expected .json or .csv)", *dataFile)
	}

	// Create strategy
	strat := createStrategy()

	log.Printf("Running backtest with strategy: %s", *strategy)
	log.Printf("Initial balance: $%.2f", *balance)

	// Run backtest
	ctx := context.Background()
	result, err := bt.Run(ctx, strat)
	if err != nil {
		log.Fatalf("Backtest failed: %v", err)
	}

	// Print results
	printResults(result)

	// Export results
	if *outputFile != "" {
		if err := exportResults(result, *outputFile); err != nil {
			log.Printf("Failed to export results: %v", err)
		} else {
			log.Printf("Results exported to: %s", *outputFile)
		}
	}
}

func createStrategy() backtest.Strategy {
	switch strings.ToLower(*strategy) {
	case "momentum", "ma":
		return backtest.NewMomentumStrategy(*maPeriod, *positionSize, *thresholdPct)
	case "meanreversion", "revert":
		return backtest.NewMeanReversionStrategy(*maPeriod, *positionSize, *entryThreshold, *exitThreshold)
	case "buyhold", "hold":
		return backtest.NewBuyAndHoldStrategy(*positionSize)
	case "forecaster", "llm":
		// LLM-based forecaster strategy (simulated for backtest)
		config := &backtest.ForecasterStrategyConfig{
			Forecaster:      nil, // nil = use simulated forecaster
			PositionSize:    *positionSize,
			MinEdgeBps:      500, // 5% minimum edge
			MinConfidence:   0.6,
			ForecastEveryN:  5, // Forecast every 5 ticks
			MaxPositionSize: *positionSize * 10,
			Verbose:         *verbose,
		}
		return backtest.NewForecasterStrategy(config)
	case "edge":
		// Edge-based strategy using EMA
		return backtest.NewEdgeStrategy(*positionSize, 300, 100, *maPeriod, true)
	default:
		log.Printf("Unknown strategy %s, defaulting to momentum", *strategy)
		return backtest.NewMomentumStrategy(*maPeriod, *positionSize, *thresholdPct)
	}
}

func printResults(result *backtest.Result) {
	fmt.Println()
	fmt.Println("==================== BACKTEST RESULTS ====================")
	fmt.Println()
	fmt.Printf("  Period:          %s to %s\n",
		result.StartTime.Format("2006-01-02"),
		result.EndTime.Format("2006-01-02"))
	fmt.Printf("  Duration:        %s\n", result.Duration.Round(time.Hour))
	fmt.Println()
	fmt.Printf("  Initial Balance: $%.2f\n", result.InitialBalance.InexactFloat64())
	fmt.Printf("  Final Balance:   $%.2f\n", result.FinalBalance.InexactFloat64())
	fmt.Printf("  Total PnL:       $%.2f\n", result.TotalPnL.InexactFloat64())
	fmt.Printf("  Total Return:    %.2f%%\n", result.TotalReturn.InexactFloat64())
	fmt.Println()
	fmt.Printf("  Total Trades:    %d\n", result.TotalTrades)
	fmt.Printf("  Winning Trades:  %d\n", result.WinningTrades)
	fmt.Printf("  Losing Trades:   %d\n", result.LosingTrades)
	fmt.Printf("  Win Rate:        %.1f%%\n", result.WinRate.Mul(decimal.NewFromInt(100)).InexactFloat64())
	fmt.Println()
	fmt.Printf("  Max Drawdown:    %.2f%%\n", result.MaxDrawdown.Mul(decimal.NewFromInt(100)).InexactFloat64())
	fmt.Printf("  Sharpe Ratio:    %.2f\n", result.SharpeRatio.InexactFloat64())
	fmt.Printf("  Total Volume:    $%.2f\n", result.TotalVolume.InexactFloat64())
	fmt.Printf("  Total Fees:      $%.2f\n", result.TotalFees.InexactFloat64())
	fmt.Println()
	fmt.Println("===========================================================")

	if *verbose && len(result.Trades) > 0 {
		fmt.Println()
		fmt.Println("Trade History:")
		fmt.Println("--------------")
		for i, trade := range result.Trades {
			fmt.Printf("  %d. %s %s %s @ %s (PnL: $%.2f)\n",
				i+1,
				trade.Timestamp.Format("2006-01-02 15:04"),
				trade.Side,
				trade.Size.String(),
				trade.Price.String(),
				trade.PnL.InexactFloat64())
		}
	}
}

func exportResults(result *backtest.Result, filename string) error {
	if strings.HasSuffix(filename, ".json") {
		return exportJSON(result, filename)
	} else if strings.HasSuffix(filename, ".csv") {
		return exportCSV(result, filename)
	}
	// Default to JSON
	return exportJSON(result, filename+".json")
}

func exportJSON(result *backtest.Result, filename string) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal result: %w", err)
	}
	return os.WriteFile(filename, data, 0644)
}

func exportCSV(result *backtest.Result, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	w := csv.NewWriter(file)
	defer w.Flush()

	// Write summary
	w.Write([]string{"Metric", "Value"})
	w.Write([]string{"start_time", result.StartTime.Format(time.RFC3339)})
	w.Write([]string{"end_time", result.EndTime.Format(time.RFC3339)})
	w.Write([]string{"initial_balance", result.InitialBalance.String()})
	w.Write([]string{"final_balance", result.FinalBalance.String()})
	w.Write([]string{"total_pnl", result.TotalPnL.String()})
	w.Write([]string{"total_return_pct", result.TotalReturn.String()})
	w.Write([]string{"total_trades", fmt.Sprintf("%d", result.TotalTrades)})
	w.Write([]string{"winning_trades", fmt.Sprintf("%d", result.WinningTrades)})
	w.Write([]string{"losing_trades", fmt.Sprintf("%d", result.LosingTrades)})
	w.Write([]string{"win_rate", result.WinRate.String()})
	w.Write([]string{"max_drawdown", result.MaxDrawdown.String()})
	w.Write([]string{"sharpe_ratio", result.SharpeRatio.String()})

	// Write blank line
	w.Write([]string{})

	// Write trades
	if len(result.Trades) > 0 {
		w.Write([]string{"timestamp", "token_id", "side", "price", "size", "fee", "pnl"})
		for _, trade := range result.Trades {
			w.Write([]string{
				trade.Timestamp.Format(time.RFC3339),
				trade.TokenID,
				trade.Side,
				trade.Price.String(),
				trade.Size.String(),
				trade.Fee.String(),
				trade.PnL.String(),
			})
		}
	}

	return nil
}

// runDemo runs a demo backtest with synthetic data
func runDemo() {
	fmt.Println()
	fmt.Println("POLYMARKET BACKTEST DEMO")
	fmt.Println("========================")
	fmt.Println()

	// Create synthetic price data simulating a prediction market
	tokenID := "demo-token-yes"
	market := "Will X happen by 2025?"
	startTime := time.Now().Add(-30 * 24 * time.Hour) // 30 days ago

	points := make([]backtest.PricePoint, 0)

	// Simulate price movement: starts at 0.5, trends up to 0.75
	price := 0.5
	for i := 0; i < 720; i++ { // 720 hours = 30 days
		ts := startTime.Add(time.Duration(i) * time.Hour)

		// Add trend and noise
		trend := 0.25 * float64(i) / 720.0     // +25% over period
		noise := (float64(i%17) - 8.5) / 100.0 // +/- 8.5%

		price = 0.5 + trend + noise
		if price < 0.01 {
			price = 0.01
		}
		if price > 0.99 {
			price = 0.99
		}

		points = append(points, backtest.PricePoint{
			Timestamp: ts,
			TokenID:   tokenID,
			Market:    market,
			Price:     decimal.NewFromFloat(price),
			Volume:    decimal.NewFromFloat(10000),
		})
	}

	data := &backtest.HistoricalData{
		TokenID:   tokenID,
		Market:    market,
		StartTime: points[0].Timestamp,
		EndTime:   points[len(points)-1].Timestamp,
		Points:    points,
	}

	// Run each strategy
	strategies := []struct {
		name  string
		strat backtest.Strategy
	}{
		{"Momentum (MA=10)", backtest.NewMomentumStrategy(10, 100.0, 2.0)},
		{"Mean Reversion", backtest.NewMeanReversionStrategy(10, 100.0, 5.0, 3.0)},
		{"Buy & Hold", backtest.NewBuyAndHoldStrategy(500.0)},
		{"LLM Forecaster", backtest.NewForecasterStrategy(&backtest.ForecasterStrategyConfig{
			PositionSize:    100.0,
			MinEdgeBps:      500,
			MinConfidence:   0.6,
			ForecastEveryN:  5,
			MaxPositionSize: 1000,
		})},
		{"Edge (EMA)", backtest.NewEdgeStrategy(100.0, 300, 100, 10, true)},
	}

	fmt.Println("Running strategies on synthetic data (30 days, price 0.50 -> 0.75)")
	fmt.Println()

	for _, s := range strategies {
		bt := backtest.New(backtest.DefaultConfig())
		bt.LoadData(data)

		result, err := bt.Run(context.Background(), s.strat)
		if err != nil {
			log.Printf("Strategy %s failed: %v", s.name, err)
			continue
		}

		fmt.Printf("%-20s | PnL: $%8.2f | Return: %6.2f%% | Trades: %3d | MaxDD: %5.2f%%\n",
			s.name,
			result.TotalPnL.InexactFloat64(),
			result.TotalReturn.InexactFloat64(),
			result.TotalTrades,
			result.MaxDrawdown.Mul(decimal.NewFromInt(100)).InexactFloat64())
	}

	fmt.Println()
	fmt.Println("To run with real data, use:")
	fmt.Println("  polymarket-backtest -data prices.json -strategy momentum")
	fmt.Println()
}
