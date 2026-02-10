package sports

import (
	"testing"
	"time"
)

func TestParseSlug(t *testing.T) {
	parser := NewMarketParser(NewTeamsClient())

	tests := []struct {
		name       string
		slug       string
		wantLeague string
		wantHome   string
		wantAway   string
		wantDate   string
		wantType   string
		wantLine   float64
	}{
		{
			name:       "EPL home win",
			slug:       "epl-mun-new-2025-12-26-mun",
			wantLeague: "epl",
			wantHome:   "mun",
			wantAway:   "new",
			wantDate:   "2025-12-26",
			wantType:   "home_win",
		},
		{
			name:       "EPL draw",
			slug:       "epl-mun-new-2025-12-26-draw",
			wantLeague: "epl",
			wantHome:   "mun",
			wantAway:   "new",
			wantDate:   "2025-12-26",
			wantType:   "draw",
		},
		{
			name:       "EPL away win",
			slug:       "epl-mun-new-2025-12-26-new",
			wantLeague: "epl",
			wantHome:   "mun",
			wantAway:   "new",
			wantDate:   "2025-12-26",
			wantType:   "away_win",
		},
		{
			name:       "BTTS market",
			slug:       "epl-mun-new-2025-12-26-btts",
			wantLeague: "epl",
			wantHome:   "mun",
			wantAway:   "new",
			wantDate:   "2025-12-26",
			wantType:   "btts",
		},
		{
			name:       "Total over/under",
			slug:       "epl-mun-new-2025-12-26-total-2pt5",
			wantLeague: "epl",
			wantHome:   "mun",
			wantAway:   "new",
			wantDate:   "2025-12-26",
			wantType:   "total",
			wantLine:   2.5,
		},
		{
			name:       "Spread/handicap",
			slug:       "epl-mun-new-2025-12-26-spread-home-1pt5",
			wantLeague: "epl",
			wantHome:   "mun",
			wantAway:   "new",
			wantDate:   "2025-12-26",
			wantType:   "spread",
			wantLine:   1.5,
		},
		{
			name:       "La Liga market",
			slug:       "la-liga-bar-mad-2025-12-21-bar",
			wantLeague: "la-liga",
			wantHome:   "bar",
			wantAway:   "mad",
			wantDate:   "2025-12-21",
			wantType:   "home_win",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parser.ParseSlug(tt.slug)
			if err != nil {
				t.Fatalf("ParseSlug() error = %v", err)
			}
			if got == nil {
				t.Fatal("ParseSlug() returned nil")
			}

			if got.League != tt.wantLeague {
				t.Errorf("League = %v, want %v", got.League, tt.wantLeague)
			}
			if got.HomeAbbrev != tt.wantHome {
				t.Errorf("HomeAbbrev = %v, want %v", got.HomeAbbrev, tt.wantHome)
			}
			if got.AwayAbbrev != tt.wantAway {
				t.Errorf("AwayAbbrev = %v, want %v", got.AwayAbbrev, tt.wantAway)
			}
			if got.Date != tt.wantDate {
				t.Errorf("Date = %v, want %v", got.Date, tt.wantDate)
			}
			if got.MarketType != tt.wantType {
				t.Errorf("MarketType = %v, want %v", got.MarketType, tt.wantType)
			}
			if tt.wantLine != 0 && got.Line != tt.wantLine {
				t.Errorf("Line = %v, want %v", got.Line, tt.wantLine)
			}
		})
	}
}

func TestParseSlug_Invalid(t *testing.T) {
	parser := NewMarketParser(NewTeamsClient())

	tests := []struct {
		name string
		slug string
	}{
		{"empty", ""},
		{"no date", "epl-mun-new-mun"},
		{"wrong format", "will-trump-win"},
		{"politics", "presidential-election-2024-winner"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parser.ParseSlug(tt.slug)
			if err != nil {
				t.Fatalf("ParseSlug() error = %v", err)
			}
			if got != nil {
				t.Errorf("ParseSlug() = %v, want nil for invalid slug", got)
			}
		})
	}
}

func TestParse_FullMarket(t *testing.T) {
	parser := NewMarketParser(NewTeamsClient())

	market := &PolymarketMarket{
		Slug:        "epl-mun-new-2025-12-26-draw",
		Question:    "Will Manchester United FC vs. Newcastle United FC end in a draw?",
		ConditionID: "0x123abc",
		Tags:        []string{"Sports", "Premier League", "EPL", "Soccer"},
		Tokens: []Token{
			{TokenID: "yes123", Outcome: "Yes"},
			{TokenID: "no456", Outcome: "No"},
		},
		EndDate: "2025-12-26T23:00:00Z",
		Closed:  false,
	}

	spec, err := parser.Parse(market)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if spec == nil {
		t.Fatal("Parse() returned nil")
	}

	// Check parsed values
	if spec.Kind != MarketKindDraw {
		t.Errorf("Kind = %v, want %v", spec.Kind, MarketKindDraw)
	}
	if spec.League != LeagueEPL {
		t.Errorf("League = %v, want %v", spec.League, LeagueEPL)
	}
	if spec.ConditionID != "0x123abc" {
		t.Errorf("ConditionID = %v, want %v", spec.ConditionID, "0x123abc")
	}
	if spec.YesTokenID != "yes123" {
		t.Errorf("YesTokenID = %v, want %v", spec.YesTokenID, "yes123")
	}
	if spec.NoTokenID != "no456" {
		t.Errorf("NoTokenID = %v, want %v", spec.NoTokenID, "no456")
	}
	if !spec.Tradeable {
		t.Error("Tradeable = false, want true for DRAW market")
	}
	if spec.MatchDate.Format("2006-01-02") != "2025-12-26" {
		t.Errorf("MatchDate = %v, want 2025-12-26", spec.MatchDate.Format("2006-01-02"))
	}
}

func TestParse_HomeWinMarket(t *testing.T) {
	parser := NewMarketParser(NewTeamsClient())

	market := &PolymarketMarket{
		Slug:        "epl-ars-che-2025-12-28-ars",
		Question:    "Will Arsenal FC win on 2025-12-28?",
		ConditionID: "0xabc123",
		Tags:        []string{"Sports", "Premier League", "Soccer"},
		Tokens: []Token{
			{TokenID: "tok1", Outcome: "Yes"},
			{TokenID: "tok2", Outcome: "No"},
		},
		Closed: false,
	}

	spec, err := parser.Parse(market)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if spec == nil {
		t.Fatal("Parse() returned nil")
	}

	if spec.Kind != MarketKindHomeWin {
		t.Errorf("Kind = %v, want %v", spec.Kind, MarketKindHomeWin)
	}
	if !spec.Tradeable {
		t.Error("Tradeable = false, want true for HOME_WIN market")
	}
}

func TestParse_NonSoccerMarket(t *testing.T) {
	parser := NewMarketParser(NewTeamsClient())

	market := &PolymarketMarket{
		Slug:        "presidential-election-2024-winner",
		Question:    "Who will win the 2024 presidential election?",
		ConditionID: "0xpolitics",
		Tags:        []string{"Politics", "Elections"},
		Closed:      false,
	}

	spec, err := parser.Parse(market)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if spec != nil {
		t.Errorf("Parse() = %v, want nil for non-soccer market", spec)
	}
}

func TestMarketKind_IsTradeable(t *testing.T) {
	tests := []struct {
		kind     MarketKind
		tradable bool
	}{
		{MarketKindHomeWin, true},
		{MarketKindDraw, true},
		{MarketKindAwayWin, true},
		{MarketKindBTTS, false},
		{MarketKindTotal, false},
		{MarketKindSpread, false},
		{MarketKindOther, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			if got := tt.kind.IsTradeable(); got != tt.tradable {
				t.Errorf("%v.IsTradeable() = %v, want %v", tt.kind, got, tt.tradable)
			}
		})
	}
}

func TestGroupByMatch(t *testing.T) {
	matchDate := time.Date(2025, 12, 26, 0, 0, 0, 0, time.UTC)

	specs := []*SoccerMarketSpec{
		{
			League:    LeagueEPL,
			MatchDate: matchDate,
			HomeTeam:  "Manchester United",
			AwayTeam:  "Newcastle",
			Kind:      MarketKindHomeWin,
		},
		{
			League:    LeagueEPL,
			MatchDate: matchDate,
			HomeTeam:  "Manchester United",
			AwayTeam:  "Newcastle",
			Kind:      MarketKindDraw,
		},
		{
			League:    LeagueEPL,
			MatchDate: matchDate,
			HomeTeam:  "Manchester United",
			AwayTeam:  "Newcastle",
			Kind:      MarketKindAwayWin,
		},
		{
			League:    LeagueEPL,
			MatchDate: matchDate,
			HomeTeam:  "Manchester United",
			AwayTeam:  "Newcastle",
			Kind:      MarketKindBTTS,
		},
	}

	grouped := GroupByMatch(specs)

	if len(grouped) != 1 {
		t.Errorf("GroupByMatch() returned %d groups, want 1", len(grouped))
	}

	for _, mm := range grouped {
		if mm.HomeWin == nil {
			t.Error("HomeWin is nil")
		}
		if mm.Draw == nil {
			t.Error("Draw is nil")
		}
		if mm.AwayWin == nil {
			t.Error("AwayWin is nil")
		}
		if len(mm.BTTS) != 1 {
			t.Errorf("BTTS has %d entries, want 1", len(mm.BTTS))
		}
	}
}
