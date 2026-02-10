package sports

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// MarketParser parses Polymarket soccer markets into SoccerMarketSpec.
type MarketParser struct {
	teams        *TeamsClient
	leagueConfig map[League]LeagueConfig
}

// NewMarketParser creates a new market parser.
func NewMarketParser(teams *TeamsClient) *MarketParser {
	return &MarketParser{
		teams:        teams,
		leagueConfig: DefaultLeagueConfigs(),
	}
}

// ParsedSlug represents a parsed market slug.
type ParsedSlug struct {
	League     string
	HomeAbbrev string
	AwayAbbrev string
	Date       string
	MarketType string
	Line       float64
	Side       string
}

// slugPattern matches: {league}-{home}-{away}-{date}-{type}[-{extra}]
// Examples:
//
//	epl-mun-new-2025-12-26-mun
//	epl-mun-new-2025-12-26-draw
//	epl-mun-new-2025-12-26-total-2pt5
//	epl-mun-new-2025-12-26-btts
//	epl-mun-new-2025-12-26-spread-home-1pt5
var slugPattern = regexp.MustCompile(`^([a-z0-9-]+)-([a-z]+)-([a-z]+)-(\d{4}-\d{2}-\d{2})-(.+)$`)

// ParseSlug parses a Polymarket market slug.
func (p *MarketParser) ParseSlug(slug string) (*ParsedSlug, error) {
	matches := slugPattern.FindStringSubmatch(slug)
	if matches == nil {
		return nil, nil // Not a soccer market
	}

	league := matches[1]
	homeAbbrev := matches[2]
	awayAbbrev := matches[3]
	dateStr := matches[4]
	marketPart := matches[5]

	parsed := &ParsedSlug{
		League:     league,
		HomeAbbrev: homeAbbrev,
		AwayAbbrev: awayAbbrev,
		Date:       dateStr,
	}

	// Parse market type
	// Possible values: {team_abbrev}, draw, btts, total-Xpt5, spread-home-Xpt5
	switch {
	case marketPart == "draw":
		parsed.MarketType = "draw"

	case marketPart == "btts":
		parsed.MarketType = "btts"

	case strings.HasPrefix(marketPart, "total-"):
		parsed.MarketType = "total"
		lineStr := strings.TrimPrefix(marketPart, "total-")
		parsed.Line = parseLineValue(lineStr)

	case strings.HasPrefix(marketPart, "spread-"):
		parsed.MarketType = "spread"
		parts := strings.Split(marketPart, "-")
		if len(parts) >= 3 {
			parsed.Side = parts[1] // "home" or "away"
			parsed.Line = parseLineValue(parts[2])
		}

	case marketPart == homeAbbrev:
		parsed.MarketType = "home_win"
		parsed.Side = homeAbbrev

	case marketPart == awayAbbrev:
		parsed.MarketType = "away_win"
		parsed.Side = awayAbbrev

	default:
		// Could be a team abbreviation we don't recognize
		parsed.MarketType = "unknown"
		parsed.Side = marketPart
	}

	return parsed, nil
}

// parseLineValue converts "2pt5" to 2.5, etc.
func parseLineValue(s string) float64 {
	s = strings.ReplaceAll(s, "pt", ".")
	val, _ := strconv.ParseFloat(s, 64)
	return val
}

// PolymarketMarket represents raw market data from Polymarket.
type PolymarketMarket struct {
	Slug        string   `json:"slug"`
	Question    string   `json:"question"`
	ConditionID string   `json:"condition_id"`
	Tokens      []Token  `json:"tokens"`
	Tags        []string `json:"tags"`
	EndDate     string   `json:"end_date"`
	Closed      bool     `json:"closed"`
}

// Token represents a token in a market.
type Token struct {
	TokenID string `json:"token_id"`
	Outcome string `json:"outcome"` // "Yes", "No", team name, etc.
	Price   string `json:"price"`
}

// Parse parses a Polymarket market into a SoccerMarketSpec.
func (p *MarketParser) Parse(market *PolymarketMarket) (*SoccerMarketSpec, error) {
	// First, check if this is a soccer market
	if !p.isSoccerMarket(market) {
		return nil, nil
	}

	// Parse the slug
	parsedSlug, err := p.ParseSlug(market.Slug)
	if err != nil || parsedSlug == nil {
		return nil, err
	}

	// Determine the league
	league := p.matchLeague(parsedSlug.League, market.Tags)
	if league == "" {
		return nil, nil
	}

	// Parse the date
	matchDate, err := time.Parse("2006-01-02", parsedSlug.Date)
	if err != nil {
		return nil, nil
	}

	// Find teams - first try from question, then from slug abbreviations
	homeTeam, awayTeam, found := p.teams.MatchTeamFromQuestion(market.Question)

	// If teams not found from question, try slug abbreviations
	if !found || homeTeam == nil || awayTeam == nil {
		homeTeam, awayTeam, found = p.matchTeamsFromSlug(parsedSlug, market.Question)
	}

	// Extract token IDs
	var yesTokenID, noTokenID string
	for _, token := range market.Tokens {
		outcome := strings.ToLower(token.Outcome)
		if outcome == "yes" || outcome == "over" {
			yesTokenID = token.TokenID
		} else if outcome == "no" || outcome == "under" {
			noTokenID = token.TokenID
		} else {
			// Could be team name for win markets
			// The "winning" outcome is the YES side
			if homeTeam != nil && strings.Contains(strings.ToLower(token.Outcome), strings.ToLower(homeTeam.Name)) {
				yesTokenID = token.TokenID
			} else if awayTeam != nil && strings.Contains(strings.ToLower(token.Outcome), strings.ToLower(awayTeam.Name)) {
				// This is the away team winning - but check if this IS the away win market
				if parsedSlug.MarketType == "away_win" {
					yesTokenID = token.TokenID
				}
			}
		}
	}

	// If we couldn't find tokens via outcome, use order (first = YES, second = NO)
	if yesTokenID == "" && len(market.Tokens) >= 2 {
		yesTokenID = market.Tokens[0].TokenID
		noTokenID = market.Tokens[1].TokenID
	}

	// Determine market kind
	kind := p.determineMarketKind(parsedSlug, market)

	// Build spec
	spec := &SoccerMarketSpec{
		EventSlug:   market.Slug,
		MarketSlug:  market.Slug,
		ConditionID: market.ConditionID,
		YesTokenID:  yesTokenID,
		NoTokenID:   noTokenID,
		League:      League(league),
		MatchDate:   matchDate,
		Kind:        kind,
		Line:        parsedSlug.Line,
		Question:    market.Question,
		Closed:      market.Closed,
		Tradeable:   kind.IsTradeable(),
	}

	// Set team info
	if found {
		if homeTeam != nil {
			spec.HomeTeamID = homeTeam.ID
			spec.HomeTeam = homeTeam.Name
		}
		if awayTeam != nil {
			spec.AwayTeamID = awayTeam.ID
			spec.AwayTeam = awayTeam.Name
		}
	}

	// Parse end date
	if market.EndDate != "" {
		if endDate, err := time.Parse(time.RFC3339, market.EndDate); err == nil {
			spec.EndDate = endDate
		}
	}

	return spec, nil
}

// isSoccerMarket checks if a market is a soccer market.
func (p *MarketParser) isSoccerMarket(market *PolymarketMarket) bool {
	soccerTags := []string{"Soccer", "Premier League", "EPL", "La Liga", "Serie A", "Bundesliga", "Ligue 1"}

	for _, tag := range market.Tags {
		for _, soccerTag := range soccerTags {
			if strings.EqualFold(tag, soccerTag) {
				return true
			}
		}
	}

	// Also check slug prefix
	for _, config := range p.leagueConfig {
		for _, prefix := range config.SlugPrefixes {
			if strings.HasPrefix(market.Slug, prefix+"-") {
				return true
			}
		}
	}

	return false
}

// matchLeague matches a slug prefix to a league.
func (p *MarketParser) matchLeague(slugPrefix string, tags []string) string {
	for league, config := range p.leagueConfig {
		for _, prefix := range config.SlugPrefixes {
			if slugPrefix == prefix {
				return string(league)
			}
		}
	}

	// Try to match by tags
	for league, config := range p.leagueConfig {
		for _, configTag := range config.PolymarketTags {
			for _, marketTag := range tags {
				if strings.EqualFold(configTag, marketTag) {
					return string(league)
				}
			}
		}
	}

	return ""
}

// determineMarketKind determines the MarketKind from parsed slug and market data.
func (p *MarketParser) determineMarketKind(slug *ParsedSlug, market *PolymarketMarket) MarketKind {
	switch slug.MarketType {
	case "home_win":
		return MarketKindHomeWin
	case "away_win":
		return MarketKindAwayWin
	case "draw":
		return MarketKindDraw
	case "btts":
		return MarketKindBTTS
	case "total":
		return MarketKindTotal
	case "spread":
		return MarketKindSpread
	default:
		// Try to infer from question
		q := strings.ToLower(market.Question)
		if strings.Contains(q, "end in a draw") {
			return MarketKindDraw
		}
		if strings.Contains(q, "both teams to score") {
			return MarketKindBTTS
		}
		if strings.Contains(q, "o/u") || strings.Contains(q, "over/under") {
			return MarketKindTotal
		}
		if strings.Contains(q, "spread") {
			return MarketKindSpread
		}
		if strings.Contains(q, " win ") || strings.Contains(q, " win?") {
			// Need to determine if home or away
			// This is ambiguous without team matching
			return MarketKindOther
		}
		return MarketKindOther
	}
}

// ParseMarkets parses multiple markets and groups them by match.
func (p *MarketParser) ParseMarkets(markets []*PolymarketMarket) (map[string][]*SoccerMarketSpec, error) {
	result := make(map[string][]*SoccerMarketSpec)

	for _, market := range markets {
		spec, err := p.Parse(market)
		if err != nil {
			continue // Skip invalid markets
		}
		if spec == nil {
			continue // Not a soccer market
		}

		key := spec.MatchKey()
		result[key] = append(result[key], spec)
	}

	return result, nil
}

// MatchMarkets holds all markets for a single match.
type MatchMarkets struct {
	League    League
	MatchDate time.Time
	HomeTeam  string
	AwayTeam  string

	HomeWin *SoccerMarketSpec
	Draw    *SoccerMarketSpec
	AwayWin *SoccerMarketSpec

	// Non-tradeable (for now)
	BTTS    []*SoccerMarketSpec
	Totals  []*SoccerMarketSpec
	Spreads []*SoccerMarketSpec
}

// EPL team abbreviation to name mapping
// These are the 3-letter codes used in Polymarket slugs
var eplTeamAbbrevs = map[string]string{
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
	"cpl": "Crystal Palace",
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
	// Championship teams (for EPL-related markets)
	"sun": "Sunderland",
	"lee": "Leeds United",
	"mid": "Middlesbrough",
	"swa": "Swansea",
	"hul": "Hull City",
	"nor": "Norwich City",
	"cov": "Coventry City",
	"bla": "Blackburn",
	"pre": "Preston",
	"wat": "Watford",
	"wba": "West Brom",
	"shw": "Sheffield Wednesday",
	"pbo": "Peterborough",
	"bfc": "Bristol City",
	"bcf": "Bristol City",
	"qpr": "QPR",
	"mil": "Millwall",
	"ply": "Plymouth",
	"str": "Stoke City",
	"sto": "Stoke City",
	"oxu": "Oxford United",
	"oxf": "Oxford United",
	"lbr": "Luton Town",
	"car": "Cardiff",
	"por": "Portsmouth",
	"der": "Derby County",
}

// matchTeamsFromSlug attempts to match team names using slug abbreviations.
func (p *MarketParser) matchTeamsFromSlug(slug *ParsedSlug, question string) (*Team, *Team, bool) {
	homeName, homeOk := eplTeamAbbrevs[slug.HomeAbbrev]
	awayName, awayOk := eplTeamAbbrevs[slug.AwayAbbrev]

	if !homeOk || !awayOk {
		return nil, nil, false
	}

	homeTeam := &Team{
		ID:           slug.HomeAbbrev,
		Name:         homeName,
		Abbreviation: strings.ToUpper(slug.HomeAbbrev),
		League:       slug.League,
	}

	awayTeam := &Team{
		ID:           slug.AwayAbbrev,
		Name:         awayName,
		Abbreviation: strings.ToUpper(slug.AwayAbbrev),
		League:       slug.League,
	}

	return homeTeam, awayTeam, true
}

// GroupByMatch groups market specs into MatchMarkets.
func GroupByMatch(specs []*SoccerMarketSpec) map[string]*MatchMarkets {
	result := make(map[string]*MatchMarkets)

	for _, spec := range specs {
		key := spec.MatchKey()

		mm, exists := result[key]
		if !exists {
			mm = &MatchMarkets{
				League:    spec.League,
				MatchDate: spec.MatchDate,
				HomeTeam:  spec.HomeTeam,
				AwayTeam:  spec.AwayTeam,
			}
			result[key] = mm
		}

		switch spec.Kind {
		case MarketKindHomeWin:
			mm.HomeWin = spec
		case MarketKindDraw:
			mm.Draw = spec
		case MarketKindAwayWin:
			mm.AwayWin = spec
		case MarketKindBTTS:
			mm.BTTS = append(mm.BTTS, spec)
		case MarketKindTotal:
			mm.Totals = append(mm.Totals, spec)
		case MarketKindSpread:
			mm.Spreads = append(mm.Spreads, spec)
		}
	}

	return result
}
