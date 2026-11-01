package official

import (
	"encoding/json"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
	"strings"
)

func effectivePaging(query contracts.ApprovedOfficialQuery, request contracts.OfficialQueryRequest) contracts.ApprovedOfficialQuery {
	parameters := map[string]any{}
	for name, raw := range request.Parameters {
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.UseNumber()
		var value any
		if decoder.Decode(&value) == nil {
			parameters[name] = value
		}
	}
	for _, variant := range query.PagingVariants {
		value, present := readValue(parameters, strings.Split(variant.Field, "."))
		if !present || !contains(variant.Values, subjectID(value)) {
			continue
		}
		query.PagingKind = variant.Kind
		query.PageField = ""
		query.OmitInitialCursor = variant.OmitInitialCursor
		query.PageInfoPath = "page_info"
		if variant.Kind == "cursor" {
			query.CursorField = "cursor"
		} else {
			query.CursorField = ""
		}
	}
	return query
}

func projectPaging(query contracts.ApprovedOfficialQuery, request contracts.OfficialQueryRequest, data any, page, rowCount int) (bool, string, error) {
	invalid := func() (bool, string, error) { return false, "", failure("PAGINATION_INVALID", "paging", true) }
	switch query.PagingKind {
	case "none", "unverified":
		return false, "", nil
	case "page":
		actualPage, pageSize, total, totalPages := page, 0, 0, 0
		if query.PageInfoPath != "" {
			value, present := readValue(data, strings.Split(query.PageInfoPath, "."))
			info, ok := value.(map[string]any)
			if !present || !ok {
				return invalid()
			}
			var valid bool
			actualPage, valid = positiveInt(info["page"])
			if !valid {
				return invalid()
			}
			pageSize, valid = positiveInt(info["page_size"])
			if !valid {
				return invalid()
			}
			total, valid = nonnegativeInt(info[query.TotalField])
			if !valid {
				return invalid()
			}
			totalPages = (total + pageSize - 1) / pageSize
			if value, present := info["total_page"]; present {
				reported, valid := nonnegativeInt(value)
				if !valid || reported != totalPages {
					return invalid()
				}
			}
		} else {
			value, present := readValue(data, []string{query.TotalField})
			var valid bool
			total, valid = nonnegativeInt(value)
			if !present || !valid {
				return invalid()
			}
			pageSize = 100
			if raw, present := request.Parameters[query.PageSizeField]; present {
				var value json.Number
				if json.Unmarshal(raw, &value) != nil {
					return invalid()
				}
				pageSize, valid = positiveInt(value)
				if !valid {
					return invalid()
				}
			}
			totalPages = (total + pageSize - 1) / pageSize
		}
		expectedRows := min(pageSize, max(total-(page-1)*pageSize, 0))
		if actualPage != page || pageSize < 1 || pageSize > 1000 || rowCount != expectedRows || totalPages > 0 && page > totalPages {
			return invalid()
		}
		return page < totalPages, "", nil
	case "cursor":
		value, present := readValue(data, strings.Split(query.PageInfoPath, "."))
		info, ok := value.(map[string]any)
		if !present || !ok {
			return invalid()
		}
		more, ok := info["has_more"].(bool)
		if !ok {
			for _, field := range query.OutputFields {
				if field.Path != "data."+query.PageInfoPath+".has_more" || field.Type != "number" {
					continue
				}
				if number, numeric := info["has_more"].(json.Number); numeric && (number.String() == "0" || number.String() == "1") {
					more, ok = number.String() == "1", true
				}
			}
		}
		if !ok {
			return invalid()
		}
		cursor := subjectID(info["cursor"])
		if len(cursor) > 4096 || more && (cursor == "" || rowCount == 0) {
			return invalid()
		}
		return more, cursor, nil
	case "offset":
		value, present := readValue(data, []string{"offset"})
		cursor := subjectID(value)
		if !present || !integerToken.MatchString(cursor) || len(cursor) > 19 || cursor != "0" && rowCount == 0 {
			return invalid()
		}
		return cursor != "0", cursor, nil
	default:
		return invalid()
	}
}
