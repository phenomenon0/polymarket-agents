package eth

import "github.com/ethereum/go-ethereum/common"

// Polymarket CTF Exchange contract addresses on Polygon.
var (
	// CTFExchangeAddress is the main CTF Exchange contract.
	CTFExchangeAddress = common.HexToAddress("0x4bFb41d5B3570DeFd03C39a9A4D8dE6Bd8B8982E")

	// NegRiskCTFExchangeAddress is the Neg Risk CTF Exchange contract.
	NegRiskCTFExchangeAddress = common.HexToAddress("0xC5d563A36AE78145C45a50134d48A1215220f80a")
)
