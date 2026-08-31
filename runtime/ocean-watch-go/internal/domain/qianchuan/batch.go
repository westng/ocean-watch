package qianchuan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

var ErrDuplicateItemConflict = errors.New("DUPLICATE_ITEM_CONFLICT")

// BatchItem is the normalized, user-provided identity portion of one work row.
// An empty PlanType or Business is intentional and never inherits a neighbor.
type BatchItem struct {
	InputIndex int    `json:"input_index"`
	WorkURL    string `json:"work_url"`
	PlanType   string `json:"plan_type"`
	Business   string `json:"business"`
}

// VerifiedBatchItem is the domain input after official work, creator and
// product checks have succeeded.
type VerifiedBatchItem struct {
	BatchItem
	WorkID     string   `json:"work_id"`
	CreatorID  string   `json:"creator_id"`
	ProductIDs []string `json:"product_ids"`
}

type PlanGroupIdentity struct {
	AdvertiserID string   `json:"advertiser_id"`
	TemplateID   string   `json:"template_id"`
	CreatorID    string   `json:"creator_id"`
	ProductIDs   []string `json:"product_ids"`
	PlanType     string   `json:"plan_type"`
	Business     string   `json:"business"`
}

type PlanGroup struct {
	GroupID  string
	Identity PlanGroupIdentity
	Items    []VerifiedBatchItem
}

func NormalizeBatchItems(items []BatchItem) []BatchItem {
	result := make([]BatchItem, len(items))
	copy(result, items)
	for index := range result {
		result[index].WorkURL = strings.TrimSpace(result[index].WorkURL)
		result[index].PlanType = strings.TrimSpace(result[index].PlanType)
		result[index].Business = strings.TrimSpace(result[index].Business)
	}
	return result
}

func NormalizeProductIDs(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, errors.New("product identity contains an empty ID")
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	if len(result) == 0 {
		return nil, errors.New("product identity requires at least one ID")
	}
	sort.Strings(result)
	return result, nil
}

func NewPlanGroupIdentity(advertiserID, templateID, creatorID string, productIDs []string, planType, business string) (PlanGroupIdentity, error) {
	advertiserID, templateID, creatorID = strings.TrimSpace(advertiserID), strings.TrimSpace(templateID), strings.TrimSpace(creatorID)
	if advertiserID == "" || templateID == "" || creatorID == "" {
		return PlanGroupIdentity{}, errors.New("plan group identity requires advertiser, template and creator IDs")
	}
	products, err := NormalizeProductIDs(productIDs)
	if err != nil {
		return PlanGroupIdentity{}, err
	}
	return PlanGroupIdentity{
		AdvertiserID: advertiserID, TemplateID: templateID, CreatorID: creatorID,
		ProductIDs: products, PlanType: strings.TrimSpace(planType), Business: strings.TrimSpace(business),
	}, nil
}

func CanonicalPlanGroupIdentity(identity PlanGroupIdentity) ([]byte, error) {
	normalized, err := NewPlanGroupIdentity(identity.AdvertiserID, identity.TemplateID, identity.CreatorID, identity.ProductIDs, identity.PlanType, identity.Business)
	if err != nil {
		return nil, err
	}
	return json.Marshal(normalized)
}

func GroupID(identity PlanGroupIdentity) (string, error) {
	canonical, err := CanonicalPlanGroupIdentity(identity)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return "qcg_" + hex.EncodeToString(digest[:]), nil
}

func GroupVerifiedItems(advertiserID, templateID string, items []VerifiedBatchItem) ([]PlanGroup, error) {
	groups := make([]PlanGroup, 0)
	indexes := map[string]int{}
	for _, item := range items {
		identity, err := NewPlanGroupIdentity(advertiserID, templateID, item.CreatorID, item.ProductIDs, item.PlanType, item.Business)
		if err != nil {
			return nil, err
		}
		groupID, err := GroupID(identity)
		if err != nil {
			return nil, err
		}
		index, exists := indexes[groupID]
		if !exists {
			index = len(groups)
			indexes[groupID] = index
			groups = append(groups, PlanGroup{GroupID: groupID, Identity: identity, Items: []VerifiedBatchItem{}})
		}
		groups[index].Items = append(groups[index].Items, item)
	}
	return groups, nil
}

func DeduplicateVerifiedItems(items []VerifiedBatchItem) ([]VerifiedBatchItem, []int, error) {
	result := make([]VerifiedBatchItem, 0, len(items))
	duplicates := make([]int, 0)
	seen := map[string]VerifiedBatchItem{}
	for _, item := range items {
		item.WorkID = strings.TrimSpace(item.WorkID)
		item.CreatorID = strings.TrimSpace(item.CreatorID)
		item.BatchItem = NormalizeBatchItems([]BatchItem{item.BatchItem})[0]
		products, err := NormalizeProductIDs(item.ProductIDs)
		if err != nil {
			return nil, nil, err
		}
		item.ProductIDs = products
		previous, exists := seen[item.WorkID]
		if !exists {
			seen[item.WorkID] = item
			result = append(result, item)
			continue
		}
		if previous.CreatorID != item.CreatorID || previous.PlanType != item.PlanType || previous.Business != item.Business || strings.Join(previous.ProductIDs, "\x00") != strings.Join(item.ProductIDs, "\x00") {
			return nil, nil, ErrDuplicateItemConflict
		}
		duplicates = append(duplicates, item.InputIndex)
	}
	return result, duplicates, nil
}
