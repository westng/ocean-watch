package official

import (
	"bytes"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/jsonvalidation"
	port "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/official"
)

func prepareArguments(query contracts.ApprovedOfficialQuery, request contracts.OfficialQueryRequest) (map[string]json.RawMessage, error) {
	parameters := map[string]any{}
	for name, raw := range request.Parameters {
		if name == "cursor" || name == "offset" {
			return nil, failure("USE_BOUND_CURSOR", "parameters", false)
		}
		if contains(query.RestrictedParameters, name) {
			return nil, failure("SCHEMA_REVIEW_REQUIRED", "parameters", false)
		}
		if name == "page" || name == query.PageField {
			return nil, failure("USE_BOUND_CURSOR", "parameters", false)
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var value any
		if decoder.Decode(&value) != nil {
			return nil, failure("INVALID_PARAMETERS", "parameters", false)
		}
		parameters[name] = value
	}
	var source map[string]any
	decoder := json.NewDecoder(bytes.NewReader(query.ApprovedInput))
	decoder.UseNumber()
	if decoder.Decode(&source) != nil {
		return nil, failure("CONTRACT_INVALID", "parameters", false)
	}
	properties, _ := source["properties"].(map[string]any)
	for _, rule := range query.SubjectRules {
		if strings.Contains(rule.Path, ".") || request.Scope.Kind == "authorization" || request.Scope.Kind == "public_catalog" {
			continue
		}
		if rule.Kind != request.Scope.Kind && rule.Kind != "account" {
			continue
		}
		if _, exists := parameters[rule.Path]; !exists {
			field, _ := properties[rule.Path].(map[string]any)
			if field["type"] == "array" {
				parameters[rule.Path] = []any{request.Scope.ID}
			} else {
				parameters[rule.Path] = request.Scope.ID
			}
		}
	}
	if query.PageField != "" {
		parameters[query.PageField] = json.Number("1")
	}
	if query.PagingKind == "page" && query.PageInfoPath == "" && query.TotalField != "" {
		if _, exists := parameters[query.PageSizeField]; !exists && query.PageSizeField != "" {
			parameters[query.PageSizeField] = json.Number("100")
		}
	}
	if (query.PagingKind == "cursor" || query.PagingKind == "offset") && !query.OmitInitialCursor {
		parameters[query.CursorField] = json.Number("0")
		var cursorField map[string]any
		cursorField, _ = properties[query.CursorField].(map[string]any)
		if cursorField["type"] == "string" {
			parameters[query.CursorField] = "0"
		}
	}
	if query.PageSizeField != "" {
		if value, exists := parameters[query.PageSizeField]; exists {
			number, ok := value.(json.Number)
			size, err := strconv.Atoi(number.String())
			if !ok || err != nil || size < 1 || size > 1000 {
				return nil, failure("INVALID_PARAMETERS", "paging", false)
			}
		}
	}
	var host map[string]any
	if json.Unmarshal(query.HostInput, &host) != nil {
		return nil, failure("CONTRACT_INVALID", "parameters", false)
	}
	if missing := missingRequiredParameters(host, parameters, ""); len(missing) > 0 {
		sort.Strings(missing)
		return nil, &port.Failure{Code: "MISSING_REQUIRED_PARAMETERS", Stage: "parameters", RequiredFields: missing}
	}
	for _, rule := range query.ConditionalRules {
		if !conditionalRuleApplies(rule, parameters) {
			continue
		}
		missing := []string{}
		for _, path := range rule.Required {
			value, present := readValue(parameters, strings.Split(path, "."))
			if !present || value == nil || value == "" {
				missing = append(missing, path)
				continue
			}
			if values, ok := value.([]any); ok && len(values) == 0 {
				missing = append(missing, path)
			}
		}
		if len(missing) > 0 {
			return nil, &port.Failure{Code: "MISSING_REQUIRED_PARAMETERS", Stage: "parameters", RequiredFields: missing}
		}
		for _, path := range rule.Forbidden {
			if _, present := readValue(parameters, strings.Split(path, ".")); present {
				return nil, failure("PARAMETER_CONDITION_CONFLICT", "parameters", false)
			}
		}
		if rule.MaxDateSpanDays > 0 {
			for _, pair := range [][2]string{{"start_time", "end_time"}, {"start_date", "end_date"}} {
				startText, startOK := parameters[pair[0]].(string)
				endText, endOK := parameters[pair[1]].(string)
				start, startValid := parseDate(startText)
				end, endValid := parseDate(endText)
				if startOK && endOK && startValid && endValid && end.Sub(start) > time.Duration(rule.MaxDateSpanDays)*24*time.Hour {
					return nil, failure("INVALID_DATE_RANGE", "parameters", false)
				}
			}
		}
	}
	if jsonvalidation.Validate(query.HostInput, parameters) != nil {
		return nil, failure("INVALID_PARAMETERS", "parameters", false)
	}
	if err := validateParameterDates(parameters); err != nil {
		return nil, err
	}
	if query.MaxDateSpanDays > 0 {
		for _, pair := range [][2]string{{"start_time", "end_time"}, {"start_date", "end_date"}} {
			startText, startOK := parameters[pair[0]].(string)
			endText, endOK := parameters[pair[1]].(string)
			if startOK && endOK {
				start, _ := parseDate(startText)
				end, _ := parseDate(endText)
				if end.Sub(start) > time.Duration(query.MaxDateSpanDays)*24*time.Hour {
					return nil, failure("INVALID_DATE_RANGE", "parameters", false)
				}
			}
		}
	}
	normalized, err := normalizeParameters(source, parameters)
	if err != nil {
		return nil, err
	}
	object, ok := normalized.(map[string]any)
	if !ok {
		return nil, failure("INVALID_PARAMETERS", "parameters", false)
	}
	result := map[string]json.RawMessage{}
	for name, value := range object {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, failure("INVALID_PARAMETERS", "parameters", false)
		}
		result[name] = encoded
	}
	return result, nil
}

func conditionalRuleApplies(rule contracts.OfficialConditionalRule, parameters map[string]any) bool {
	for _, condition := range rule.When {
		value, present := readValue(parameters, strings.Split(condition.Field, "."))
		if condition.Operator == "present" {
			if !present || value == nil || value == "" {
				return false
			}
			continue
		}
		if condition.Operator != "equals" || !present {
			return false
		}
		text := subjectID(value)
		if boolean, ok := value.(bool); ok {
			text = strconv.FormatBool(boolean)
		}
		if !contains(condition.Values, text) {
			return false
		}
	}
	return true
}

// Hints contain only paths from the frozen Host contract, never input values
// or upstream error text. Nested requirements apply when their object exists.
func missingRequiredParameters(schema map[string]any, value map[string]any, prefix string) []string {
	missing := []string{}
	requiredNames, _ := schema["required"].([]any)
	for _, required := range requiredNames {
		name, _ := required.(string)
		if _, present := value[name]; name != "" && !present {
			missing = append(missing, prefix+name)
		}
	}
	properties, _ := schema["properties"].(map[string]any)
	for name, childValue := range value {
		child, _ := properties[name].(map[string]any)
		if object, ok := childValue.(map[string]any); ok {
			missing = append(missing, missingRequiredParameters(child, object, prefix+name+".")...)
		}
	}
	return missing
}

func normalizeParameters(schema map[string]any, value any) (any, error) {
	switch schema["type"] {
	case "integer":
		var text string
		switch typed := value.(type) {
		case string:
			text = typed
		case json.Number:
			text = typed.String()
		default:
			return nil, failure("INVALID_PARAMETERS", "parameters", false)
		}
		bits := 64
		if schema["format"] == "int32" {
			bits = 32
		}
		number, err := strconv.ParseInt(text, 10, bits)
		if err != nil || strconv.FormatInt(number, 10) != text {
			return nil, failure("INVALID_PARAMETERS", "parameters", false)
		}
		return json.Number(text), nil
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return nil, failure("INVALID_PARAMETERS", "parameters", false)
		}
		properties, _ := schema["properties"].(map[string]any)
		result := map[string]any{}
		for name, value := range object {
			child, known := properties[name].(map[string]any)
			if !known {
				return nil, failure("UNKNOWN_PARAMETER", "parameters", false)
			}
			normalized, err := normalizeParameters(child, value)
			if err != nil {
				return nil, err
			}
			result[name] = normalized
		}
		return result, nil
	case "array":
		array, ok := value.([]any)
		if !ok || len(array) > 1000 {
			return nil, failure("INVALID_PARAMETERS", "parameters", false)
		}
		items, _ := schema["items"].(map[string]any)
		result := make([]any, len(array))
		for i, value := range array {
			normalized, err := normalizeParameters(items, value)
			if err != nil {
				return nil, err
			}
			result[i] = normalized
		}
		return result, nil
	default:
		return value, nil
	}
}

func validateParameterDates(parameters map[string]any) error {
	for _, pair := range [][2]string{{"start_time", "end_time"}, {"start_date", "end_date"}, {"create_start_date", "create_end_date"}, {"modify_start_date", "modify_end_date"}} {
		startText, startOK := parameters[pair[0]].(string)
		endText, endOK := parameters[pair[1]].(string)
		if startOK {
			if _, valid := parseDate(startText); !valid {
				return failure("INVALID_DATE_RANGE", "parameters", false)
			}
		}
		if endOK {
			if _, valid := parseDate(endText); !valid {
				return failure("INVALID_DATE_RANGE", "parameters", false)
			}
		}
		if startOK && endOK {
			start, startValid := parseDate(startText)
			end, endValid := parseDate(endText)
			if !startValid || !endValid || end.Before(start) {
				return failure("INVALID_DATE_RANGE", "parameters", false)
			}
		}
	}
	for _, value := range parameters {
		if object, ok := value.(map[string]any); ok {
			if err := validateParameterDates(object); err != nil {
				return err
			}
		}
	}
	return nil
}

func parseDate(value string) (time.Time, bool) {
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02", time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}
func jsonNumber(value int) string { return strconv.Itoa(value) }
