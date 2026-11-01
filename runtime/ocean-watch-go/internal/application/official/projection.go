package official

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
)

var decimalToken = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)
var integerToken = regexp.MustCompile(`^[0-9]+$`)
var signedIntegerToken = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)$`)

type projectionPage struct {
	result               contracts.OfficialQueryResult
	nextPage, nextOffset int
	nextUpstream         string
}

func projectResult(query contracts.ApprovedOfficialQuery, request contracts.OfficialQueryRequest, data any, page, offset int) (projectionPage, error) {
	result := contracts.OfficialQueryResult{QueryID: request.QueryID, Source: "official_mcp", Channel: "qianchuan", Scope: request.Scope, Status: "ok", StatisticalScope: "not_applicable", Columns: []contracts.OfficialQueryColumn{}, Items: []map[string]*string{}, Failures: []contracts.OfficialQueryFailure{}, Completeness: contracts.OfficialQueryCompleteness{Limit: request.Limit}}
	projection := projectionPage{result: result, nextPage: page, nextOffset: offset}
	if start, ok := request.Parameters["start_time"]; ok {
		var startText, endText string
		_ = json.Unmarshal(start, &startText)
		_ = json.Unmarshal(request.Parameters["end_time"], &endText)
		result.Period = &contracts.OfficialQueryPeriod{Start: startText, End: endText}
	}
	if start, ok := request.Parameters["start_date"]; ok {
		var startText, endText string
		_ = json.Unmarshal(start, &startText)
		_ = json.Unmarshal(request.Parameters["end_date"], &endText)
		result.Period = &contracts.OfficialQueryPeriod{Start: startText, End: endText}
	}
	for _, name := range []string{"data_period", "adlab_scene", "marketing_goal"} {
		if raw, ok := request.Parameters[name]; ok {
			var value string
			_ = json.Unmarshal(raw, &value)
			if value != "" {
				if result.StatisticalScope == "not_applicable" {
					result.StatisticalScope = value
				} else {
					result.StatisticalScope += "/" + value
				}
			}
		}
	}
	if data == nil {
		result.Status = "no_data"
		result.Completeness.Complete = query.PagingKind == "none"
		if !result.Completeness.Complete {
			result.Completeness.Reason = "pagination_metadata_missing"
		}
		projection.result = result
		return projection, nil
	}
	rows := []any{data}
	base := "data"
	if query.PrimaryArray != "" {
		base = "data." + query.PrimaryArray
		var value any
		var present bool
		if query.PrimaryArray == "$data" {
			base = "data"
			value, present = data, true
		} else {
			value, present = readValue(data, strings.Split(query.PrimaryArray, "."))
		}
		if !present || value == nil {
			return projection, failure("REQUIRED_OUTPUT_MISSING", "projection", true)
		}
		array, ok := value.([]any)
		if !ok {
			return projection, failure("INVALID_RESPONSE", "projection", true)
		}
		rows = array
	}
	if len(rows) > 10000 || offset > len(rows) {
		return projection, failure("INVALID_RESPONSE", "projection", true)
	}
	selected := map[string]bool{}
	if raw, ok := request.Parameters["fields"]; ok {
		var fields []string
		if json.Unmarshal(raw, &fields) != nil {
			return projection, failure("INVALID_PARAMETERS", "projection", false)
		}
		for _, field := range fields {
			selected[field] = true
		}
	}
	fields := []contracts.OfficialDocumentField{}
	for _, field := range query.OutputFields {
		if field.Path != base && !strings.HasPrefix(field.Path, base+".") {
			continue
		}
		if prohibitedOutputField(field.Name) {
			continue
		}
		container := false
		for _, other := range query.OutputFields {
			if strings.HasPrefix(other.Path, field.Path+".") {
				container = true
				break
			}
		}
		if container {
			continue
		}
		if len(selected) > 0 && strings.Contains(field.Path, ".stats_info.") && !selected[field.Name] {
			continue
		}
		if query.PrimaryArray != "" && field.Path == base {
			field.Type = strings.TrimSuffix(field.Type, "[]")
		}
		fields = append(fields, field)
		columnName := strings.TrimPrefix(strings.TrimPrefix(field.Path, base), ".")
		if columnName == "" {
			columnName = field.Name
		}
		result.Columns = append(result.Columns, contracts.OfficialQueryColumn{Field: columnName, Label: columnLabel(field), ValueType: field.Type, Unit: fieldUnit(field, query.OutputFields)})
	}
	if len(fields) == 0 {
		return projection, failure("CONTRACT_PENDING", "projection", false)
	}
	end := min(offset+request.Limit, len(rows))
	for _, row := range rows[offset:end] {
		item := map[string]*string{}
		seen := 0
		for index, field := range fields {
			path := strings.TrimPrefix(strings.TrimPrefix(field.Path, base), ".")
			value, present := row, true
			if path != "" {
				value, present = readValue(row, strings.Split(path, "."))
			}
			if selected[field.Name] && (!present || value == nil) {
				return projection, failure("REQUIRED_OUTPUT_MISSING", "projection", true)
			}
			var cell *string
			if present && value != nil {
				var err error
				cell, err = projectCell(value, field)
				if err != nil {
					return projection, err
				}
				seen++
			}
			item[result.Columns[index].Field] = cell
		}
		if seen == 0 && len(fields) > 0 {
			return projection, failure("INVALID_RESPONSE", "projection", true)
		}
		result.Items = append(result.Items, item)
	}
	result.Completeness.Returned = len(result.Items)
	if len(result.Items) == 0 {
		result.Status = "no_data"
	}
	moreOnPage := end < len(rows)
	morePages, nextUpstream, err := projectPaging(query, request, data, page, len(rows))
	if err != nil {
		return projection, err
	}
	projection.nextUpstream = nextUpstream
	if query.PagingKind == "unverified" {
		result.Completeness.Reason = "pagination_contract_unverified"
	}
	result.HasMore = moreOnPage || morePages
	result.Completeness.Complete = !result.HasMore && query.PagingKind != "unverified" && page == 1 && offset == 0
	if moreOnPage {
		projection.nextOffset = end
		result.Completeness.Reason = "result_limit"
	} else if morePages {
		projection.nextPage = page + 1
		projection.nextOffset = 0
		result.Completeness.Reason = "more_pages"
	} else if page > 1 || offset > 0 {
		result.Completeness.Reason = "continuation_slice"
	}
	if !result.Completeness.Complete && result.Status == "ok" {
		result.Status = "partial"
	}
	if query.QueryID == "qianchuan_uni_promotion_list_v1" {
		seen := map[string]bool{}
		for _, row := range rows {
			value, ok := readValue(row, []string{"ad_info", "id"})
			id := subjectID(value)
			if !ok || id == "" || seen[id] {
				return projection, failure("PAGINATION_INVALID", "paging", true)
			}
			seen[id] = true
		}
	}
	projection.result = result
	return projection, nil
}

func readValue(value any, path []string) (any, bool) {
	if len(path) == 0 {
		return value, true
	}
	if object, ok := value.(map[string]any); ok {
		child, present := object[path[0]]
		if !present {
			return nil, false
		}
		return readValue(child, path[1:])
	}
	if array, ok := value.([]any); ok {
		result := []any{}
		for _, child := range array {
			value, present := readValue(child, path)
			if !present {
				return nil, false
			}
			result = append(result, value)
		}
		return result, true
	}
	return nil, false
}

func projectCell(value any, field contracts.OfficialDocumentField) (*string, error) {
	numeric := contains([]string{"number", "float", "double", "integer", "int", "int32", "int64"}, strings.ToLower(field.Type))
	integral := contains([]string{"integer", "int", "int32", "int64"}, strings.ToLower(field.Type))
	if object, ok := value.(map[string]any); ok {
		projected, err := safeObjectCell(object, 0)
		if err != nil {
			return nil, err
		}
		encoded, _ := json.Marshal(projected)
		if len(encoded) > 8192 {
			return nil, failure("RESULT_TOO_LARGE", "projection", true)
		}
		text := string(encoded)
		return &text, nil
	}
	if array, ok := value.([]any); ok {
		projected := []*string{}
		scalar := field
		scalar.Type = strings.TrimSuffix(scalar.Type, "[]")
		for _, value := range array {
			if value == nil {
				projected = append(projected, nil)
				continue
			}
			cell, err := projectCell(value, scalar)
			if err != nil {
				return nil, err
			}
			projected = append(projected, cell)
		}
		encoded, _ := json.Marshal(projected)
		text := string(encoded)
		return &text, nil
	}
	var text string
	switch typed := value.(type) {
	case json.Number:
		if !decimalToken.MatchString(typed.String()) {
			return nil, failure("INVALID_RESPONSE", "projection", true)
		}
		if contracts.OfficialIdentifierField(field.Name) && !integerToken.MatchString(typed.String()) {
			return nil, failure("INVALID_RESPONSE", "projection", true)
		}
		if integral && !signedIntegerToken.MatchString(typed.String()) {
			return nil, failure("OUTPUT_TYPE_CHANGED", "projection", true)
		}
		text = typed.String()
	case string:
		text = typed
		if len(text) > 8192 {
			return nil, failure("INVALID_RESPONSE", "projection", true)
		}
		if numeric && !decimalToken.MatchString(text) || integral && !signedIntegerToken.MatchString(text) {
			return nil, failure("OUTPUT_TYPE_CHANGED", "projection", true)
		}
	case bool:
		if numeric || contracts.OfficialIdentifierField(field.Name) && !contains([]string{"bool", "boolean"}, strings.ToLower(field.Type)) {
			return nil, failure("OUTPUT_TYPE_CHANGED", "projection", true)
		}
		text = strconv.FormatBool(typed)
	default:
		return nil, failure("INVALID_RESPONSE", "projection", true)
	}
	return &text, nil
}

func prohibitedOutputField(name string) bool {
	normalized := strings.ToLower(name)
	compact := strings.ReplaceAll(strings.ReplaceAll(normalized, "_", ""), "-", "")
	if strings.Contains(compact, "token") || compact == "clientsecret" || compact == "appsecret" {
		return true
	}
	switch normalized {
	case "access_token", "refresh_token", "secret", "cookie", "headers", "request_id", "app_secret", "authorization", "app_id", "developer_id":
		return true
	}
	return strings.HasSuffix(normalized, "_url") || strings.HasSuffix(normalized, "_image") || strings.HasSuffix(normalized, "_avatar") || normalized == "url"
}

func safeObjectCell(value any, depth int) (any, error) {
	if depth > 8 {
		return nil, failure("RESULT_TOO_LARGE", "projection", true)
	}
	switch typed := value.(type) {
	case map[string]any:
		if len(typed) > 128 {
			return nil, failure("RESULT_TOO_LARGE", "projection", true)
		}
		result := map[string]any{}
		for name, value := range typed {
			if prohibitedOutputField(name) {
				continue
			}
			cell, err := safeObjectCell(value, depth+1)
			if err != nil {
				return nil, err
			}
			result[name] = cell
		}
		return result, nil
	case []any:
		if len(typed) > 1000 {
			return nil, failure("RESULT_TOO_LARGE", "projection", true)
		}
		result := []any{}
		for _, value := range typed {
			cell, err := safeObjectCell(value, depth+1)
			if err != nil {
				return nil, err
			}
			result = append(result, cell)
		}
		return result, nil
	case nil:
		return nil, nil
	default:
		cell, err := projectCell(value, contracts.OfficialDocumentField{})
		if err != nil {
			return nil, err
		}
		return *cell, nil
	}
}
func columnLabel(field contracts.OfficialDocumentField) string {
	text := strings.Join(strings.Fields(field.Description), " ")
	if text == "" {
		return field.Name
	}
	runes := []rune(text)
	if len(runes) > 40 {
		return string(runes[:40])
	}
	return text
}
func fieldUnit(field contracts.OfficialDocumentField, fields []contracts.OfficialDocumentField) string {
	if contracts.OfficialIdentifierField(field.Name) {
		return ""
	}
	description := strings.Join(strings.Fields(field.Description), "")
	for _, parent := range fields {
		if strings.HasPrefix(field.Path, parent.Path+".") && strings.Contains(parent.Description, "千分之一分") {
			description += " 千分之一分"
		}
	}
	switch {
	case strings.Contains(description, "千分之一分"):
		return "CNY/100000"
	case strings.Contains(description, "单位元") || strings.Contains(description, "单位：元") || strings.Contains(description, "单位为元") || strings.Contains(description, "单位:元"):
		return "CNY"
	case strings.Contains(description, "单位分") || strings.Contains(description, "单位：分") || strings.Contains(description, "单位为分") || strings.Contains(description, "单位:分"):
		return "CNY/100"
	case strings.Contains(description, "单位为秒") || strings.Contains(description, "单位秒") || strings.Contains(description, "单位：秒"):
		return "seconds"
	default:
		return ""
	}
}
func nonnegativeInt(value any) (int, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	parsed, err := strconv.Atoi(number.String())
	return parsed, err == nil && parsed >= 0
}
func positiveInt(value any) (int, bool) {
	parsed, ok := nonnegativeInt(value)
	return parsed, ok && parsed > 0
}
