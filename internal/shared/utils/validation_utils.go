package utils

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
)

var emailPattern = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

var currencyDecimals = map[string]int{
	"USD":   2,
	"EUR":   2,
	"GBP":   2,
	"BRL":   2,
	"JPY":   0,
	"ETH":   18,
	"BTC":   8,
	"SOL":   9,
	"ADA":   6,
	"USDT":  6,
	"USDC":  6,
	"DAI":   18,
	"BNB":   18,
	"MATIC": 18,
}

var chainSymbols = map[types.BlockchainType]string{
	types.BlockchainEthereum: "ETH",
	types.BlockchainBitcoin:  "BTC",
	types.BlockchainSolana:   "SOL",
	types.BlockchainCardano:  "ADA",
}

func GetCurrencyDecimals(currency string) (int, bool) {
	d, ok := currencyDecimals[strings.ToUpper(currency)]
	return d, ok
}

func NormalizeCurrency(currency string) string {
	return strings.ToUpper(strings.TrimSpace(currency))
}

func NativeSymbol(chain types.BlockchainType) string {
	return chainSymbols[chain]
}

func ValidateEmail(email string) bool {
	return emailPattern.MatchString(strings.TrimSpace(email))
}

func ValidateAmount(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("amount is required")
	}
	if _, ok := new(big.Rat).SetString(value); !ok {
		return fmt.Errorf("amount must be a valid decimal")
	}
	rat, _ := new(big.Rat).SetString(value)
	if rat.Sign() <= 0 {
		return fmt.Errorf("amount must be greater than zero")
	}
	return nil
}

func ValidatePositiveInt(value, max int, field string) error {
	if value <= 0 {
		return fmt.Errorf("%s must be greater than zero", field)
	}
	if max > 0 && value > max {
		return fmt.Errorf("%s must be at most %d", field, max)
	}
	return nil
}

func NormalizeBlockchain(value string) (types.BlockchainType, error) {
	v := strings.ToLower(strings.TrimSpace(value))
	switch v {
	case "ethereum", "eth", "erc20", "erc721":
		return types.BlockchainEthereum, nil
	case "bitcoin", "btc":
		return types.BlockchainBitcoin, nil
	case "solana", "sol":
		return types.BlockchainSolana, nil
	case "cardano", "ada":
		return types.BlockchainCardano, nil
	case "polygon", "matic":
		return types.BlockchainPolygon, nil
	case "bsc", "binance", "binance-smart-chain":
		return types.BlockchainBinanceSmartChain, nil
	default:
		return "", fmt.Errorf("unsupported blockchain: %s", value)
	}
}

func NormalizeNetwork(chain types.BlockchainType, value string) (types.BlockchainNetwork, error) {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" {
		switch chain {
		case types.BlockchainBitcoin:
			return types.NetworkBitcoinMainnet, nil
		case types.BlockchainEthereum:
			return types.NetworkEthereumMainnet, nil
		case types.BlockchainSolana:
			return types.NetworkMainnet, nil
		default:
			return types.NetworkMainnet, nil
		}
	}

	switch v {
	case "mainnet", "main":
		switch chain {
		case types.BlockchainBitcoin:
			return types.NetworkBitcoinMainnet, nil
		case types.BlockchainEthereum:
			return types.NetworkEthereumMainnet, nil
		default:
			return types.NetworkMainnet, nil
		}
	case "testnet", "test":
		switch chain {
		case types.BlockchainBitcoin:
			return types.NetworkBitcoinTestnet, nil
		case types.BlockchainEthereum:
			return types.NetworkEthereumSepolia, nil
		default:
			return types.NetworkTestnet, nil
		}
	case "devnet":
		return types.NetworkDevnet, nil
	case "regtest":
		return types.NetworkBitcoinRegtest, nil
	case "sepolia":
		return types.NetworkEthereumSepolia, nil
	case "goerli":
		return types.NetworkEthereumGoerli, nil
	default:
		return types.BlockchainNetwork(v), nil
	}
}

func AmountFromDecimal(value string, decimals int) (*types.Amount, error) {
	return types.NewAmountFromDecimal(value, decimals)
}
