package marketing

import (
	"context"
	"errors"
	"sort"
	"strings"

	applicationdiscovery "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/discovery"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/configuration"
	domainmarketing "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/marketing"
)

const (
	creatorCoverPageSize = 20
	creatorCoverMaxPages = 50
	creatorCoverOKStatus = "MATERIAL_STATUS_OK"
)

type CreatorPromotionDiscovery interface {
	QueryPromotions(context.Context, applicationdiscovery.PromotionQuery) (applicationdiscovery.Result, error)
}

type HistoricalCreatorCoverResolver struct {
	Discovery CreatorPromotionDiscovery
}

type CreatorCoverError struct {
	Code    string
	Message string
	Details map[string]any
}

func (err *CreatorCoverError) Error() string { return err.Message }

func (resolver HistoricalCreatorCoverResolver) Resolve(
	ctx context.Context,
	request CreatorCoverRequest,
) (CreatorCoverResult, error) {
	candidates := cloneCreatorCandidates(request.Candidates)
	missing := missingCreatorCoverIndexes(candidates)
	if len(missing) == 0 {
		return CreatorCoverResult{Candidates: candidates, Diagnostics: map[string]any{"status": "not_required"}}, nil
	}
	if resolver.Discovery == nil {
		return CreatorCoverResult{}, errors.New("Marketing promotion discovery is required for creator cover recovery")
	}
	advertiserID := strings.TrimSpace(request.AdvertiserID)
	for _, index := range missing {
		if candidates[index].OwnerAdvertiserID != advertiserID || !validPositiveID(advertiserID) {
			return CreatorCoverResult{}, &CreatorCoverError{
				Code: "creator_cover_owner_mismatch", Message: "missing creator covers must belong to one valid advertiser",
			}
		}
		if candidates[index].ItemID == "" || candidates[index].MaterialID == "" {
			return CreatorCoverResult{}, &CreatorCoverError{
				Code: "creator_cover_identity_missing", Message: "missing creator covers require both item_id and material_id",
			}
		}
	}
	promotions, requestIDs, err := resolver.fetchPromotions(ctx, advertiserID, request.AuthAccountID)
	if err != nil {
		return CreatorCoverResult{}, err
	}
	references := matchingCreatorCoverReferences(promotions, advertiserID, candidates, missing)
	resolvedRows := make([]any, 0, len(missing))
	unresolved := make([]string, 0)
	for _, index := range missing {
		candidate := &candidates[index]
		key := creatorCoverKey(candidate.ItemID, candidate.MaterialID)
		covers := references[key]
		if len(covers) > 1 {
			coverIDs := make([]string, 0, len(covers))
			for coverID := range covers {
				coverIDs = append(coverIDs, coverID)
			}
			sort.Strings(coverIDs)
			return CreatorCoverResult{Candidates: candidates}, &CreatorCoverError{
				Code:    "creator_cover_selection_required",
				Message: "multiple historical covers match the selected creator material",
				Details: map[string]any{"item_id": candidate.ItemID, "material_id": candidate.MaterialID, "candidate_cover_ids": coverIDs},
			}
		}
		if len(covers) == 0 {
			unresolved = append(unresolved, candidate.ItemID)
			continue
		}
		for coverID, coverReferences := range covers {
			candidate.VideoCoverID = coverID
			candidate.UnusableReasons = withoutString(candidate.UnusableReasons, "missing_video_cover_id")
			candidate.Usable = len(candidate.UnusableReasons) == 0
			resolvedRows = append(resolvedRows, map[string]any{
				"item_id": candidate.ItemID, "material_id": candidate.MaterialID,
				"video_cover_id": coverID, "references": coverReferences,
			})
		}
	}
	status := "resolved"
	if len(unresolved) != 0 {
		status = "partially_resolved"
	}
	diagnostics := map[string]any{
		"status": status, "source": "matching_official_promotion", "resolved": resolvedRows,
		"unresolved_item_ids": unresolved, "promotion_count": len(promotions), "request_ids": requestIDs,
	}
	return CreatorCoverResult{Candidates: candidates, Diagnostics: diagnostics}, nil
}

func (resolver HistoricalCreatorCoverResolver) fetchPromotions(
	ctx context.Context,
	advertiserID string,
	authAccountID string,
) ([]map[string]any, []string, error) {
	rows := []map[string]any{}
	requestIDs := []string{}
	totalPages := 1
	for page := 1; page <= totalPages; page++ {
		result, err := resolver.Discovery.QueryPromotions(
			ctx,
			applicationdiscovery.PromotionQuery{
				CredentialScope: applicationdiscovery.CredentialScope{
					AdvertiserID: advertiserID, AuthAccountID: authAccountID,
				},
				Page: page, PageSize: creatorCoverPageSize,
			},
		)
		if err != nil {
			return nil, nil, &CreatorCoverError{
				Code:    "creator_cover_history_query_failed",
				Message: "official promotion query failed while resolving a creator cover",
				Details: map[string]any{"endpoint": applicationdiscovery.PromotionEndpoint, "cause": err.Error()},
			}
		}
		if result.RequestID != "" {
			requestIDs = append(requestIDs, result.RequestID)
		}
		pageRows, err := promotionRows(result.Response)
		if err != nil {
			return nil, nil, err
		}
		rows = append(rows, pageRows...)
		declared, err := creatorCoverTotalPages(result.Response, page, len(pageRows))
		if err != nil {
			return nil, nil, err
		}
		if page == 1 {
			totalPages = declared
		} else if declared != totalPages {
			return nil, nil, creatorCoverPaginationError("promotion history changed total_page during traversal")
		}
	}
	return rows, requestIDs, nil
}

func promotionRows(response map[string]any) ([]map[string]any, error) {
	value := configuration.Value(response, "data.list")
	if value == nil {
		return []map[string]any{}, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, creatorCoverPaginationError("promotion history returned an invalid list")
	}
	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			return nil, creatorCoverPaginationError("promotion history returned a non-object row")
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func creatorCoverTotalPages(response map[string]any, page, rowCount int) (int, error) {
	value := configuration.Value(response, "data.page_info.total_page")
	if value == nil {
		if rowCount >= creatorCoverPageSize {
			return 0, creatorCoverPaginationError("promotion history omitted pagination metadata for a full page")
		}
		return page, nil
	}
	total, err := configuration.Integer(value)
	if err != nil || total < page || total < 0 || total == 0 && rowCount != 0 {
		return 0, creatorCoverPaginationError("promotion history returned contradictory pagination metadata")
	}
	if total > creatorCoverMaxPages {
		return 0, &CreatorCoverError{
			Code: "creator_cover_history_too_large", Message: "promotion history exceeds the creator-cover safety limit",
			Details: map[string]any{"total_pages": total, "max_pages": creatorCoverMaxPages},
		}
	}
	return total, nil
}

func matchingCreatorCoverReferences(
	promotions []map[string]any,
	advertiserID string,
	candidates []domainmarketing.CreatorCandidate,
	missing []int,
) map[string]map[string][]any {
	targets := map[string]struct{}{}
	for _, index := range missing {
		targets[creatorCoverKey(candidates[index].ItemID, candidates[index].MaterialID)] = struct{}{}
	}
	result := map[string]map[string][]any{}
	for _, promotion := range promotions {
		owner := textValue(configuration.Value(promotion, "advertiser_id"))
		if owner != "" && owner != advertiserID {
			continue
		}
		for _, raw := range configuration.List(configuration.Value(promotion, "promotion_materials.video_material_list")) {
			material := configuration.Object(raw)
			key := creatorCoverKey(textValue(material["item_id"]), textValue(material["material_id"]))
			if _, ok := targets[key]; !ok || textValue(material["material_status"]) != creatorCoverOKStatus {
				continue
			}
			coverID := textValue(material["video_cover_id"])
			if coverID == "" {
				continue
			}
			if result[key] == nil {
				result[key] = map[string][]any{}
			}
			result[key][coverID] = append(result[key][coverID], map[string]any{
				"project_id":   textValue(promotion["project_id"]),
				"promotion_id": textValue(promotion["promotion_id"]),
				"material_id":  textValue(material["material_id"]),
			})
		}
	}
	return result
}

func missingCreatorCoverIndexes(candidates []domainmarketing.CreatorCandidate) []int {
	result := []int{}
	for index, candidate := range candidates {
		if candidate.VideoCoverID == "" && creatorReasonContains(candidate.UnusableReasons, "missing_video_cover_id") {
			result = append(result, index)
		}
	}
	return result
}

func cloneCreatorCandidates(source []domainmarketing.CreatorCandidate) []domainmarketing.CreatorCandidate {
	result := append([]domainmarketing.CreatorCandidate(nil), source...)
	for index := range result {
		result[index].WarningTypes = append([]string(nil), result[index].WarningTypes...)
		result[index].UnusableReasons = append([]string(nil), result[index].UnusableReasons...)
		if result[index].SourceKey != nil {
			copy := *result[index].SourceKey
			result[index].SourceKey = &copy
		}
	}
	return result
}

func creatorCoverKey(itemID, materialID string) string { return itemID + "\x00" + materialID }

func withoutString(values []string, removed string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != removed {
			result = append(result, value)
		}
	}
	return result
}

func creatorCoverPaginationError(message string) error {
	return &CreatorCoverError{Code: "creator_cover_history_pagination_invalid", Message: message}
}
