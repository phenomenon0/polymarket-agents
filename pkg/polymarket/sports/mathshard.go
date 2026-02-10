package sports

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/shopspring/decimal"
)

// MathShardClient is a client for the MathShard prediction API.
type MathShardClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewMathShardClient creates a new MathShard client.
func NewMathShardClient(baseURL string) *MathShardClient {
	if baseURL == "" {
		baseURL = "http://localhost:8081"
	}
	return &MathShardClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// HealthCheck checks if the MathShard server is healthy.
func (c *MathShardClient) HealthCheck(ctx context.Context) error {
	url := c.baseURL + "/api/health"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unhealthy: status %d", resp.StatusCode)
	}

	return nil
}

// PredictRequest is the request to the MathShard predict API.
type PredictRequest struct {
	League   string     `json:"league"`
	HomeTeam string     `json:"home_team"`
	AwayTeam string     `json:"away_team"`
	Odds     *OddsInput `json:"odds,omitempty"`
}

// OddsInput provides market odds for edge calculation.
type OddsInput struct {
	Home float64 `json:"home"`
	Draw float64 `json:"draw"`
	Away float64 `json:"away"`
}

// PredictMatchResponse is the response from /api/predict-match.
type PredictMatchResponse struct {
	HomeTeam      string  `json:"home_team"`
	AwayTeam      string  `json:"away_team"`
	PHome         float64 `json:"p_home"`
	PDraw         float64 `json:"p_draw"`
	PAway         float64 `json:"p_away"`
	HomeElo       float64 `json:"home_elo"`
	AwayElo       float64 `json:"away_elo"`
	EloDiff       float64 `json:"elo_diff"`
	Model         string  `json:"model"`
	ModelFeatures int     `json:"model_features"`
	Error         string  `json:"error,omitempty"`
}

// Predict gets a prediction for a match using /api/predict-match.
func (c *MathShardClient) Predict(ctx context.Context, req *PredictRequest) (*Prediction, error) {
	// Build URL with query params
	url := fmt.Sprintf("%s/api/predict-match?home=%s&away=%s&league=%s",
		c.baseURL,
		req.HomeTeam,
		req.AwayTeam,
		req.League)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)
		if errResp.Error != "" {
			return nil, fmt.Errorf("%s", errResp.Error)
		}
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var predResp PredictMatchResponse
	if err := json.NewDecoder(resp.Body).Decode(&predResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	if predResp.Error != "" {
		return nil, fmt.Errorf("%s", predResp.Error)
	}

	return &Prediction{
		HomeProb:     decimal.NewFromFloat(predResp.PHome),
		DrawProb:     decimal.NewFromFloat(predResp.PDraw),
		AwayProb:     decimal.NewFromFloat(predResp.PAway),
		Confidence:   decimal.NewFromFloat(0.7), // Default confidence
		ModelVersion: predResp.Model,
		Timestamp:    time.Now(),
		EloDiff:      predResp.EloDiff,
	}, nil
}

// SlateFixture represents a fixture from MathShard's slate.
type SlateFixture struct {
	MatchID    string    `json:"match_id"`
	MatchDate  string    `json:"match_date"`
	Kickoff    string    `json:"kickoff"`
	HomeTeam   string    `json:"home_team"`
	AwayTeam   string    `json:"away_team"`
	HomeTeamID int       `json:"home_team_id"`
	AwayTeamID int       `json:"away_team_id"`
	Status     string    `json:"status"`
	Odds       *OddsData `json:"odds,omitempty"`
}

// OddsData contains odds from various sources.
type OddsData struct {
	Best   *OddsInput `json:"best,omitempty"`
	Bet365 *OddsInput `json:"bet365,omitempty"`
}

// GetSlate gets upcoming fixtures from MathShard.
func (c *MathShardClient) GetSlate(ctx context.Context, league string) ([]SlateFixture, error) {
	url := c.baseURL + "/api/slate"
	if league != "" {
		url += "?league=" + league
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var fixtures []SlateFixture
	if err := json.NewDecoder(resp.Body).Decode(&fixtures); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return fixtures, nil
}

// WeeklyBet represents a recommended bet from MathShard.
type WeeklyBet struct {
	MatchID   string  `json:"match_id"`
	Match     string  `json:"match"`
	Date      string  `json:"date"`
	Pick      string  `json:"pick"` // "HOME", "DRAW", "AWAY"
	ModelProb float64 `json:"model_prob"`
	Prob      float64 `json:"confidence"` // Alias for confidence
	Odds      float64 `json:"odds"`
	Edge      float64 `json:"edge"`
	Stake     float64 `json:"stake"`
	Potential float64 `json:"potential_return"`
	Profit    float64 `json:"potential_profit"`
	Status    string  `json:"status"`
	Rationale string  `json:"rationale"`
}

// WeeklyBetsSummary contains summary stats for the week's bets.
type WeeklyBetsSummary struct {
	TotalBets          int     `json:"total_bets"`
	TotalStake         float64 `json:"total_stake"`
	ExposurePct        float64 `json:"exposure_pct"`
	AvgConfidence      float64 `json:"avg_confidence"`
	AvgOdds            float64 `json:"avg_odds"`
	MaxPotentialProfit float64 `json:"max_potential_profit"`
	MaxPotentialLoss   float64 `json:"max_potential_loss"`
}

// WeeklyBetsResponse is the response from the weekly bets endpoint.
type WeeklyBetsResponse struct {
	Week           int               `json:"week"`
	GeneratedAt    string            `json:"generated_at"`
	BankrollBefore float64           `json:"bankroll_before"`
	StakePerBet    float64           `json:"stake_per_bet"`
	Strategy       string            `json:"strategy"`
	Bets           []WeeklyBet       `json:"bets"`
	Summary        WeeklyBetsSummary `json:"summary"`
}

// GetWeeklyBets gets recommended bets for the week.
func (c *MathShardClient) GetWeeklyBets(ctx context.Context) (*WeeklyBetsResponse, error) {
	url := c.baseURL + "/api/bets/current"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var betsResp WeeklyBetsResponse
	if err := json.NewDecoder(resp.Body).Decode(&betsResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &betsResp, nil
}

// AlphaProvider is the interface for alpha sources.
type AlphaProvider interface {
	// CanScore returns true if this provider can score the given market.
	CanScore(spec *SoccerMarketSpec) bool

	// Score returns the model probability for the market's YES outcome.
	Score(ctx context.Context, spec *SoccerMarketSpec) (*Prediction, error)
}

// MathShardAlphaProvider implements AlphaProvider using MathShard.
type MathShardAlphaProvider struct {
	client *MathShardClient
	cache  map[string]*Prediction // matchKey -> prediction
}

// NewMathShardAlphaProvider creates a new MathShard alpha provider.
func NewMathShardAlphaProvider(client *MathShardClient) *MathShardAlphaProvider {
	return &MathShardAlphaProvider{
		client: client,
		cache:  make(map[string]*Prediction),
	}
}

// CanScore returns true for tradeable soccer markets.
func (p *MathShardAlphaProvider) CanScore(spec *SoccerMarketSpec) bool {
	// Only score 1X2 markets
	if !spec.Kind.IsTradeable() {
		return false
	}

	// Need team info
	if spec.HomeTeam == "" || spec.AwayTeam == "" {
		return false
	}

	// Check league is supported
	config := DefaultLeagueConfigs()
	leagueConf, ok := config[spec.League]
	if !ok || !leagueConf.Enabled {
		return false
	}

	return true
}

// Score returns the probability for the market's YES outcome.
func (p *MathShardAlphaProvider) Score(ctx context.Context, spec *SoccerMarketSpec) (*Prediction, error) {
	if !p.CanScore(spec) {
		return nil, fmt.Errorf("cannot score market: %s", spec.MarketSlug)
	}

	// Check cache (prediction is for the whole match)
	matchKey := spec.MatchKey()
	if cached, ok := p.cache[matchKey]; ok {
		return cached, nil
	}

	// Get league config
	config := DefaultLeagueConfigs()
	leagueConf := config[spec.League]

	// Call MathShard
	pred, err := p.client.Predict(ctx, &PredictRequest{
		League:   leagueConf.MathShardKey,
		HomeTeam: spec.HomeTeam,
		AwayTeam: spec.AwayTeam,
	})
	if err != nil {
		return nil, fmt.Errorf("MathShard prediction failed: %w", err)
	}

	// Cache for other markets in same match
	p.cache[matchKey] = pred

	return pred, nil
}

// ClearCache clears the prediction cache.
func (p *MathShardAlphaProvider) ClearCache() {
	p.cache = make(map[string]*Prediction)
}
