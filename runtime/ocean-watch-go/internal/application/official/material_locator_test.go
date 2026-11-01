package official

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/policy"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/workmetadata"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	domainqc "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
	portqc "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/qianchuan"
	portmetadata "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/workmetadata"
)

type materialLinkFixture struct{}

func (materialLinkFixture) Resolve(_ context.Context, url string) (domain.ResolvedWorkLink, error) {
	return domain.ResolvedWorkLink{InputURL: url, AwemeItemID: "700"}, nil
}

type materialMetadataFixture struct {
	calls int
	err   error
	uid   string
}

func (metadata *materialMetadataFixture) ResolveMany(context.Context, []string, int) (portmetadata.MetadataResult, error) {
	metadata.calls++
	return portmetadata.MetadataResult{Rows: map[string]portmetadata.MetadataRow{"700": {AwemeItemID: "700", AwemeID: metadata.uid, AwemeShowID: "fixture.author", ProductID: "500"}}}, metadata.err
}

type materialReaderFixture struct {
	portqc.Reader
	creatorCalls, videoCalls atomic.Int64
	missingProduct           bool
	foreignWork              bool
	err                      error
}

func (reader *materialReaderFixture) FetchAuthorizedCreators(_ context.Context, request portqc.AuthorizedCreatorPageRequest) (domainqc.AuthorizedCreatorPage, error) {
	reader.creatorCalls.Add(1)
	if request.AdvertiserID != testAD || request.SearchKeyword != "fixture.author" {
		return domainqc.AuthorizedCreatorPage{}, errors.New("unexpected identity scope")
	}
	return domainqc.AuthorizedCreatorPage{Rows: []domainqc.AuthorizedCreator{{AwemeID: "200", VisibleID: "fixture.author", Name: "合成达人"}}, PageInfo: domainqc.PageInfo{Page: 1, TotalPages: 1, TotalNumber: 1}}, reader.err
}
func (reader *materialReaderFixture) FetchCreatorVideos(_ context.Context, request portqc.CreatorVideoPageRequest) (domainqc.CreatorVideoPage, error) {
	reader.videoCalls.Add(1)
	if request.AdvertiserID != testAD || request.AwemeID != "200" || len(request.AwemeItemIDs) != 1 || request.AwemeItemIDs[0] != "700" {
		return domainqc.CreatorVideoPage{}, errors.New("unexpected work scope")
	}
	if request.ProductID != "" && reader.missingProduct {
		return domainqc.CreatorVideoPage{}, nil
	}
	id := "700"
	if reader.foreignWork {
		id = "701"
	}
	return domainqc.CreatorVideoPage{Rows: []domainqc.CreatorVideo{{AwemeItemID: id, VideoID: "800", MaterialID: "77", Title: "合成作品"}}}, reader.err
}

func TestMaterialWorksAlwaysTryCurrentF2AndOfficialChecks(t *testing.T) {
	for _, failMetadata := range []bool{false, true} {
		s, _, _, _ := fixtureService(t)
		metadata := &materialMetadataFixture{uid: "200"}
		if failMetadata {
			metadata.err = errors.New("synthetic F2 failure")
		}
		reader := &materialReaderFixture{}
		locator := WorkLocator{Queries: s, Links: workmetadata.Resolver{Links: materialLinkFixture{}, Metadata: metadata}, Reader: reader}
		inputs := []MaterialWork{{URL: "https://www.douyin.com/video/700", ProductID: "500", CreatorUID: "200", CreatorDouyinID: "fixture.author"}}
		ids, err := locator.Resolve(fixtureContext(t), testAD, "", inputs)
		if err != nil || len(ids) != 1 || ids[0] != "77" || metadata.calls != 1 || reader.creatorCalls.Load() != 1 || reader.videoCalls.Load() != 2 {
			t.Fatalf("identity hints skipped a current check: %v", err)
		}
		reader.missingProduct = true
		if _, err := locator.Resolve(fixtureContext(t), testAD, "", inputs); errorCode(err) != "WORK_IDENTIFICATION_INCOMPLETE" {
			t.Fatal("unmatched product became a material identity")
		}
		reader.missingProduct = false
		reader.foreignWork = true
		if _, err := locator.Resolve(fixtureContext(t), testAD, "", inputs); errorCode(err) != "WORK_IDENTIFICATION_INCOMPLETE" {
			t.Fatal("foreign work became a material identity")
		}
	}
}

func TestMaterialIdentityConflictGrantAndReauthorizationBoundaries(t *testing.T) {
	s, _, tokens, _ := fixtureService(t)
	metadata := &materialMetadataFixture{uid: "201"}
	reader := &materialReaderFixture{}
	locator := WorkLocator{Queries: s, Links: workmetadata.Resolver{Links: materialLinkFixture{}, Metadata: metadata}, Reader: reader}
	inputs := []MaterialWork{{URL: "https://www.douyin.com/video/700", ProductID: "500", CreatorUID: "200", CreatorDouyinID: "fixture.author"}}
	if _, err := locator.Resolve(fixtureContext(t), testAD, "", inputs); errorCode(err) != "CREATOR_IDENTITY_CONFLICT" || tokens.calls != 0 || reader.creatorCalls.Load() != 0 {
		t.Fatal("conflicting identity reached official API")
	}
	metadata.uid = "200"
	locator.Queries.Policy, _ = policy.New(policy.Config{Mode: "read", Toolsets: []string{"qianchuan"}, AllowedSubjects: map[string]map[string][]string{"qianchuan": {"advertiser": {testAD}, "authorization": {"foreign-grant"}}}})
	before := metadata.calls
	if _, err := locator.Resolve(fixtureContext(t), testAD, "", inputs); errorCode(err) != "SCOPE_DENIED" || tokens.calls != 0 || metadata.calls != before {
		t.Fatal("foreign grant reached F2 or TokenManager")
	}
	locator.Queries.Policy = policy.Default()
	reader.err = domain.NewError("reauthorization_required", "synthetic-private-error", 1, nil)
	_, err := locator.Resolve(fixtureContext(t), testAD, "", inputs)
	if errorCode(err) != "REAUTHORIZATION_REQUIRED" {
		t.Fatal("official ownership reauthorization became an identity mismatch")
	}
	ctx, cancel := context.WithCancel(fixtureContext(t))
	cancel()
	before = metadata.calls
	if _, err := locator.Resolve(ctx, testAD, "", inputs); !errors.Is(err, context.Canceled) || metadata.calls != before {
		t.Fatal("cancelled identity request made a new public query")
	}
}
