package eth

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
)

// EIP712Signer handles EIP-712 typed data signing for Polymarket.
type EIP712Signer struct {
	wallet *Wallet
}

// NewEIP712Signer creates a new EIP-712 signer.
func NewEIP712Signer(wallet *Wallet) *EIP712Signer {
	return &EIP712Signer{wallet: wallet}
}

// SignClobAuth signs an authentication message for the CLOB API (L1 auth).
func (s *EIP712Signer) SignClobAuth(chainID int64, timestamp string, nonce *big.Int) (string, error) {
	// EIP-712 domain separator for ClobAuthDomain
	domainSep := hashEIP712Domain("ClobAuthDomain", "1", chainID)

	// Message type hash: ClobAuth(address address,string timestamp,uint256 nonce)
	typeHash := crypto.Keccak256Hash([]byte("ClobAuth(address address,string timestamp,uint256 nonce)"))

	// Encode the message
	addrHash := crypto.Keccak256Hash(s.wallet.Address().Bytes())
	tsHash := crypto.Keccak256Hash([]byte(timestamp))
	nonceHash := common.LeftPadBytes(nonce.Bytes(), 32)

	msgHash := crypto.Keccak256Hash(
		typeHash.Bytes(),
		addrHash.Bytes(),
		tsHash.Bytes(),
		nonceHash,
	)

	// Compute the final hash: \x19\x01 ++ domainSep ++ msgHash
	finalHash := crypto.Keccak256Hash(
		[]byte{0x19, 0x01},
		domainSep.Bytes(),
		msgHash.Bytes(),
	)

	sig, err := s.wallet.SignHash(finalHash.Bytes())
	if err != nil {
		return "", fmt.Errorf("sign auth: %w", err)
	}

	return fmt.Sprintf("0x%x", sig), nil
}

// OrderData contains the data for an order to be signed.
type OrderData struct {
	Salt          *big.Int
	Maker         common.Address
	Signer        common.Address
	Taker         common.Address
	TokenID       *big.Int
	MakerAmount   *big.Int
	TakerAmount   *big.Int
	Expiration    *big.Int
	Nonce         *big.Int
	FeeRateBps    *big.Int
	Side          uint8
	SignatureType uint8
}

// SignOrder signs a Polymarket CTF exchange order using EIP-712.
func (s *EIP712Signer) SignOrder(chainID int64, exchangeAddress common.Address, order *OrderData) (string, error) {
	// Domain separator for the CTF Exchange
	domainSep := hashEIP712DomainWithContract("CTFExchange", "1", chainID, exchangeAddress)

	// Order type hash
	typeHash := crypto.Keccak256Hash([]byte(
		"Order(uint256 salt,address maker,address signer,address taker," +
			"uint256 tokenId,uint256 makerAmount,uint256 takerAmount," +
			"uint256 expiration,uint256 nonce,uint256 feeRateBps," +
			"uint8 side,uint8 signatureType)"))

	// Encode each field as a 32-byte word
	msgHash := crypto.Keccak256Hash(
		typeHash.Bytes(),
		math.U256Bytes(order.Salt),
		common.LeftPadBytes(order.Maker.Bytes(), 32),
		common.LeftPadBytes(order.Signer.Bytes(), 32),
		common.LeftPadBytes(order.Taker.Bytes(), 32),
		math.U256Bytes(order.TokenID),
		math.U256Bytes(order.MakerAmount),
		math.U256Bytes(order.TakerAmount),
		math.U256Bytes(order.Expiration),
		math.U256Bytes(order.Nonce),
		math.U256Bytes(order.FeeRateBps),
		common.LeftPadBytes([]byte{order.Side}, 32),
		common.LeftPadBytes([]byte{order.SignatureType}, 32),
	)

	finalHash := crypto.Keccak256Hash(
		[]byte{0x19, 0x01},
		domainSep.Bytes(),
		msgHash.Bytes(),
	)

	sig, err := s.wallet.SignHash(finalHash.Bytes())
	if err != nil {
		return "", fmt.Errorf("sign order: %w", err)
	}

	return fmt.Sprintf("0x%x", sig), nil
}

// hashEIP712Domain computes the domain separator hash (no verifyingContract).
func hashEIP712Domain(name, version string, chainID int64) common.Hash {
	typeHash := crypto.Keccak256Hash([]byte(
		"EIP712Domain(string name,string version,uint256 chainId)"))

	nameHash := crypto.Keccak256Hash([]byte(name))
	versionHash := crypto.Keccak256Hash([]byte(version))
	chainIDBytes := common.LeftPadBytes(big.NewInt(chainID).Bytes(), 32)

	return crypto.Keccak256Hash(
		typeHash.Bytes(),
		nameHash.Bytes(),
		versionHash.Bytes(),
		chainIDBytes,
	)
}

// hashEIP712DomainWithContract computes the domain separator hash (with verifyingContract).
func hashEIP712DomainWithContract(name, version string, chainID int64, contract common.Address) common.Hash {
	typeHash := crypto.Keccak256Hash([]byte(
		"EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"))

	nameHash := crypto.Keccak256Hash([]byte(name))
	versionHash := crypto.Keccak256Hash([]byte(version))
	chainIDBytes := common.LeftPadBytes(big.NewInt(chainID).Bytes(), 32)

	return crypto.Keccak256Hash(
		typeHash.Bytes(),
		nameHash.Bytes(),
		versionHash.Bytes(),
		chainIDBytes,
		common.LeftPadBytes(contract.Bytes(), 32),
	)
}
