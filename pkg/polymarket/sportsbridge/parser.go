package sportsbridge

import (
	"regexp"
	"strings"
	"time"

	"github.com/phenomenon0/polymarket-agents/pkg/polymarket/gamma"
	"github.com/phenomenon0/polymarket-agents/pkg/polymarket/sports"
)

// Parser converts Polymarket markets to typed Contracts with EventSpecs.
type Parser struct {
	teamsClient *sports.TeamsClient
	slugParser  *sports.MarketParser
}

// NewParser creates a new parser.
func NewParser() *Parser {
	teamsClient := sports.NewTeamsClient()
	return &Parser{
		teamsClient: teamsClient,
		slugParser:  sports.NewMarketParser(teamsClient),
	}
}

// slugPattern matches: {league}-{home}-{away}-{date}-{type}
// Examples:
//
//	epl-liv-wol-2025-12-27-liv (Liverpool to win)
//	epl-liv-wol-2025-12-27-draw
//	epl-liv-wol-2025-12-27-wol (Wolves to win)
var slugPattern = regexp.MustCompile(`^([a-z0-9-]+)-([a-z]+)-([a-z]+)-(\d{4}-\d{2}-\d{2})-(.+)$`)

// EPL team abbreviation to canonical name
var teamAbbrevs = map[string]string{
	"ars": "Arsenal",
	"avl": "Aston Villa",
	"ast": "Aston Villa",
	"bou": "Bournemouth",
	"bre": "Brentford",
	"bha": "Brighton",
	"bri": "Brighton",
	"bur": "Burnley",
	"che": "Chelsea",
	"cry": "Crystal Palace",
	"cpa": "Crystal Palace",
	"eve": "Everton",
	"ful": "Fulham",
	"lei": "Leicester City",
	"liv": "Liverpool",
	"lut": "Luton Town",
	"mac": "Manchester City",
	"mci": "Manchester City",
	"mun": "Manchester United",
	"man": "Manchester United",
	"new": "Newcastle United",
	"nfo": "Nottingham Forest",
	"not": "Nottingham Forest",
	"she": "Sheffield United",
	"sou": "Southampton",
	"tot": "Tottenham",
	"whu": "West Ham",
	"wes": "West Ham",
	"wol": "Wolves",
	"ips": "Ipswich Town",
	// Championship
	"sun": "Sunderland",
	"lee": "Leeds United",
	"mid": "Middlesbrough",
	"nor": "Norwich City",
	"wat": "Watford",
	"wba": "West Brom",
}

// ParseSlug parses a Polymarket soccer slug into components.
type ParsedSlug struct {
	League     string
	HomeAbbrev string
	AwayAbbrev string
	HomeTeam   string
	AwayTeam   string
	Date       time.Time
	MarketType string // "home", "draw", "away", "btts", "total", etc.
	Outcome    Outcome
}

// ParseSlug extracts match info from a Polymarket slug.
func (p *Parser) ParseSlug(slug string) (*ParsedSlug, error) {
	matches := slugPattern.FindStringSubmatch(slug)
	if matches == nil {
		return nil, nil // Not a soccer market
	}

	league := matches[1]
	homeAbbrev := matches[2]
	awayAbbrev := matches[3]
	dateStr := matches[4]
	marketPart := matches[5]

	// Parse date
	matchDate, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return nil, nil
	}

	// Resolve team names
	homeTeam := teamAbbrevs[homeAbbrev]
	awayTeam := teamAbbrevs[awayAbbrev]
	if homeTeam == "" {
		homeTeam = strings.ToUpper(homeAbbrev)
	}
	if awayTeam == "" {
		awayTeam = strings.ToUpper(awayAbbrev)
	}

	parsed := &ParsedSlug{
		League:     league,
		HomeAbbrev: homeAbbrev,
		AwayAbbrev: awayAbbrev,
		HomeTeam:   homeTeam,
		AwayTeam:   awayTeam,
		Date:       matchDate,
	}

	// Determine outcome from market part
	switch {
	case marketPart == "draw":
		parsed.MarketType = "draw"
		parsed.Outcome = OutcomeDraw
	case marketPart == homeAbbrev:
		parsed.MarketType = "home"
		parsed.Outcome = OutcomeHome
	case marketPart == awayAbbrev:
		parsed.MarketType = "away"
		parsed.Outcome = OutcomeAway
	case strings.HasPrefix(marketPart, "btts"):
		parsed.MarketType = "btts"
	case strings.HasPrefix(marketPart, "total"):
		parsed.MarketType = "total"
	case strings.HasPrefix(marketPart, "spread"):
		parsed.MarketType = "spread"
	default:
		parsed.MarketType = "unknown"
	}

	return parsed, nil
}

// ParseGammaMarket converts a Gamma market to a Contract with EventSpec.
func (p *Parser) ParseGammaMarket(m *gamma.Market, league string) (*Contract, error) {
	// Parse slug
	parsed, err := p.ParseSlug(m.Slug)
	if err != nil || parsed == nil {
		return nil, err
	}

	// Only handle 1X2 markets for now
	if parsed.MarketType != "home" && parsed.MarketType != "draw" && parsed.MarketType != "away" {
		return nil, nil
	}

	// Get token IDs
	tokenIDs := m.ClobTokenIDs()
	tokenYesID := ""
	if len(tokenIDs) > 0 {
		tokenYesID = tokenIDs[0]
	}

	// Build EventSpec
	event := Soccer1X2Event{
		League:    parsed.League,
		HomeTeam:  parsed.HomeTeam,
		AwayTeam:  parsed.AwayTeam,
		MatchDate: parsed.Date,
		Outcome:   parsed.Outcome,
		IsYesSide: true, // We only parse YES contracts
	}

	contract := &Contract{
		MarketID:    m.ID,
		TokenID:     tokenYesID,
		ConditionID: m.ConditionID,
		Slug:        m.Slug,
		Question:    m.Question,
		Event:       event,
		Category:    "soccer",
		Closed:      m.Closed,
		EndDate:     m.EndDate,
		Liquidity:   m.Liquidity.Float64(),
	}

	// Get prices - OutcomePrices returns []string, YesPrice returns float64
	contract.MidPx = m.YesPrice()
	contract.BestAsk = contract.MidPx // Use mid as best ask approximation

	return contract, nil
}

// ParseGammaEvent extracts all 1X2 contracts from a Gamma event.
func (p *Parser) ParseGammaEvent(event *gamma.Event, league string) ([]*Contract, error) {
	var contracts []*Contract

	for _, m := range event.Markets {
		if m.Closed {
			continue
		}

		contract, err := p.ParseGammaMarket(&m, league)
		if err != nil {
			continue
		}
		if contract == nil {
			continue
		}

		contracts = append(contracts, contract)
	}

	return contracts, nil
}

// MatchKey returns a unique key for a match (for grouping contracts).
func MatchKey(c *Contract) string {
	if event, ok := c.Event.(Soccer1X2Event); ok {
		return event.League + "_" + event.MatchDate.Format("2006-01-02") + "_" + event.HomeTeam + "_" + event.AwayTeam
	}
	return c.Slug
}

// GroupByMatch groups contracts by match.
func GroupByMatch(contracts []*Contract) map[string][]*Contract {
	result := make(map[string][]*Contract)
	for _, c := range contracts {
		key := MatchKey(c)
		result[key] = append(result[key], c)
	}
	return result
}

// MatchContracts holds the 1X2 contracts for a single match.
type MatchContracts struct {
	MatchKey  string
	League    string
	HomeTeam  string
	AwayTeam  string
	MatchDate time.Time

	HomeWin *Contract
	Draw    *Contract
	AwayWin *Contract
}

// GroupedByMatch groups contracts into MatchContracts.
func GroupedByMatch(contracts []*Contract) []*MatchContracts {
	grouped := GroupByMatch(contracts)
	var result []*MatchContracts

	for matchKey, cs := range grouped {
		mc := &MatchContracts{MatchKey: matchKey}

		for _, c := range cs {
			if event, ok := c.Event.(Soccer1X2Event); ok {
				mc.League = event.League
				mc.HomeTeam = event.HomeTeam
				mc.AwayTeam = event.AwayTeam
				mc.MatchDate = event.MatchDate

				switch event.Outcome {
				case OutcomeHome:
					mc.HomeWin = c
				case OutcomeDraw:
					mc.Draw = c
				case OutcomeAway:
					mc.AwayWin = c
				}
			}
		}

		result = append(result, mc)
	}

	return result
}
