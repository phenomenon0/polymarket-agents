package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

// Polymarket blocked jurisdictions (as of 2024)
// See: https://polymarket.com/terms
var BlockedCountries = map[string]string{
	"US": "United States",
	"BY": "Belarus",
	"CU": "Cuba",
	"IR": "Iran",
	"KP": "North Korea",
	"RU": "Russia",
	"SY": "Syria",
	"UA": "Ukraine (certain regions)",
	"VE": "Venezuela",
	"MM": "Myanmar",
	"CI": "Ivory Coast",
	"LR": "Liberia",
	"SD": "Sudan",
	"ZW": "Zimbabwe",
	"IQ": "Iraq",
	"LY": "Libya",
	"SO": "Somalia",
	"YE": "Yemen",
}

// GeoBlocker checks if trading is allowed based on jurisdiction.
type GeoBlocker struct {
	httpClient  *http.Client
	mu          sync.RWMutex
	cachedIP    string
	cachedGeo   *GeoInfo
	cacheExpiry time.Time
	cacheTTL    time.Duration
}

// GeoInfo contains geographic information about an IP.
type GeoInfo struct {
	IP          string `json:"ip"`
	Country     string `json:"country"`
	CountryCode string `json:"countryCode"`
	Region      string `json:"regionName"`
	City        string `json:"city"`
	ISP         string `json:"isp"`
	Timezone    string `json:"timezone"`
}

// NewGeoBlocker creates a new geo blocker.
func NewGeoBlocker() *GeoBlocker {
	return &GeoBlocker{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		cacheTTL: 5 * time.Minute,
	}
}

// CheckAllowed checks if trading is allowed from the current location.
func (g *GeoBlocker) CheckAllowed(ctx context.Context) error {
	geo, err := g.GetGeoInfo(ctx)
	if err != nil {
		return fmt.Errorf("failed to get geo info: %w", err)
	}

	if name, blocked := BlockedCountries[geo.CountryCode]; blocked {
		return fmt.Errorf("trading not allowed from %s (%s)", name, geo.CountryCode)
	}

	return nil
}

// GetGeoInfo returns geographic information for the current IP.
func (g *GeoBlocker) GetGeoInfo(ctx context.Context) (*GeoInfo, error) {
	g.mu.RLock()
	if g.cachedGeo != nil && time.Now().Before(g.cacheExpiry) {
		geo := g.cachedGeo
		g.mu.RUnlock()
		return geo, nil
	}
	g.mu.RUnlock()

	// Fetch fresh geo info
	geo, err := g.fetchGeoInfo(ctx)
	if err != nil {
		return nil, err
	}

	g.mu.Lock()
	g.cachedGeo = geo
	g.cacheExpiry = time.Now().Add(g.cacheTTL)
	g.mu.Unlock()

	return geo, nil
}

// CheckIP checks if a specific IP is allowed.
func (g *GeoBlocker) CheckIP(ctx context.Context, ip string) error {
	geo, err := g.fetchGeoInfoForIP(ctx, ip)
	if err != nil {
		return fmt.Errorf("failed to get geo info for IP: %w", err)
	}

	if name, blocked := BlockedCountries[geo.CountryCode]; blocked {
		return fmt.Errorf("IP %s is in blocked jurisdiction: %s (%s)", ip, name, geo.CountryCode)
	}

	return nil
}

// IsBlocked returns true if the country code is blocked.
func IsBlocked(countryCode string) bool {
	_, blocked := BlockedCountries[countryCode]
	return blocked
}

// --- Internal methods ---

func (g *GeoBlocker) fetchGeoInfo(ctx context.Context) (*GeoInfo, error) {
	return g.fetchGeoInfoForIP(ctx, "")
}

func (g *GeoBlocker) fetchGeoInfoForIP(ctx context.Context, ip string) (*GeoInfo, error) {
	// Use ip-api.com (free, no key required, 45 requests/minute)
	url := "http://ip-api.com/json/"
	if ip != "" {
		url += ip
	}
	url += "?fields=status,message,country,countryCode,regionName,city,isp,timezone,query"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Status  string `json:"status"`
		Message string `json:"message,omitempty"`
		GeoInfo
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if result.Status != "success" {
		return nil, fmt.Errorf("geo lookup failed: %s", result.Message)
	}

	geo := &result.GeoInfo
	geo.IP = result.GeoInfo.IP
	if geo.IP == "" {
		// The API uses "query" field for IP
		var rawResult map[string]interface{}
		json.Unmarshal(body, &rawResult)
		if q, ok := rawResult["query"].(string); ok {
			geo.IP = q
		}
	}

	return geo, nil
}

// ValidateWalletJurisdiction checks if a wallet address might be in a blocked jurisdiction.
// This is a heuristic based on transaction patterns and is not definitive.
// For proper compliance, integrate with a KYC provider.
func ValidateWalletJurisdiction(walletAddress string) error {
	// Basic validation - address format
	if len(walletAddress) != 42 || walletAddress[:2] != "0x" {
		return fmt.Errorf("invalid wallet address format")
	}
	// Additional heuristics could be added here:
	// - Check against known sanctioned addresses (OFAC SDN list)
	// - Analyze transaction patterns
	// - Integrate with blockchain analytics providers
	return nil
}

// GetPublicIP returns the current public IP address.
func GetPublicIP(ctx context.Context) (string, error) {
	// Use multiple services for redundancy
	services := []string{
		"https://api.ipify.org",
		"https://icanhazip.com",
		"https://ifconfig.me/ip",
	}

	client := &http.Client{Timeout: 5 * time.Second}

	for _, svc := range services {
		req, err := http.NewRequestWithContext(ctx, "GET", svc, nil)
		if err != nil {
			continue
		}

		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			continue
		}

		ip := string(body)
		// Validate it's an IP
		if net.ParseIP(ip) != nil {
			return ip, nil
		}
	}

	return "", fmt.Errorf("failed to get public IP from all services")
}

// JurisdictionCheck performs a comprehensive jurisdiction check.
type JurisdictionCheck struct {
	Allowed     bool   `json:"allowed"`
	IP          string `json:"ip"`
	Country     string `json:"country"`
	CountryCode string `json:"country_code"`
	Reason      string `json:"reason,omitempty"`
	CheckedAt   string `json:"checked_at"`
}

// PerformJurisdictionCheck runs a full jurisdiction check.
func (g *GeoBlocker) PerformJurisdictionCheck(ctx context.Context) (*JurisdictionCheck, error) {
	check := &JurisdictionCheck{
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
	}

	geo, err := g.GetGeoInfo(ctx)
	if err != nil {
		check.Allowed = false
		check.Reason = fmt.Sprintf("Failed to determine location: %v", err)
		return check, nil
	}

	check.IP = geo.IP
	check.Country = geo.Country
	check.CountryCode = geo.CountryCode

	if name, blocked := BlockedCountries[geo.CountryCode]; blocked {
		check.Allowed = false
		check.Reason = fmt.Sprintf("Trading is not permitted from %s per Polymarket Terms of Service", name)
	} else {
		check.Allowed = true
	}

	return check, nil
}
