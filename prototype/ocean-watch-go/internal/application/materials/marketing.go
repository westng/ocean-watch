package materials

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	authapplication "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
	domainmarketing "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/marketing"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/pagination"
	portmarketing "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/ports/marketing"
)

const (
	LibraryVideoEndpoint         = "/2/file/video/get/"
	AdVideoEndpoint              = "/2/file/video/ad/get/"
	CoverSuggestionEndpoint      = "/2/tools/video_cover/suggest/"
	CreatorAuthorizationEndpoint = "/2/tools/aweme_auth_list/"
	CreatorHomepageEndpoint      = "/2/file/video/aweme/get/"
	LibraryImageEndpoint         = "/2/file/image/get/"
	AdImageEndpoint              = "/2/file/image/ad/get/"
	ProductEndpoint              = "/2/dpa/clue_product/list/"
	DefaultPageSize              = 20
	DefaultCreatorPageSize       = 100
	DefaultMaxPages              = 100
	MaxPageSize                  = 100
	MaxBatchSize                 = 100
	CreatorAuthorizedSource      = "CREATOR_AUTHORIZED"
	CreatorHomepageSource        = "CREATOR_HOMEPAGE"
	OfficialAuthorizedStatus     = "AUTHRIZED"
	OfficialVideoItemAuthType    = "VIDEO_ITEM"
)

type TokenProvider interface {
	Ensure(context.Context, authapplication.TokenQuery) (authapplication.TokenLease, error)
}

type ConfigReader interface {
	Read(context.Context) (map[string]any, error)
}

type Service struct {
	Tokens TokenProvider
	Reader portmarketing.MaterialReader
	Now    func() time.Time
}

type CredentialScope struct {
	AdvertiserID  string
	AuthAccountID string
}

type VideoQuery struct {
	CredentialScope
	Mode          string
	VideoIDs      []string
	MaterialIDs   []string
	Signatures    []string
	Filename      string
	Date          string
	StartTime     string
	EndTime       string
	Page          int
	PageSize      int
	FetchAll      bool
	CoverAttempts int
	CoverWait     time.Duration
}

type MaterialDiagnosticData struct {
	List     any                       `json:"list"`
	PageInfo *domainmarketing.PageInfo `json:"page_info,omitempty"`
	Status   string                    `json:"status,omitempty"`
}

type MaterialDiagnosticResponse struct {
	Code      int64                  `json:"code"`
	Message   string                 `json:"message,omitempty"`
	RequestID string                 `json:"request_id,omitempty"`
	Data      MaterialDiagnosticData `json:"data"`
}

type VideoResult struct {
	Endpoint        string                          `json:"endpoint"`
	Params          map[string]any                  `json:"params"`
	ResponseCode    int64                           `json:"response_code"`
	ResponseMessage string                          `json:"response_message,omitempty"`
	RequestID       string                          `json:"request_id,omitempty"`
	RequestIDs      []string                        `json:"request_ids"`
	Status          string                          `json:"status,omitempty"`
	PageInfo        *domainmarketing.PageInfo       `json:"page_info,omitempty"`
	MatchedCount    int                             `json:"matched_count"`
	SelectedVideos  []domainmarketing.SelectedVideo `json:"selected_videos"`
	MatchedList     any                             `json:"matched_list"`
	SelectedCoverID string                          `json:"selected_cover_id,omitempty"`
	Response        MaterialDiagnosticResponse      `json:"response"`
}

type CreatorQuery struct {
	CredentialScope
	Source               string
	AwemeIDs             []string
	ItemIDs              []string
	MinimumRemainingDays int
	PageSize             int
	MaxPages             int
	IncludeUnusable      bool
}

type CreatorFilters struct {
	AuthType   []string `json:"auth_type"`
	AuthStatus []string `json:"auth_status"`
	AwemeIDs   []string `json:"aweme_ids,omitempty"`
	ItemIDs    []string `json:"item_ids,omitempty"`
}

type CreatorResult struct {
	Endpoint       string                             `json:"endpoint"`
	AdvertiserID   string                             `json:"advertiser_id"`
	AwemeID        string                             `json:"aweme_id,omitempty"`
	Filters        *CreatorFilters                    `json:"filters,omitempty"`
	PageCount      int                                `json:"page_count"`
	RequestIDs     []string                           `json:"request_ids"`
	SourceType     string                             `json:"source_type"`
	CandidateCount int                                `json:"candidate_count"`
	Candidates     []domainmarketing.CreatorCandidate `json:"candidates"`
}

type ImageQuery struct {
	CredentialScope
	Mode        string
	ImageIDs    []string
	MaterialIDs []string
	Page        int
	PageSize    int
}

type ImageResult struct {
	Endpoint        string                     `json:"endpoint"`
	Params          map[string]any             `json:"params"`
	ResponseCode    int64                      `json:"response_code"`
	ResponseMessage string                     `json:"response_message,omitempty"`
	RequestID       string                     `json:"request_id,omitempty"`
	Response        MaterialDiagnosticResponse `json:"response"`
}

type ProductQuery struct {
	CredentialScope
	Path              string
	ProductPlatformID string
	ProductID         string
	Name              string
	Page              int
	PageSize          int
}

type ProductResult struct {
	Endpoint        string                     `json:"endpoint"`
	Params          map[string]any             `json:"params"`
	ResponseCode    int64                      `json:"response_code"`
	ResponseMessage string                     `json:"response_message,omitempty"`
	RequestID       string                     `json:"request_id,omitempty"`
	Response        MaterialDiagnosticResponse `json:"response"`
}

func (service Service) QueryVideos(ctx context.Context, query VideoQuery) (VideoResult, error) {
	if err := service.validate(); err != nil {
		return VideoResult{}, err
	}
	query = service.normalizeVideoQuery(query)
	if err := validateVideoQuery(query); err != nil {
		return VideoResult{}, err
	}
	lease, err := service.token(ctx, query.CredentialScope)
	if err != nil {
		return VideoResult{}, err
	}
	ctx, err = authapplication.WithTokenLease(ctx, lease)
	if err != nil {
		return VideoResult{}, err
	}
	params := map[string]any{"advertiser_id": query.AdvertiserID}
	result := VideoResult{ResponseCode: 0, SelectedVideos: []domainmarketing.SelectedVideo{}, RequestIDs: []string{}}
	switch query.Mode {
	case "ad-get":
		result.Endpoint = AdVideoEndpoint
		params["video_ids"] = append([]string(nil), query.VideoIDs...)
		batch, fetchErr := service.Reader.FetchAdVideos(ctx, portmarketing.AdVideoRequest{
			AdvertiserID: query.AdvertiserID, AccessToken: lease.AccessToken, VideoIDs: query.VideoIDs,
		})
		if fetchErr != nil {
			return VideoResult{}, fmt.Errorf("query promotion-ready videos: %w", fetchErr)
		}
		result.MatchedList = batch.Rows
		result.MatchedCount = len(batch.Rows)
		result.RequestID, result.ResponseMessage = batch.RequestID, batch.Message
		result.RequestIDs = requestIDs(batch.RequestID)
		result.Response = diagnosticResponse(batch.Message, batch.RequestID, batch.Rows, nil, "")
	case "cover-suggest":
		result.Endpoint = CoverSuggestionEndpoint
		params["video_id"] = query.VideoIDs[0]
		covers, fetchErr := service.Reader.FetchCoverSuggestions(ctx, portmarketing.CoverSuggestionRequest{
			AdvertiserID: query.AdvertiserID, AccessToken: lease.AccessToken, VideoID: query.VideoIDs[0],
			Attempts: query.CoverAttempts, Wait: query.CoverWait,
		})
		if fetchErr != nil {
			return VideoResult{}, fmt.Errorf("query video cover suggestions: %w", fetchErr)
		}
		result.Status, result.MatchedList, result.MatchedCount = covers.Status, covers.Rows, len(covers.Rows)
		result.RequestID, result.ResponseMessage = covers.RequestID, covers.Message
		result.RequestIDs = requestIDs(covers.RequestID)
		if len(covers.Rows) != 0 {
			result.SelectedCoverID = covers.Rows[0].ID
		}
		result.Response = diagnosticResponse(covers.Message, covers.RequestID, covers.Rows, nil, covers.Status)
	case "library-get":
		result.Endpoint = LibraryVideoEndpoint
		filtering := videoFiltering(query)
		params["filtering"], params["page"], params["page_size"] = nullableMap(filtering), query.Page, query.PageSize
		rows, pages, last, fetchErr := service.collectVideos(ctx, lease.AccessToken, query)
		if fetchErr != nil {
			return VideoResult{}, fmt.Errorf("query video library: %w", fetchErr)
		}
		if query.Filename != "" {
			filtered := make([]domainmarketing.VideoAsset, 0, len(rows))
			needle := strings.ToLower(query.Filename)
			for _, row := range rows {
				if strings.Contains(strings.ToLower(row.Filename), needle) {
					filtered = append(filtered, row)
				}
			}
			rows = filtered
		}
		result.MatchedList, result.MatchedCount = rows, len(rows)
		result.SelectedVideos = compactVideos(rows)
		result.PageInfo = &last.PageInfo
		result.RequestID, result.ResponseMessage = last.RequestID, last.Message
		result.RequestIDs = pages
		result.Response = diagnosticResponse(last.Message, last.RequestID, last.Rows, &last.PageInfo, "")
	}
	result.Params = params
	return result, nil
}

func (service Service) QueryCreator(ctx context.Context, query CreatorQuery) (CreatorResult, error) {
	if err := service.validate(); err != nil {
		return CreatorResult{}, err
	}
	query = normalizeCreatorQuery(query)
	if err := validateCreatorQuery(query); err != nil {
		return CreatorResult{}, err
	}
	lease, err := service.token(ctx, query.CredentialScope)
	if err != nil {
		return CreatorResult{}, err
	}
	ctx, err = authapplication.WithTokenLease(ctx, lease)
	if err != nil {
		return CreatorResult{}, err
	}
	if query.Source == "homepage" {
		return service.queryCreatorHomepage(ctx, lease.AccessToken, query)
	}
	return service.queryCreatorAuthorizations(ctx, lease.AccessToken, query)
}

func (service Service) QueryImages(ctx context.Context, query ImageQuery) (ImageResult, error) {
	if err := service.validate(); err != nil {
		return ImageResult{}, err
	}
	query = normalizeImageQuery(query)
	if err := validateImageQuery(query); err != nil {
		return ImageResult{}, err
	}
	lease, err := service.token(ctx, query.CredentialScope)
	if err != nil {
		return ImageResult{}, err
	}
	ctx, err = authapplication.WithTokenLease(ctx, lease)
	if err != nil {
		return ImageResult{}, err
	}
	params := map[string]any{"advertiser_id": query.AdvertiserID}
	if query.Mode == "ad-get" {
		params["image_ids"] = append([]string(nil), query.ImageIDs...)
		batch, fetchErr := service.Reader.FetchAdImages(ctx, portmarketing.AdImageRequest{
			AdvertiserID: query.AdvertiserID, AccessToken: lease.AccessToken, ImageIDs: query.ImageIDs,
		})
		if fetchErr != nil {
			return ImageResult{}, fmt.Errorf("query promotion-ready images: %w", fetchErr)
		}
		return ImageResult{Endpoint: AdImageEndpoint, Params: params, ResponseCode: 0,
			ResponseMessage: batch.Message, RequestID: batch.RequestID,
			Response: diagnosticResponse(batch.Message, batch.RequestID, batch.Rows, nil, "")}, nil
	}
	filtering := map[string]any{}
	if len(query.ImageIDs) != 0 {
		filtering["image_ids"] = append([]string(nil), query.ImageIDs...)
	}
	if len(query.MaterialIDs) != 0 {
		filtering["material_ids"] = append([]string(nil), query.MaterialIDs...)
	}
	params["filtering"], params["page"], params["page_size"] = nullableMap(filtering), query.Page, query.PageSize
	page, fetchErr := service.Reader.FetchLibraryImages(ctx, portmarketing.LibraryImageRequest{
		AdvertiserID: query.AdvertiserID, AccessToken: lease.AccessToken,
		ImageIDs: query.ImageIDs, MaterialIDs: query.MaterialIDs, Page: query.Page, PageSize: query.PageSize,
	})
	if fetchErr != nil {
		return ImageResult{}, fmt.Errorf("query image library: %w", fetchErr)
	}
	if err := validateSinglePage(query.Page, page.PageInfo, imageKeys(page.Rows)); err != nil {
		return ImageResult{}, fmt.Errorf("query image library: %w", err)
	}
	return ImageResult{Endpoint: LibraryImageEndpoint, Params: params, ResponseCode: 0,
		ResponseMessage: page.Message, RequestID: page.RequestID,
		Response: diagnosticResponse(page.Message, page.RequestID, page.Rows, &page.PageInfo, "")}, nil
}

func (service Service) QueryProducts(ctx context.Context, query ProductQuery) (ProductResult, error) {
	if err := service.validate(); err != nil {
		return ProductResult{}, err
	}
	query = normalizeProductQuery(query)
	if err := validateProductQuery(query); err != nil {
		return ProductResult{}, err
	}
	lease, err := service.token(ctx, query.CredentialScope)
	if err != nil {
		return ProductResult{}, err
	}
	ctx, err = authapplication.WithTokenLease(ctx, lease)
	if err != nil {
		return ProductResult{}, err
	}
	page, err := service.Reader.FetchProducts(ctx, portmarketing.ProductRequest{
		AdvertiserID: query.AdvertiserID, AccessToken: lease.AccessToken,
		ProductID: query.ProductID, Name: query.Name, Page: query.Page, PageSize: query.PageSize,
	})
	if err != nil {
		return ProductResult{}, fmt.Errorf("query DPA products: %w", err)
	}
	if err := validateSinglePage(query.Page, page.PageInfo, productKeys(page.Rows)); err != nil {
		return ProductResult{}, fmt.Errorf("query DPA products: %w", err)
	}
	params := map[string]any{
		"advertiser_id": query.AdvertiserID, "page": query.Page, "page_size": query.PageSize,
		"product_platform_id": nullableString(query.ProductPlatformID),
		"product_id":          nullableString(query.ProductID), "name": nullableString(query.Name),
	}
	return ProductResult{Endpoint: ProductEndpoint, Params: params, ResponseCode: 0,
		ResponseMessage: page.Message, RequestID: page.RequestID,
		Response: diagnosticResponse(page.Message, page.RequestID, page.Rows, &page.PageInfo, "")}, nil
}

func (service Service) collectVideos(
	ctx context.Context,
	accessToken string,
	query VideoQuery,
) ([]domainmarketing.VideoAsset, []string, domainmarketing.VideoPage, error) {
	fetch := func(ctx context.Context, page int) (domainmarketing.VideoPage, error) {
		return service.Reader.FetchLibraryVideos(ctx, portmarketing.LibraryVideoRequest{
			AdvertiserID: query.AdvertiserID, AccessToken: accessToken,
			VideoIDs: query.VideoIDs, MaterialIDs: query.MaterialIDs, Signatures: query.Signatures,
			StartTime: query.StartTime, EndTime: query.EndTime, Page: page, PageSize: query.PageSize,
		})
	}
	first, err := fetch(ctx, query.Page)
	if err != nil {
		return nil, nil, domainmarketing.VideoPage{}, err
	}
	if err := validateSinglePage(query.Page, first.PageInfo, videoKeys(first.Rows)); err != nil {
		return nil, nil, domainmarketing.VideoPage{}, err
	}
	rows := append([]domainmarketing.VideoAsset(nil), first.Rows...)
	requestIDs := requestIDs(first.RequestID)
	last := first
	if !query.FetchAll {
		return rows, requestIDs, last, nil
	}
	seen := keySet(videoKeys(rows))
	expected := first.PageInfo
	for pageNumber := query.Page + 1; pageNumber <= expected.TotalPages; pageNumber++ {
		if pageNumber-query.Page+1 > DefaultMaxPages {
			return nil, nil, domainmarketing.VideoPage{}, errors.New("video pagination exceeds safety cap")
		}
		page, fetchErr := fetch(ctx, pageNumber)
		if fetchErr != nil {
			return nil, nil, domainmarketing.VideoPage{}, fetchErr
		}
		if page.PageInfo.TotalPages != expected.TotalPages || page.PageInfo.TotalNumber != expected.TotalNumber ||
			page.PageInfo.PageSize != expected.PageSize {
			return nil, nil, domainmarketing.VideoPage{}, fmt.Errorf("page %d changed declared pagination totals", pageNumber)
		}
		if err := validateSinglePage(pageNumber, page.PageInfo, videoKeys(page.Rows)); err != nil {
			return nil, nil, domainmarketing.VideoPage{}, err
		}
		for _, row := range page.Rows {
			key := videoKey(row)
			if _, exists := seen[key]; exists {
				return nil, nil, domainmarketing.VideoPage{}, fmt.Errorf("page %d returned duplicate video key %q", pageNumber, key)
			}
			seen[key] = struct{}{}
			rows = append(rows, row)
		}
		if page.RequestID != "" {
			requestIDs = append(requestIDs, page.RequestID)
		}
		last = page
	}
	if query.Page == 1 && len(rows) != expected.TotalNumber {
		return nil, nil, domainmarketing.VideoPage{}, fmt.Errorf("video pagination returned %d rows but declared %d", len(rows), expected.TotalNumber)
	}
	return rows, requestIDs, last, nil
}

func (service Service) queryCreatorAuthorizations(
	ctx context.Context,
	accessToken string,
	query CreatorQuery,
) (CreatorResult, error) {
	requestIDs := []string{}
	pageCount := 0
	rows, err := pagination.CollectPages(ctx, pagination.PageOptions[domainmarketing.CreatorAuthorization]{
		MaxPages: query.MaxPages,
		Key:      creatorAuthorizationKey,
		Fetch: func(ctx context.Context, page int) (pagination.Page[domainmarketing.CreatorAuthorization], error) {
			pageCount++
			result, fetchErr := service.Reader.FetchCreatorAuthorizations(ctx, portmarketing.CreatorAuthorizationRequest{
				AdvertiserID: query.AdvertiserID, AccessToken: accessToken,
				AwemeIDs: query.AwemeIDs, ItemIDs: query.ItemIDs, Page: page, PageSize: query.PageSize,
			})
			if fetchErr != nil {
				return pagination.Page[domainmarketing.CreatorAuthorization]{}, fetchErr
			}
			if result.RequestID != "" {
				requestIDs = append(requestIDs, result.RequestID)
			}
			return pagination.Page[domainmarketing.CreatorAuthorization]{
				Number: result.PageInfo.Page, TotalPages: result.PageInfo.TotalPages,
				TotalNumber: result.PageInfo.TotalNumber, Rows: result.Rows,
			}, nil
		},
	})
	if err != nil {
		return CreatorResult{}, fmt.Errorf("query creator authorizations: %w", err)
	}
	now := service.now()
	awemeFilter, itemFilter := stringSet(query.AwemeIDs), stringSet(query.ItemIDs)
	candidates := make([]domainmarketing.CreatorCandidate, 0, len(rows))
	seen := map[string]struct{}{}
	for _, row := range rows {
		if len(awemeFilter) != 0 {
			if _, ok := awemeFilter[row.AwemeID]; !ok {
				continue
			}
		}
		if len(itemFilter) != 0 {
			if _, ok := itemFilter[row.Video.ItemID]; !ok {
				continue
			}
		}
		candidate := normalizeAuthorizedCandidate(row, query.AdvertiserID, query.MinimumRemainingDays, now)
		if candidate.SourceKey != nil && candidate.SourceKey.Canonical != "" {
			if _, exists := seen[candidate.SourceKey.Canonical]; exists {
				continue
			}
			seen[candidate.SourceKey.Canonical] = struct{}{}
		}
		if query.IncludeUnusable || candidate.Usable {
			candidates = append(candidates, candidate)
		}
	}
	filters := &CreatorFilters{AuthType: []string{OfficialVideoItemAuthType},
		AuthStatus: []string{OfficialAuthorizedStatus}, AwemeIDs: query.AwemeIDs, ItemIDs: query.ItemIDs}
	return CreatorResult{Endpoint: CreatorAuthorizationEndpoint, AdvertiserID: query.AdvertiserID,
		Filters: filters, PageCount: pageCount, RequestIDs: requestIDs,
		SourceType: CreatorAuthorizedSource, CandidateCount: len(candidates), Candidates: candidates}, nil
}

func (service Service) queryCreatorHomepage(
	ctx context.Context,
	accessToken string,
	query CreatorQuery,
) (CreatorResult, error) {
	requestIDs := []string{}
	pageCount := 0
	awemeID := query.AwemeIDs[0]
	rows, err := pagination.CollectPages(ctx, pagination.PageOptions[domainmarketing.CreatorVideo]{
		MaxPages: query.MaxPages,
		Key:      creatorVideoKey,
		Fetch: func(ctx context.Context, page int) (pagination.Page[domainmarketing.CreatorVideo], error) {
			pageCount++
			result, fetchErr := service.Reader.FetchCreatorHomepage(ctx, portmarketing.CreatorHomepageRequest{
				AdvertiserID: query.AdvertiserID, AccessToken: accessToken,
				AwemeID: awemeID, Page: page, PageSize: query.PageSize,
			})
			if fetchErr != nil {
				return pagination.Page[domainmarketing.CreatorVideo]{}, fetchErr
			}
			if result.RequestID != "" {
				requestIDs = append(requestIDs, result.RequestID)
			}
			return pagination.Page[domainmarketing.CreatorVideo]{Number: result.PageInfo.Page,
				TotalPages: result.PageInfo.TotalPages, TotalNumber: result.PageInfo.TotalNumber,
				Rows: result.Rows}, nil
		},
	})
	if err != nil {
		return CreatorResult{}, fmt.Errorf("query creator homepage videos: %w", err)
	}
	candidates := make([]domainmarketing.CreatorCandidate, 0, len(rows))
	for _, row := range rows {
		candidate := normalizeHomepageCandidate(row, query.AdvertiserID, awemeID)
		if query.IncludeUnusable || candidate.Usable {
			candidates = append(candidates, candidate)
		}
	}
	return CreatorResult{Endpoint: CreatorHomepageEndpoint, AdvertiserID: query.AdvertiserID,
		AwemeID: awemeID, PageCount: pageCount, RequestIDs: requestIDs,
		SourceType: CreatorHomepageSource, CandidateCount: len(candidates), Candidates: candidates}, nil
}

func (service Service) validate() error {
	if service.Tokens == nil || service.Reader == nil {
		return errors.New("Marketing material service dependencies are incomplete")
	}
	return nil
}

func (service Service) token(ctx context.Context, scope CredentialScope) (authapplication.TokenLease, error) {
	return service.Tokens.Ensure(ctx, authapplication.TokenQuery{
		Channel: "marketing", AdvertiserID: scope.AdvertiserID, AuthAccountID: scope.AuthAccountID,
	})
}

func (service Service) now() time.Time {
	if service.Now != nil {
		return service.Now()
	}
	return time.Now()
}

func (service Service) normalizeVideoQuery(query VideoQuery) VideoQuery {
	query.CredentialScope = normalizeScope(query.CredentialScope)
	query.Mode = strings.TrimSpace(query.Mode)
	if query.Mode == "" {
		query.Mode = "library-get"
	}
	query.VideoIDs, query.MaterialIDs, query.Signatures = uniqueStrings(query.VideoIDs), uniqueStrings(query.MaterialIDs), uniqueStrings(query.Signatures)
	query.Filename, query.Date = strings.TrimSpace(query.Filename), strings.TrimSpace(query.Date)
	query.StartTime, query.EndTime = normalizeDatePrefix(query.StartTime), normalizeDatePrefix(query.EndTime)
	if query.Page == 0 {
		query.Page = 1
	}
	if query.PageSize == 0 {
		query.PageSize = DefaultPageSize
	}
	if query.Date != "" {
		date, err := resolveDate(query.Date, service.now())
		if err == nil {
			if query.StartTime == "" {
				query.StartTime = date
			}
			if query.EndTime == "" {
				query.EndTime = date
			}
		}
	}
	return query
}

func normalizeCreatorQuery(query CreatorQuery) CreatorQuery {
	query.CredentialScope = normalizeScope(query.CredentialScope)
	query.Source = strings.TrimSpace(query.Source)
	if query.Source == "" {
		query.Source = "authorized"
	}
	query.AwemeIDs, query.ItemIDs = uniqueStrings(query.AwemeIDs), uniqueStrings(query.ItemIDs)
	if query.PageSize == 0 {
		query.PageSize = DefaultCreatorPageSize
	}
	if query.MaxPages == 0 {
		query.MaxPages = DefaultMaxPages
	}
	return query
}

func normalizeImageQuery(query ImageQuery) ImageQuery {
	query.CredentialScope = normalizeScope(query.CredentialScope)
	query.Mode = strings.TrimSpace(query.Mode)
	if query.Mode == "" {
		query.Mode = "ad-get"
	}
	query.ImageIDs, query.MaterialIDs = uniqueStrings(query.ImageIDs), uniqueStrings(query.MaterialIDs)
	if query.Page == 0 {
		query.Page = 1
	}
	if query.PageSize == 0 {
		query.PageSize = DefaultPageSize
	}
	return query
}

func normalizeProductQuery(query ProductQuery) ProductQuery {
	query.CredentialScope = normalizeScope(query.CredentialScope)
	query.Path, query.ProductPlatformID = strings.TrimSpace(query.Path), strings.TrimSpace(query.ProductPlatformID)
	query.ProductID, query.Name = strings.TrimSpace(query.ProductID), strings.TrimSpace(query.Name)
	if query.Path == "" {
		query.Path = ProductEndpoint
	}
	if query.Page == 0 {
		query.Page = 1
	}
	if query.PageSize == 0 {
		query.PageSize = DefaultPageSize
	}
	return query
}

func normalizeScope(scope CredentialScope) CredentialScope {
	scope.AdvertiserID, scope.AuthAccountID = strings.TrimSpace(scope.AdvertiserID), strings.TrimSpace(scope.AuthAccountID)
	return scope
}

func validateVideoQuery(query VideoQuery) error {
	if err := validateScope(query.CredentialScope); err != nil {
		return err
	}
	if query.Mode != "library-get" && query.Mode != "ad-get" && query.Mode != "cover-suggest" {
		return errors.New("unsupported video material mode")
	}
	if err := validatePage(query.Page, query.PageSize); err != nil {
		return err
	}
	if query.Mode == "library-get" {
		if nonEmptyGroups(query.VideoIDs, query.MaterialIDs, query.Signatures) > 1 {
			return errors.New("use only one of video_ids, material_ids, or signatures for library filtering")
		}
		if err := validateNumericIDs(query.MaterialIDs, "material_id"); err != nil {
			return err
		}
		if query.Date != "" {
			if _, err := resolveDate(query.Date, time.Now()); err != nil {
				return err
			}
		}
		if err := validateDateRange(query.StartTime, query.EndTime); err != nil {
			return err
		}
	} else if len(query.VideoIDs) == 0 {
		return errors.New("video_ids are required for this mode")
	}
	if query.CoverAttempts < 0 || query.CoverAttempts > 100 {
		return errors.New("cover_attempts must be between 1 and 100")
	}
	if query.CoverWait < 0 || query.CoverWait > 5*time.Minute {
		return errors.New("cover_wait must be between 0 and 5 minutes")
	}
	if query.Mode != "cover-suggest" && (query.CoverAttempts != 0 || query.CoverWait != 0) {
		return errors.New("cover retry settings are only supported for cover-suggest")
	}
	if len(query.VideoIDs) > MaxBatchSize || len(query.MaterialIDs) > MaxBatchSize || len(query.Signatures) > MaxBatchSize {
		return errors.New("video material filters accept at most 100 values")
	}
	return nil
}

func validateCreatorQuery(query CreatorQuery) error {
	if err := validateScope(query.CredentialScope); err != nil {
		return err
	}
	if query.Source != "authorized" && query.Source != "homepage" {
		return errors.New("creator source must be authorized or homepage")
	}
	if query.Source == "homepage" && len(query.AwemeIDs) != 1 {
		return errors.New("homepage source requires exactly one aweme_id")
	}
	if query.MinimumRemainingDays < 0 {
		return errors.New("minimum_remaining_days must not be negative")
	}
	if query.PageSize < 1 || query.PageSize > MaxPageSize || query.MaxPages < 1 || query.MaxPages > DefaultMaxPages {
		return errors.New("creator pagination is outside the supported range")
	}
	if err := validateNumericIDs(query.ItemIDs, "item_id"); err != nil {
		return err
	}
	if len(query.AwemeIDs) > MaxBatchSize || len(query.ItemIDs) > MaxBatchSize {
		return errors.New("creator filters accept at most 100 values")
	}
	return nil
}

func validateImageQuery(query ImageQuery) error {
	if err := validateScope(query.CredentialScope); err != nil {
		return err
	}
	if query.Mode != "ad-get" && query.Mode != "library-get" {
		return errors.New("unsupported image material mode")
	}
	if err := validatePage(query.Page, query.PageSize); err != nil {
		return err
	}
	if query.Mode == "ad-get" && len(query.ImageIDs) == 0 {
		return errors.New("image_ids are required for ad-get")
	}
	if query.Mode == "library-get" && len(query.ImageIDs) != 0 && len(query.MaterialIDs) != 0 {
		return errors.New("use only one of image_ids or material_ids for library filtering")
	}
	if len(query.ImageIDs) > MaxBatchSize || len(query.MaterialIDs) > MaxBatchSize {
		return errors.New("image material filters accept at most 100 values")
	}
	return validateNumericIDs(query.MaterialIDs, "material_id")
}

func validateProductQuery(query ProductQuery) error {
	if err := validateScope(query.CredentialScope); err != nil {
		return err
	}
	if query.Path != ProductEndpoint {
		return fmt.Errorf("product path is frozen to %s", ProductEndpoint)
	}
	if err := validatePage(query.Page, query.PageSize); err != nil {
		return err
	}
	if query.ProductID != "" {
		return domain.ValidateDecimalID(query.ProductID, "product_id")
	}
	return nil
}

func validateScope(scope CredentialScope) error {
	if err := domain.ValidateDecimalID(scope.AdvertiserID, "advertiser_id"); err != nil {
		return err
	}
	if scope.AuthAccountID != "" {
		return domain.ValidateDecimalID(scope.AuthAccountID, "auth_account_id")
	}
	return nil
}

func validatePage(page, pageSize int) error {
	if page < 1 {
		return errors.New("page must be positive")
	}
	if pageSize < 1 || pageSize > MaxPageSize {
		return errors.New("page_size must be between 1 and 100")
	}
	return nil
}

func validateSinglePage(page int, info domainmarketing.PageInfo, keys []string) error {
	if info.Page != page || info.PageSize < 1 || info.TotalNumber < 0 || info.TotalPages < 0 {
		return fmt.Errorf("page %d returned malformed pagination metadata", page)
	}
	if info.TotalPages == 0 {
		if page != 1 || info.TotalNumber != 0 || len(keys) != 0 {
			return fmt.Errorf("page %d returned contradictory empty pagination metadata", page)
		}
		return nil
	}
	if page > info.TotalPages {
		return fmt.Errorf("page %d exceeds declared total pages %d", page, info.TotalPages)
	}
	if len(keys) > info.TotalNumber || (page == 1 && info.TotalPages == 1 && len(keys) != info.TotalNumber) {
		return fmt.Errorf("page %d returned %d rows but declared %d total rows", page, len(keys), info.TotalNumber)
	}
	seen := map[string]struct{}{}
	for _, key := range keys {
		if key == "" {
			return fmt.Errorf("page %d returned an empty unique key", page)
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("page %d returned duplicate unique key %q", page, key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func normalizeAuthorizedCandidate(
	row domainmarketing.CreatorAuthorization,
	advertiserID string,
	minimumDays int,
	now time.Time,
) domainmarketing.CreatorCandidate {
	reasons := []string{}
	if row.AuthType != OfficialVideoItemAuthType {
		reasons = append(reasons, "unsupported_auth_type")
	}
	if row.AuthStatus != OfficialAuthorizedStatus {
		reasons = append(reasons, "authorization_not_active")
	}
	if row.AwemeID == "" {
		reasons = append(reasons, "missing_aweme_id")
	}
	if row.Video.ItemID == "" {
		reasons = append(reasons, "missing_item_id")
	}
	if row.Video.VideoID == "" {
		reasons = append(reasons, "missing_video_id")
	}
	if row.Video.VideoCoverID == "" {
		reasons = append(reasons, "missing_video_cover_id")
	}
	expiresAt, ok := parseOfficialTime(row.EndTime, now.Location())
	if !ok {
		reasons = append(reasons, "authorization_end_time_missing")
	} else if !expiresAt.After(now) {
		reasons = append(reasons, "authorization_expired")
	} else if minimumDays > 0 && expiresAt.Before(now.Add(time.Duration(minimumDays)*24*time.Hour)) {
		reasons = append(reasons, "authorization_expires_too_soon")
	}
	var sourceKey *domainmarketing.MaterialSourceKey
	if row.Video.ItemID != "" {
		canonical := strings.Join([]string{"marketing", advertiserID, CreatorAuthorizedSource, "ITEM_ID", row.Video.ItemID}, ":")
		sourceKey = &domainmarketing.MaterialSourceKey{IDType: "ITEM_ID", IDValue: row.Video.ItemID, Canonical: canonical}
	}
	status := "INVALID"
	if row.AuthStatus == OfficialAuthorizedStatus {
		status = "VALID"
	}
	return domainmarketing.CreatorCandidate{
		Channel: "marketing", OwnerAdvertiserID: advertiserID, SourceType: CreatorAuthorizedSource,
		SourceKey: sourceKey, MaterialID: row.Video.MaterialID, VideoID: row.Video.VideoID,
		ItemID: row.Video.ItemID, ImageMode: row.Video.ImageMode, VideoCoverID: row.Video.VideoCoverID,
		VideoCoverURL: row.Video.VideoCoverURL, Title: row.Video.Title, Duration: row.Video.Duration,
		CreatorID: row.AwemeID, CreatorName: row.AwemeName, AuthorizationSubjectID: row.OpenID,
		AuthorizationType: row.AuthType, AuthorizationStatus: status, RawAuthorizationStatus: row.AuthStatus,
		AuthorizationStartAt: row.StartTime, AuthorizationExpiresAt: row.EndTime,
		WarningTypes: nonNilStrings(row.WarningTypes), Usable: len(reasons) == 0, UnusableReasons: reasons,
	}
}

func normalizeHomepageCandidate(row domainmarketing.CreatorVideo, advertiserID, awemeID string) domainmarketing.CreatorCandidate {
	reasons := []string{}
	if row.ItemID == "" {
		reasons = append(reasons, "missing_item_id")
	}
	if row.VideoID == "" {
		reasons = append(reasons, "missing_video_id")
	}
	if row.VideoCoverID == "" {
		reasons = append(reasons, "missing_video_cover_id")
	}
	return domainmarketing.CreatorCandidate{
		Channel: "marketing", OwnerAdvertiserID: advertiserID, SourceType: CreatorHomepageSource,
		MaterialID: row.MaterialID, VideoID: row.VideoID, ItemID: row.ItemID,
		ImageMode: row.ImageMode, VideoCoverID: row.VideoCoverID, VideoCoverURL: row.VideoCoverURL,
		Title: row.Title, Duration: row.Duration, CreatorID: awemeID,
		Usable: len(reasons) == 0, UnusableReasons: reasons,
	}
}

func diagnosticResponse(message, requestID string, list any, info *domainmarketing.PageInfo, status string) MaterialDiagnosticResponse {
	return MaterialDiagnosticResponse{Code: 0, Message: message, RequestID: requestID,
		Data: MaterialDiagnosticData{List: list, PageInfo: info, Status: status}}
}

func compactVideos(rows []domainmarketing.VideoAsset) []domainmarketing.SelectedVideo {
	result := make([]domainmarketing.SelectedVideo, 0, len(rows))
	for _, row := range rows {
		result = append(result, domainmarketing.SelectedVideo{VideoID: row.ID, MaterialID: row.MaterialID,
			Filename: row.Filename, CreateTime: row.CreateTime, Width: row.Width, Height: row.Height,
			Duration: row.Duration, Format: row.Format, Source: row.Source,
			Signature: row.Signature, PosterURL: row.PosterURL})
	}
	return result
}

func videoFiltering(query VideoQuery) map[string]any {
	result := map[string]any{}
	if len(query.MaterialIDs) != 0 {
		result["material_ids"] = append([]string(nil), query.MaterialIDs...)
	} else if len(query.VideoIDs) != 0 {
		result["video_ids"] = append([]string(nil), query.VideoIDs...)
	} else if len(query.Signatures) != 0 {
		result["signatures"] = append([]string(nil), query.Signatures...)
	}
	if query.StartTime != "" {
		result["start_time"] = query.StartTime
	}
	if query.EndTime != "" {
		result["end_time"] = query.EndTime
	}
	return result
}

func resolveDate(value string, now time.Time) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "today":
		return now.Format("2006-01-02"), nil
	case "yesterday":
		return now.AddDate(0, 0, -1).Format("2006-01-02"), nil
	default:
		parsed, err := time.Parse("2006-01-02", value)
		if err != nil {
			return "", errors.New("date must be today, yesterday, or yyyy-mm-dd")
		}
		return parsed.Format("2006-01-02"), nil
	}
}

func validateDateRange(start, end string) error {
	if start == "" && end == "" {
		return nil
	}
	if start != "" {
		if _, err := time.Parse("2006-01-02", start); err != nil {
			return errors.New("start_time must use yyyy-mm-dd")
		}
	}
	if end != "" {
		if _, err := time.Parse("2006-01-02", end); err != nil {
			return errors.New("end_time must use yyyy-mm-dd")
		}
	}
	if start != "" && end != "" && start > end {
		return errors.New("start_time must not be after end_time")
	}
	return nil
}

func parseOfficialTime(value string, location *time.Location) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02"} {
		if parsed, err := time.ParseInLocation(layout, value, location); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func validateNumericIDs(values []string, field string) error {
	for index, value := range values {
		if err := domain.ValidateDecimalID(value, fmt.Sprintf("%s[%d]", field, index)); err != nil {
			return err
		}
	}
	return nil
}

func videoKey(row domainmarketing.VideoAsset) string {
	if row.ID != "" {
		return "video:" + row.ID
	}
	if row.MaterialID != "" {
		return "material:" + row.MaterialID
	}
	return ""
}

func videoKeys(rows []domainmarketing.VideoAsset) []string {
	result := make([]string, 0, len(rows))
	for _, row := range rows {
		result = append(result, videoKey(row))
	}
	return result
}

func imageKeys(rows []domainmarketing.ImageAsset) []string {
	result := make([]string, 0, len(rows))
	for _, row := range rows {
		key := ""
		if row.ID != "" {
			key = "image:" + row.ID
		} else if row.MaterialID != "" {
			key = "material:" + row.MaterialID
		}
		result = append(result, key)
	}
	return result
}

func productKeys(rows []domainmarketing.DPAProduct) []string {
	result := make([]string, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.ProductID)
	}
	return result
}

func creatorAuthorizationKey(row domainmarketing.CreatorAuthorization) string {
	if row.Video.ItemID != "" {
		return row.AwemeID + ":" + row.Video.ItemID
	}
	return strings.Join([]string{row.AwemeID, row.OpenID, row.StartTime, row.Video.VideoID}, ":")
}

func creatorVideoKey(row domainmarketing.CreatorVideo) string {
	if row.ItemID != "" {
		return row.ItemID
	}
	if row.VideoID != "" {
		return "video:" + row.VideoID
	}
	return ""
}

func uniqueStrings(values []string) []string {
	result := []string{}
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

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return append([]string(nil), values...)
}

func keySet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func stringSet(values []string) map[string]struct{} { return keySet(values) }

func requestIDs(value string) []string {
	if value == "" {
		return []string{}
	}
	return []string{value}
}

func nullableMap(value map[string]any) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func normalizeDatePrefix(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 10 {
		return value[:10]
	}
	return value
}

func nonEmptyGroups(groups ...[]string) int {
	count := 0
	for _, values := range groups {
		if len(values) != 0 {
			count++
		}
	}
	return count
}
