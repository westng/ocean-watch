package official

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	planqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans/qianchuan"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/workmetadata"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	domainqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
	port "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/official"
	portqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/qianchuan"
)

// WorkLocator reuses current F2 resolution and exact official ownership/product
// verification, without creating a plan preflight or consulting warm caches.
type WorkLocator struct {
	Queries Service
	Links   workmetadata.Resolver
	Reader  portqianchuan.Reader
}

func (locator WorkLocator) Resolve(ctx context.Context, advertiser, authAccount string, inputs []MaterialWork) ([]string, error) {
	if ctx == nil {
		return nil, failure("INVALID_ARGUMENT", "identity", false)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validTaskID(advertiser) || len(inputs) == 0 || len(inputs) > 100 || locator.Reader == nil {
		return nil, failure("INVALID_ARGUMENT", "identity", false)
	}
	if locator.Queries.Policy.CheckCapability("qianchuan.material_quality") != nil || locator.Queries.Policy.CheckSubject("qianchuan", "advertiser", advertiser) != nil {
		return nil, failure("POLICY_DENIED", "policy", false)
	}
	if locator.Queries.Authorizations == nil || locator.Queries.Tokens == nil {
		return nil, failure("BUSINESS_CONFIGURATION_REQUIRED", "identity", false)
	}
	state, err := locator.Queries.Authorizations.LoadChannel(ctx, "qianchuan")
	if err != nil {
		return nil, failure("ACCOUNT_SYNC_REQUIRED", "scope", false)
	}
	binding, domainErr := domain.ResolveAuthorization("qianchuan", state, advertiser, authAccount, "", false)
	if domainErr != nil {
		return nil, failure("SCOPE_DENIED", "scope", false)
	}
	if locator.Queries.Policy.CheckAuthorization("qianchuan", binding.AuthorizationID) != nil {
		return nil, failure("SCOPE_DENIED", "scope", false)
	}
	urls := []string{}
	for _, input := range inputs {
		if input.URL == "" {
			return nil, failure("INVALID_ARGUMENT", "identity", false)
		}
		urls = append(urls, input.URL)
	}
	links, err := locator.Links.ResolveLinks(ctx, workmetadata.ResolveRequest{URLs: urls, Concurrency: 2})
	if err != nil {
		return nil, failure("WORK_LINK_INVALID", "identity", false)
	}
	resolved, err := locator.Links.ResolveMetadata(ctx, links, 2)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var linkError *domain.WorkLinkError
		allHints := true
		for _, input := range inputs {
			if input.CreatorDouyinID == "" {
				allHints = false
			}
		}
		if allHints {
			resolved = links
			err = nil
		} else {
			if errors.As(err, &linkError) {
				if linkError.Code == "f2_cookie_missing" || linkError.Code == "f2_cookie_invalid" || linkError.Code == "f2_credential_unavailable" {
					return nil, failure("F2_CONFIGURATION_REQUIRED", "identity", false)
				}
				if strings.HasPrefix(linkError.Code, "f2_") {
					return nil, failure("F2_QUERY_FAILED", "identity", false)
				}
				return nil, failure("WORK_LINK_INVALID", "identity", false)
			}
			return nil, failure("F2_QUERY_FAILED", "identity", false)
		}
	}
	// An explicit identity can recover a failed metadata row, but the current
	// public lookup above is still attempted and official checks still follow.
	if len(resolved.Skipped) > 0 {
		for _, row := range links.Resolved {
			present := false
			for _, existing := range resolved.Resolved {
				if existing.InputIndex == row.InputIndex {
					present = true
					break
				}
			}
			if !present && row.InputIndex >= 0 && row.InputIndex < len(inputs) && inputs[row.InputIndex].CreatorDouyinID != "" {
				resolved.Resolved = append(resolved.Resolved, row)
			}
		}
	}
	if len(resolved.Resolved) != len(inputs) {
		return nil, failure("WORK_IDENTIFICATION_INCOMPLETE", "identity", false)
	}
	works := []planqianchuan.WorkInput{}
	products := []string{}
	seenProducts := map[string]bool{}
	seenInputs := map[int]bool{}
	for _, row := range resolved.Resolved {
		if row.InputIndex < 0 || row.InputIndex >= len(inputs) || seenInputs[row.InputIndex] || !validTaskID(row.AwemeItemID) {
			return nil, failure("INVALID_RESPONSE", "identity", false)
		}
		seenInputs[row.InputIndex] = true
		input := inputs[row.InputIndex]
		product := input.ProductID
		if product == "" {
			hints := map[string]bool{}
			if row.ProductHint != nil && row.ProductHint.ProductID != "" {
				hints[row.ProductHint.ProductID] = true
			}
			for _, hint := range row.ProductHints {
				if hint.ProductID != "" {
					hints[hint.ProductID] = true
				}
			}
			if len(hints) == 1 {
				for id := range hints {
					product = id
				}
			}
		}
		if !validTaskID(product) {
			return nil, failure("PRODUCT_SELECTION_REQUIRED", "identity", false)
		}
		if !seenProducts[product] {
			products = append(products, product)
			seenProducts[product] = true
		}
		hint := &planqianchuan.OwnerHint{}
		if row.OwnerHint != nil {
			hint.AwemeID = row.OwnerHint.AwemeID
			hint.AwemeShowID = row.OwnerHint.AwemeShowID
		}
		if input.CreatorUID != "" {
			if hint.AwemeID != "" && hint.AwemeID != input.CreatorUID {
				return nil, failure("CREATOR_IDENTITY_CONFLICT", "identity", false)
			}
			hint.AwemeID = input.CreatorUID
		}
		if input.CreatorDouyinID != "" {
			if hint.AwemeShowID != "" && hint.AwemeShowID != input.CreatorDouyinID {
				return nil, failure("CREATOR_IDENTITY_CONFLICT", "identity", false)
			}
			hint.AwemeShowID = input.CreatorDouyinID
		}
		if hint.AwemeShowID == "" {
			return nil, failure("EXACT_CREATOR_IDENTITY_REQUIRED", "identity", false)
		}
		works = append(works, planqianchuan.WorkInput{InputIndex: row.InputIndex, AwemeItemID: row.AwemeItemID, TargetProductID: product, OwnerHint: hint})
	}
	if len(products) > 30 {
		return nil, failure("IDENTITY_QUERY_LIMIT", "identity", false)
	}
	lease, err := locator.Queries.Tokens.Ensure(ctx, auth.TokenQuery{Channel: "qianchuan", AdvertiserID: advertiser, AuthAccountID: authAccount, AuthorizationID: binding.AuthorizationID})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var domainErr *domain.Error
		if errors.As(err, &domainErr) && domainErr.Code == "reauthorization_required" {
			return nil, &port.Failure{Code: "REAUTHORIZATION_REQUIRED", Stage: "authorization", AuthorizationID: binding.AuthorizationID, AdvertiserID: advertiser}
		}
		return nil, failure("AUTHORIZATION_PREPARATION_FAILED", "authorization", false)
	}
	if lease.Channel != "qianchuan" || lease.AuthorizationID != binding.AuthorizationID || lease.TokenRevision < binding.TokenRevision {
		return nil, failure("AUTHORIZATION_INVALID", "authorization", false)
	}
	scoped, err := auth.WithAdvertiserTokenLease(ctx, lease, advertiser)
	if err != nil {
		return nil, failure("AUTHORIZATION_INVALID", "authorization", false)
	}
	reader := &materialIdentityReader{Reader: locator.Reader}
	verification, err := (planqianchuan.WorkVerifier{Reader: reader}).Verify(scoped, planqianchuan.WorkVerificationRequest{AdvertiserID: advertiser, AccessToken: lease.AccessToken, ProductIDs: products, Works: works, MaxPages: 2})
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if reader.requiresReauthorization() {
		return nil, &port.Failure{Code: "REAUTHORIZATION_REQUIRED", Stage: "official_identity", Dispatched: true, AuthorizationID: binding.AuthorizationID, AdvertiserID: advertiser}
	}
	if err != nil || len(verification.Matched) != len(works) || len(verification.Skipped) > 0 || len(verification.QueryFailures) > 0 {
		return nil, failure("WORK_IDENTIFICATION_INCOMPLETE", "official_identity", true)
	}
	ids := []string{}
	for _, work := range verification.Matched {
		if !validTaskID(work.Material.MaterialID) {
			return nil, failure("MATERIAL_ID_UNAVAILABLE", "official_identity", true)
		}
		ids = append(ids, work.Material.MaterialID)
	}
	return ids, nil
}

// WorkVerifier keeps row failures in its own DTO. Capture the typed OAuth
// condition at the private Reader boundary rather than parsing error text.
type materialIdentityReader struct {
	portqianchuan.Reader
	mu          sync.Mutex
	reauthorize bool
}

func (reader *materialIdentityReader) record(err error) {
	var official interface{ RequiresReauthorization() bool }
	var local *domain.Error
	if errors.As(err, &official) && official.RequiresReauthorization() || errors.As(err, &local) && local.Code == "reauthorization_required" {
		reader.mu.Lock()
		reader.reauthorize = true
		reader.mu.Unlock()
	}
}
func (reader *materialIdentityReader) requiresReauthorization() bool {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	return reader.reauthorize
}
func (reader *materialIdentityReader) FetchAuthorizedCreators(ctx context.Context, request portqianchuan.AuthorizedCreatorPageRequest) (domainqianchuan.AuthorizedCreatorPage, error) {
	result, err := reader.Reader.FetchAuthorizedCreators(ctx, request)
	reader.record(err)
	return result, err
}
func (reader *materialIdentityReader) FetchCreatorVideos(ctx context.Context, request portqianchuan.CreatorVideoPageRequest) (domainqianchuan.CreatorVideoPage, error) {
	result, err := reader.Reader.FetchCreatorVideos(ctx, request)
	reader.record(err)
	return result, err
}

var _ MaterialLocator = WorkLocator{}
