package domain

import (
	"encoding/json"
	"testing"
)

func TestDecimalArithmeticAndBankersRoundingRemainExact(t *testing.T) {
	value := MustDecimal("1e-1").Add(MustDecimal("2E-1"))
	if value.String() != "0.3" {
		t.Fatalf("decimal sum = %s", value.String())
	}
	if got := MustDecimal("1.005").Round(2).StringFixed(2); got != "1.00" {
		t.Fatalf("half-even down = %s", got)
	}
	if got := MustDecimal("1.015").Round(2).StringFixed(2); got != "1.02" {
		t.Fatalf("half-even up = %s", got)
	}
	payload, err := json.Marshal(value)
	if err != nil || string(payload) != "0.3" {
		t.Fatalf("decimal JSON = %s, %v", payload, err)
	}
}
