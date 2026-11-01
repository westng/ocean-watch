package officialmcp

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strings"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/jsonvalidation"
)

// CheckSchemaCompatibility separates source changes from the frozen contract.
// Optional additions never expand accepted Host parameters. Changes whose
// semantic meaning cannot be proved (including description changes) fail shut.
func CheckSchemaCompatibility(approved, current json.RawMessage) (string, error) {
	var oldSchema, newSchema map[string]any
	if decodeSchema(approved, &oldSchema) != nil || decodeSchema(current, &newSchema) != nil || oldSchema == nil || newSchema == nil {
		return "", ErrProbeSchema
	}
	if !compatibleSchema(oldSchema, newSchema) {
		return "", ErrProbeSchema
	}
	if bytes.Equal(approved, current) {
		return "unchanged", nil
	}
	return "compatible_source_change", nil
}

func decodeSchema(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return ErrProbeSchema
	}
	return nil
}

func compatibleSchema(old, new map[string]any) bool {
	if !reflect.DeepEqual(old["type"], new["type"]) {
		return false
	}
	if normalizeDescription(old["description"]) != normalizeDescription(new["description"]) {
		return false
	}
	oldRequired := schemaStringSet(old["required"])
	for required := range schemaStringSet(new["required"]) {
		if !oldRequired[required] {
			return false
		}
	}
	oldProperties, _ := old["properties"].(map[string]any)
	newProperties, _ := new["properties"].(map[string]any)
	for name := range newProperties {
		if _, known := oldProperties[name]; !known && authoritySchemaField(name) {
			return false
		}
	}
	for name, value := range oldProperties {
		oldChild, ok := value.(map[string]any)
		if !ok {
			return false
		}
		newChild, ok := newProperties[name].(map[string]any)
		if !ok || !compatibleSchema(oldChild, newChild) {
			return false
		}
	}
	if oldItems, ok := old["items"].(map[string]any); ok {
		newItems, ok := new["items"].(map[string]any)
		if !ok || !compatibleSchema(oldItems, newItems) {
			return false
		}
	}
	for key, value := range old {
		switch key {
		case "title", "examples", "$id", "$schema", "description", "properties", "required", "items":
			continue
		case "enum":
			oldEnum, ok := value.([]any)
			if !ok {
				return false
			}
			newEnum, ok := new[key].([]any)
			if !ok {
				return false
			}
			for _, candidate := range oldEnum {
				found := false
				for _, current := range newEnum {
					if reflect.DeepEqual(candidate, current) {
						found = true
						break
					}
				}
				if !found {
					return false
				}
			}
		default:
			if !reflect.DeepEqual(canonicalSchemaValue(value), canonicalSchemaValue(new[key])) {
				return false
			}
		}
	}
	// New constraints can invalidate an old request. Metadata and optional
	// properties are the only unconditionally compatible additions.
	for key := range new {
		if _, exists := old[key]; !exists {
			switch key {
			case "title", "examples", "$id", "$schema", "properties", "required":
			default:
				return false
			}
		}
	}
	return true
}

func authoritySchemaField(name string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(name, "_", ""), "-", ""))
	switch normalized {
	case "advertiserid", "advertiserids", "agentid", "agentids", "shopid", "shopids", "organizationid", "organizationids", "ccaccountid", "customercenterid", "accountid", "accountids", "appid", "developerid":
		return true
	}
	return strings.Contains(normalized, "token") || strings.Contains(normalized, "secret") || normalized == "headers" || normalized == "serverurl"
}

func normalizeDescription(value any) string {
	text, _ := value.(string)
	return strings.Join(strings.Fields(text), " ")
}

func schemaStringSet(value any) map[string]bool {
	result := map[string]bool{}
	values, _ := value.([]any)
	for _, value := range values {
		if text, ok := value.(string); ok {
			result[text] = true
		}
	}
	return result
}

func canonicalSchemaValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		copy := map[string]any{}
		for key, value := range typed {
			if key != "title" && key != "examples" && key != "$id" && key != "$schema" {
				copy[key] = canonicalSchemaValue(value)
			}
		}
		return copy
	case []any:
		copy := make([]any, len(typed))
		for i, value := range typed {
			copy[i] = canonicalSchemaValue(value)
		}
		return copy
	default:
		return typed
	}
}

func ValidateSchemaArguments(raw json.RawMessage, arguments map[string]any) error {
	if jsonvalidation.Validate(raw, arguments) != nil {
		return ErrProbeSchema
	}
	return nil
}
