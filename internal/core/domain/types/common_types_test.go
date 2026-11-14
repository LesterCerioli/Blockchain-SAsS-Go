package types

import (
	"encoding/json"
	"testing"
)

func TestNewAmountFromDecimal(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		decimals int
		want     string
		wantErr  bool
	}{
		{name: "ether", value: "1.5", decimals: 18, want: "1.5"},
		{name: "bitcoin", value: "0.00000001", decimals: 8, want: "0.00000001"},
		{name: "integer", value: "42", decimals: 2, want: "42"},
		{name: "trims zeros", value: "1.2300", decimals: 4, want: "1.23"},
		{name: "negative", value: "-1", decimals: 2, want: "-1"},
		{name: "too many decimals", value: "1.001", decimals: 2, wantErr: true},
		{name: "not a number", value: "abc", decimals: 2, wantErr: true},
		{name: "empty", value: "", decimals: 2, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			amount, err := NewAmountFromDecimal(tt.value, tt.decimals)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got amount %v", amount)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := amount.ToDecimalString(); got != tt.want {
				t.Fatalf("ToDecimalString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAmountJSONRoundTrip(t *testing.T) {
	amount, err := NewAmountFromDecimal("123.456789", 6)
	if err != nil {
		t.Fatalf("NewAmountFromDecimal: %v", err)
	}

	encoded, err := json.Marshal(amount)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded Amount
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Cmp(amount) != 0 {
		t.Fatalf("round trip mismatch: got %s, want %s", decoded.ToDecimalString(), amount.ToDecimalString())
	}
	if decoded.Decimals != 6 {
		t.Fatalf("decimals = %d, want 6", decoded.Decimals)
	}
}

func TestAmountComparison(t *testing.T) {
	a, _ := NewAmountFromDecimal("1.5", 8)
	b, _ := NewAmountFromDecimal("1.25", 8)
	c, _ := NewAmountFromDecimal("1.5", 8)

	if a.Cmp(b) <= 0 {
		t.Fatal("expected a > b")
	}
	if a.Cmp(c) != 0 {
		t.Fatal("expected a == c")
	}
	if a.IsZero() {
		t.Fatal("expected a not to be zero")
	}
	zero, _ := NewAmount("0", 2)
	if !zero.IsZero() {
		t.Fatal("expected zero amount")
	}
	negative, _ := NewAmount("-5", 2)
	if !negative.IsNegative() {
		t.Fatal("expected negative amount")
	}
}
