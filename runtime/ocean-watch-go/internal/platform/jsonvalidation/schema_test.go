package jsonvalidation

import (
	"encoding/json"
	"testing"
)

func TestExactNumbersValidateWithoutChangingWireValue(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"id":{"type":"integer","maximum":9007199254740993},"amount":{"type":"number","multipleOf":0.1}},"required":["id","amount"],"additionalProperties":false}`)
	value := map[string]any{"id": json.Number("9007199254740993"), "amount": json.Number("0.3")}
	if err := Validate(schema, value); err != nil {
		t.Fatal("valid exact decimal or long integer was rejected", err)
	}
	if value["id"].(json.Number).String() != "9007199254740993" || value["amount"].(json.Number).String() != "0.3" {
		t.Fatal("wire numbers were mutated")
	}
	value["id"] = json.Number("9007199254740994")
	if Validate(schema, value) == nil {
		t.Fatal("rounded integer exceeded exact maximum")
	}
	value["id"] = json.Number("1")
	value["amount"] = json.Number("0.31")
	if Validate(schema, value) == nil {
		t.Fatal("invalid exact decimal multiple accepted")
	}
}

func TestValidationRejectsRemoteReferencesAndUnexpectedFields(t *testing.T) {
	for _, schema := range []json.RawMessage{json.RawMessage(`{"type":"object","properties":{"x":{"$ref":"https://example.invalid/schema"}}}`), json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)} {
		if Validate(schema, map[string]any{"x": "value"}) == nil {
			t.Fatal("remote or unknown field was accepted")
		}
	}
}
