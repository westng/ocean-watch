package qianchuan

import (
	"context"
	"errors"
	"fmt"
	"strings"

	domainqianchuan "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/qianchuan"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/pagination"
	portqianchuan "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/ports/qianchuan"
)

const (
	WorkQueryBatchSize = 50
	WorkVerifyMaxPages = 100
)

type WorkInput struct {
	InputIndex    int        `json:"input_index"`
	InputURL      string     `json:"input_url,omitempty"`
	AwemeItemID   string     `json:"aweme_item_id,omitempty"`
	CreatorName   string     `json:"creator_name_hint,omitempty"`
	PlanType      string     `json:"plan_type,omitempty"`
	Business      string     `json:"business,omitempty"`
	OwnerHint     *OwnerHint `json:"owner_hint,omitempty"`
	ProductIDHint string     `json:"product_id_hint,omitempty"`
}

type VerifiedWork struct {
	InputIndex        int                               `json:"input_index"`
	InputURL          string                            `json:"input_url,omitempty"`
	AwemeItemID       string                            `json:"aweme_item_id"`
	CreatorName       string                            `json:"creator_name_hint,omitempty"`
	PlanType          string                            `json:"plan_type,omitempty"`
	Business          string                            `json:"business,omitempty"`
	Creator           domainqianchuan.AuthorizedCreator `json:"creator"`
	Material          domainqianchuan.CreatorVideo      `json:"material"`
	MatchedProductIDs []string                          `json:"matched_product_ids"`
}

type SkippedWork struct {
	InputIndex         int      `json:"input_index"`
	InputURL           string   `json:"input_url,omitempty"`
	AwemeItemID        string   `json:"aweme_item_id,omitempty"`
	Reason             string   `json:"reason"`
	Message            string   `json:"message"`
	CandidateAwemeIDs  []string `json:"candidate_aweme_ids,omitempty"`
	HintedProductID    string   `json:"hinted_product_id,omitempty"`
	TemplateProductIDs []string `json:"template_product_ids,omitempty"`
}

type WorkQueryFailure struct {
	AwemeID   string `json:"aweme_id"`
	ProductID string `json:"product_id,omitempty"`
	Message   string `json:"message"`
}

type WorkVerificationRequest struct {
	AdvertiserID string
	AccessToken  string
	ProductIDs   []string
	Works        []WorkInput
	MaxPages     int
}

type WorkVerificationResult struct {
	Matched                    []VerifiedWork                      `json:"matched"`
	Skipped                    []SkippedWork                       `json:"skipped"`
	QueryFailures              []WorkQueryFailure                  `json:"query_failures"`
	Creators                   []domainqianchuan.AuthorizedCreator `json:"creators"`
	DisabledCreators           []domainqianchuan.AuthorizedCreator `json:"disabled_creators"`
	AuthorizedCreatorScanCount int                                 `json:"authorized_creator_scan_count"`
	AuthorizedCreatorPageCount int                                 `json:"authorized_creator_page_count"`
	OwnershipQueryCount        int                                 `json:"ownership_query_count"`
	ProductQueryCount          int                                 `json:"product_query_count"`
	ResolvedOwnerHints         map[string]OwnerHint                `json:"resolved_owner_hints"`
	OwnerHintSummary           OwnerHintSummary                    `json:"owner_hint_summary"`
}

type WorkVerifier struct {
	Reader portqianchuan.Reader
}

func (verifier WorkVerifier) Verify(
	ctx context.Context,
	request WorkVerificationRequest,
) (WorkVerificationResult, error) {
	if ctx == nil {
		return WorkVerificationResult{}, errors.New("Qianchuan work verification context is required")
	}
	if verifier.Reader == nil {
		return WorkVerificationResult{}, errors.New("Qianchuan work verification reader is required")
	}
	request.AdvertiserID = strings.TrimSpace(request.AdvertiserID)
	request.AccessToken = strings.TrimSpace(request.AccessToken)
	if !validPositiveID(request.AdvertiserID) {
		return WorkVerificationResult{}, errors.New("advertiser_id must be a positive decimal ID")
	}
	if request.AccessToken == "" {
		return WorkVerificationResult{}, errors.New("Qianchuan work verification access token is required")
	}
	_, productIDs, err := normalizeIDSet(request.ProductIDs, "product_id", 30)
	if err != nil {
		return WorkVerificationResult{}, err
	}
	works, skipped := normalizeWorkInputs(request.Works)
	result := WorkVerificationResult{
		Matched: []VerifiedWork{}, Skipped: skipped, QueryFailures: []WorkQueryFailure{},
		Creators:           []domainqianchuan.AuthorizedCreator{},
		DisabledCreators:   []domainqianchuan.AuthorizedCreator{},
		ResolvedOwnerHints: map[string]OwnerHint{},
	}
	if len(works) == 0 {
		return result, nil
	}
	maxPages := request.MaxPages
	if maxPages == 0 {
		maxPages = WorkVerifyMaxPages
	}
	owners := make(map[string]map[string]verifiedOwner, len(works))
	creatorsByID := map[string]domainqianchuan.AuthorizedCreator{}
	creatorOrder := []string{}
	suppliedHints := normalizedOwnerHints(works)
	result.OwnerHintSummary.Supplied = len(suppliedHints)
	eligibleHints := map[string]OwnerHint{}
	showIDOrder, itemIDsByShowID := ownerHintShowIDGroups(works, suppliedHints)
	for _, showID := range showIDOrder {
		result.OwnerHintSummary.AuthorizedHintQueryCount++
		creator, found, _, fetchErr := verifier.resolveAuthorizedHint(ctx, request, showID, maxPages)
		if fetchErr != nil {
			result.OwnerHintSummary.AuthorizedHintFailureCount++
			continue
		}
		if !found || !creatorUsable(creator) {
			if found && !creatorUsable(creator) {
				result.DisabledCreators = appendCreatorUnique(result.DisabledCreators, creator)
			}
			continue
		}
		for _, itemID := range itemIDsByShowID[showID] {
			hint := suppliedHints[itemID]
			if hint.AwemeID != creator.AwemeID {
				continue
			}
			eligibleHints[itemID] = hint
		}
		creatorsByID, creatorOrder = addCreator(creatorsByID, creatorOrder, creator)
	}
	result.OwnerHintSummary.Eligible = len(eligibleHints)

	ownershipFailed := false
	targetedByCreator := workIDsByHintedCreator(works, eligibleHints)
	for _, creatorID := range creatorOrder {
		itemIDs := targetedByCreator[creatorID]
		if len(itemIDs) == 0 {
			continue
		}
		creator := creatorsByID[creatorID]
		for _, batch := range stringBatches(itemIDs, WorkQueryBatchSize) {
			result.OwnershipQueryCount++
			result.OwnerHintSummary.OfficialVideoQueryCount++
			videos, fetchErr := verifier.queryWorks(ctx, request, creator.AwemeID, "", batch)
			if fetchErr != nil {
				ownershipFailed = true
				result.QueryFailures = append(result.QueryFailures, WorkQueryFailure{
					AwemeID: creator.AwemeID, Message: fetchErr.Error(),
				})
				continue
			}
			if err := collectVerifiedOwners(owners, creator, batch, videos); err != nil {
				return WorkVerificationResult{}, err
			}
		}
	}

	verifiedHintIDs := map[string]struct{}{}
	for itemID, hint := range eligibleHints {
		if _, exists := owners[itemID][hint.AwemeID]; exists {
			verifiedHintIDs[itemID] = struct{}{}
		}
	}
	result.OwnerHintSummary.Verified = len(verifiedHintIDs)
	result.OwnerHintSummary.Stale = len(suppliedHints) - len(verifiedHintIDs)
	broadWorkIDs := make([]string, 0, len(works)-len(verifiedHintIDs))
	for _, work := range works {
		if _, verified := verifiedHintIDs[work.AwemeItemID]; !verified {
			broadWorkIDs = append(broadWorkIDs, work.AwemeItemID)
		}
	}
	result.OwnerHintSummary.BroadScanWorkCount = len(broadWorkIDs)
	if len(broadWorkIDs) != 0 {
		creators, pages, fetchErr := verifier.listAuthorizedCreators(ctx, request, maxPages)
		if fetchErr != nil {
			return WorkVerificationResult{}, fetchErr
		}
		result.AuthorizedCreatorScanCount = 1
		result.AuthorizedCreatorPageCount = pages
		usable := make([]domainqianchuan.AuthorizedCreator, 0, len(creators))
		for _, creator := range creators {
			if creatorUsable(creator) {
				usable = append(usable, creator)
				creatorsByID, creatorOrder = addCreator(creatorsByID, creatorOrder, creator)
				continue
			}
			result.DisabledCreators = appendCreatorUnique(result.DisabledCreators, creator)
		}
		for _, creator := range usable {
			creatorWorkIDs := broadWorkIDsForCreator(broadWorkIDs, creator.AwemeID, eligibleHints)
			for _, batch := range stringBatches(creatorWorkIDs, WorkQueryBatchSize) {
				result.OwnershipQueryCount++
				result.OwnerHintSummary.OfficialVideoQueryCount++
				videos, queryErr := verifier.queryWorks(ctx, request, creator.AwemeID, "", batch)
				if queryErr != nil {
					ownershipFailed = true
					result.QueryFailures = append(result.QueryFailures, WorkQueryFailure{
						AwemeID: creator.AwemeID, Message: queryErr.Error(),
					})
					continue
				}
				if err := collectVerifiedOwners(owners, creator, batch, videos); err != nil {
					return WorkVerificationResult{}, err
				}
			}
		}
	}
	for _, creatorID := range creatorOrder {
		result.Creators = append(result.Creators, creatorsByID[creatorID])
	}

	resolved := map[string]verifiedOwner{}
	for _, work := range works {
		candidates := owners[work.AwemeItemID]
		switch len(candidates) {
		case 1:
			for _, owner := range candidates {
				resolved[work.AwemeItemID] = owner
			}
		case 0:
			reason, message := "not_found_under_authorized_creators", "作品未在当前广告主可投的授权达人中找到"
			if ownershipFailed {
				reason, message = "creator_query_incomplete", "达人作品查询不完整，未将作品视为未授权"
			}
			result.Skipped = append(result.Skipped, skippedFromWork(work, reason, message, nil))
		default:
			result.Skipped = append(result.Skipped, skippedFromWork(
				work, "ambiguous_creator", "作品匹配到多个授权达人", sortedMapKeys(candidates),
			))
		}
	}

	worksByCreator := map[string][]WorkInput{}
	resolvedCreatorOrder := []string{}
	resolvedCreatorSeen := map[string]struct{}{}
	for _, work := range works {
		owner, ok := resolved[work.AwemeItemID]
		if !ok {
			continue
		}
		worksByCreator[owner.Creator.AwemeID] = append(worksByCreator[owner.Creator.AwemeID], work)
		creatorsByID[owner.Creator.AwemeID] = owner.Creator
		if _, exists := resolvedCreatorSeen[owner.Creator.AwemeID]; !exists {
			resolvedCreatorSeen[owner.Creator.AwemeID] = struct{}{}
			resolvedCreatorOrder = append(resolvedCreatorOrder, owner.Creator.AwemeID)
		}
		result.ResolvedOwnerHints[work.AwemeItemID] = OwnerHint{
			AwemeID: owner.Creator.AwemeID, AwemeShowID: owner.Creator.VisibleID,
		}
	}
	matches := map[string]domainqianchuan.CreatorVideo{}
	matchedProducts := map[string]map[string]struct{}{}
	failedProductWorks := map[string]bool{}
	for _, creatorID := range resolvedCreatorOrder {
		creator := creatorsByID[creatorID]
		creatorWorks := worksByCreator[creatorID]
		if len(creatorWorks) == 0 {
			continue
		}
		idsByProduct := workIDsByProductHint(creatorWorks, productIDs)
		for _, productID := range productIDs {
			for _, batch := range stringBatches(idsByProduct[productID], WorkQueryBatchSize) {
				result.ProductQueryCount++
				videos, fetchErr := verifier.queryWorks(
					ctx, request, creator.AwemeID, productID, batch,
				)
				if fetchErr != nil {
					for _, itemID := range batch {
						failedProductWorks[itemID] = true
					}
					result.QueryFailures = append(result.QueryFailures, WorkQueryFailure{
						AwemeID: creator.AwemeID, ProductID: productID, Message: fetchErr.Error(),
					})
					continue
				}
				requested := stringSetFrom(batch)
				for _, video := range videos {
					if _, ok := requested[video.AwemeItemID]; !ok {
						return WorkVerificationResult{}, errors.New("Qianchuan product query returned an unrequested work")
					}
					matches[video.AwemeItemID] = video
					set := matchedProducts[video.AwemeItemID]
					if set == nil {
						set = map[string]struct{}{}
						matchedProducts[video.AwemeItemID] = set
					}
					set[productID] = struct{}{}
				}
			}
		}
	}
	for _, work := range works {
		owner, ok := resolved[work.AwemeItemID]
		if !ok {
			continue
		}
		productSet := matchedProducts[work.AwemeItemID]
		if len(productSet) == 0 {
			reason, message := "product_mismatch", "作品与模板绑定商品不匹配"
			if failedProductWorks[work.AwemeItemID] {
				reason, message = "product_query_incomplete", "作品商品复核不完整，未将作品视为商品不匹配"
			}
			result.Skipped = append(result.Skipped, skippedFromWork(work, reason, message, nil))
			continue
		}
		orderedProducts := make([]string, 0, len(productSet))
		for _, productID := range productIDs {
			if _, exists := productSet[productID]; exists {
				orderedProducts = append(orderedProducts, productID)
			}
		}
		result.Matched = append(result.Matched, VerifiedWork{
			InputIndex: work.InputIndex, InputURL: work.InputURL, AwemeItemID: work.AwemeItemID,
			CreatorName: work.CreatorName, PlanType: work.PlanType, Business: work.Business,
			Creator: creatorsByID[owner.Creator.AwemeID], Material: matches[work.AwemeItemID],
			MatchedProductIDs: orderedProducts,
		})
	}
	return result, nil
}

type verifiedOwner struct {
	Creator  domainqianchuan.AuthorizedCreator
	Material domainqianchuan.CreatorVideo
}

func (verifier WorkVerifier) listAuthorizedCreators(
	ctx context.Context,
	request WorkVerificationRequest,
	maxPages int,
) ([]domainqianchuan.AuthorizedCreator, int, error) {
	pageCount := 0
	rows, err := pagination.CollectPages(ctx, pagination.PageOptions[domainqianchuan.AuthorizedCreator]{
		MaxPages: maxPages,
		Key:      func(row domainqianchuan.AuthorizedCreator) string { return row.AwemeID },
		Fetch: func(ctx context.Context, page int) (pagination.Page[domainqianchuan.AuthorizedCreator], error) {
			pageCount++
			result, fetchErr := verifier.Reader.FetchAuthorizedCreators(ctx, portqianchuan.AuthorizedCreatorPageRequest{
				AdvertiserID: request.AdvertiserID, AccessToken: request.AccessToken,
				MarketingGoal: "VIDEO_PROM_GOODS", Scene: "CREATE", Page: page, PageSize: 100,
			})
			if fetchErr != nil {
				return pagination.Page[domainqianchuan.AuthorizedCreator]{}, fetchErr
			}
			return pagination.Page[domainqianchuan.AuthorizedCreator]{
				Number: result.PageInfo.Page, TotalPages: result.PageInfo.TotalPages,
				TotalNumber: result.PageInfo.TotalNumber, Rows: result.Rows,
			}, nil
		},
	})
	if err != nil {
		return nil, pageCount, fmt.Errorf("scan Qianchuan authorized creators: %w", err)
	}
	return rows, pageCount, nil
}

func (verifier WorkVerifier) resolveAuthorizedHint(
	ctx context.Context,
	request WorkVerificationRequest,
	showID string,
	maxPages int,
) (domainqianchuan.AuthorizedCreator, bool, int, error) {
	showID = strings.TrimSpace(showID)
	if showID == "" {
		return domainqianchuan.AuthorizedCreator{}, false, 0, nil
	}
	for page := 1; page <= maxPages; page++ {
		result, err := verifier.Reader.FetchAuthorizedCreators(ctx, portqianchuan.AuthorizedCreatorPageRequest{
			AdvertiserID: request.AdvertiserID, AccessToken: request.AccessToken,
			SearchKeyword: showID, MarketingGoal: "VIDEO_PROM_GOODS", Scene: "CREATE",
			Page: page, PageSize: 100,
		})
		if err != nil {
			return domainqianchuan.AuthorizedCreator{}, false, page, err
		}
		if result.PageInfo.Page != page || result.PageInfo.TotalPages < 0 || result.PageInfo.TotalNumber < 0 {
			return domainqianchuan.AuthorizedCreator{}, false, page, errors.New("targeted authorized creator query returned invalid pagination")
		}
		matches := map[string]domainqianchuan.AuthorizedCreator{}
		for _, creator := range result.Rows {
			if strings.TrimSpace(creator.VisibleID) == showID && validPositiveID(strings.TrimSpace(creator.AwemeID)) {
				matches[creator.AwemeID] = creator
			}
		}
		if len(matches) == 1 {
			for _, creator := range matches {
				return creator, true, page, nil
			}
		}
		if len(matches) > 1 {
			return domainqianchuan.AuthorizedCreator{}, false, page, errors.New("targeted authorized creator query returned multiple exact matches")
		}
		if result.PageInfo.TotalPages == 0 || page >= result.PageInfo.TotalPages {
			return domainqianchuan.AuthorizedCreator{}, false, page, nil
		}
	}
	return domainqianchuan.AuthorizedCreator{}, false, maxPages, errors.New("targeted authorized creator query exceeded the page limit")
}

func (verifier WorkVerifier) queryWorks(
	ctx context.Context,
	request WorkVerificationRequest,
	awemeID string,
	productID string,
	itemIDs []string,
) ([]domainqianchuan.CreatorVideo, error) {
	result, err := verifier.Reader.FetchCreatorVideos(ctx, portqianchuan.CreatorVideoPageRequest{
		AdvertiserID: request.AdvertiserID, AccessToken: request.AccessToken,
		AwemeID: awemeID, ProductID: productID, AwemeItemIDs: itemIDs, Count: WorkQueryBatchSize,
	})
	if err != nil {
		return nil, err
	}
	if result.HasMore {
		return nil, errors.New("filtered Qianchuan work query unexpectedly requires cursor pagination")
	}
	return result.Rows, nil
}

func normalizeWorkInputs(values []WorkInput) ([]WorkInput, []SkippedWork) {
	result := make([]WorkInput, 0, len(values))
	skipped := make([]SkippedWork, 0)
	seen := map[string]struct{}{}
	for index, value := range values {
		value.InputURL = strings.TrimSpace(value.InputURL)
		value.AwemeItemID = strings.TrimSpace(value.AwemeItemID)
		value.CreatorName = strings.TrimSpace(value.CreatorName)
		value.PlanType = strings.TrimSpace(value.PlanType)
		value.Business = strings.TrimSpace(value.Business)
		value.ProductIDHint = strings.TrimSpace(value.ProductIDHint)
		if value.InputIndex < 0 {
			value.InputIndex = index
		}
		if !validPositiveID(value.AwemeItemID) {
			skipped = append(skipped, skippedFromWork(value, "invalid_work_id", "作品链接未解析出有效作品 ID", nil))
			continue
		}
		if _, duplicate := seen[value.AwemeItemID]; duplicate {
			skipped = append(skipped, skippedFromWork(value, "duplicate_input", "同一作品在本批次中重复出现", nil))
			continue
		}
		seen[value.AwemeItemID] = struct{}{}
		result = append(result, value)
	}
	return result, skipped
}

func creatorUsable(creator domainqianchuan.AuthorizedCreator) bool {
	return (creator.HasAuthorized == nil || *creator.HasAuthorized) &&
		(creator.ProductPromotionDisabled == nil || !*creator.ProductPromotionDisabled)
}

func normalizedOwnerHints(works []WorkInput) map[string]OwnerHint {
	result := map[string]OwnerHint{}
	for _, work := range works {
		if work.OwnerHint == nil {
			continue
		}
		hint := OwnerHint{
			AwemeID:     strings.TrimSpace(work.OwnerHint.AwemeID),
			AwemeShowID: strings.TrimSpace(work.OwnerHint.AwemeShowID),
		}
		if validPositiveID(hint.AwemeID) {
			result[work.AwemeItemID] = hint
		}
	}
	return result
}

func ownerHintShowIDGroups(
	works []WorkInput,
	hints map[string]OwnerHint,
) ([]string, map[string][]string) {
	order := []string{}
	items := map[string][]string{}
	for _, work := range works {
		hint, exists := hints[work.AwemeItemID]
		if !exists || hint.AwemeShowID == "" {
			continue
		}
		if _, seen := items[hint.AwemeShowID]; !seen {
			order = append(order, hint.AwemeShowID)
		}
		items[hint.AwemeShowID] = append(items[hint.AwemeShowID], work.AwemeItemID)
	}
	return order, items
}

func workIDsByHintedCreator(works []WorkInput, hints map[string]OwnerHint) map[string][]string {
	result := map[string][]string{}
	for _, work := range works {
		if hint, exists := hints[work.AwemeItemID]; exists {
			result[hint.AwemeID] = append(result[hint.AwemeID], work.AwemeItemID)
		}
	}
	return result
}

func broadWorkIDsForCreator(workIDs []string, creatorID string, hints map[string]OwnerHint) []string {
	result := make([]string, 0, len(workIDs))
	for _, itemID := range workIDs {
		if hint, exists := hints[itemID]; exists && hint.AwemeID == creatorID {
			continue
		}
		result = append(result, itemID)
	}
	return result
}

func collectVerifiedOwners(
	owners map[string]map[string]verifiedOwner,
	creator domainqianchuan.AuthorizedCreator,
	requestedIDs []string,
	videos []domainqianchuan.CreatorVideo,
) error {
	requested := stringSetFrom(requestedIDs)
	for _, video := range videos {
		if _, ok := requested[video.AwemeItemID]; !ok {
			return errors.New("Qianchuan ownership query returned an unrequested work")
		}
		byCreator := owners[video.AwemeItemID]
		if byCreator == nil {
			byCreator = map[string]verifiedOwner{}
			owners[video.AwemeItemID] = byCreator
		}
		byCreator[creator.AwemeID] = verifiedOwner{Creator: creator, Material: video}
	}
	return nil
}

func addCreator(
	creators map[string]domainqianchuan.AuthorizedCreator,
	order []string,
	creator domainqianchuan.AuthorizedCreator,
) (map[string]domainqianchuan.AuthorizedCreator, []string) {
	if _, exists := creators[creator.AwemeID]; !exists {
		order = append(order, creator.AwemeID)
	}
	creators[creator.AwemeID] = creator
	return creators, order
}

func appendCreatorUnique(
	creators []domainqianchuan.AuthorizedCreator,
	creator domainqianchuan.AuthorizedCreator,
) []domainqianchuan.AuthorizedCreator {
	for _, existing := range creators {
		if existing.AwemeID == creator.AwemeID {
			return creators
		}
	}
	return append(creators, creator)
}

func skippedFromWork(work WorkInput, reason, message string, candidates []string) SkippedWork {
	return SkippedWork{
		InputIndex: work.InputIndex, InputURL: work.InputURL, AwemeItemID: work.AwemeItemID,
		Reason: reason, Message: message, CandidateAwemeIDs: candidates,
	}
}

func workInputIDs(works []WorkInput) []string {
	result := make([]string, len(works))
	for index, work := range works {
		result[index] = work.AwemeItemID
	}
	return result
}

func workIDsByProductHint(works []WorkInput, productIDs []string) map[string][]string {
	result := make(map[string][]string, len(productIDs))
	allowed := stringSetFrom(productIDs)
	for _, work := range works {
		if _, valid := allowed[work.ProductIDHint]; work.ProductIDHint != "" && valid {
			result[work.ProductIDHint] = append(result[work.ProductIDHint], work.AwemeItemID)
			continue
		}
		for _, productID := range productIDs {
			result[productID] = append(result[productID], work.AwemeItemID)
		}
	}
	return result
}

func stringBatches(values []string, size int) [][]string {
	result := make([][]string, 0, (len(values)+size-1)/size)
	for start := 0; start < len(values); start += size {
		end := min(start+size, len(values))
		result = append(result, append([]string(nil), values[start:end]...))
	}
	return result
}

func stringSetFrom(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func sortedMapKeys[V any](values map[string]V) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	for index := 1; index < len(result); index++ {
		for cursor := index; cursor > 0 && result[cursor] < result[cursor-1]; cursor-- {
			result[cursor], result[cursor-1] = result[cursor-1], result[cursor]
		}
	}
	return result
}
