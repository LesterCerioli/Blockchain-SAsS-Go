package types

import (
	"encoding/json"
	"fmt"
	"math/big"
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
	return json.Marshal(a.Value.String())
}

func (a *Amount) UnmarshalJSON(data []byte) error {
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
