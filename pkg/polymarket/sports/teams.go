package sports

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

const (
	// Polymarket Sports API base URL
	sportsAPIBase = "https://gamma-api.polymarket.com"

	// Cache TTL
	teamsCacheTTL = 24 * time.Hour
)

// TeamsClient fetches and caches team data from Polymarket.
type TeamsClient struct {
	httpClient *http.Client
	baseURL    string

	mu          sync.RWMutex
	teams       map[string]*Team   // ID -> Team
	byName      map[string]*Team   // normalized name -> Team
	byAbbrev    map[string]*Team   // abbreviation -> Team
	byLeague    map[string][]*Team // league -> Teams
	lastRefresh time.Time
}

// NewTeamsClient creates a new teams client.
func NewTeamsClient() *TeamsClient {
	return &TeamsClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    sportsAPIBase,
		teams:      make(map[string]*Team),
		byName:     make(map[string]*Team),
		byAbbrev:   make(map[string]*Team),
		byLeague:   make(map[string][]*Team),
	}
}

// teamAPIEntry represents a single team from the Polymarket teams API.
type teamAPIEntry struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	Abbreviation string  `json:"abbreviation"`
	Alias        *string `json:"alias"` // Can be null or a single string
	League       string  `json:"league"`
	ProviderId   int     `json:"providerId"`
}

// Refresh fetches fresh team data from the API.
func (c *TeamsClient) Refresh(ctx context.Context) error {
	url := c.baseURL + "/teams"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetching teams: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("teams API returned %d", resp.StatusCode)
	}

	// API returns a flat array of teams
	var apiTeams []teamAPIEntry
	if err := json.NewDecoder(resp.Body).Decode(&apiTeams); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Clear existing caches
	c.teams = make(map[string]*Team)
	c.byName = make(map[string]*Team)
	c.byAbbrev = make(map[string]*Team)
	c.byLeague = make(map[string][]*Team)

	// Build caches
	for _, t := range apiTeams {
		// Convert alias from *string to []string
		var aliases []string
		if t.Alias != nil && *t.Alias != "" {
			aliases = []string{*t.Alias}
		}

		team := &Team{
			ID:           fmt.Sprintf("%d", t.ID),
			Name:         t.Name,
			Abbreviation: t.Abbreviation,
			Aliases:      aliases,
			League:       t.League,
		}

		c.teams[team.ID] = team

		// Index by normalized name
		normName := normalizeName(team.Name)
		c.byName[normName] = team

		// Index by abbreviation (lowercase)
		if team.Abbreviation != "" {
			c.byAbbrev[strings.ToLower(team.Abbreviation)] = team
		}

		// Index by aliases
		for _, alias := range team.Aliases {
			normAlias := normalizeName(alias)
			c.byName[normAlias] = team
		}

		// Index by league
		c.byLeague[team.League] = append(c.byLeague[team.League], team)
	}

	c.lastRefresh = time.Now()
	return nil
}

// TeamCount returns the number of loaded teams.
func (c *TeamsClient) TeamCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.teams)
}

// EnsureLoaded ensures team data is loaded, refreshing if needed.
func (c *TeamsClient) EnsureLoaded(ctx context.Context) error {
	c.mu.RLock()
	needsRefresh := len(c.teams) == 0 || time.Since(c.lastRefresh) > teamsCacheTTL
	c.mu.RUnlock()

	if needsRefresh {
		return c.Refresh(ctx)
	}
	return nil
}

// GetTeam returns a team by ID.
func (c *TeamsClient) GetTeam(id string) (*Team, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	team, ok := c.teams[id]
	return team, ok
}

// FindTeamByName finds a team by name (fuzzy matching).
func (c *TeamsClient) FindTeamByName(name string) (*Team, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	normName := normalizeName(name)
	team, ok := c.byName[normName]
	return team, ok
}

// FindTeamByAbbrev finds a team by abbreviation.
func (c *TeamsClient) FindTeamByAbbrev(abbrev string) (*Team, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	team, ok := c.byAbbrev[strings.ToLower(abbrev)]
	return team, ok
}

// GetTeamsByLeague returns all teams in a league.
func (c *TeamsClient) GetTeamsByLeague(league string) []*Team {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.byLeague[league]
}

// MatchTeamFromQuestion attempts to extract and match team names from a market question.
// Returns (homeTeam, awayTeam, found).
func (c *TeamsClient) MatchTeamFromQuestion(question string) (*Team, *Team, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Common patterns:
	// "Will Manchester United FC win on 2025-12-26?"
	// "Will Manchester United FC vs. Newcastle United FC end in a draw?"
	// "Manchester United FC vs. Newcastle United FC: O/U 2.5"

	// Try to find "X vs Y" or "X vs. Y"
	vsPatterns := []string{" vs. ", " vs ", " v ", " v. "}

	for _, pat := range vsPatterns {
		if idx := strings.Index(question, pat); idx > 0 {
			beforeVs := question[:idx]
			afterVs := question[idx+len(pat):]

			// Clean up the team names
			homeTeamName := extractTeamName(beforeVs)
			awayTeamName := extractTeamName(afterVs)

			homeTeam := c.findBestMatch(homeTeamName)
			awayTeam := c.findBestMatch(awayTeamName)

			if homeTeam != nil && awayTeam != nil {
				return homeTeam, awayTeam, true
			}
		}
	}

	// Try to find single team (for "Will X win?" questions)
	// Look for "Will X win" or "Will X FC win"
	if strings.HasPrefix(question, "Will ") && strings.Contains(question, " win") {
		teamPart := strings.TrimPrefix(question, "Will ")
		if idx := strings.Index(teamPart, " win"); idx > 0 {
			teamName := extractTeamName(teamPart[:idx])
			team := c.findBestMatch(teamName)
			if team != nil {
				// Return just the winning team as home, nil for away
				return team, nil, true
			}
		}
	}

	return nil, nil, false
}

// findBestMatch finds the best matching team for a name (must hold read lock).
func (c *TeamsClient) findBestMatch(name string) *Team {
	normName := normalizeName(name)

	// Exact match
	if team, ok := c.byName[normName]; ok {
		return team
	}

	// Try without common suffixes
	suffixes := []string{" fc", " afc", " united", " city"}
	for _, suffix := range suffixes {
		stripped := strings.TrimSuffix(normName, suffix)
		if team, ok := c.byName[stripped]; ok {
			return team
		}
	}

	// Try partial match
	for key, team := range c.byName {
		if strings.Contains(key, normName) || strings.Contains(normName, key) {
			return team
		}
	}

	return nil
}

// extractTeamName cleans up a team name from a question fragment.
func extractTeamName(s string) string {
	// Remove common suffixes/prefixes
	s = strings.TrimSpace(s)

	// Remove trailing punctuation and common patterns
	cutPatterns := []string{
		" end in a draw?",
		" end in a draw",
		":",
		"?",
		" on 20", // "on 2025-12-26"
	}

	for _, pat := range cutPatterns {
		if idx := strings.Index(s, pat); idx > 0 {
			s = s[:idx]
		}
	}

	return strings.TrimSpace(s)
}

// normalizeName normalizes a team name for matching.
func normalizeName(name string) string {
	// Lowercase
	name = strings.ToLower(name)

	// Remove accents
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	name, _, _ = transform.String(t, name)

	// Remove common suffixes
	name = strings.ReplaceAll(name, " fc", "")
	name = strings.ReplaceAll(name, " afc", "")

	// Normalize spaces
	name = strings.Join(strings.Fields(name), " ")

	return strings.TrimSpace(name)
}

// LoadMathShardTeams loads team mappings from MathShard data.
// This provides a fallback when Polymarket Teams API doesn't have complete data.
func (c *TeamsClient) LoadMathShardTeams(teams map[string]MathShardTeam) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for id, mst := range teams {
		// Create Polymarket-style team entry
		team := &Team{
			ID:           "ms_" + id, // Prefix to distinguish
			Name:         mst.Name,
			Abbreviation: mst.Short,
			League:       "epl", // Assume EPL for now
		}

		// Index by normalized name (don't overwrite Polymarket data)
		normName := normalizeName(team.Name)
		if _, exists := c.byName[normName]; !exists {
			c.byName[normName] = team
		}

		// Index by abbreviation
		if team.Abbreviation != "" {
			abbrev := strings.ToLower(team.Abbreviation)
			if _, exists := c.byAbbrev[abbrev]; !exists {
				c.byAbbrev[abbrev] = team
			}
		}
	}
}

// MathShardTeam represents a team from MathShard's teams.json.
type MathShardTeam struct {
	Name  string `json:"name"`
	Short string `json:"short"`
	Color string `json:"color"`
}
