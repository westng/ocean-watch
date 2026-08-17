package templates

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type Error struct {
	Message string
	Details map[string]any
}

func (err *Error) Error() string {
	return err.Message
}

func configurationError(message string, details map[string]any) error {
	if details == nil {
		details = map[string]any{}
	}
	return &Error{Message: message, Details: details}
}

type DecimalFloat64 float64

func (value DecimalFloat64) MarshalJSON() ([]byte, error) {
	rendered := strconv.FormatFloat(float64(value), 'f', -1, 64)
	if !strings.Contains(rendered, ".") {
		rendered += ".0"
	}
	return []byte(rendered), nil
}

func clone(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[key] = clone(item)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = clone(item)
		}
		return result
	default:
		return typed
	}
}

func cloneMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return clone(value).(map[string]any)
}

func asMap(value any) map[string]any {
	result, _ := value.(map[string]any)
	return result
}

func mapOrEmpty(value any) map[string]any {
	if result := asMap(value); result != nil {
		return result
	}
	return map[string]any{}
}

func asList(value any) []any {
	result, _ := value.([]any)
	return result
}

func listOrEmpty(value any) []any {
	if result := asList(value); result != nil {
		return result
	}
	return []any{}
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case json.Number:
		return typed.String()
	case bool:
		if typed {
			return "True"
		}
		return "False"
	default:
		return fmt.Sprint(value)
	}
}

func hasValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return typed != ""
	case json.Number:
		number, err := typed.Float64()
		return err != nil || number != 0
	case float64:
		return typed != 0
	case float32:
		return typed != 0
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case []any:
		return len(typed) != 0
	case map[string]any:
		return len(typed) != 0
	default:
		return true
	}
}

func isMissing(value any) bool {
	if value == nil {
		return true
	}
	if text, ok := value.(string); ok {
		text = strings.TrimSpace(text)
		return text == "" || strings.HasPrefix(text, "REPLACE_WITH")
	}
	if values, ok := value.([]any); ok {
		return len(values) == 0
	}
	return false
}

func parseVersion(value any, defaultValue int, field string) (int, error) {
	if !hasValue(value) {
		return defaultValue, nil
	}
	text := strings.TrimSpace(stringValue(value))
	parsed, err := strconv.Atoi(text)
	if err != nil {
		return 0, configurationError(field+" must be an integer", nil)
	}
	return parsed, nil
}

func requireTemplateSchema(config map[string]any, key string, supported int, label string) error {
	value, exists := config[key]
	if !exists || !hasValue(value) {
		return configurationError(key+" is required", nil)
	}
	version, err := parseVersion(value, 0, key)
	if err != nil {
		return err
	}
	if version != supported {
		return configurationError(fmt.Sprintf(
			"%s template schema %d is unsupported; only schema %d is supported",
			label, version, supported,
		), nil)
	}
	return nil
}

func requiredText(value any, field string) (string, error) {
	if !hasValue(value) {
		return "", configurationError(field+" is required", nil)
	}
	text := strings.TrimSpace(stringValue(value))
	if isMissing(text) {
		return "", configurationError(field+" is required", nil)
	}
	return text, nil
}

func positiveID(value any, field string) (string, error) {
	text := ""
	if hasValue(value) {
		text = strings.TrimSpace(stringValue(value))
	}
	if text == "" {
		return "", configurationError(field+" must be a positive integer", nil)
	}
	for _, character := range text {
		if character < '0' || character > '9' {
			return "", configurationError(field+" must be a positive integer", nil)
		}
	}
	trimmed := strings.TrimLeft(text, "0")
	if trimmed == "" {
		return "", configurationError(field+" must be a positive integer", nil)
	}
	return text, nil
}

func decimalExponent(value any) (int, error) {
	text := strings.ToLower(strings.TrimSpace(stringValue(value)))
	if text == "" {
		return 0, errors.New("empty decimal")
	}
	exponent := 0
	if index := strings.IndexByte(text, 'e'); index >= 0 {
		parsed, err := strconv.Atoi(text[index+1:])
		if err != nil {
			return 0, err
		}
		exponent = parsed
		text = text[:index]
	}
	if index := strings.IndexByte(text, '.'); index >= 0 {
		exponent -= len(text) - index - 1
	}
	return exponent, nil
}

func sortedKeys(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func deepMerge(base, override map[string]any) map[string]any {
	result := cloneMap(base)
	for key, value := range override {
		baseNested, baseOK := result[key].(map[string]any)
		overrideNested, overrideOK := value.(map[string]any)
		if baseOK && overrideOK {
			result[key] = deepMerge(baseNested, overrideNested)
		} else {
			result[key] = clone(value)
		}
	}
	return result
}

func exactFalse(value any) bool {
	result, ok := value.(bool)
	return ok && !result
}

func exactTrue(value any) bool {
	result, ok := value.(bool)
	return ok && result
}

func errorDetails(err error) map[string]any {
	var templateError *Error
	if errors.As(err, &templateError) {
		return templateError.Details
	}
	return map[string]any{}
}
