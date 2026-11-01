package contracts

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Corrections are compiled only from the frozen 156 tools and their verified
// GET request tables. The original schema remains the drift-check baseline.
func correctPublishedInput(id string, schema map[string]any, fields []OfficialDocumentField) string {
	properties, _ := schema["properties"].(map[string]any)
	evidence := []string{}
	if paths := correctDocumentedNumericEnums(schema, fields, ""); len(paths) > 0 {
		evidence = append(evidence, "Official GET request types correct captured integer enums encoded as strings: "+strings.Join(paths, ","))
	}
	switch id {
	case "tools_interest_action_action_category_v2", "tools_interest_action_action_keyword_v2", "tools_interest_action_keyword_suggest_v2":
		if hasDocumentField(fields, "action_scene") && properties["action_scene"] == nil {
			properties["action_scene"] = map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []any{"E-COMMERCE", "NEWS", "APP"}}, "minItems": 1, "maxItems": 3, "uniqueItems": true, "description": "行为场景，允许值 E-COMMERCE、NEWS、APP；依据该接口官方 GET 文档"}
			evidence = append(evidence, "Official GET request table supplies missing string[] action_scene; conditional requirements are frozen separately")
		}
	case "qianchuan_suggest_bid_v1":
		// The catalog has no verified merchant-role lookup. Requiring the exact
		// creator also satisfies the documented non-merchant branch.
		if properties["aweme_id"] != nil {
			required, _ := schema["required"].([]any)
			schema["required"] = append(required, "aweme_id")
			evidence = append(evidence, "Local restriction: exact aweme_id is required because this catalog cannot establish the documented merchant-role exemption")
		}
	}
	if id == "qianchuan_estimate_effect_v1" || id == "qianchuan_suggest_budget_v1" {
		if duration, ok := properties["schedule_fixed_range"].(map[string]any); ok && hasDocumentField(fields, "schedule_fixed_range") {
			duration["minimum"], duration["maximum"], duration["multipleOf"] = 1800, 86400, 1800
			evidence = append(evidence, "Official GET schedule_fixed_range is 1800..86400 seconds in multiples of 1800")
		}
	}
	if id == "qianchuan_estimate_effect_v1" {
		if budget, ok := properties["budget"].(map[string]any); ok {
			for _, field := range fields {
				if field.Path == "budget" && strings.Contains(strings.ReplaceAll(field.Description, " ", ""), "300-9999999.99") {
					budget["minimum"], budget["maximum"], budget["multipleOf"] = json.Number("300"), json.Number("9999999.99"), json.Number("0.01")
					evidence = append(evidence, "Official GET budget range 300..9999999.99 with two decimal places corrects captured maximum=0")
				}
			}
		}
	}
	if id == "qianchuan_ad_keywords_get_v1" {
		for _, path := range []string{"advertiser_id", "filtering.ad_id"} {
			node := schema
			for _, part := range strings.Split(path, ".") {
				props, _ := node["properties"].(map[string]any)
				node, _ = props[part].(map[string]any)
			}
			if node != nil && node["format"] == "int64" && hasDocumentField(fields, path) {
				node["maximum"] = json.Number("9223372036854775807")
				evidence = append(evidence, "Official GET positive int64 identity and pinned SDK correct captured maximum=0: "+path)
			}
		}
	}
	return strings.Join(evidence, "; ")
}

func correctDocumentedNumericEnums(schema map[string]any, fields []OfficialDocumentField, path string) []string {
	result := []string{}
	for _, field := range fields {
		if field.Path != strings.TrimSuffix(path, ".*") || !containsString([]string{"number", "number[]", "float", "float[]", "double", "integer"}, field.Type) || schema["type"] != "integer" {
			continue
		}
		values, _ := schema["enum"].([]any)
		corrected := []any{}
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				corrected = nil
				break
			}
			integer, err := strconv.ParseInt(text, 10, 64)
			if err != nil || strconv.FormatInt(integer, 10) != text {
				corrected = nil
				break
			}
			corrected = append(corrected, json.Number(text))
		}
		if len(corrected) > 0 {
			schema["enum"] = corrected
			result = append(result, path)
		}
	}
	properties, _ := schema["properties"].(map[string]any)
	names := []string{}
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		child, _ := properties[name].(map[string]any)
		childPath := name
		if path != "" {
			childPath = path + "." + name
		}
		result = append(result, correctDocumentedNumericEnums(child, fields, childPath)...)
	}
	if items, ok := schema["items"].(map[string]any); ok {
		result = append(result, correctDocumentedNumericEnums(items, fields, path+".*")...)
	}
	return result
}

var dateSpanPattern = regexp.MustCompile(`(?:跨度|间隔)(?:不|不能)?超过\s*([0-9]{1,3})\s*天`)
var arrayBoundPattern = regexp.MustCompile(`(?:最多(?:支持)?|不超过|上限为?)\s*([0-9]{1,3})\s*个`)

func documentedDateSpan(fields []OfficialDocumentField) int {
	result := 0
	for _, field := range fields {
		if !containsString([]string{"start_time", "end_time", "start_date", "end_date"}, field.Name) || strings.Contains(field.Description, "当") {
			continue
		}
		match := dateSpanPattern.FindStringSubmatch(field.Description)
		if len(match) != 2 {
			continue
		}
		limit, _ := strconv.Atoi(match[1])
		if limit > 0 && (result == 0 || limit < result) {
			result = limit
		}
	}
	return result
}

func boundHostParameters(schema map[string]any) {
	properties, _ := schema["properties"].(map[string]any)
	for name, raw := range properties {
		child, _ := raw.(map[string]any)
		if child["type"] == "array" {
			if _, set := child["maxItems"]; !set {
				child["maxItems"] = 1000
			}
			if description, _ := child["description"].(string); description != "" {
				if match := arrayBoundPattern.FindStringSubmatch(description); len(match) == 2 {
					limit, _ := strconv.Atoi(match[1])
					if limit > 0 {
						child["maxItems"] = limit
					}
				}
			}
		}
		if name == "count" && child["type"] == "integer" {
			if _, set := child["minimum"]; !set {
				child["minimum"] = 1
			}
			if _, set := child["maximum"]; !set {
				child["maximum"] = 1000
			}
		}
		if name == "account_type" || name == "platform" || name == "account_source" {
			if values, ok := child["enum"].([]any); ok {
				for _, value := range values {
					if value == "QIANCHUAN" {
						child["enum"] = []any{"QIANCHUAN"}
						break
					}
				}
			}
		}
		boundHostParameters(child)
		if items, ok := child["items"].(map[string]any); ok {
			boundHostParameters(items)
		}
	}
}
