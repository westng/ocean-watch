package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

type Decimal struct {
	value *big.Rat
}

func ParseDecimal(value string) (Decimal, error) {
	text := strings.TrimSpace(value)
	if text == "" {
		return Decimal{}, nil
	}
	exponent := 0
	if index := strings.IndexAny(text, "eE"); index >= 0 {
		if strings.ContainsAny(text[index+1:], "eE") {
			return Decimal{}, errors.New("invalid decimal value")
		}
		parsedExponent, err := strconv.Atoi(text[index+1:])
		if err != nil || parsedExponent > 10000 || parsedExponent < -10000 {
			return Decimal{}, errors.New("invalid decimal value")
		}
		exponent = parsedExponent
		text = text[:index]
	}
	negative := false
	if strings.HasPrefix(text, "+") || strings.HasPrefix(text, "-") {
		negative = text[0] == '-'
		text = text[1:]
	}
	parts := strings.Split(text, ".")
	if len(parts) > 2 || text == "" {
		return Decimal{}, errors.New("invalid decimal value")
	}
	integerPart := parts[0]
	fractionPart := ""
	if len(parts) == 2 {
		fractionPart = parts[1]
	}
	if integerPart == "" {
		integerPart = "0"
	}
	digits := integerPart + fractionPart
	for _, digit := range digits {
		if digit < '0' || digit > '9' {
			return Decimal{}, errors.New("invalid decimal value")
		}
	}
	numerator, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return Decimal{}, errors.New("invalid decimal value")
	}
	if negative {
		numerator.Neg(numerator)
	}
	scale := len(fractionPart) - exponent
	if scale < 0 {
		numerator.Mul(numerator, decimalPower(-scale))
		return Decimal{value: new(big.Rat).SetInt(numerator)}, nil
	}
	return Decimal{value: new(big.Rat).SetFrac(numerator, decimalPower(scale))}, nil
}

func DecimalFromFloat64(value float64) (Decimal, error) {
	text := strconv.FormatFloat(value, 'f', -1, 64)
	if text == "NaN" || text == "+Inf" || text == "-Inf" {
		return Decimal{}, errors.New("invalid non-finite decimal value")
	}
	return ParseDecimal(text)
}

func MustDecimal(value string) Decimal {
	result, err := ParseDecimal(value)
	if err != nil {
		panic(err)
	}
	return result
}

func (value Decimal) rat() *big.Rat {
	if value.value == nil {
		return new(big.Rat)
	}
	return new(big.Rat).Set(value.value)
}

func (value Decimal) Add(other Decimal) Decimal {
	return Decimal{value: new(big.Rat).Add(value.rat(), other.rat())}
}

func (value Decimal) Divide(other Decimal) (Decimal, error) {
	if other.Sign() == 0 {
		return Decimal{}, errors.New("cannot divide decimal by zero")
	}
	return Decimal{value: new(big.Rat).Quo(value.rat(), other.rat())}, nil
}

func (value Decimal) Sign() int {
	return value.rat().Sign()
}

func (value Decimal) Compare(other Decimal) int {
	return value.rat().Cmp(other.rat())
}

func (value Decimal) Int64Exact() (int64, error) {
	rational := value.rat()
	if rational.Denom().Cmp(big.NewInt(1)) != 0 || !rational.Num().IsInt64() {
		return 0, errors.New("decimal is not an exact int64")
	}
	return rational.Num().Int64(), nil
}

func (value Decimal) Round(scale int) Decimal {
	if scale < 0 {
		panic("decimal scale must not be negative")
	}
	power := decimalPower(scale)
	scaled := new(big.Rat).Mul(value.rat(), new(big.Rat).SetInt(power))
	numerator := new(big.Int).Set(scaled.Num())
	denominator := new(big.Int).Set(scaled.Denom())
	quotient := new(big.Int)
	remainder := new(big.Int)
	quotient.QuoRem(numerator, denominator, remainder)
	absRemainder := new(big.Int).Abs(new(big.Int).Set(remainder))
	twiceRemainder := new(big.Int).Lsh(absRemainder, 1)
	comparison := twiceRemainder.Cmp(denominator)
	if comparison > 0 || comparison == 0 && new(big.Int).Abs(new(big.Int).Set(quotient)).Bit(0) == 1 {
		if numerator.Sign() < 0 {
			quotient.Sub(quotient, big.NewInt(1))
		} else {
			quotient.Add(quotient, big.NewInt(1))
		}
	}
	return Decimal{value: new(big.Rat).SetFrac(quotient, power)}
}

func decimalPower(scale int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)
}

func (value Decimal) StringFixed(scale int) string {
	return value.Round(scale).rat().FloatString(scale)
}

func (value Decimal) String() string {
	text := value.StringFixed(12)
	text = strings.TrimRight(text, "0")
	text = strings.TrimRight(text, ".")
	if text == "" || text == "-0" {
		return "0"
	}
	return text
}

func (value Decimal) MarshalJSON() ([]byte, error) {
	return []byte(value.String()), nil
}

func (value *Decimal) UnmarshalJSON(payload []byte) error {
	if value == nil {
		return errors.New("cannot unmarshal decimal into nil receiver")
	}
	trimmed := bytes.TrimSpace(payload)
	if bytes.Equal(trimmed, []byte("null")) {
		*value = Decimal{}
		return nil
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil {
		return fmt.Errorf("decode decimal: %w", err)
	}
	parsed, err := ParseDecimal(number.String())
	if err != nil {
		return err
	}
	*value = parsed
	return nil
}
