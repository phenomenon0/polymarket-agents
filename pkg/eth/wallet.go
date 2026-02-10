// Package eth provides Ethereum wallet and signing utilities for Polymarket.
package eth

import (
	"crypto/ecdsa"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Wallet wraps an ECDSA private key for Ethereum signing.
type Wallet struct {
	privateKey *ecdsa.PrivateKey
	address    common.Address
}

// NewWallet creates a wallet from a hex-encoded private key.
func NewWallet(hexKey string) (*Wallet, error) {
	hexKey = strings.TrimPrefix(hexKey, "0x")

	key, err := crypto.HexToECDSA(hexKey)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}

	addr := crypto.PubkeyToAddress(key.PublicKey)

	return &Wallet{
		privateKey: key,
		address:    addr,
	}, nil
}

// Address returns the wallet's Ethereum address.
func (w *Wallet) Address() common.Address {
	return w.address
}

// AddressHex returns the wallet address as a checksummed hex string.
func (w *Wallet) AddressHex() string {
	return w.address.Hex()
}

// PrivateKey returns the underlying ECDSA private key.
func (w *Wallet) PrivateKey() *ecdsa.PrivateKey {
	return w.privateKey
}

// SignHash signs a 32-byte hash and returns the 65-byte signature.
func (w *Wallet) SignHash(hash []byte) ([]byte, error) {
	sig, err := crypto.Sign(hash, w.privateKey)
	if err != nil {
		return nil, fmt.Errorf("sign hash: %w", err)
	}
	// Adjust V value from 0/1 to 27/28 (EIP-155)
	if sig[64] < 27 {
		sig[64] += 27
	}
	return sig, nil
}
