package mcpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"unicode/utf8"
)

func presentList(result map[string]any) ([]templateItem, error) {
	channels, ok := result["channels"].(map[string]any)
	if !ok {
		return nil, errors.New("template channels are missing")
	}
	items := make([]templateItem, 0)
	for _, channel := range []string{"marketing", "qianchuan"} {
		rawChannel, exists := channels[channel]
		if !exists {
			continue
		}
		channelResult, ok := rawChannel.(map[string]any)
		if !ok {
			return nil, errors.New("template channel is invalid")
		}
		rows, ok := channelResult["templates"].([]any)
		if !ok {
			return nil, errors.New("template rows are invalid")
		}
		for _, raw := range rows {
			row, ok := raw.(map[string]any)
			if !ok {
				return nil, errors.New("template row is invalid")
			}
			item, err := presentListItem(channel, row)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
	}
	return items, nil
}

func presentListItem(channel string, row map[string]any) (templateItem, error) {
	templateIDKey := "template_id"
	kind := textValue(row["template_kind"])
	if channel == "marketing" {
		templateIDKey = "name"
		kind = "marketing"
	}
	templateID, err := requiredBoundedText(row[templateIDKey], 256)
	if err != nil {
		return templateItem{}, err
	}
	name, err := requiredBoundedText(row["name"], 256)
	if err != nil {
		return templateItem{}, err
	}
	if channel != "marketing" && channel != "qianchuan" || kind != "marketing" && kind != "product" && kind != "live" {
		return templateItem{}, errors.New("template channel or kind is invalid")
	}
	status, err := nullableBoundedText(row["status"], 64)
	if err != nil {
		return templateItem{}, err
	}
	if channel == "marketing" {
		status = nil
	}
	advertiserID, err := nullableID(row["advertiser_id"])
	if err != nil {
		return templateItem{}, err
	}
	ready, ok := row["ready_for_plan_creation"].(bool)
	if !ok {
		return templateItem{}, errors.New("template readiness is invalid")
	}
	return templateItem{
		TemplateID: templateID, Channel: channel, TemplateKind: kind, Name: name,
		Status: status, AdvertiserID: advertiserID, ReadyForPlanCreation: ready,
	}, nil
}

func presentDetail(result map[string]any) (templateDetail, error) {
	channel, ok := result["channel"].(string)
	if !ok || channel != "marketing" && channel != "qianchuan" {
		return templateDetail{}, errors.New("template channel is invalid")
	}
	row, ok := result["template"].(map[string]any)
	if !ok {
		return templateDetail{}, errors.New("template detail is invalid")
	}
	ready, ok := result["ready_for_plan_creation"].(bool)
	if !ok {
		return templateDetail{}, errors.New("template readiness is invalid")
	}
	detail := templateDetail{Channel: channel, ReadyForPlanCreation: ready, ProductIDs: []string{}, ValidationIssues: []string{}}
	var err error
	if channel == "marketing" {
		detail.TemplateKind = "marketing"
		detail.TemplateID, err = requiredBoundedText(row["name"], 256)
		if err != nil {
			return templateDetail{}, err
		}
		detail.Name = detail.TemplateID
		if detail.AdvertiserID, err = nullableID(row["advertiser_id"]); err != nil {
			return templateDetail{}, err
		}
		if detail.ProductID, err = nullableID(row["product_id"]); err != nil {
			return templateDetail{}, err
		}
		if detail.ProductName, err = nullableBoundedText(row["product_name"], 256); err != nil {
			return templateDetail{}, err
		}
		if detail.MaterialSourceType, err = nullableBoundedText(row["material_source_type"], 64); err != nil {
			return templateDetail{}, err
		}
		delivery, ok := row["delivery_settings"].(map[string]any)
		if !ok {
			return templateDetail{}, errors.New("template delivery settings are invalid")
		}
		if detail.DailyBudget, err = nullableNumber(delivery["daily_budget"]); err != nil {
			return templateDetail{}, err
		}
		if detail.ROIGoal, err = nullableNumber(delivery["roi_goal"]); err != nil {
			return templateDetail{}, err
		}
		if detail.ProjectNameTemplate, err = nullableBoundedText(row["project_name_template"], 512); err != nil {
			return templateDetail{}, err
		}
		if detail.PromotionNameTemplate, err = nullableBoundedText(row["promotion_name_template"], 512); err != nil {
			return templateDetail{}, err
		}
		if issue, ok := row["binding_error"].(string); ok && issue != "" {
			if utf8.RuneCountInString(issue) > 500 {
				return templateDetail{}, errors.New("template validation issue is too long")
			}
			detail.ValidationIssues = append(detail.ValidationIssues, issue)
		} else if row["binding_error"] != nil {
			return templateDetail{}, errors.New("template validation issue is invalid")
		}
	} else {
		detail.TemplateKind, ok = row["template_kind"].(string)
		if !ok {
			return templateDetail{}, errors.New("template kind is invalid")
		}
		detail.TemplateID, err = requiredBoundedText(row["template_id"], 256)
		if err != nil {
			return templateDetail{}, err
		}
		if detail.Name, err = requiredBoundedText(row["display_name"], 256); err != nil {
			return templateDetail{}, err
		}
		if detail.Status, err = nullableBoundedText(row["status"], 64); err != nil {
			return templateDetail{}, err
		}
		bindings, ok := row["bindings"].(map[string]any)
		if !ok {
			return templateDetail{}, errors.New("template bindings are invalid")
		}
		if detail.AdvertiserID, err = nullableID(bindings["advertiser_id"]); err != nil {
			return templateDetail{}, err
		}
		if detail.ProductName, err = nullableBoundedText(bindings["product_name"], 256); err != nil {
			return templateDetail{}, err
		}
		if detail.CreatorName, err = nullableBoundedText(bindings["creator_name"], 256); err != nil {
			return templateDetail{}, err
		}
		if detail.AwemeID, err = nullableID(bindings["aweme_id"]); err != nil {
			return templateDetail{}, err
		}
		if rawIDs, ok := bindings["product_ids"].([]any); ok {
			if len(rawIDs) > 30 {
				return templateDetail{}, errors.New("too many product IDs")
			}
			for _, rawID := range rawIDs {
				id, idErr := requiredID(rawID)
				if idErr != nil {
					return templateDetail{}, idErr
				}
				detail.ProductIDs = append(detail.ProductIDs, id)
			}
		}
		strategy, ok := row["material_strategy"].(map[string]any)
		if !ok {
			return templateDetail{}, errors.New("template material strategy is invalid")
		}
		if detail.MaterialSourceType, err = nullableBoundedText(strategy["source_type"], 64); err != nil {
			return templateDetail{}, err
		}
		delivery, ok := row["delivery_setting"].(map[string]any)
		if !ok {
			return templateDetail{}, errors.New("template delivery setting is invalid")
		}
		if detail.DailyBudget, err = nullableNumber(delivery["budget"]); err != nil {
			return templateDetail{}, err
		}
		if detail.ROIGoal, err = nullableNumber(delivery["roi2_goal"]); err != nil {
			return templateDetail{}, err
		}
		if detail.SmartBidType, err = nullableBoundedText(delivery["smart_bid_type"], 64); err != nil {
			return templateDetail{}, err
		}
		if detail.TemplateKind == "product" {
			if detail.ProjectNameTemplate, err = nullableBoundedText(row["plan_name_template"], 512); err != nil {
				return templateDetail{}, err
			}
		}
	}
	if err != nil || detail.Name == "" || detail.TemplateKind != "marketing" && detail.TemplateKind != "product" && detail.TemplateKind != "live" {
		return templateDetail{}, errors.New("template identity is invalid")
	}
	if err := validateDetail(detail); err != nil {
		return templateDetail{}, err
	}
	return detail, nil
}

func validateDetail(detail templateDetail) error {
	ids := []*string{detail.AdvertiserID, detail.ProductID, detail.AwemeID}
	for _, id := range ids {
		if id != nil && utf8.RuneCountInString(*id) > 64 {
			return errors.New("template ID is too long")
		}
	}
	for _, number := range []*float64{detail.DailyBudget, detail.ROIGoal} {
		if number != nil && (*number < 0 || math.IsNaN(*number) || math.IsInf(*number, 0)) {
			return errors.New("template number is invalid")
		}
	}
	return nil
}

func textValue(value any) string {
	text, _ := value.(string)
	return text
}

func requiredBoundedText(value any, maximum int) (string, error) {
	text, ok := value.(string)
	if !ok || text == "" || utf8.RuneCountInString(text) > maximum {
		return "", errors.New("required template text is invalid")
	}
	return text, nil
}

func nullableBoundedText(value any, maximum int) (*string, error) {
	if value == nil {
		return nil, nil
	}
	text, ok := value.(string)
	if !ok || text == "" || utf8.RuneCountInString(text) > maximum {
		return nil, errors.New("template text is invalid")
	}
	return &text, nil
}

func nullableID(value any) (*string, error) {
	if value == nil || value == "" {
		return nil, nil
	}
	id, err := requiredID(value)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func requiredID(value any) (string, error) {
	var text string
	switch typed := value.(type) {
	case string:
		text = typed
	case json.Number:
		text = typed.String()
	case int:
		text = strconv.Itoa(typed)
	case int64:
		text = strconv.FormatInt(typed, 10)
	case uint64:
		text = strconv.FormatUint(typed, 10)
	default:
		return "", fmt.Errorf("template ID has unsupported type %T", value)
	}
	if text == "" || utf8.RuneCountInString(text) > 64 {
		return "", errors.New("template ID is invalid")
	}
	return text, nil
}

func nullableNumber(value any) (*float64, error) {
	if value == nil {
		return nil, nil
	}
	var number float64
	var err error
	switch typed := value.(type) {
	case float64:
		number = typed
	case float32:
		number = float64(typed)
	case int:
		number = float64(typed)
	case int64:
		number = float64(typed)
	case json.Number:
		number, err = typed.Float64()
	default:
		err = fmt.Errorf("unsupported number type %T", value)
	}
	if err != nil || number < 0 || math.IsNaN(number) || math.IsInf(number, 0) {
		return nil, errors.New("template number is invalid")
	}
	return &number, nil
}
