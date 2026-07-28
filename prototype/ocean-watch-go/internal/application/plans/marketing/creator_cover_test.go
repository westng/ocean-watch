package marketing

import (
	"context"
	"errors"
	"reflect"
	"testing"

	applicationdiscovery "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/discovery"
	domainmarketing "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/marketing"
)

func TestHistoricalCreatorCoverResolverSkipsDiscoveryWhenCoversExist(t *testing.T) {
	candidate := creatorCoverCandidate("cover-current")
	result, err := (HistoricalCreatorCoverResolver{}).Resolve(context.Background(), CreatorCoverRequest{
		AdvertiserID: "1234567890", Candidates: []domainmarketing.CreatorCandidate{candidate},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Diagnostics["status"] != "not_required" || !reflect.DeepEqual(result.Candidates, []domainmarketing.CreatorCandidate{candidate}) {
		t.Fatalf("complete authorization snapshot changed: %#v", result)
	}
}

func TestHistoricalCreatorCoverResolverRecoversUniqueMatchingCover(t *testing.T) {
	discovery := &creatorPromotionDiscoveryStub{pages: map[int]applicationdiscovery.Result{
		1: creatorPromotionPage(1, 1,
			creatorPromotion("1234567890", "8101", "8201", creatorCoverOKStatus, "cover-history"),
		),
	}}
	original := creatorCoverCandidate("")
	result, err := (HistoricalCreatorCoverResolver{Discovery: discovery}).Resolve(
		context.Background(),
		CreatorCoverRequest{AdvertiserID: "1234567890", AuthAccountID: "auth-1", Candidates: []domainmarketing.CreatorCandidate{original}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(discovery.pagesCalled, []int{1}) || len(discovery.queries) != 1 ||
		discovery.queries[0].AdvertiserID != "1234567890" || discovery.queries[0].AuthAccountID != "auth-1" {
		t.Fatalf("promotion discovery scope changed: calls=%#v queries=%#v", discovery.pagesCalled, discovery.queries)
	}
	resolved := result.Candidates[0]
	if resolved.VideoCoverID != "cover-history" || !resolved.Usable || len(resolved.UnusableReasons) != 0 ||
		result.Diagnostics["status"] != "resolved" || result.Diagnostics["source"] != "matching_official_promotion" {
		t.Fatalf("unique historical cover was not recovered: %#v", result)
	}
	if original.VideoCoverID != "" || original.Usable || !reflect.DeepEqual(original.UnusableReasons, []string{"missing_video_cover_id"}) {
		t.Fatalf("resolver mutated the authorization snapshot: %#v", original)
	}
}

func TestHistoricalCreatorCoverResolverIgnoresWrongIdentityAndStatus(t *testing.T) {
	discovery := &creatorPromotionDiscoveryStub{pages: map[int]applicationdiscovery.Result{
		1: creatorPromotionPage(1, 1,
			creatorPromotion("1234567890", "8101", "wrong-material", creatorCoverOKStatus, "wrong-material-cover"),
			creatorPromotion("1234567890", "8101", "8201", "MATERIAL_STATUS_REJECTED", "wrong-status-cover"),
			creatorPromotion("9876543210", "8101", "8201", creatorCoverOKStatus, "wrong-owner-cover"),
		),
	}}
	result, err := (HistoricalCreatorCoverResolver{Discovery: discovery}).Resolve(
		context.Background(),
		CreatorCoverRequest{AdvertiserID: "1234567890", Candidates: []domainmarketing.CreatorCandidate{creatorCoverCandidate("")}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Candidates[0].VideoCoverID != "" || result.Candidates[0].Usable ||
		result.Diagnostics["status"] != "partially_resolved" ||
		!reflect.DeepEqual(result.Diagnostics["unresolved_item_ids"], []string{"8101"}) {
		t.Fatalf("non-matching promotion material was accepted: %#v", result)
	}
}

func TestHistoricalCreatorCoverResolverRejectsDistinctCovers(t *testing.T) {
	discovery := &creatorPromotionDiscoveryStub{pages: map[int]applicationdiscovery.Result{
		1: creatorPromotionPage(1, 1,
			creatorPromotion("1234567890", "8101", "8201", creatorCoverOKStatus, "cover-a"),
			creatorPromotion("1234567890", "8101", "8201", creatorCoverOKStatus, "cover-b"),
		),
	}}
	_, err := (HistoricalCreatorCoverResolver{Discovery: discovery}).Resolve(
		context.Background(),
		CreatorCoverRequest{AdvertiserID: "1234567890", Candidates: []domainmarketing.CreatorCandidate{creatorCoverCandidate("")}},
	)
	var coverErr *CreatorCoverError
	if !errors.As(err, &coverErr) || coverErr.Code != "creator_cover_selection_required" ||
		!reflect.DeepEqual(coverErr.Details["candidate_cover_ids"], []string{"cover-a", "cover-b"}) {
		t.Fatalf("ambiguous historical covers were not blocked: %v", err)
	}
}

func TestHistoricalCreatorCoverResolverTraversesEveryDeclaredPage(t *testing.T) {
	discovery := &creatorPromotionDiscoveryStub{pages: map[int]applicationdiscovery.Result{
		1: creatorPromotionPage(1, 2,
			creatorPromotion("1234567890", "other-item", "other-material", creatorCoverOKStatus, "other-cover"),
		),
		2: creatorPromotionPage(2, 2,
			creatorPromotion("1234567890", "8101", "8201", creatorCoverOKStatus, "cover-page-two"),
		),
	}}
	result, err := (HistoricalCreatorCoverResolver{Discovery: discovery}).Resolve(
		context.Background(),
		CreatorCoverRequest{AdvertiserID: "1234567890", Candidates: []domainmarketing.CreatorCandidate{creatorCoverCandidate("")}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(discovery.pagesCalled, []int{1, 2}) || result.Candidates[0].VideoCoverID != "cover-page-two" {
		t.Fatalf("promotion pagination stopped early: calls=%#v result=%#v", discovery.pagesCalled, result)
	}
}

type creatorPromotionDiscoveryStub struct {
	pages       map[int]applicationdiscovery.Result
	pagesCalled []int
	queries     []applicationdiscovery.PromotionQuery
}

func (stub *creatorPromotionDiscoveryStub) QueryPromotions(
	_ context.Context,
	query applicationdiscovery.PromotionQuery,
) (applicationdiscovery.Result, error) {
	stub.pagesCalled = append(stub.pagesCalled, query.Page)
	stub.queries = append(stub.queries, query)
	result, ok := stub.pages[query.Page]
	if !ok {
		return applicationdiscovery.Result{}, errors.New("unexpected promotion page")
	}
	return result, nil
}

func creatorCoverCandidate(coverID string) domainmarketing.CreatorCandidate {
	candidate := domainmarketing.CreatorCandidate{
		OwnerAdvertiserID: "1234567890", CreatorID: "7001", CreatorName: "fixture creator",
		ItemID: "8101", MaterialID: "8201", VideoID: "video-8101", VideoCoverID: coverID,
		ImageMode: "CREATIVE_IMAGE_MODE_VIDEO_VERTICAL", Title: "fixture work", Usable: coverID != "",
	}
	if coverID == "" {
		candidate.UnusableReasons = []string{"missing_video_cover_id"}
	}
	return candidate
}

func creatorPromotionPage(page, totalPages int, rows ...map[string]any) applicationdiscovery.Result {
	list := make([]any, len(rows))
	for index, row := range rows {
		list[index] = row
	}
	return applicationdiscovery.Result{
		RequestID: "request-page-" + string(rune('0'+page)),
		Response: map[string]any{"data": map[string]any{
			"list": list, "page_info": map[string]any{"total_page": totalPages},
		}},
	}
}

func creatorPromotion(advertiserID, itemID, materialID, status, coverID string) map[string]any {
	return map[string]any{
		"advertiser_id": advertiserID, "project_id": "2001", "promotion_id": "3001",
		"promotion_materials": map[string]any{"video_material_list": []any{map[string]any{
			"item_id": itemID, "material_id": materialID, "material_status": status, "video_cover_id": coverID,
		}}},
	}
}
