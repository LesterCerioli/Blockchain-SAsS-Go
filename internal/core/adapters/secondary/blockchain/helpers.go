package blockchain

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
)

func parseHexInt64(value string) (int64, error) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "0x")
	if value == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(value, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("parse hex int %q: %w", value, err)
	}
	return n, nil
}

func parseHexBigInt(value string) (*big.Int, error) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "0x")
	if value == "" {
		return new(big.Int), nil
	}
	n, ok := new(big.Int).SetString(value, 16)
	if !ok {
		return nil, fmt.Errorf("parse hex big int %q", value)
	}
	return n, nil
}

func formatHexInt64(value int64) string {
	return fmt.Sprintf("0x%x", value)
}

func amountFromAtomicString(value string, decimals int) (*types.Amount, error) {
	return types.NewAmount(value, decimals)
}

func amountFromFloat(value float64, decimals int) *types.Amount {
	str := strconv.FormatFloat(value, 'f', decimals, 64)
	amount, err := types.NewAmountFromDecimal(str, decimals)
	if err != nil {
		return &types.Amount{Value: new(big.Int), Decimals: decimals}
	}
	return amount
}

func mergeFee(gasUsed, gasPrice *big.Int) *types.Amount {
	if gasUsed == nil || gasPrice == nil {
		return nil
	}
	fee := new(big.Int).Mul(gasUsed, gasPrice)
	return &types.Amount{Value: fee, Decimals: 18}
}

func multiplyAmount(amount *types.Amount, multiplier float64) *types.Amount {
	if amount == nil || amount.Value == nil || multiplier == 0 {
		return amount
	}
	rat := new(big.Rat).SetInt(amount.Value)
	rat.Mul(rat, new(big.Rat).SetFloat64(multiplier))
	rat.Quo(rat, new(big.Rat).SetInt64(1))
	value := new(big.Int).Quo(rat.Num(), rat.Denom())
	return &types.Amount{Value: value, Decimals: amount.Decimals}
}
