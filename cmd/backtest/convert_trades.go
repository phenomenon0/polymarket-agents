//go:build ignore
// +build ignore

// This script converts trade data from the polymarket-scraper to backtest format.
// Usage: go run convert_trades.go -input trades.csv -output prices.json -interval 1h
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strconv"
	"time"
)

type Trade struct {
	Timestamp time.Time `json:"timestamp_dt"`
	Price     float64   `json:"price"`
	TokenID   string    `json:"takerAssetId"`
	Volume    float64   `json:"usdc_amount"`
}

type PricePoint struct {
	Timestamp string `json:"timestamp"`
	TokenID   string `json:"token_id"`
	Market    string `json:"market"`
	Price     string `json:"price"`
	Volume    string `json:"volume"`
}

type HistoricalData struct {
	TokenID   string       `json:"token_id"`
	Market    string       `json:"market"`
	StartTime string       `json:"start_time"`
	EndTime   string       `json:"end_time"`
	Points    []PricePoint `json:"points"`
}

var (
	inputFile   = flag.String("input", "", "Input CSV file with trades")
	outputFile  = flag.String("output", "prices.json", "Output JSON file")
	intervalStr = flag.String("interval", "1h", "Aggregation interval (1m, 5m, 15m, 1h, 4h, 1d)")
	tokenFilter = flag.String("token", "", "Filter by specific token ID")
	marketName  = flag.String("market", "Polymarket", "Market name for output")
)

func main() {
	flag.Parse()

	if *inputFile == "" {
		log.Fatal("Please provide -input file")
	}

	interval := parseInterval(*intervalStr)
	log.Printf("Aggregating trades with interval: %s", interval)

	// Read trades
	file, err := os.Open(*inputFile)
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)

	// Read header
	header, err := reader.Read()
	if err != nil {
		log.Fatalf("Failed to read header: %v", err)
	}

	// Build column index
	colIndex := make(map[string]int)
	for i, col := range header {
		colIndex[col] = i
	}

	// Read trades
	trades := make(map[string][]Trade) // tokenID -> trades
	lineCount := 0

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		lineCount++

		var trade Trade

		// Parse timestamp
		if idx, ok := colIndex["timestamp_dt"]; ok && idx < len(record) {
			t, err := time.Parse("2006-01-02T15:04:05", record[idx])
			if err != nil {
				t, _ = time.Parse(time.RFC3339, record[idx])
			}
			trade.Timestamp = t
		}

		// Parse price
		if idx, ok := colIndex["price"]; ok && idx < len(record) {
			trade.Price, _ = strconv.ParseFloat(record[idx], 64)
		}

		// Parse token ID (use takerAssetId)
		if idx, ok := colIndex["takerAssetId"]; ok && idx < len(record) {
			trade.TokenID = record[idx]
		}

		// Parse volume
		if idx, ok := colIndex["usdc_amount"]; ok && idx < len(record) {
			trade.Volume, _ = strconv.ParseFloat(record[idx], 64)
		}

		// Filter by token if specified
		if *tokenFilter != "" && trade.TokenID != *tokenFilter {
			continue
		}

		if trade.TokenID == "" || trade.Timestamp.IsZero() {
			continue
		}

		trades[trade.TokenID] = append(trades[trade.TokenID], trade)
	}

	log.Printf("Read %d trades for %d tokens", lineCount, len(trades))

	// Aggregate trades into price points
	allData := make([]*HistoricalData, 0)

	for tokenID, tokenTrades := range trades {
		if len(tokenTrades) < 10 {
			continue // Skip tokens with too few trades
		}

		// Sort by timestamp
		sort.Slice(tokenTrades, func(i, j int) bool {
			return tokenTrades[i].Timestamp.Before(tokenTrades[j].Timestamp)
		})

		// Aggregate into intervals
		points := aggregateTrades(tokenTrades, interval)
		if len(points) < 5 {
			continue // Skip if too few data points
		}

		data := &HistoricalData{
			TokenID:   tokenID,
			Market:    *marketName,
			StartTime: points[0].Timestamp,
			EndTime:   points[len(points)-1].Timestamp,
			Points:    points,
		}
		allData = append(allData, data)

		shortID := tokenID
		if len(shortID) > 16 {
			shortID = shortID[:16] + "..."
		}
		log.Printf("  Token %s: %d trades -> %d price points",
			shortID, len(tokenTrades), len(points))
	}

	// Write output
	if len(allData) == 0 {
		log.Fatal("No data to write")
	}

	// If single token, write just that data; otherwise write array
	var output interface{}
	if len(allData) == 1 {
		output = allData[0]
	} else {
		output = allData
	}

	outFile, err := os.Create(*outputFile)
	if err != nil {
		log.Fatalf("Failed to create output file: %v", err)
	}
	defer outFile.Close()

	encoder := json.NewEncoder(outFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		log.Fatalf("Failed to write JSON: %v", err)
	}

	log.Printf("Wrote %d token datasets to %s", len(allData), *outputFile)
}

func parseInterval(s string) time.Duration {
	switch s {
	case "1m":
		return time.Minute
	case "5m":
		return 5 * time.Minute
	case "15m":
		return 15 * time.Minute
	case "1h":
		return time.Hour
	case "4h":
		return 4 * time.Hour
	case "1d":
		return 24 * time.Hour
	default:
		return time.Hour
	}
}

func aggregateTrades(trades []Trade, interval time.Duration) []PricePoint {
	if len(trades) == 0 {
		return nil
	}

	points := make([]PricePoint, 0)

	// Group trades by interval
	currentStart := trades[0].Timestamp.Truncate(interval)
	currentTrades := make([]Trade, 0)

	for _, trade := range trades {
		tradeInterval := trade.Timestamp.Truncate(interval)

		if tradeInterval != currentStart {
			// New interval - aggregate previous
			if len(currentTrades) > 0 {
				point := aggregateInterval(currentTrades, currentStart)
				points = append(points, point)
			}
			currentStart = tradeInterval
			currentTrades = []Trade{trade}
		} else {
			currentTrades = append(currentTrades, trade)
		}
	}

	// Final interval
	if len(currentTrades) > 0 {
		point := aggregateInterval(currentTrades, currentStart)
		points = append(points, point)
	}

	return points
}

func aggregateInterval(trades []Trade, start time.Time) PricePoint {
	// Use VWAP (volume-weighted average price)
	var totalVolume, volumePrice float64
	for _, t := range trades {
		totalVolume += t.Volume
		volumePrice += t.Price * t.Volume
	}

	var avgPrice float64
	if totalVolume > 0 {
		avgPrice = volumePrice / totalVolume
	} else {
		// Simple average if no volume
		for _, t := range trades {
			avgPrice += t.Price
		}
		avgPrice /= float64(len(trades))
	}

	return PricePoint{
		Timestamp: start.Format(time.RFC3339),
		TokenID:   trades[0].TokenID,
		Market:    "Polymarket",
		Price:     fmt.Sprintf("%.6f", avgPrice),
		Volume:    fmt.Sprintf("%.2f", totalVolume),
	}
}
