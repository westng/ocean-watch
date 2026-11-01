// Package jsonvalidation validates exact JSON numbers without changing wire
// arguments. json.Number is a string type to reflection-based validators.
package jsonvalidation

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/google/jsonschema-go/jsonschema"
	"math"
	"math/big"
	"net/url"
	"strconv"
	"strings"
)

var ErrSchema = errors.New("JSON schema validation failed")

func Validate(raw json.RawMessage, value any) error {
	var schema jsonschema.Schema
	var exact map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if json.Unmarshal(raw, &schema) != nil || decoder.Decode(&exact) != nil || schema.Type != "object" {
		return ErrSchema
	}
	if err := validateExactNumbers(exact, value); err != nil {
		return err
	}
	// The library applies numeric constraints using float64. Those constraints
	// were already checked with rational arithmetic; avoid re-rounding decimals.
	removeCheckedNumericConstraints(exact, value)
	validationSchema, err := json.Marshal(exact)
	if err != nil || json.Unmarshal(validationSchema, &schema) != nil {
		return ErrSchema
	}
	validation, err := validationValue(value)
	if err != nil {
		return err
	}
	resolved, err := schema.Resolve(&jsonschema.ResolveOptions{Loader: func(*url.URL) (*jsonschema.Schema, error) { return nil, ErrSchema }})
	if err != nil || resolved.Validate(validation) != nil {
		return ErrSchema
	}
	return nil
}

func validationValue(value any) (any, error) {
	switch typed := value.(type) {
	case json.Number:
		if !strings.ContainsAny(typed.String(), ".eE") {
			if integer, err := strconv.ParseInt(typed.String(), 10, 64); err == nil {
				return integer, nil
			}
		}
		number, err := strconv.ParseFloat(typed.String(), 64)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, ErrSchema
		}
		return number, nil
	case map[string]any:
		copy := map[string]any{}
		for name, value := range typed {
			item, err := validationValue(value)
			if err != nil {
				return nil, err
			}
			copy[name] = item
		}
		return copy, nil
	case []any:
		copy := []any{}
		for _, value := range typed {
			item, err := validationValue(value)
			if err != nil {
				return nil, err
			}
			copy = append(copy, item)
		}
		return copy, nil
	default:
		return value, nil
	}
}

func validateExactNumbers(schema map[string]any, value any) error {
	if number, ok := value.(json.Number); ok {
		numeric, valid := new(big.Rat).SetString(number.String())
		if !valid {
			return ErrSchema
		}
		if schema["type"] == "integer" && !numeric.IsInt() {
			return ErrSchema
		}
		if choices, ok := schema["enum"].([]any); ok {
			matched := false
			for _, choice := range choices {
				if raw, ok := choice.(json.Number); ok {
					candidate, valid := new(big.Rat).SetString(raw.String())
					if valid && numeric.Cmp(candidate) == 0 {
						matched = true
						break
					}
				}
			}
			if !matched {
				return ErrSchema
			}
		}
		for _, constraint := range []string{"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf"} {
			raw, present := schema[constraint].(json.Number)
			if !present {
				continue
			}
			bound, valid := new(big.Rat).SetString(raw.String())
			if !valid {
				return ErrSchema
			}
			comparison := numeric.Cmp(bound)
			switch constraint {
			case "minimum":
				if comparison < 0 {
					return ErrSchema
				}
			case "maximum":
				if comparison > 0 {
					return ErrSchema
				}
			case "exclusiveMinimum":
				if comparison <= 0 {
					return ErrSchema
				}
			case "exclusiveMaximum":
				if comparison >= 0 {
					return ErrSchema
				}
			case "multipleOf":
				if bound.Sign() <= 0 || !new(big.Rat).Quo(numeric, bound).IsInt() {
					return ErrSchema
				}
			}
		}
	}
	if object, ok := value.(map[string]any); ok {
		properties, _ := schema["properties"].(map[string]any)
		for name, value := range object {
			child, _ := properties[name].(map[string]any)
			if err := validateExactNumbers(child, value); err != nil {
				return err
			}
		}
	}
	if array, ok := value.([]any); ok {
		child, _ := schema["items"].(map[string]any)
		for _, value := range array {
			if err := validateExactNumbers(child, value); err != nil {
				return err
			}
		}
	}
	return nil
}

func removeCheckedNumericConstraints(schema map[string]any, value any) {
	if _, ok := value.(json.Number); ok {
		for _, name := range []string{"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf"} {
			delete(schema, name)
		}
	}
	if object, ok := value.(map[string]any); ok {
		properties, _ := schema["properties"].(map[string]any)
		for name, childValue := range object {
			child, _ := properties[name].(map[string]any)
			removeCheckedNumericConstraints(child, childValue)
		}
	}
	if array, ok := value.([]any); ok {
		child, _ := schema["items"].(map[string]any)
		for _, childValue := range array {
			removeCheckedNumericConstraints(child, childValue)
		}
	}
}
