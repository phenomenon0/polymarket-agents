package sports

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestEdgeCalculator_CalculateEdge(t *testing.T) {
	calc := NewEdgeCalculator(&EdgeCalculatorConfig{
		MinEdgeBps:  500,  // 5% minimum
		KellyFrac:   0.10, // 10% Kelly
		MaxStakePct: 0.05, // 5% max stake
	})

	tests := []struct {
		name         string
		modelProb    float64
		marketPrice  float64
		bankroll     float64
		wantValueBet bool
		wantEdgeBps  float64 // approximate
	}{
		{
			name:         "clear value bet",
			modelProb:    0.70,
			marketPrice:  0.55,
			bankroll:     1000,
			wantValueBet: true,
			wantEdgeBps:  1500, // 15% edge
		},
		{
			name:         "marginal value bet",
			modelProb:    0.60,
			marketPrice:  0.53,
			bankroll:     1000,
			wantValueBet: true,
			wantEdgeBps:  700, // 7% edge
		},
		{
			name:         "no edge",
			modelProb:    0.50,
			marketPrice:  0.50,
			bankroll:     1000,
			wantValueBet: false,
			wantEdgeBps:  0,
		},
		{
			name:         "negative edge",
			modelProb:    0.45,
			marketPrice:  0.55,
			bankroll:     1000,
			wantValueBet: false,
			wantEdgeBps:  -1000, // -10% edge
		},
		{
			name:         "below minimum threshold",
			modelProb:    0.54,
			marketPrice:  0.50,
			bankroll:     1000,
			wantValueBet: false,
			wantEdgeBps:  400, // 4% edge, below 5% minimum
		},
		{
			name:         "large edge on underdog",
			modelProb:    0.35,
			marketPrice:  0.20,
			bankroll:     1000,
			wantValueBet: true,
			wantEdgeBps:  1500, // 15% edge
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calc.CalculateEdge(
				decimal.NewFromFloat(tt.modelProb),
				decimal.NewFromFloat(tt.marketPrice),
				decimal.NewFromFloat(tt.bankroll),
			)

			if result.IsValueBet != tt.wantValueBet {
				t.Errorf("IsValueBet = %v, want %v (edge: %.0f bps, reason: %s)",
					result.IsValueBet, tt.wantValueBet,
					result.EdgeBps.InexactFloat64(), result.Reason)
			}

			gotEdgeBps := result.EdgeBps.InexactFloat64()
			if tt.wantValueBet && (gotEdgeBps < tt.wantEdgeBps-50 || gotEdgeBps > tt.wantEdgeBps+50) {
				t.Errorf("EdgeBps = %.0f, want ~%.0f", gotEdgeBps, tt.wantEdgeBps)
			}

			// Check Kelly fraction is reasonable
			if tt.wantValueBet {
				if result.KellyFraction.IsNegative() {
					t.Error("KellyFraction should be positive for value bet")
				}
				if result.SuggestedSize.IsNegative() {
					t.Error("SuggestedSize should be positive for value bet")
				}
			}
		})
	}
}

func TestEdgeCalculator_KellyCapping(t *testing.T) {
	calc := NewEdgeCalculator(&EdgeCalculatorConfig{
		MinEdgeBps:  100,  // 1% minimum (low for testing)
		KellyFrac:   0.25, // 25% Kelly
		MaxStakePct: 0.02, // 2% max stake
	})

	// Huge edge that would produce large Kelly
	result := calc.CalculateEdge(
		decimal.NewFromFloat(0.90),  // 90% model prob
		decimal.NewFromFloat(0.40),  // 40% market price
		decimal.NewFromFloat(10000), // $10k bankroll
	)

	if !result.IsValueBet {
		t.Fatal("Should be a value bet")
	}

	// Suggested size should be capped at 2% of bankroll ($200)
	maxAllowed := decimal.NewFromFloat(200)
	if result.SuggestedSize.GreaterThan(maxAllowed) {
		t.Errorf("SuggestedSize = $%.2f, should be capped at $%.2f",
			result.SuggestedSize.InexactFloat64(), maxAllowed.InexactFloat64())
	}
}

func TestCalculateVWAP(t *testing.T) {
	tests := []struct {
		name       string
		asks       []PriceLevel
		targetSize float64
		wantVWAP   float64
		wantFilled float64
		wantOK     bool
	}{
		{
			name: "single level fill",
			asks: []PriceLevel{
				{Price: decimal.NewFromFloat(0.50), Size: decimal.NewFromFloat(100)},
			},
			targetSize: 50,
			wantVWAP:   0.50,
			wantFilled: 50,
			wantOK:     true,
		},
		{
			name: "multi level fill",
			asks: []PriceLevel{
				{Price: decimal.NewFromFloat(0.50), Size: decimal.NewFromFloat(50)},
				{Price: decimal.NewFromFloat(0.52), Size: decimal.NewFromFloat(50)},
				{Price: decimal.NewFromFloat(0.55), Size: decimal.NewFromFloat(100)},
			},
			targetSize: 100,
			wantVWAP:   0.51, // (50*0.50 + 50*0.52) / 100
			wantFilled: 100,
			wantOK:     true,
		},
		{
			name: "partial fill",
			asks: []PriceLevel{
				{Price: decimal.NewFromFloat(0.50), Size: decimal.NewFromFloat(30)},
				{Price: decimal.NewFromFloat(0.55), Size: decimal.NewFromFloat(20)},
			},
			targetSize: 100,
			wantVWAP:   0.52, // (30*0.50 + 20*0.55) / 50
			wantFilled: 50,   // only 50 available
			wantOK:     true,
		},
		{
			name:       "empty orderbook",
			asks:       []PriceLevel{},
			targetSize: 100,
			wantOK:     false,
		},
		{
			name: "zero target size",
			asks: []PriceLevel{
				{Price: decimal.NewFromFloat(0.50), Size: decimal.NewFromFloat(100)},
			},
			targetSize: 0,
			wantOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vwap, filled, ok := CalculateVWAP(tt.asks, decimal.NewFromFloat(tt.targetSize))

			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
				return
			}

			if !tt.wantOK {
				return
			}

			gotVWAP := vwap.InexactFloat64()
			if gotVWAP < tt.wantVWAP-0.01 || gotVWAP > tt.wantVWAP+0.01 {
				t.Errorf("VWAP = %.4f, want ~%.4f", gotVWAP, tt.wantVWAP)
			}

			gotFilled := filled.InexactFloat64()
			if gotFilled < tt.wantFilled-0.01 || gotFilled > tt.wantFilled+0.01 {
				t.Errorf("Filled = %.2f, want %.2f", gotFilled, tt.wantFilled)
			}
		})
	}
}

func TestRiskManager_CheckLimits(t *testing.T) {
	rm := NewRiskManager(&RiskLimits{
		MaxStakePerMarket: decimal.NewFromFloat(100),
		MaxDailyLoss:      decimal.NewFromFloat(500),
		MaxWeeklyExposure: decimal.NewFromFloat(1000),
		MaxCorrelatedExp:  decimal.NewFromFloat(150),
		MinLiquidity:      decimal.NewFromFloat(500),
		CooldownAfterLoss: 3,
	})

	tests := []struct {
		name       string
		setup      func()
		matchKey   string
		size       float64
		liquidity  float64
		wantOK     bool
		wantReason string
	}{
		{
			name:      "normal trade",
			matchKey:  "epl_2025-12-26_MUN_NEW",
			size:      50,
			liquidity: 1000,
			wantOK:    true,
		},
		{
			name:       "exceeds per-market limit",
			matchKey:   "epl_2025-12-26_MUN_NEW",
			size:       150,
			liquidity:  1000,
			wantOK:     false,
			wantReason: "exceeds per-market stake limit",
		},
		{
			name:       "insufficient liquidity",
			matchKey:   "epl_2025-12-26_MUN_NEW",
			size:       50,
			liquidity:  100, // below 500 minimum
			wantOK:     false,
			wantReason: "insufficient market liquidity",
		},
		{
			name: "weekly exposure limit",
			setup: func() {
				// Simulate prior trades
				rm.weeklyExposure = decimal.NewFromFloat(950)
			},
			matchKey:   "epl_2025-12-26_MUN_NEW",
			size:       100, // would exceed 1000 limit
			liquidity:  1000,
			wantOK:     false,
			wantReason: "would exceed weekly exposure limit",
		},
		{
			name: "correlated exposure",
			setup: func() {
				rm.weeklyExposure = decimal.Zero
				rm.matchExposure["epl_2025-12-26_MUN_NEW"] = decimal.NewFromFloat(100)
			},
			matchKey:   "epl_2025-12-26_MUN_NEW",
			size:       100, // would exceed 150 correlated limit
			liquidity:  1000,
			wantOK:     false,
			wantReason: "would exceed correlated exposure limit for match",
		},
		{
			name: "in cooldown",
			setup: func() {
				rm.matchExposure = make(map[string]decimal.Decimal)
				rm.weeklyExposure = decimal.Zero
				rm.consecutiveLosses = 5
			},
			matchKey:   "epl_2025-12-27_ARS_CHE",
			size:       50,
			liquidity:  1000,
			wantOK:     false,
			wantReason: "in cooldown after consecutive losses",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset risk manager state
			rm.dailyPnL = decimal.Zero
			rm.weeklyExposure = decimal.Zero
			rm.matchExposure = make(map[string]decimal.Decimal)
			rm.consecutiveLosses = 0

			if tt.setup != nil {
				tt.setup()
			}

			ok, reason := rm.CheckLimits(
				tt.matchKey,
				decimal.NewFromFloat(tt.size),
				decimal.NewFromFloat(tt.liquidity),
			)

			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v (reason: %s)", ok, tt.wantOK, reason)
			}

			if !tt.wantOK && reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", reason, tt.wantReason)
			}
		})
	}
}

func TestRiskManager_RecordTrade(t *testing.T) {
	rm := NewRiskManager(nil)

	// Record a winning trade
	rm.RecordTrade("match1", decimal.NewFromFloat(100), decimal.NewFromFloat(50))

	if !rm.dailyPnL.Equal(decimal.NewFromFloat(50)) {
		t.Errorf("dailyPnL = %v, want 50", rm.dailyPnL)
	}
	if !rm.weeklyExposure.Equal(decimal.NewFromFloat(100)) {
		t.Errorf("weeklyExposure = %v, want 100", rm.weeklyExposure)
	}
	if rm.consecutiveLosses != 0 {
		t.Errorf("consecutiveLosses = %d, want 0 (win resets)", rm.consecutiveLosses)
	}

	// Record a losing trade
	rm.RecordTrade("match2", decimal.NewFromFloat(50), decimal.NewFromFloat(-50))

	if rm.consecutiveLosses != 1 {
		t.Errorf("consecutiveLosses = %d, want 1", rm.consecutiveLosses)
	}

	// Record another loss
	rm.RecordTrade("match3", decimal.NewFromFloat(75), decimal.NewFromFloat(-75))

	if rm.consecutiveLosses != 2 {
		t.Errorf("consecutiveLosses = %d, want 2", rm.consecutiveLosses)
	}

	// Win resets consecutive losses
	rm.RecordTrade("match4", decimal.NewFromFloat(100), decimal.NewFromFloat(100))

	if rm.consecutiveLosses != 0 {
		t.Errorf("consecutiveLosses = %d, want 0 (win resets)", rm.consecutiveLosses)
	}
}

func TestBpsFeeModel(t *testing.T) {
	model := &BpsFeeModel{
		TakerFeeBps: decimal.NewFromFloat(50), // 0.5%
		MakerFeeBps: decimal.NewFromFloat(25), // 0.25%
	}

	// Test fee calculation
	fee := model.Calculate("BUY", decimal.NewFromFloat(0.50), decimal.NewFromFloat(100))
	expectedFee := 0.50 * 100 * 0.005 // $0.25
	if !fee.Equal(decimal.NewFromFloat(expectedFee)) {
		t.Errorf("Calculate() = %v, want %v", fee, expectedFee)
	}

	// Test effective price for buy
	effPrice := model.EffectivePrice("BUY", decimal.NewFromFloat(0.50))
	// 0.50 + (0.50 * 0.005) = 0.5025
	if !effPrice.Equal(decimal.NewFromFloat(0.5025)) {
		t.Errorf("EffectivePrice(BUY) = %v, want 0.5025", effPrice)
	}

	// Test effective price for sell
	effPriceSell := model.EffectivePrice("SELL", decimal.NewFromFloat(0.50))
	// 0.50 - (0.50 * 0.005) = 0.4975
	if !effPriceSell.Equal(decimal.NewFromFloat(0.4975)) {
		t.Errorf("EffectivePrice(SELL) = %v, want 0.4975", effPriceSell)
	}
}

func TestZeroFeeModel(t *testing.T) {
	model := &ZeroFeeModel{}

	fee := model.Calculate("BUY", decimal.NewFromFloat(0.50), decimal.NewFromFloat(100))
	if !fee.IsZero() {
		t.Errorf("ZeroFeeModel.Calculate() = %v, want 0", fee)
	}

	effPrice := model.EffectivePrice("BUY", decimal.NewFromFloat(0.50))
	if !effPrice.Equal(decimal.NewFromFloat(0.50)) {
		t.Errorf("ZeroFeeModel.EffectivePrice() = %v, want 0.50", effPrice)
	}
}
