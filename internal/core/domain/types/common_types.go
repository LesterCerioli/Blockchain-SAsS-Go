package types

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"
)

type Amount struct {
	Value    *big.Int `json:"value"`
	Decimals int      `json:"decimals"`
}

func NewAmount(value string, decimals int) (*Amount, error) {
	bigVal, ok := new(big.Int).SetString(value, 10)
	if !ok {
		return nil, fmt.Errorf("invalid amount value: %s", value)
	}
	return &Amount{Value: bigVal, Decimals: decimals}, nil
}

func NewAmountFromFloat(value float64, decimals int) *Amount {
	multiplier := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	bigVal := new(big.Int)
	bigVal.SetInt64(int64(value * float64(multiplier.Int64())))
	return &Amount{Value: bigVal, Decimals: decimals}
}

func (a *Amount) ToFloat() float64 {
	if a.Value == nil {
		return 0
	}
	divisor := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(a.Decimals)), nil))
	value := new(big.Float).SetInt(a.Value)
	result, _ := new(big.Float).Quo(value, divisor).Float64()
	return result
}

func (a *Amount) String() string {
	return a.Value.String()
}

func (a *Amount) Add(other *Amount) (*Amount, error) {
	if a.Decimals != other.Decimals {
		return nil, fmt.Errorf("cannot add amounts with different decimals")
	}
	return &Amount{
		Value:    new(big.Int).Add(a.Value, other.Value),
		Decimals: a.Decimals,
	}, nil
}

func NewAmountFromDecimal(value string, decimals int) (*Amount, error) {
	if decimals < 0 || decimals > 36 {
		return nil, fmt.Errorf("invalid decimals: %d", decimals)
	}
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == "-" {
		return nil, fmt.Errorf("invalid amount value: %q", value)
	}

	negative := false
	if len(value) > 0 && value[0] == '-' {
		negative = true
		value = value[1:]
	}

	intPart := value
	fracPart := ""
	if idx := indexByte(value, '.'); idx >= 0 {
		intPart = value[:idx]
		fracPart = value[idx+1:]
	}
	if intPart == "" {
		intPart = "0"
	}
	for len(fracPart) > 0 && fracPart[len(fracPart)-1] == '0' {
		fracPart = fracPart[:len(fracPart)-1]
	}
	if len(fracPart) > decimals {
		return nil, fmt.Errorf("amount %s exceeds %d decimal places", value, decimals)
	}
	for len(fracPart) < decimals {
		fracPart += "0"
	}

	digits := intPart + fracPart
	bigVal, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return nil, fmt.Errorf("invalid amount value: %s", value)
	}
	if negative {
		bigVal.Neg(bigVal)
	}

	return &Amount{Value: bigVal, Decimals: decimals}, nil
}

func (a *Amount) ToDecimalString() string {
	if a == nil || a.Value == nil {
		return "0"
	}

	neg := a.Value.Sign() < 0
	digits := new(big.Int).Abs(a.Value).String()
	if a.Decimals == 0 {
		if neg {
			return "-" + digits
		}
		return digits
	}

	for len(digits) <= a.Decimals {
		digits = "0" + digits
	}
	split := len(digits) - a.Decimals
	result := digits[:split] + "." + digits[split:]
	result = trimTrailingZeros(result)
	if neg {
		result = "-" + result
	}
	return result
}

func (a *Amount) Cmp(other *Amount) int {
	if a == nil && other == nil {
		return 0
	}
	if a == nil {
		return -1
	}
	if other == nil {
		return 1
	}
	if a.Decimals != other.Decimals {
		left := a.scaledTo(maxInt(a.Decimals, other.Decimals))
		right := other.scaledTo(maxInt(a.Decimals, other.Decimals))
		return left.Value.Cmp(right.Value)
	}
	return a.Value.Cmp(other.Value)
}

func (a *Amount) scaledTo(decimals int) *Amount {
	if a.Decimals == decimals {
		return a
	}
	multiplier := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals-a.Decimals)), nil)
	return &Amount{Value: new(big.Int).Mul(a.Value, multiplier), Decimals: decimals}
}

func (a *Amount) IsZero() bool {
	return a == nil || a.Value == nil || a.Value.Sign() == 0
}

func (a *Amount) IsNegative() bool {
	return a != nil && a.Value != nil && a.Value.Sign() < 0
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func trimTrailingZeros(s string) string {
	idx := len(s) - 1
	for idx > 0 && s[idx] == '0' {
		idx--
	}
	if s[idx] == '.' {
		return s[:idx]
	}
	return s[:idx+1]
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

type Currency struct {
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Decimals int    `json:"decimals"`
	Type     string `json:"type"` // native, token, stablecoin, etc.
}

type Price struct {
	Currency  string    `json:"currency"` // USD, EUR, etc.
	Value     float64   `json:"value"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"` // coingecko, binance, etc.
}

type TimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type Pagination struct {
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
	Total    int `json:"total"`
}

type SortOrder string

const (
	SortOrderAsc  SortOrder = "asc"
	SortOrderDesc SortOrder = "desc"
)

type SortBy string

const (
	SortByTimestamp     SortBy = "timestamp"
	SortByAmount        SortBy = "amount"
	SortByFee           SortBy = "fee"
	SortByConfirmations SortBy = "confirmations"
)

type QueryOptions struct {
	Pagination *Pagination `json:"pagination,omitempty"`
	SortBy     SortBy      `json:"sort_by,omitempty"`
	SortOrder  SortOrder   `json:"sort_order,omitempty"`
	TimeRange  *TimeRange  `json:"time_range,omitempty"`
}

type ErrorResponse struct {
	Code      string    `json:"code"`
	Message   string    `json:"message"`
	Details   []string  `json:"details,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	RequestID string    `json:"request_id,omitempty"`
}

type SuccessResponse struct {
	Data      interface{} `json:"data"`
	Timestamp time.Time   `json:"timestamp"`
	RequestID string      `json:"request_id,omitempty"`
}

func (a *Amount) MarshalJSON() ([]byte, error) {
	if a == nil || a.Value == nil {
		return []byte(`{"value":"0","decimals":0}`), nil
	}
	return json.Marshal(map[string]interface{}{
		"value":    a.Value.String(),
		"decimals": a.Decimals,
	})
}

func (a *Amount) UnmarshalJSON(data []byte) error {
	var obj struct {
		Value    string `json:"value"`
		Decimals int    `json:"decimals"`
	}
	if err := json.Unmarshal(data, &obj); err == nil && obj.Value != "" {
		bigVal, ok := new(big.Int).SetString(obj.Value, 10)
		if !ok {
			return fmt.Errorf("invalid amount value: %s", obj.Value)
		}
		a.Value = bigVal
		a.Decimals = obj.Decimals
		return nil
	}

	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}

	bigVal, ok := new(big.Int).SetString(str, 10)
	if !ok {
		return fmt.Errorf("invalid amount value: %s", str)
	}

	a.Value = bigVal
	return nil
}
