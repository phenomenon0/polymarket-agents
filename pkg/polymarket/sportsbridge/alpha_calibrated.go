package sportsbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
)

// ModelMode selects the alpha source used by sportsbridge.
type ModelMode string

const (
	ModelV0        ModelMode = "v0"        // normalized market (no alpha)
	ModelV0Blend   ModelMode = "v0blend"   // log-space blend of market + mathshard
	ModelMathShard ModelMode = "mathshard" // raw mathshard (legacy/unsafe)
	ModelV1        ModelMode = "v1"        // temperature-scaled market probs (trained calibration)
)

// V1CalibrationParams holds the trained calibration parameters.
type V1CalibrationParams struct {
	Method      string  `json:"method"`      // "temperature" or "platt"
	Temperature float64 `json:"temperature"` // for temperature scaling
	A           float64 `json:"a"`           // for platt scaling
	B           float64 `json:"b"`           // for platt scaling
}

// DefaultV1Params returns the default V1 calibration params (from training on EPL data).
// Temperature = 1.046 means market is ~4.6% overconfident.
func DefaultV1Params() V1CalibrationParams {
	return V1CalibrationParams{
		Method:      "temperature",
		Temperature: 1.0464,
	}
}

// LoadV1Params loads V1 calibration params from a JSON file.
func LoadV1Params(path string) (V1CalibrationParams, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return V1CalibrationParams{}, err
	}

	var raw struct {
		Version string          `json:"version"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return V1CalibrationParams{}, err
	}

	params := V1CalibrationParams{Method: raw.Method}

	switch raw.Method {
	case "temperature":
		var p struct {
			Temperature float64 `json:"temperature"`
		}
		json.Unmarshal(raw.Params, &p)
		params.Temperature = p.Temperature
	case "platt":
		var p struct {
			A float64 `json:"a"`
			B float64 `json:"b"`
		}
		json.Unmarshal(raw.Params, &p)
		params.A = p.A
		params.B = p.B
	}

	return params, nil
}

// Prob3Source provides 3-way probabilities for a match.
// This interface allows for easy testing and swapping of probability sources.
type Prob3Source interface {
	// Prob3 returns the 3-way probabilities for a match group.
	Prob3(ctx context.Context, mc *MatchContracts) (Prob3, error)
}

// CalibratedProvider wraps MathShard and market prices to produce calibrated q's.
// It implements AlphaProvider and can operate in four modes:
//   - v0: returns normalized market probabilities (no edge by design)
//   - v0blend: blends market + MathShard in log-space (safe, recommended)
//   - mathshard: returns raw MathShard probabilities (legacy, may fight market)
//   - v1: temperature-scaled market probabilities (trained on historical data)
type CalibratedProvider struct {
	Mode  ModelMode
	Alpha float64 // only used for v0blend (0 = market, 1 = mathshard)

	ms       Prob3Source         // MathShard probability source
	v1Params V1CalibrationParams // V1 calibration params

	// Cache for match groups (needed to get 3-way market probs)
	matchGroups map[string]*MatchContracts
}

// NewCalibratedProvider creates a calibrated alpha provider.
func NewCalibratedProvider(mode ModelMode, alpha float64, ms Prob3Source) *CalibratedProvider {
	if alpha < 0 {
		alpha = 0
	}
	if alpha > 1 {
		alpha = 1
	}
	return &CalibratedProvider{
		Mode:        mode,
		Alpha:       alpha,
		ms:          ms,
		v1Params:    DefaultV1Params(),
		matchGroups: make(map[string]*MatchContracts),
	}
}

// WithV1Params sets custom V1 calibration parameters.
func (p *CalibratedProvider) WithV1Params(params V1CalibrationParams) *CalibratedProvider {
	p.v1Params = params
	return p
}

// SetMatchGroups sets the match groups for 3-way normalization.
// This must be called before scoring contracts.
func (p *CalibratedProvider) SetMatchGroups(groups []*MatchContracts) {
	p.matchGroups = make(map[string]*MatchContracts)
	for _, g := range groups {
		p.matchGroups[g.MatchKey] = g
	}
}

// Name implements AlphaProvider.
func (p *CalibratedProvider) Name() string {
	switch p.Mode {
	case ModelV0:
		return "v0"
	case ModelV0Blend:
		return fmt.Sprintf("v0blend(alpha=%.2f)", p.Alpha)
	case ModelMathShard:
		return "mathshard"
	case ModelV1:
		return fmt.Sprintf("v1(T=%.3f)", p.v1Params.Temperature)
	default:
		return "unknown"
	}
}

// CanScore implements AlphaProvider.
func (p *CalibratedProvider) CanScore(c *Contract) bool {
	if c == nil || c.Event == nil {
		return false
	}
	// Only handle soccer 1X2 events
	_, ok := c.Event.(Soccer1X2Event)
	return ok
}

// Score implements AlphaProvider.
// It returns a calibrated probability based on the selected mode.
func (p *CalibratedProvider) Score(ctx context.Context, c *Contract) (*ScoreResult, error) {
	event, ok := c.Event.(Soccer1X2Event)
	if !ok {
		return nil, fmt.Errorf("not a soccer 1X2 event")
	}

	matchKey := MatchKey(c)
	mc, ok := p.matchGroups[matchKey]
	if !ok {
		return nil, fmt.Errorf("match group not found for %s", matchKey)
	}

	// Get market-normalized 3-way probs from the contract group
	pMkt, err := marketProbsFromGroup(mc)
	if err != nil {
		return nil, fmt.Errorf("market probs: %w", err)
	}

	// Get MathShard 3-way probs (if needed)
	var pMS Prob3
	if p.Mode == ModelMathShard || p.Mode == ModelV0Blend {
		pMS, err = p.ms.Prob3(ctx, mc)
		if err != nil {
			return nil, fmt.Errorf("mathshard probs: %w", err)
		}
	}

	// Compute final q distribution based on mode
	var q Prob3
	switch p.Mode {
	case ModelV0:
		q = pMkt
	case ModelMathShard:
		q = pMS
	case ModelV0Blend:
		// Blend market and MathShard probabilities in log-space
		qBlend, err := blendLogSpace(pMkt, pMS, p.Alpha)
		if err != nil {
			return nil, fmt.Errorf("blend: %w", err)
		}
		q = qBlend

		// Apply agree-direction gate for v0blend
		// If MathShard and blended disagree on direction vs market, neutralize
		if !agreeDirectionGate(event.Outcome, pMkt, pMS, q) {
			// Neutralize: return market prob for this outcome (no edge)
			q = pMkt
		}
	case ModelV1:
		// Apply temperature scaling to market probs
		q = applyTemperatureScaling(pMkt, p.v1Params.Temperature)
	default:
		return nil, fmt.Errorf("unknown mode: %s", p.Mode)
	}

	// Get probability for this specific outcome
	qOut := q.ProbFor(event.Outcome)

	// Clamp to avoid edge cases
	qOut = clamp01(qOut)

	return &ScoreResult{
		Q:          qOut,
		Confidence: 1.0,
		Aux: map[string]any{
			"provider":   p.Name(),
			"mode":       string(p.Mode),
			"mkt_home":   pMkt.Home,
			"mkt_draw":   pMkt.Draw,
			"mkt_away":   pMkt.Away,
			"model_home": pMS.Home,
			"model_draw": pMS.Draw,
			"model_away": pMS.Away,
			"q_home":     q.Home,
			"q_draw":     q.Draw,
			"q_away":     q.Away,
		},
	}, nil
}

// marketProbsFromGroup extracts and normalizes 3-way probs from a match group.
func marketProbsFromGroup(mc *MatchContracts) (Prob3, error) {
	var h, d, a float64
	var okH, okD, okA bool

	if mc.HomeWin != nil {
		h = mc.HomeWin.MidPx
		okH = true
	}
	if mc.Draw != nil {
		d = mc.Draw.MidPx
		okD = true
	}
	if mc.AwayWin != nil {
		a = mc.AwayWin.MidPx
		okA = true
	}

	if !okH || !okD || !okA {
		return Prob3{}, fmt.Errorf("incomplete 1X2 markets for match=%s (home=%v draw=%v away=%v)",
			mc.MatchKey, okH, okD, okA)
	}

	// Normalize to sum to 1
	p := Prob3{Home: h, Draw: d, Away: a}
	return p.Normalize(), nil
}

// agreeDirectionGate checks if MathShard and blended q agree on direction vs market.
// This prevents betting when the model disagrees with the market on which way to move.
//
// For example: if market says home=0.79 and MathShard says home=0.61 (disagreement),
// but the blend says home=0.77, we should NOT bet on home because MathShard thinks
// it's overpriced, yet the edge calculation would show positive edge.
//
// Returns true if it's safe to use the blended probability.
func agreeDirectionGate(outcome Outcome, pMkt, pMS, q Prob3) bool {
	mktP := pMkt.ProbFor(outcome)
	msP := pMS.ProbFor(outcome)
	qP := q.ProbFor(outcome)

	// Direction of MathShard vs market
	msDirection := msP - mktP // positive = MS thinks higher, negative = MS thinks lower

	// Direction of blended vs market
	qDirection := qP - mktP // positive = blend thinks higher

	// Check agreement: both should have same sign
	// If MS thinks lower but blend is higher than market -> disagreement
	// If MS thinks higher but blend is lower than market -> disagreement
	if msDirection > 0.001 && qDirection < -0.001 {
		return false
	}
	if msDirection < -0.001 && qDirection > 0.001 {
		return false
	}

	// Also check: if MS thinks it's overpriced (msP < mktP), we shouldn't bet YES
	// The blend might still show small positive edge due to rounding, but MS disagrees
	if msP < mktP-0.01 {
		// MathShard thinks this outcome is overpriced by market
		// Only allow betting if blend also agrees (q < mkt)
		if qP > mktP {
			return false
		}
	}

	return true
}

// clamp01 clamps probability to [0.0001, 0.9999] to avoid log(0) and div-by-zero.
func clamp01(x float64) float64 {
	if x < 0.0001 {
		return 0.0001
	}
	if x > 0.9999 {
		return 0.9999
	}
	return x
}

// applyTemperatureScaling applies temperature scaling to 3-way probabilities.
// Temperature > 1 reduces confidence (spreads probs toward 1/3).
// Temperature < 1 increases confidence (sharpens probs).
//
// Formula: calibrated_logit = logit / T, then softmax.
func applyTemperatureScaling(p Prob3, T float64) Prob3 {
	if T <= 0 {
		T = 1.0
	}

	// Clamp to avoid log(0)
	eps := 1e-7
	h := clampFloat(p.Home, eps, 1-eps)
	d := clampFloat(p.Draw, eps, 1-eps)
	a := clampFloat(p.Away, eps, 1-eps)

	// Convert to logits (log probs)
	logH := math.Log(h)
	logD := math.Log(d)
	logA := math.Log(a)

	// Apply temperature
	scaledH := logH / T
	scaledD := logD / T
	scaledA := logA / T

	// Softmax back to probs
	maxLog := math.Max(scaledH, math.Max(scaledD, scaledA))
	expH := math.Exp(scaledH - maxLog)
	expD := math.Exp(scaledD - maxLog)
	expA := math.Exp(scaledA - maxLog)
	sum := expH + expD + expA

	return Prob3{
		Home: expH / sum,
		Draw: expD / sum,
		Away: expA / sum,
	}
}

func clampFloat(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

// blendLogSpace blends two Prob3 distributions in log-space.
// alpha=0 returns p1 (market), alpha=1 returns p2 (model).
func blendLogSpace(p1, p2 Prob3, alpha float64) (Prob3, error) {
	eps := 1e-7
	h1 := clampFloat(p1.Home, eps, 1-eps)
	d1 := clampFloat(p1.Draw, eps, 1-eps)
	a1 := clampFloat(p1.Away, eps, 1-eps)

	h2 := clampFloat(p2.Home, eps, 1-eps)
	d2 := clampFloat(p2.Draw, eps, 1-eps)
	a2 := clampFloat(p2.Away, eps, 1-eps)

	// Blend in log-space: log(q) = (1-alpha)*log(p1) + alpha*log(p2)
	logH := (1-alpha)*math.Log(h1) + alpha*math.Log(h2)
	logD := (1-alpha)*math.Log(d1) + alpha*math.Log(d2)
	logA := (1-alpha)*math.Log(a1) + alpha*math.Log(a2)

	// Softmax back to probs
	maxLog := math.Max(logH, math.Max(logD, logA))
	expH := math.Exp(logH - maxLog)
	expD := math.Exp(logD - maxLog)
	expA := math.Exp(logA - maxLog)
	sum := expH + expD + expA

	return Prob3{
		Home: expH / sum,
		Draw: expD / sum,
		Away: expA / sum,
	}, nil
}
