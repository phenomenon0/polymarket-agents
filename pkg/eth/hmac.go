package eth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
)

// APICredentials holds Polymarket L2 API credentials.
type APICredentials struct {
	APIKey     string `json:"apiKey"`
	Secret     string `json:"secret"`
	Passphrase string `json:"passphrase"`
}

// HMACSigner signs requests using HMAC-SHA256 for L2 authentication.
type HMACSigner struct {
	creds *APICredentials
}

// NewHMACSigner creates a new HMAC signer with the given credentials.
func NewHMACSigner(creds *APICredentials) *HMACSigner {
	return &HMACSigner{creds: creds}
}

// SignRequest signs an HTTP request for L2 authentication.
// Returns headers to add to the request.
func (s *HMACSigner) SignRequest(timestamp, method, path string, body []byte, funder string) (map[string]string, error) {
	// Build the message to sign: timestamp + method + path + body
	message := timestamp + method + path
	if len(body) > 0 {
		message += string(body)
	}

	// Decode the base64 secret
	secret, err := base64.URLEncoding.DecodeString(s.creds.Secret)
	if err != nil {
		// Try standard base64
		secret, err = base64.StdEncoding.DecodeString(s.creds.Secret)
		if err != nil {
			return nil, fmt.Errorf("decode secret: %w", err)
		}
	}

	// HMAC-SHA256
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(message))
	signature := base64.URLEncoding.EncodeToString(mac.Sum(nil))

	return map[string]string{
		"POLY_ADDRESS":    funder,
		"POLY_SIGNATURE":  signature,
		"POLY_TIMESTAMP":  timestamp,
		"POLY_API_KEY":    s.creds.APIKey,
		"POLY_PASSPHRASE": s.creds.Passphrase,
	}, nil
}

// L1AuthHeaders returns headers for L1 (EIP-712) authenticated requests.
func L1AuthHeaders(address, signature, timestamp string, nonce int64) map[string]string {
	return map[string]string{
		"POLY_ADDRESS":   address,
		"POLY_SIGNATURE": signature,
		"POLY_TIMESTAMP": timestamp,
		"POLY_NONCE":     strconv.FormatInt(nonce, 10),
	}
}
