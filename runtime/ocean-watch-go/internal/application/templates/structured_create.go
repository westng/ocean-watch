package templates

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/policy"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/configuration"
	dt "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/templates"
)

type CreateRequest struct {
	Channel        string                          `json:"channel,omitempty"`
	TemplateKind   string                          `json:"template_kind,omitempty"`
	SourceTemplate string                          `json:"source_template,omitempty"`
	Marketing      *dt.MarketingCreateInput        `json:"marketing,omitempty"`
	Product        *dt.QianchuanProductCreateInput `json:"product,omitempty"`
	Live           *dt.QianchuanLiveCreateInput    `json:"live,omitempty"`
	Delivery       *CreateDelivery                 `json:"delivery,omitempty"`
}

type CreateDelivery struct {
	Budget            json.Number `json:"budget,omitempty"`
	SmartBidType      string      `json:"smart_bid_type,omitempty"`
	ROIGoal           json.Number `json:"roi2_goal,omitempty"`
	DailyDeliveryTime json.Number `json:"daily_delivery_time,omitempty"`
}

type CreateOptions struct {
	Submit           bool
	ExpectedRevision string
	PreviewDigest    string
}

type CreateResult struct {
	OK            bool                `json:"ok"`
	Status        string              `json:"status"`
	Changed       bool                `json:"changed"`
	MissingFields []string            `json:"missing_fields"`
	Choices       map[string][]string `json:"choices,omitempty"`
	StateVersion  string              `json:"state_version,omitempty"`
	PreviewDigest string              `json:"preview_digest,omitempty"`
	Template      map[string]any      `json:"template,omitempty"`
	Authorization string              `json:"authorization_status,omitempty"`
}

type CreateAuthorizationReader interface {
	ReadChannel(context.Context, string) (domain.AuthorizationState, error)
}

type StructuredCreator struct {
	Store          ConfigStore
	Authorizations CreateAuthorizationReader
	Policy         *policy.Instance
}

func createError(code, message string) error { return domain.NewError(code, message, 2, nil) }

func (creator StructuredCreator) Create(ctx context.Context, input CreateRequest, options CreateOptions) (CreateResult, error) {
	result := CreateResult{OK: true, Status: "needs_input", MissingFields: []string{}, Choices: map[string][]string{}}
	if input.Channel == "" {
		result.MissingFields = []string{"channel"}
		result.Choices["channel"] = []string{"marketing", "qianchuan"}
		return result, nil
	}
	if input.Channel != "marketing" && input.Channel != "qianchuan" {
		return result, createError("invalid_arguments", "template channel must be marketing or qianchuan")
	}
	if input.Channel == "qianchuan" && input.TemplateKind == "" {
		result.MissingFields = []string{"template_kind"}
		result.Choices["template_kind"] = []string{"product", "live"}
		return result, nil
	}
	if input.Channel == "marketing" && (input.Product != nil || input.Live != nil || input.Delivery != nil || input.TemplateKind != "" && input.TemplateKind != "marketing") ||
		input.Channel == "qianchuan" && (input.Marketing != nil || input.TemplateKind != "product" && input.TemplateKind != "live" || input.TemplateKind == "product" && input.Live != nil || input.TemplateKind == "live" && input.Product != nil) {
		return result, createError("invalid_arguments", "template fields do not match the selected channel and kind")
	}
	if input.Channel == "marketing" && (input.Marketing == nil || input.Marketing.MaterialSourceType == "") {
		result.MissingFields = []string{"marketing.material_source_type"}
		result.Choices["marketing.material_source_type"] = []string{"ACCOUNT_UPLOAD", "CREATOR_AUTHORIZED"}
		return result, nil
	}
	if input.Channel == "marketing" && input.Marketing.MaterialSourceType != "ACCOUNT_UPLOAD" && input.Marketing.MaterialSourceType != "CREATOR_AUTHORIZED" {
		return result, createError("invalid_arguments", "invalid Marketing material source type")
	}
	if input.Product != nil && !stringIDInput(input.Product.ProductIDs) || input.Marketing != nil && !stringIDInput(input.Marketing.CreatorIDs) {
		return result, createError("invalid_arguments", "product_ids and creator_ids must contain string IDs")
	}
	if input.Marketing != nil {
		for key := range input.Marketing.ProductInfo {
			switch key {
			case "product_name_type", "titles", "product_image_type", "product_image_fields", "product_selling_point_type", "selling_points":
			default:
				return result, createError("invalid_arguments", "unsupported product_info field")
			}
		}
	}
	instance := policy.Default()
	if creator.Policy != nil {
		instance = *creator.Policy
	}
	if instance.CheckCapability("shared.template_create") != nil {
		return result, policy.ErrDenied
	}
	if creator.Store == nil {
		return result, errors.New("template store is unavailable")
	}
	config, revision, err := creator.Store.ReadWithRevision(ctx)
	if err != nil {
		return result, err
	}
	result.StateVersion = revision
	if options.Submit && (options.ExpectedRevision == "" || options.PreviewDigest == "" || options.ExpectedRevision != revision) {
		return result, createError("template_preview_changed", "preview the current template configuration before submitting")
	}
	var source map[string]any
	sourceName := input.SourceTemplate
	found := sourceName == "default"
	choices := []string{"default"}
	switch {
	case input.Channel == "marketing":
		var sources []dt.MarketingCreateSource
		config, sources, err = dt.MarketingCreateSources(config, input.Marketing.MaterialSourceType)
		if err == nil {
			for index, item := range sources {
				if index > 0 {
					choices = append(choices, item.Name)
				}
				if item.Name == sourceName || index == 0 && sourceName == "default" {
					source, sourceName, found = item.Template, item.Name, true
				}
			}
		}
	case input.TemplateKind == "product":
		var sources []dt.QianchuanProductCreateSource
		config, sources, err = dt.QianchuanProductCreateSources(config)
		if err == nil {
			for index, item := range sources {
				if index > 0 {
					choices = append(choices, item.ID)
				}
				if item.ID == sourceName || index == 0 && sourceName == "default" {
					source, found = item.Template, true
				}
			}
		}
	case input.TemplateKind == "live":
		var sources []dt.QianchuanLiveCreateSource
		config, sources, err = dt.QianchuanLiveCreateSources(config)
		if err == nil {
			for index, item := range sources {
				if index > 0 {
					choices = append(choices, item.ID)
				}
				if item.ID == sourceName || index == 0 && sourceName == "default" {
					source, found = item.Template, true
				}
			}
		}
	}
	if err != nil {
		return result, err
	}
	if input.SourceTemplate == "" {
		result.MissingFields = []string{"source_template"}
		result.Choices["source_template"] = choices
		return result, nil
	}
	if !found {
		return result, createError("template_not_found", "source template does not match the channel and kind")
	}
	missing := func(key, value string) {
		if strings.TrimSpace(value) == "" {
			result.MissingFields = append(result.MissingFields, key)
		}
	}
	advertiser := ""
	switch {
	case input.Marketing != nil:
		v := input.Marketing
		advertiser = v.AdvertiserID
		for _, field := range []struct{ key, value string }{{"template_name", v.TemplateName}, {"advertiser_id", v.AdvertiserID}, {"platform", v.Platform}, {"traffic_source", v.TrafficSource}, {"product_id", v.ProductID}, {"product_name", v.ProductName}} {
			missing("marketing."+field.key, field.value)
		}
	case input.TemplateKind == "product":
		v := dt.QianchuanProductCreateInput{}
		if input.Product != nil {
			v = *input.Product
		}
		advertiser = v.AdvertiserID
		missing("product.template_name", v.TemplateName)
		missing("product.advertiser_id", v.AdvertiserID)
		missing("product.product_name", v.ProductName)
		if v.ProductIDs == nil {
			result.MissingFields = append(result.MissingFields, "product.product_ids")
		}
	case input.TemplateKind == "live":
		v := dt.QianchuanLiveCreateInput{}
		if input.Live != nil {
			v = *input.Live
		}
		advertiser = v.AdvertiserID
		missing("live.advertiser_id", v.AdvertiserID)
		missing("live.creator_name", v.CreatorName)
		missing("live.aweme_id", v.AwemeID)
	}
	if len(result.MissingFields) > 0 {
		return result, nil
	}
	if instance.CheckSubject(input.Channel, "advertiser", advertiser) != nil {
		return result, policy.ErrDenied
	}
	if creator.Authorizations == nil {
		return result, errors.New("template authorization reader is unavailable")
	}
	state, err := creator.Authorizations.ReadChannel(ctx, input.Channel)
	if err != nil {
		return result, err
	}
	result.Authorization = "unverified"
	if len(state.AdvertiserIDs) > 0 {
		for _, id := range state.AdvertiserIDs {
			if id == advertiser {
				result.Authorization = "local_snapshot"
			}
		}
		if result.Authorization != "local_snapshot" {
			return result, createError("advertiser_not_authorized", "advertiser is outside the saved channel authorization scope")
		}
	}
	encoded, err := json.Marshal(struct {
		Revision string
		Request  CreateRequest
	}{revision, input})
	if err != nil {
		return result, err
	}
	idDigest := sha256.Sum256(encoded)
	if input.Delivery != nil {
		source = configuration.CloneMap(source)
		delivery := configuration.CloneMap(configuration.Object(source["delivery_setting"]))
		for key, value := range map[string]string{"budget": string(input.Delivery.Budget), "smart_bid_type": input.Delivery.SmartBidType, "roi2_goal": string(input.Delivery.ROIGoal), "daily_delivery_time": string(input.Delivery.DailyDeliveryTime)} {
			if value != "" {
				delivery[key] = value
			}
		}
		if input.Delivery.SmartBidType == "SMART_BID_CUSTOM" {
			delete(delivery, "daily_delivery_time")
		} else if input.Delivery.SmartBidType != "" {
			delete(delivery, "roi2_goal")
		}
		source["delivery_setting"] = delivery
	}
	var updated map[string]any
	switch {
	case input.Marketing != nil:
		var name string
		name, result.Template, err = dt.BuildMarketingTemplate(config, sourceName, source, *input.Marketing)
		if err == nil {
			readiness := dt.MarketingCandidateReadiness(config, name, result.Template)
			if readiness["ready_for_plan_creation"] != true {
				for _, field := range configuration.List(readiness["template_missing_fields"]) {
					if text, ok := field.(string); ok {
						result.MissingFields = append(result.MissingFields, text)
					}
				}
				return result, nil
			}
			updated, err = dt.ApplyMarketingTemplate(config, name, result.Template)
		}
	case input.Product != nil:
		v := *input.Product
		v.TemplateID = "qcpt_" + hex.EncodeToString(idDigest[:12])
		result.Template, err = dt.BuildQianchuanProductTemplate(source, v)
		if err == nil {
			updated, err = dt.ApplyQianchuanProductTemplate(config, result.Template)
		}
	case input.Live != nil:
		v := *input.Live
		v.TemplateID = "qclt_" + hex.EncodeToString(idDigest[:12])
		result.Template, err = dt.BuildQianchuanLiveTemplate(source, v)
		if err == nil {
			updated, err = dt.ApplyQianchuanLiveTemplate(config, result.Template)
		}
	}
	if err != nil {
		return result, err
	}
	preview, err := json.Marshal(struct {
		Revision string
		Template map[string]any
	}{revision, result.Template})
	if err != nil {
		return result, err
	}
	digest := sha256.Sum256(preview)
	result.PreviewDigest = "sha256:" + hex.EncodeToString(digest[:])
	result.Status = "preview"
	if options.Submit {
		if options.PreviewDigest != result.PreviewDigest {
			return result, createError("template_preview_changed", "template inputs differ from the reviewed preview")
		}
		if err := creator.Store.CompareAndSwap(ctx, revision, updated); err != nil {
			return result, err
		}
		result.Status, result.Changed = "created", true
	}
	return result, nil
}

func stringIDInput(value any) bool {
	if value == nil {
		return true
	}
	if _, ok := value.(string); ok {
		return true
	}
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if _, ok := item.(string); !ok {
			return false
		}
	}
	return true
}
