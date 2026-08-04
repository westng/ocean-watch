package templates

import (
	"encoding/json"
	"strconv"
	"strings"
)

type QianchuanTemplateKind string

const (
	QianchuanTemplateProduct QianchuanTemplateKind = "product"
	QianchuanTemplateLive    QianchuanTemplateKind = "live"
)

type QianchuanPlanPayload struct {
	TemplateID       string
	DisplayName      string
	Kind             QianchuanTemplateKind
	AdvertiserID     string
	ProductName      string
	PlanNameTemplate string
	CreatorName      string
	ProductIDs       []string
	Active           bool
	Payload          json.RawMessage
}

func ExportQianchuanPlanPayload(
	config map[string]any,
	kind QianchuanTemplateKind,
	selector string,
	name string,
) (QianchuanPlanPayload, error) {
	selector = strings.TrimSpace(selector)
	name = strings.TrimSpace(name)
	if selector == "" {
		return QianchuanPlanPayload{}, configurationError("an explicit Qianchuan template is required", nil)
	}
	switch kind {
	case QianchuanTemplateProduct:
		return exportQianchuanProductPayload(config, selector, name)
	case QianchuanTemplateLive:
		if name != "" {
			return QianchuanPlanPayload{}, configurationError("Qianchuan live plans do not accept name", nil)
		}
		return exportQianchuanLivePayload(config, selector)
	default:
		return QianchuanPlanPayload{}, configurationError("unsupported Qianchuan template kind", map[string]any{
			"template_kind": kind,
		})
	}
}

func exportQianchuanProductPayload(
	config map[string]any,
	selector string,
	name string,
) (QianchuanPlanPayload, error) {
	template, err := resolveQianchuanProductTemplate(config, selector)
	if err != nil {
		return QianchuanPlanPayload{}, err
	}
	bindings := mapOrEmpty(template["bindings"])
	advertiserID := stringValue(bindings["advertiser_id"])
	productIDs := stringList(listOrEmpty(bindings["product_ids"]))
	advertiserNumber, err := qianchuanInt64ID(advertiserID, "advertiser_id")
	if err != nil {
		return QianchuanPlanPayload{}, err
	}
	productNumbers, err := qianchuanInt64IDs(productIDs)
	if err != nil {
		return QianchuanPlanPayload{}, err
	}
	payload := map[string]any{
		"advertiser_id":    advertiserNumber,
		"marketing_goal":   "VIDEO_PROM_GOODS",
		"product_ids":      productNumbers,
		"delivery_setting": cloneMap(mapOrEmpty(template["delivery_setting"])),
	}
	if name != "" {
		payload["name"] = name
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return QianchuanPlanPayload{}, configurationError("failed to encode Qianchuan product payload", nil)
	}
	return QianchuanPlanPayload{
		TemplateID: stringValue(template["template_id"]), DisplayName: stringValue(template["display_name"]),
		Kind: QianchuanTemplateProduct, AdvertiserID: advertiserID,
		ProductName: stringValue(bindings["product_name"]), ProductIDs: append([]string(nil), productIDs...),
		PlanNameTemplate: stringValue(template["plan_name_template"]),
		Active:           template["status"] == "active", Payload: raw,
	}, nil
}

func exportQianchuanLivePayload(
	config map[string]any,
	selector string,
) (QianchuanPlanPayload, error) {
	template, err := resolveQianchuanLiveTemplate(config, selector)
	if err != nil {
		return QianchuanPlanPayload{}, err
	}
	bindings := mapOrEmpty(template["bindings"])
	advertiserID := stringValue(bindings["advertiser_id"])
	awemeID := stringValue(bindings["aweme_id"])
	advertiserNumber, err := qianchuanInt64ID(advertiserID, "advertiser_id")
	if err != nil {
		return QianchuanPlanPayload{}, err
	}
	awemeNumber, err := qianchuanInt64ID(awemeID, "aweme_id")
	if err != nil {
		return QianchuanPlanPayload{}, err
	}
	payload := map[string]any{
		"advertiser_id":    advertiserNumber,
		"aweme_id":         awemeNumber,
		"marketing_goal":   "LIVE_PROM_GOODS",
		"delivery_setting": cloneMap(mapOrEmpty(template["delivery_setting"])),
		"creative_setting": cloneMap(mapOrEmpty(template["creative_setting"])),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return QianchuanPlanPayload{}, configurationError("failed to encode Qianchuan live payload", nil)
	}
	return QianchuanPlanPayload{
		TemplateID: stringValue(template["template_id"]), DisplayName: stringValue(template["display_name"]),
		Kind: QianchuanTemplateLive, AdvertiserID: advertiserID,
		CreatorName: stringValue(bindings["creator_name"]), Active: template["status"] == "active", Payload: raw,
	}, nil
}

func qianchuanInt64ID(value string, field string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, configurationError("Qianchuan "+field+" exceeds the official SDK integer range", nil)
	}
	return parsed, nil
}

func qianchuanInt64IDs(values []string) ([]int64, error) {
	result := make([]int64, len(values))
	for index, value := range values {
		parsed, err := qianchuanInt64ID(value, "product_id")
		if err != nil {
			return nil, err
		}
		result[index] = parsed
	}
	return result, nil
}
