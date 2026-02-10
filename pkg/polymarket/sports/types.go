// Package sports provides soccer market discovery, parsing, and alpha integration
// for Polymarket sports betting markets.
package sports

import (
	"time"

	"github.com/shopspring/decimal"
)

// MarketKind represents the type of soccer market.
type MarketKind string

const (
	MarketKindHomeWin MarketKind = "HOME_WIN"
	MarketKindDraw    MarketKind = "DRAW"
	MarketKindAwayWin MarketKind = "AWAY_WIN"
	MarketKindBTTS    MarketKind = "BTTS"   // Both Teams To Score
	MarketKindTotal   MarketKind = "TOTAL"  // Over/Under
	MarketKindSpread  MarketKind = "SPREAD" // Handicap
	MarketKindOther   MarketKind = "OTHER"  // Unknown/unsupported
)

// IsTradeable returns true if this market kind can be scored by MathShard.
func (k MarketKind) IsTradeable() bool {
	switch k {
	case MarketKindHomeWin, MarketKindDraw, MarketKindAwayWin:
		return true
	default:
		return false
	}
}

// League represents a soccer league.
type League string

const (
	LeagueEPL        League = "epl"
	LeagueLaLiga     League = "la_liga"
	LeagueSerieA     League = "serie_a"
	LeagueBundesliga League = "bundesliga"
	LeagueLigue1     League = "ligue_1"
)

// LeagueConfig holds configuration for a league.
type LeagueConfig struct {
	MathShardKey   string   // Key used in MathShard API
	PolymarketTags []string // Tags to filter Polymarket events
	SlugPrefixes   []string // Possible slug prefixes (e.g., "epl", "premier-league")
	MinEdgeBps     int      // Minimum edge in basis points to trade
	Enabled        bool     // Whether trading is enabled
}

// DefaultLeagueConfigs returns the default league configurations.
func DefaultLeagueConfigs() map[League]LeagueConfig {
	return map[League]LeagueConfig{
		LeagueEPL: {
			MathShardKey:   "epl",
			PolymarketTags: []string{"Premier League", "EPL", "Soccer"},
			SlugPrefixes:   []string{"epl"},
			MinEdgeBps:     600, // 6% minimum edge
			Enabled:        true,
		},
		LeagueLaLiga: {
			MathShardKey:   "la_liga",
			PolymarketTags: []string{"La Liga"},
			SlugPrefixes:   []string{"la-liga", "laliga"},
			MinEdgeBps:     1500, // 15% - MathShard uses higher threshold
			Enabled:        true,
		},
		LeagueSerieA: {
			MathShardKey:   "serie_a",
			PolymarketTags: []string{"Serie A"},
			SlugPrefixes:   []string{"serie-a", "seriea"},
			MinEdgeBps:     1000,
			Enabled:        false, // Disabled - negative edge historically
		},
		LeagueBundesliga: {
			MathShardKey:   "bundesliga",
			PolymarketTags: []string{"Bundesliga"},
			SlugPrefixes:   []string{"bundesliga"},
			MinEdgeBps:     800,
			Enabled:        true,
		},
		LeagueLigue1: {
			MathShardKey:   "ligue_1",
			PolymarketTags: []string{"Ligue 1"},
			SlugPrefixes:   []string{"ligue-1", "ligue1"},
			MinEdgeBps:     1000,
			Enabled:        true,
		},
	}
}

// Team represents a Polymarket team entity.
type Team struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Abbreviation string   `json:"abbreviation"`
	Aliases      []string `json:"aliases"`
	League       string   `json:"league"`
	SportID      string   `json:"sport_id"`
}

// SoccerMarketSpec represents a parsed soccer market from Polymarket.
type SoccerMarketSpec struct {
	// Market identification
	EventSlug   string `json:"event_slug"`
	MarketSlug  string `json:"market_slug"`
	ConditionID string `json:"condition_id"`

	// Token IDs for trading
	YesTokenID string `json:"yes_token_id"`
	NoTokenID  string `json:"no_token_id"`

	// Match details
	League     League    `json:"league"`
	MatchDate  time.Time `json:"match_date"`
	HomeTeamID string    `json:"home_team_id"` // Polymarket team ID
	AwayTeamID string    `json:"away_team_id"`
	HomeTeam   string    `json:"home_team"` // Canonical name
	AwayTeam   string    `json:"away_team"`

	// Market type
	Kind     MarketKind `json:"kind"`
	Line     float64    `json:"line,omitempty"` // For totals/spreads
	Question string     `json:"question"`

	// Metadata
	EndDate   time.Time `json:"end_date"`
	Closed    bool      `json:"closed"`
	Tradeable bool      `json:"tradeable"` // Can we score this with MathShard?
}

// MatchKey returns a unique key for the match (league + date + teams).
func (s *SoccerMarketSpec) MatchKey() string {
	return string(s.League) + "_" + s.MatchDate.Format("2006-01-02") + "_" + s.HomeTeam + "_" + s.AwayTeam
}

// Prediction represents a MathShard probability forecast.
type Prediction struct {
	HomeProb   decimal.Decimal `json:"home_prob"`  // P(Home win)
	DrawProb   decimal.Decimal `json:"draw_prob"`  // P(Draw)
	AwayProb   decimal.Decimal `json:"away_prob"`  // P(Away win)
	Confidence decimal.Decimal `json:"confidence"` // Model confidence

	// Metadata
	ModelVersion string    `json:"model_version"`
	Timestamp    time.Time `json:"timestamp"`

	// Feature values (for debugging/logging)
	EloDiff  float64 `json:"elo_diff,omitempty"`
	FormDiff float64 `json:"form_diff,omitempty"`
}

// ProbFor returns the probability for a specific market kind.
func (p *Prediction) ProbFor(kind MarketKind) decimal.Decimal {
	switch kind {
	case MarketKindHomeWin:
		return p.HomeProb
	case MarketKindDraw:
		return p.DrawProb
	case MarketKindAwayWin:
		return p.AwayProb
	default:
		return decimal.Zero
	}
}

// EdgeResult represents the calculated edge for a market.
type EdgeResult struct {
	// Core values
	ModelProb      decimal.Decimal `json:"model_prob"`      // q from MathShard
	MarketPrice    decimal.Decimal `json:"market_price"`    // Raw best ask
	EffectivePrice decimal.Decimal `json:"effective_price"` // After fees/slippage
	Edge           decimal.Decimal `json:"edge"`            // q - p_eff
	EdgeBps        decimal.Decimal `json:"edge_bps"`        // Edge in basis points

	// Kelly sizing
	KellyFraction decimal.Decimal `json:"kelly_fraction"` // Raw Kelly f*
	AdjustedKelly decimal.Decimal `json:"adjusted_kelly"` // After caps/adjustments
	SuggestedSize decimal.Decimal `json:"suggested_size"` // In dollars

	// Decision
	IsValueBet bool   `json:"is_value_bet"`
	Reason     string `json:"reason"`
}

// Signal represents a trading signal.
type Signal struct {
	// Market
	Spec *SoccerMarketSpec `json:"spec"`

	// Prediction
	Prediction *Prediction `json:"prediction"`

	// Edge
	Edge *EdgeResult `json:"edge"`

	// Action
	Side       string          `json:"side"` // "YES" or "NO"
	TokenID    string          `json:"token_id"`
	Size       decimal.Decimal `json:"size"`
	LimitPrice decimal.Decimal `json:"limit_price,omitempty"`

	// Timing
	GeneratedAt time.Time `json:"generated_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// FeeModel calculates fees for orders.
type FeeModel interface {
	// Calculate returns the fee in the same units as the order value.
	Calculate(side string, price, size decimal.Decimal) decimal.Decimal
	// EffectivePrice returns price adjusted for fees.
	EffectivePrice(side string, price decimal.Decimal) decimal.Decimal
}

// ZeroFeeModel implements FeeModel with no fees.
type ZeroFeeModel struct{}

func (m *ZeroFeeModel) Calculate(side string, price, size decimal.Decimal) decimal.Decimal {
	return decimal.Zero
}

func (m *ZeroFeeModel) EffectivePrice(side string, price decimal.Decimal) decimal.Decimal {
	return price
}

// BpsFeeModel implements FeeModel with basis points fees.
type BpsFeeModel struct {
	TakerFeeBps decimal.Decimal
	MakerFeeBps decimal.Decimal
}

func (m *BpsFeeModel) Calculate(side string, price, size decimal.Decimal) decimal.Decimal {
	value := price.Mul(size)
	// Assume taker for now
	return value.Mul(m.TakerFeeBps).Div(decimal.NewFromInt(10000))
}

func (m *BpsFeeModel) EffectivePrice(side string, price decimal.Decimal) decimal.Decimal {
	fee := price.Mul(m.TakerFeeBps).Div(decimal.NewFromInt(10000))
	if side == "BUY" {
		return price.Add(fee)
	}
	return price.Sub(fee)
}
