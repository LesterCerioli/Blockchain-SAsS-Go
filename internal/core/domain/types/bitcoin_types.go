package types

import (
	"encoding/json"
)

type BitcoinTxVersion int32

const (
	BitcoinTxVersion1 BitcoinTxVersion = 1
	BitcoinTxVersion2 BitcoinTxVersion = 2
)

type BitcoinScriptType string

const (
	ScriptTypeP2PKH  BitcoinScriptType = "p2pkh"
	ScriptTypeP2SH   BitcoinScriptType = "p2sh"
	ScriptTypeP2WPKH BitcoinScriptType = "p2wpkh"
	ScriptTypeP2WSH  BitcoinScriptType = "p2wsh"
	ScriptTypeP2TR   BitcoinScriptType = "p2tr"
)

type BitcoinLockTimeType string

const (
	LockTimeBlockHeight BitcoinLockTimeType = "block_height"
	LockTimeTimestamp   BitcoinLockTimeType = "timestamp"
)

type BitcoinWitness struct {
	Components []string `json:"components"`
}

type BitcoinUTXO struct {
	TxID          string  `json:"tx_id"`
	Vout          uint32  `json:"vout"`
	Address       string  `json:"address"`
	Amount        float64 `json:"amount"`
	ScriptPubKey  string  `json:"script_pub_key"`
	Confirmations int     `json:"confirmations"`
	Spendable     bool    `json:"spendable"`
	Solvable      bool    `json:"solvable"`
	Safe          bool    `json:"safe"`
}

type BitcoinFeeEstimate struct {
	FeeRate            float64 `json:"fee_rate"`            // satoshi per byte
	Fee                float64 `json:"fee"`                 // fee total in BTC
	ConfirmationTarget int     `json:"confirmation_target"` // blocks
	EstimatedTime      int     `json:"estimated_time"`      // minutes
}

type BitcoinAddressType string

const (
	AddressTypeP2PKH   BitcoinAddressType = "p2pkh"
	AddressTypeP2SH    BitcoinAddressType = "p2sh"
	AddressTypeP2WPKH  BitcoinAddressType = "p2wpkh"
	AddressTypeP2WSH   BitcoinAddressType = "p2wsh"
	AddressTypeP2TR    BitcoinAddressType = "p2tr"
	AddressTypeBech32  BitcoinAddressType = "bech32"
	AddressTypeBech32m BitcoinAddressType = "bech32m"
)

type BitcoinAddressInfo struct {
	Address      string             `json:"address"`
	Type         BitcoinAddressType `json:"type"`
	ScriptPubKey string             `json:"script_pub_key"`
	IsMine       bool               `json:"is_mine"`
	IsWatchOnly  bool               `json:"is_watch_only"`
	IsScript     bool               `json:"is_script"`
	IsChange     bool               `json:"is_change"`
	IsValid      bool               `json:"is_valid"`
}

func (s BitcoinScriptType) String() string {
	return string(s)
}

func (s BitcoinScriptType) IsSegWit() bool {
	return s == ScriptTypeP2WPKH || s == ScriptTypeP2WSH || s == ScriptTypeP2TR
}

func (s BitcoinScriptType) IsTaproot() bool {
	return s == ScriptTypeP2TR
}

func GetBitcoinNetworkInfo(network BlockchainNetwork) BlockchainInfo {
	baseInfo := GetBlockchainInfo(BlockchainBitcoin, network)

	switch network {
	case NetworkBitcoinMainnet:
		baseInfo.Name = "Bitcoin Mainnet"
		baseInfo.ExplorerURL = "https://blockstream.info"
	case NetworkBitcoinTestnet:
		baseInfo.Name = "Bitcoin Testnet"
		baseInfo.ExplorerURL = "https://blockstream.info/testnet"
	case NetworkBitcoinRegtest:
		baseInfo.Name = "Bitcoin Regtest"
		baseInfo.BlockTime = 1.0 // Mais rápido para testes
	case NetworkBitcoinSignet:
		baseInfo.Name = "Bitcoin Signet"
		baseInfo.ExplorerURL = "https://explorer.bc-2.jp"
	default:

	}

	return baseInfo
}

func ValidateBitcoinAddress(address string, network BlockchainNetwork) bool {

	if len(address) < 26 || len(address) > 90 {
		return false
	}

	switch network {
	case NetworkBitcoinMainnet:
		return address[0] == '1' || address[0] == '3' || address[:3] == "bc1"
	case NetworkBitcoinTestnet, NetworkBitcoinSignet:
		return address[0] == 'm' || address[0] == 'n' || address[0] == '2' || address[:4] == "tb1"
	case NetworkBitcoinRegtest:
		return address[0] == 'm' || address[0] == 'n' || address[0] == '2' || address[:4] == "bcrt1"
	default:

		return len(address) >= 26 && len(address) <= 90
	}
}

func (v BitcoinTxVersion) MarshalJSON() ([]byte, error) {
	return json.Marshal(int32(v))
}

func (v *BitcoinTxVersion) UnmarshalJSON(data []byte) error {
	var i int32
	if err := json.Unmarshal(data, &i); err != nil {
		return err
	}

	*v = BitcoinTxVersion(i)
	return nil
}

func CalculateSize(inputCount, outputCount int, hasWitness bool) int {
	baseSize := 10 + (inputCount * 150) + (outputCount * 50)
	if hasWitness {

		baseSize += inputCount * 50
	}
	return baseSize
}

func EstimateFee(size int, feeRate float64) float64 {

	return float64(size) * feeRate / 100000000
}
