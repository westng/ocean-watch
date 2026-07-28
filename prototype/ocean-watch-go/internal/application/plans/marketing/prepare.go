package marketing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	applicationmaterials "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/materials"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/configuration"
	domainmarketing "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/marketing"
	domaintemplates "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/templates"
)

const (
	AccountUploadSource     = "ACCOUNT_UPLOAD"
	CreatorAuthorizedSource = "CREATOR_AUTHORIZED"
	defaultMaterialLimit    = 5
)

type ConfigReader interface {
	Read(context.Context) (map[string]any, error)
}

type MaterialService interface {
	QueryVideos(context.Context, applicationmaterials.VideoQuery) (applicationmaterials.VideoResult, error)
	QueryCreator(context.Context, applicationmaterials.CreatorQuery) (applicationmaterials.CreatorResult, error)
}

type CreatorCoverResolver interface {
	Resolve(context.Context, CreatorCoverRequest) (CreatorCoverResult, error)
}

type CreatorCoverRequest struct {
	AdvertiserID  string
	AuthAccountID string
	Candidates    []domainmarketing.CreatorCandidate
}

type CreatorCoverResult struct {
	Candidates  []domainmarketing.CreatorCandidate
	Diagnostics map[string]any
}

type PrepareKind string

const (
	PrepareUpload  PrepareKind = "upload"
	PrepareCreator PrepareKind = "creator"
)

type PrepareRequest struct {
	Kind            PrepareKind
	AdvertiserID    string
	AuthAccountID   string
	PlanTemplate    string
	Submit          bool
	OnlinePreflight bool
	VideoIDs        []string
	ItemIDs         []string
	ExpectedAwemeID string
	AppendCreatorID bool
	Budget          any
	CPABid          any
	ROIGoal         any
	MaterialDate    string
	ProductName     string
	ProductID       string
	ProjectName     string
	PromotionName   string
	ProjectID       string
	GroupIndex      int
	Index           int
	Suffix          string
	Now             time.Time
}

type PreparedPlan struct {
	Kind                   PrepareKind                        `json:"kind"`
	AdvertiserID           string                             `json:"advertiser_id"`
	AuthAccountID          string                             `json:"auth_account_id,omitempty"`
	SourceType             string                             `json:"source_type"`
	SelectedPlanTemplate   any                                `json:"selected_plan_template"`
	SelectedVideos         []PreparedVideo                    `json:"selected_videos,omitempty"`
	SelectedCreator        *PreparedCreator                   `json:"selected_creator,omitempty"`
	MaterialDate           string                             `json:"material_date"`
	ProductName            string                             `json:"product_name"`
	ProjectEndpoint        string                             `json:"project_endpoint"`
	PromotionEndpoint      string                             `json:"promotion_endpoint"`
	ProjectPayload         map[string]any                     `json:"project_payload"`
	PromotionPayload       map[string]any                     `json:"promotion_payload"`
	MissingFields          []string                           `json:"missing_fields"`
	PreflightSummary       map[string]any                     `json:"preflight_summary"`
	RuntimeAssetResolution map[string]any                     `json:"runtime_asset_resolution,omitempty"`
	CreatorCoverResolution map[string]any                     `json:"creator_cover_resolution,omitempty"`
	ProjectJSON            json.RawMessage                    `json:"-"`
	PromotionJSON          json.RawMessage                    `json:"-"`
	CreatorCandidates      []domainmarketing.CreatorCandidate `json:"-"`
}

type PreparedVideo struct {
	VideoID   string `json:"video_id"`
	CoverID   string `json:"video_cover_id,omitempty"`
	ItemID    string `json:"item_id,omitempty"`
	Title     string `json:"title,omitempty"`
	CreatorID string `json:"creator_id,omitempty"`
}

type PreparedCreator struct {
	AwemeID   string `json:"aweme_id"`
	AwemeName string `json:"aweme_name,omitempty"`
}

type Preparer struct {
	Config        ConfigReader
	Materials     MaterialService
	CreatorCovers CreatorCoverResolver
	RuntimeAssets RuntimeAssetResolver
	Now           func() time.Time
}

func (preparer Preparer) Prepare(ctx context.Context, request PrepareRequest) (PreparedPlan, error) {
	if ctx == nil {
		return PreparedPlan{}, errors.New("Marketing plan preparation context is required")
	}
	if preparer.Config == nil {
		return PreparedPlan{}, errors.New("Marketing plan config reader is required")
	}
	request = normalizePrepareRequest(request, preparer.Now)
	if err := validatePrepareRequest(request); err != nil {
		return PreparedPlan{}, err
	}
	raw, err := preparer.Config.Read(ctx)
	if err != nil {
		return PreparedPlan{}, fmt.Errorf("read Marketing plan config: %w", err)
	}
	runtime, _, err := configuration.Runtime(raw, "marketing", "create")
	if err != nil {
		return PreparedPlan{}, err
	}
	effective, err := domaintemplates.ApplyMarketingPlanTemplate(runtime, domaintemplates.MarketingPlanTemplateSelection{
		Name: request.PlanTemplate, AdvertiserID: request.AdvertiserID, Channel: "marketing",
	})
	if err != nil {
		return PreparedPlan{}, err
	}
	advertiserID := strings.TrimSpace(request.AdvertiserID)
	if advertiserID == "" {
		advertiserID = textValue(configuration.Value(effective, "account.advertiser_id"))
	}
	if !validPositiveID(advertiserID) {
		return PreparedPlan{}, errors.New("advertiser_id must be a positive decimal ID")
	}
	request.AdvertiserID = advertiserID
	if request.MaterialDate == "" {
		yesterday := request.Now.AddDate(0, 0, -1)
		request.MaterialDate = fmt.Sprintf("%d.%d", int(yesterday.Month()), yesterday.Day())
	}
	if request.ProductName == "" {
		request.ProductName = textValue(configuration.Value(effective, "defaults.product_name"))
	}
	sourceType := textValue(configuration.Value(effective, "material_strategy.source_type"))
	wantSource := AccountUploadSource
	if request.Kind == PrepareCreator {
		wantSource = CreatorAuthorizedSource
	}
	if sourceType != wantSource {
		return PreparedPlan{}, fmt.Errorf(
			"selected plan template uses %s materials; %s creation requires %s",
			sourceType, request.Kind, wantSource,
		)
	}
	limit, err := materialLimit(effective)
	if err != nil {
		return PreparedPlan{}, err
	}
	if request.Kind == PrepareUpload && len(request.VideoIDs) > limit {
		return PreparedPlan{}, fmt.Errorf("select no more than %d uploaded videos for one unit", limit)
	}
	if request.Kind == PrepareCreator && len(request.ItemIDs) > limit {
		return PreparedPlan{}, fmt.Errorf("select no more than %d creator materials for one unit", limit)
	}

	selectedVideos := []PreparedVideo{}
	var selectedCreator *PreparedCreator
	var candidates []domainmarketing.CreatorCandidate
	var runtimeAssetResolution map[string]any
	var creatorCoverResolution map[string]any
	online := request.Submit || request.OnlinePreflight
	if online {
		if preparer.RuntimeAssets == nil {
			return PreparedPlan{}, errors.New("Marketing runtime asset resolver is required for online preflight")
		}
		resolvedAssets, resolveErr := preparer.RuntimeAssets.Resolve(ctx, RuntimeAssetRequest{
			AdvertiserID: request.AdvertiserID, AuthAccountID: request.AuthAccountID,
			Config: effective,
		})
		if resolveErr != nil {
			return PreparedPlan{}, fmt.Errorf("resolve Marketing runtime assets: %w", resolveErr)
		}
		effective = resolvedAssets.Config
		runtimeAssetResolution = resolvedAssets.Evidence
		if preparer.Materials == nil {
			return PreparedPlan{}, errors.New("Marketing material service is required for online preflight")
		}
		switch request.Kind {
		case PrepareUpload:
			selectedVideos, err = preparer.prepareUploadedVideos(ctx, request)
		case PrepareCreator:
			selectedVideos, selectedCreator, candidates, creatorCoverResolution, err = preparer.prepareCreatorVideos(ctx, request, limit)
		}
		if err != nil {
			return PreparedPlan{}, err
		}
		applyPreparedVideos(effective, selectedVideos)
	} else if request.Kind == PrepareUpload {
		applyPreparedVideos(effective, preparedVideosFromIDs(request.VideoIDs))
	} else {
		clearRuntimeVideos(effective)
	}

	payloads, err := BuildPayloads(effective, PayloadOptions{
		AdvertiserID: request.AdvertiserID, Budget: request.Budget, CPABid: request.CPABid,
		ROIGoal: request.ROIGoal, MaterialDate: request.MaterialDate,
		ProductName: request.ProductName, ProductID: request.ProductID,
		ProjectName: request.ProjectName, PromotionName: request.PromotionName,
		ProjectID: request.ProjectID, GroupIndex: request.GroupIndex, Index: request.Index,
		Suffix: request.Suffix, Now: request.Now, CreatorID: request.ExpectedAwemeID,
		AppendCreatorID: request.AppendCreatorID,
	})
	if err != nil {
		return PreparedPlan{}, err
	}
	if request.Kind == PrepareCreator && online {
		if err := applyCreatorPayload(payloads.Promotion, candidates); err != nil {
			return PreparedPlan{}, err
		}
		payloads.MissingFields = MarketingPayloadMissingFields(effective, payloads)
	}
	projectJSON, promotionJSON, err := payloads.JSON()
	if err != nil {
		return PreparedPlan{}, err
	}
	selectedTemplate := configuration.Clone(effective["_selected_plan_template"])
	return PreparedPlan{
		Kind: request.Kind, AdvertiserID: request.AdvertiserID,
		AuthAccountID: request.AuthAccountID, SourceType: sourceType,
		SelectedPlanTemplate: selectedTemplate, SelectedVideos: selectedVideos,
		SelectedCreator: selectedCreator, MaterialDate: request.MaterialDate,
		ProductName: request.ProductName, ProjectEndpoint: ProjectCreateEndpoint,
		PromotionEndpoint: PromotionCreateEndpoint, ProjectPayload: payloads.Project,
		PromotionPayload: payloads.Promotion, MissingFields: append([]string(nil), payloads.MissingFields...),
		PreflightSummary:       marketingPreflightSummary(payloads, request.Kind, online),
		RuntimeAssetResolution: runtimeAssetResolution,
		CreatorCoverResolution: creatorCoverResolution,
		ProjectJSON:            projectJSON, PromotionJSON: promotionJSON, CreatorCandidates: candidates,
	}, nil
}

func (preparer Preparer) prepareUploadedVideos(
	ctx context.Context,
	request PrepareRequest,
) ([]PreparedVideo, error) {
	if len(request.VideoIDs) == 0 {
		return nil, errors.New("at least one video_id is required for uploaded material creation")
	}
	query := applicationmaterials.VideoQuery{
		CredentialScope: applicationmaterials.CredentialScope{
			AdvertiserID: request.AdvertiserID, AuthAccountID: request.AuthAccountID,
		},
		Mode: "ad-get", VideoIDs: append([]string(nil), request.VideoIDs...),
	}
	result, err := preparer.Materials.QueryVideos(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("validate promotion-ready uploaded videos: %w", err)
	}
	rows, ok := result.MatchedList.([]domainmarketing.VideoAsset)
	if !ok {
		return nil, errors.New("promotion-ready video query returned an invalid result type")
	}
	byID := make(map[string]domainmarketing.VideoAsset, len(rows))
	for _, row := range rows {
		if row.ID == "" {
			return nil, errors.New("promotion-ready video query returned an empty video_id")
		}
		if _, exists := byID[row.ID]; exists {
			return nil, fmt.Errorf("promotion-ready video query returned duplicate video_id %s", row.ID)
		}
		byID[row.ID] = row
	}
	selected := make([]PreparedVideo, 0, len(request.VideoIDs))
	for _, videoID := range request.VideoIDs {
		row, exists := byID[videoID]
		if !exists {
			return nil, fmt.Errorf("video_id %s is not currently promotion-ready", videoID)
		}
		cover, coverErr := preparer.Materials.QueryVideos(ctx, applicationmaterials.VideoQuery{
			CredentialScope: query.CredentialScope, Mode: "cover-suggest", VideoIDs: []string{videoID},
		})
		if coverErr != nil {
			return nil, fmt.Errorf("query cover for video_id %s: %w", videoID, coverErr)
		}
		if strings.TrimSpace(cover.SelectedCoverID) == "" {
			return nil, fmt.Errorf("video_id %s has no official suggested cover", videoID)
		}
		selected = append(selected, PreparedVideo{
			VideoID: videoID, CoverID: cover.SelectedCoverID, Title: row.Filename,
		})
	}
	return selected, nil
}

func (preparer Preparer) prepareCreatorVideos(
	ctx context.Context,
	request PrepareRequest,
	limit int,
) ([]PreparedVideo, *PreparedCreator, []domainmarketing.CreatorCandidate, map[string]any, error) {
	if len(request.ItemIDs) == 0 {
		return nil, nil, nil, nil, errors.New("at least one item_id is required for creator material creation")
	}
	result, err := preparer.Materials.QueryCreator(ctx, applicationmaterials.CreatorQuery{
		CredentialScope: applicationmaterials.CredentialScope{
			AdvertiserID: request.AdvertiserID, AuthAccountID: request.AuthAccountID,
		},
		Source: "authorized", ItemIDs: append([]string(nil), request.ItemIDs...),
		MinimumRemainingDays: 1,
	})
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("query current creator authorization snapshot: %w", err)
	}
	coverResolution := map[string]any{"status": "not_required"}
	if creatorCandidatesNeedCover(result.Candidates) {
		if preparer.CreatorCovers == nil {
			return nil, nil, nil, nil, errors.New("Marketing creator cover resolver is required for incomplete authorization snapshots")
		}
		resolved, resolveErr := preparer.CreatorCovers.Resolve(ctx, CreatorCoverRequest{
			AdvertiserID: request.AdvertiserID, AuthAccountID: request.AuthAccountID,
			Candidates: result.Candidates,
		})
		if resolveErr != nil {
			return nil, nil, nil, resolved.Diagnostics, fmt.Errorf("resolve creator covers from official promotion history: %w", resolveErr)
		}
		result.Candidates = resolved.Candidates
		coverResolution = resolved.Diagnostics
	}
	byItemID := make(map[string]domainmarketing.CreatorCandidate, len(result.Candidates))
	for _, candidate := range result.Candidates {
		if candidate.ItemID == "" {
			continue
		}
		if _, exists := byItemID[candidate.ItemID]; exists {
			return nil, nil, nil, coverResolution, fmt.Errorf("creator authorization returned duplicate item_id %s", candidate.ItemID)
		}
		byItemID[candidate.ItemID] = candidate
	}
	selectedCandidates := make([]domainmarketing.CreatorCandidate, 0, len(request.ItemIDs))
	for _, itemID := range request.ItemIDs {
		candidate, exists := byItemID[itemID]
		if !exists {
			return nil, nil, nil, coverResolution, fmt.Errorf("item_id %s was not returned by the current authorization snapshot", itemID)
		}
		if !candidate.Usable {
			return nil, nil, nil, coverResolution, fmt.Errorf(
				"item_id %s is not currently usable: %s", itemID,
				strings.Join(candidate.UnusableReasons, ", "),
			)
		}
		selectedCandidates = append(selectedCandidates, candidate)
	}
	if len(selectedCandidates) > limit {
		return nil, nil, nil, coverResolution, fmt.Errorf("select no more than %d creator materials for one unit", limit)
	}
	creatorID := selectedCandidates[0].CreatorID
	creatorName := selectedCandidates[0].CreatorName
	if request.ExpectedAwemeID != "" && request.ExpectedAwemeID != creatorID {
		return nil, nil, nil, coverResolution, fmt.Errorf(
			"selected materials belong to aweme_id %s, not %s", creatorID, request.ExpectedAwemeID,
		)
	}
	selected := make([]PreparedVideo, 0, len(selectedCandidates))
	for _, candidate := range selectedCandidates {
		if candidate.OwnerAdvertiserID != request.AdvertiserID {
			return nil, nil, nil, coverResolution, errors.New("creator materials belong to a different advertiser")
		}
		if candidate.CreatorID != creatorID {
			return nil, nil, nil, coverResolution, errors.New("one native unit can only use materials from one aweme_id")
		}
		selected = append(selected, PreparedVideo{
			VideoID: candidate.VideoID, CoverID: candidate.VideoCoverID,
			ItemID: candidate.ItemID, Title: candidate.Title, CreatorID: candidate.CreatorID,
		})
	}
	return selected, &PreparedCreator{AwemeID: creatorID, AwemeName: creatorName}, selectedCandidates, coverResolution, nil
}

func creatorCandidatesNeedCover(candidates []domainmarketing.CreatorCandidate) bool {
	for _, candidate := range candidates {
		if candidate.VideoCoverID == "" && creatorReasonContains(candidate.UnusableReasons, "missing_video_cover_id") {
			return true
		}
	}
	return false
}

func creatorReasonContains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func normalizePrepareRequest(request PrepareRequest, now func() time.Time) PrepareRequest {
	request.AdvertiserID = strings.TrimSpace(request.AdvertiserID)
	request.AuthAccountID = strings.TrimSpace(request.AuthAccountID)
	request.PlanTemplate = strings.TrimSpace(request.PlanTemplate)
	request.ExpectedAwemeID = strings.TrimSpace(request.ExpectedAwemeID)
	request.ProjectID = strings.TrimSpace(request.ProjectID)
	request.VideoIDs = uniqueNonEmpty(request.VideoIDs)
	request.ItemIDs = uniqueNonEmpty(request.ItemIDs)
	if request.Now.IsZero() {
		if now != nil {
			request.Now = now()
		} else {
			request.Now = time.Now()
		}
	}
	return request
}

func validatePrepareRequest(request PrepareRequest) error {
	if request.Kind != PrepareUpload && request.Kind != PrepareCreator {
		return errors.New("Marketing preparation kind must be upload or creator")
	}
	if request.PlanTemplate == "" {
		return errors.New("an explicit Marketing plan template is required")
	}
	if request.AdvertiserID != "" && !validPositiveID(request.AdvertiserID) {
		return errors.New("advertiser_id must be a positive decimal ID")
	}
	if request.AuthAccountID != "" && !validPositiveID(request.AuthAccountID) {
		return errors.New("auth_account_id must be a positive decimal ID")
	}
	if request.ProjectID != "" && !validPositiveID(request.ProjectID) {
		return errors.New("project_id must be a positive decimal ID")
	}
	if request.Kind == PrepareUpload && len(request.ItemIDs) != 0 {
		return errors.New("uploaded material creation does not accept item_ids")
	}
	if request.Kind == PrepareCreator && len(request.VideoIDs) != 0 {
		return errors.New("creator material creation does not accept video_ids")
	}
	for _, itemID := range request.ItemIDs {
		if !validPositiveID(itemID) {
			return errors.New("item_ids must be positive decimal IDs")
		}
	}
	return nil
}

func materialLimit(config map[string]any) (int, error) {
	value := configuration.Value(config, "material_strategy.max_materials_per_unit")
	if configuration.Missing(value) {
		value = configuration.Value(config, "defaults.max_videos_per_project")
	}
	if configuration.Missing(value) {
		return defaultMaterialLimit, nil
	}
	limit, err := configuration.Integer(value)
	if err != nil || limit < 1 || limit > defaultMaterialLimit {
		return 0, fmt.Errorf("max materials per unit must be between 1 and %d", defaultMaterialLimit)
	}
	return limit, nil
}

func preparedVideosFromIDs(videoIDs []string) []PreparedVideo {
	result := make([]PreparedVideo, 0, len(videoIDs))
	for _, videoID := range videoIDs {
		result = append(result, PreparedVideo{VideoID: videoID})
	}
	return result
}

func applyPreparedVideos(config map[string]any, videos []PreparedVideo) {
	materials := configuration.CloneMap(configuration.Object(config["materials"]))
	videoIDs := make([]any, 0, len(videos))
	covers := map[string]any{}
	for _, video := range videos {
		videoIDs = append(videoIDs, video.VideoID)
		if video.CoverID != "" {
			covers[video.VideoID] = video.CoverID
		}
	}
	materials["video_ids"] = videoIDs
	materials["video_cover_ids"] = covers
	config["materials"] = materials
}

func clearRuntimeVideos(config map[string]any) {
	applyPreparedVideos(config, nil)
}

func applyCreatorPayload(
	promotion map[string]any,
	candidates []domainmarketing.CreatorCandidate,
) error {
	if len(candidates) == 0 {
		return errors.New("creator material selection is empty")
	}
	creatorID := candidates[0].CreatorID
	materials := configuration.CloneMap(configuration.Object(promotion["promotion_materials"]))
	videoMaterials := make([]any, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.CreatorID != creatorID {
			return errors.New("one native unit can only use materials from one aweme_id")
		}
		itemID := json.Number(candidate.ItemID)
		videoMaterials = append(videoMaterials, map[string]any{
			"image_mode": defaultText(candidate.ImageMode, "CREATIVE_IMAGE_MODE_VIDEO_VERTICAL"),
			"video_id":   candidate.VideoID, "video_cover_id": candidate.VideoCoverID,
			"item_id": itemID,
		})
	}
	materials["video_material_list"] = videoMaterials
	promotion["promotion_materials"] = materials
	native := configuration.CloneMap(configuration.Object(promotion["native_setting"]))
	if existing := textValue(native["aweme_id"]); existing != "" && existing != creatorID {
		return errors.New("promotion payload already contains a different native aweme_id")
	}
	native["aweme_id"] = creatorID
	promotion["native_setting"] = native
	return nil
}

func marketingPreflightSummary(payloads Payloads, kind PrepareKind, online bool) map[string]any {
	delivery := configuration.Object(payloads.Project["delivery_setting"])
	audience := configuration.Object(payloads.Project["audience"])
	materials := configuration.Object(configuration.Object(payloads.Promotion["promotion_materials"])["video_material_list"])
	videoCount := 0
	switch rows := configuration.Object(payloads.Promotion["promotion_materials"])["video_material_list"].(type) {
	case []any:
		videoCount = len(rows)
	case []map[string]any:
		videoCount = len(rows)
	}
	_ = materials
	result := map[string]any{
		"advertiser_id": payloads.Project["advertiser_id"],
		"project_name":  payloads.Project["name"], "promotion_name": payloads.Promotion["name"],
		"budget": delivery["budget"], "cpa_bid": delivery["cpa_bid"],
		"city_count": len(configuration.List(audience["city"])), "video_count": videoCount,
		"operation": payloads.Project["operation"], "material_kind": kind,
		"online_material_snapshot": online,
	}
	if roi := delivery["roi_goal"]; roi != nil {
		result["roi_goal"] = roi
	}
	return result
}

func uniqueNonEmpty(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
