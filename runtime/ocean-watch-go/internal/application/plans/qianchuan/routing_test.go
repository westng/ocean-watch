package qianchuan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	applicationworkmetadata "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/workmetadata"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	domainqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/requestcontrol"
	portqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/qianchuan"
)

const routeAdvertiserB = "1000000000000002"
const routeProductB = "5000000000000002"

func TestRoutedWorksKeepAccountsProductsCredentialsAndSnapshotsIsolated(t *testing.T) {
	service, tokens, reader, links, writer := routingTestService()
	items := []domainqianchuan.BatchItem{
		routingTestItem(0, "测试商品"), routingTestItem(1, "商品乙"), routingTestItem(2, "测试商品"),
	}
	result, err := service.BatchWorks(context.Background(), BatchWorksCommand{Items: items})
	if err != nil || !result.Routed || len(result.Batches) != 2 || len(result.Pending) != 0 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if !reflect.DeepEqual(result.Batches[0].InputIndexes, []int{0, 2}) || !reflect.DeepEqual(result.Batches[1].InputIndexes, []int{1}) {
		t.Fatalf("input rows crossed batches: %#v", result.Batches)
	}
	if writer.createCalls != 0 || writer.addCalls != 0 || links.metadataCalls != 1 || links.linkCalls != 1 {
		t.Fatalf("preflight wrote plans or repeated F2: writer=%#v metadata=%d", writer, links.metadataCalls)
	}
	if len(tokens.queries) != 2 {
		t.Fatalf("credential lookups=%#v", tokens.queries)
	}
	ids := map[string]bool{}
	for _, batch := range result.Batches {
		if batch.Result == nil || len(batch.Result.Results) != 1 || batch.Result.Results[0].Status != "would_create" {
			t.Fatalf("child failed: %#v", batch)
		}
		if ids[batch.Result.PreflightID] || batch.Result.PreflightID == "" {
			t.Fatalf("child snapshot ID missing/reused: %#v", batch)
		}
		ids[batch.Result.PreflightID] = true
		summary, err := service.GetBatchPreflight(context.Background(), batch.Result.PreflightID)
		if err != nil || summary.AdvertiserID != batch.Template.AdvertiserID || !reflect.DeepEqual(summary.ProductIDs, batch.Template.ProductIDs) {
			t.Fatalf("snapshot crossed account/product boundary: %#v err=%v", summary, err)
		}
		if len(batch.Result.Results[0].InputItemIDs) != len(batch.InputIndexes) {
			t.Fatalf("works lost while grouping: %#v", batch)
		}
	}
	if reader.wrongScope != "" {
		t.Fatal(reader.wrongScope)
	}
}

func TestRoutingAmbiguityAndConflictingBindingsNeverProbeAccounts(t *testing.T) {
	service, tokens, _, links, _ := routingTestService()
	config := routingTestConfig()
	templates := config["qianchuan_product_templates"].(map[string]any)
	duplicate := routingTestTemplate("qcpt_duplicate", routeAdvertiserB, batchProductID, "测试商品")
	templates["qcpt_duplicate"] = duplicate
	service.Config = commandConfigReader{config: config}
	result, err := service.BatchWorks(context.Background(), BatchWorksCommand{Items: []domainqianchuan.BatchItem{
		{InputIndex: 0, WorkURL: "https://www.douyin.com/video/6000000000000001", ProductID: batchProductID},
		{InputIndex: 1, WorkURL: "https://www.douyin.com/video/6000000000000002", ProductID: "9999"},
		{InputIndex: 2, WorkURL: "https://www.douyin.com/video/6000000000000003", PlanTemplate: "qcpt_command", ProductID: routeProductB},
	}})
	if err != nil || len(result.Batches) != 0 || len(result.Pending) != 3 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if result.Pending[0].Reason != "routing_ambiguous" || len(result.Pending[0].Candidates) != 2 || result.Pending[1].Reason != "routing_not_found" || result.Pending[2].Reason != "routing_not_found" {
		t.Fatalf("incorrect routing decisions: %#v", result.Pending)
	}
	if len(tokens.queries) != 0 || links.linkCalls != 1 {
		t.Fatalf("unresolved routing crossed read boundary: tokens=%#v links=%d", tokens.queries, links.linkCalls)
	}
}

func TestAutomaticProductRoutingReusesF2AndDoesNotChooseFirstProduct(t *testing.T) {
	service, _, _, links, _ := routingTestService()
	links.products = map[string][]string{
		"6000000000000001": {batchProductID}, "6000000000000002": {routeProductB},
		"6000000000000003": {batchProductID, routeProductB},
	}
	items := []domainqianchuan.BatchItem{}
	for index := 0; index < 3; index++ {
		items = append(items, domainqianchuan.BatchItem{InputIndex: index, WorkURL: fmt.Sprintf("https://www.douyin.com/video/%d", 6000000000000001+index)})
	}
	result, err := service.BatchWorks(context.Background(), BatchWorksCommand{Items: items})
	if err != nil || len(result.Batches) != 2 || len(result.Pending) != 1 || result.Pending[0].Reason != "multiple_work_products" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if links.metadataCalls != 1 || links.linkCalls != 1 {
		t.Fatalf("public metadata fetched repeatedly: links=%d metadata=%d", links.linkCalls, links.metadataCalls)
	}
	for _, batch := range result.Batches {
		if batch.Result == nil || len(batch.Result.Results) != 1 {
			t.Fatalf("routed metadata was lost: %#v", batch)
		}
	}
}

func TestRoutingFailureIsConfinedToOneAdvertiser(t *testing.T) {
	service, tokens, _, _, _ := routingTestService()
	tokens.failAdvertiser = batchAdvertiserID
	result, err := service.BatchWorks(context.Background(), BatchWorksCommand{Items: []domainqianchuan.BatchItem{routingTestItem(0, "测试商品"), routingTestItem(1, "商品乙")}})
	if err != nil || len(result.Batches) != 2 || result.ExitCode != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if result.Batches[0].Status != "preflight_failed" || result.Batches[0].FailureStage != "authorization" || result.Batches[1].Result == nil || result.Batches[1].Result.PreflightID == "" {
		t.Fatalf("account failure blocked unrelated child: %#v", result.Batches)
	}
}

func TestRowTemplateOverridesGlobalFallbackWithoutCrossingAccounts(t *testing.T) {
	service, _, reader, _, _ := routingTestService()
	first, second := routingTestItem(0, "测试商品"), routingTestItem(1, "商品乙")
	second.PlanTemplate = "qcpt_second"
	result, err := service.BatchWorks(context.Background(), BatchWorksCommand{PlanTemplate: "qcpt_command", Items: []domainqianchuan.BatchItem{first, second}})
	if err != nil || len(result.Pending) != 0 || len(result.Batches) != 2 || result.Batches[0].Template.AdvertiserID != batchAdvertiserID || result.Batches[1].Template.AdvertiserID != routeAdvertiserB || reader.wrongScope != "" {
		t.Fatalf("row template did not override only its own row: result=%#v err=%v scope=%s", result, err, reader.wrongScope)
	}
}

func TestMixedRoutingRequiresSnapshotBeforeAnyWrite(t *testing.T) {
	service, tokens, _, links, writer := routingTestService()
	_, err := service.BatchWorks(context.Background(), BatchWorksCommand{Items: []domainqianchuan.BatchItem{routingTestItem(0, "测试商品")}, Submit: true})
	if err == nil || len(tokens.queries) != 0 || links.linkCalls != 0 || writer.createCalls != 0 {
		t.Fatalf("routed submission crossed guard: err=%v", err)
	}
}

func TestRoutingConflictingAuthorizationSelectorsStayPending(t *testing.T) {
	service, tokens, _, links, _ := routingTestService()
	left, right := routingTestItem(0, "测试商品"), routingTestItem(1, "测试商品")
	left.AuthAccountID, right.AuthAccountID = "grant-a", "grant-b"
	result, err := service.BatchWorks(context.Background(), BatchWorksCommand{Items: []domainqianchuan.BatchItem{left, right}})
	if err != nil || len(result.Batches) != 0 || len(result.Pending) != 2 || len(tokens.queries) != 0 || links.linkCalls != 1 {
		t.Fatalf("conflicting grants reached official reads: result=%#v err=%v", result, err)
	}
	for _, pending := range result.Pending {
		if pending.Reason != "authorization_selector_conflict" {
			t.Fatalf("wrong pending reason: %#v", pending)
		}
	}
}

func TestExplicitTargetProductCannotExpandToOtherTemplateProducts(t *testing.T) {
	for _, target := range []string{routeProductB, "9999"} {
		result, err := (WorkVerifier{Reader: &commandReader{}}).Verify(context.Background(), WorkVerificationRequest{
			AdvertiserID: batchAdvertiserID, AccessToken: commandToken, ProductIDs: []string{batchProductID, routeProductB},
			Works: []WorkInput{{InputIndex: 0, AwemeItemID: "6000000000000001", TargetProductID: target, OwnerHint: &OwnerHint{AwemeID: batchCreatorID, AwemeShowID: batchVisibleID}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if target == routeProductB {
			if len(result.Matched) != 1 || !reflect.DeepEqual(result.Matched[0].MatchedProductIDs, []string{target}) {
				t.Fatalf("target product expanded: %#v", result)
			}
		} else if len(result.Matched) != 0 || len(result.Skipped) != 1 || result.Skipped[0].Reason != "product_mismatch" {
			t.Fatalf("unbound target product accepted: %#v", result)
		}
	}
}

func TestRoutedCreatePayloadContainsOnlyTheVerifiedGroupProducts(t *testing.T) {
	service := BatchService{Reader: &batchStateReader{}, Reconciler: commandNoPlanFinder{}}
	result, err := service.Execute(context.Background(), BatchRequest{
		AdvertiserID: batchAdvertiserID, ReadAccessToken: batchToken,
		TemplateID: "qcpt_fixture", TemplateName: "fixture", ProductName: "fixture-product", IncludePayloads: true,
		TemplatePayload: json.RawMessage(`{"advertiser_id":1000000000000001,"marketing_goal":"VIDEO_PROM_GOODS","product_ids":[5000000000000001,5000000000000002],"delivery_setting":{"smart_bid_type":"SMART_BID_CUSTOM","roi2_goal":1.75,"budget":5000,"video_schedule_type":"SCHEDULE_FROM_NOW"}}`),
		Works:           batchVerifiedWorks(1),
	})
	if err != nil || len(result.Results) != 1 || len(result.Results[0].Writes) != 1 {
		t.Fatalf("create preview failed: result=%#v err=%v", result, err)
	}
	var payload struct {
		ProductIDs []int64 `json:"product_ids"`
		Creatives  []struct {
			ProductID int64 `json:"product_id"`
		} `json:"multi_product_creative_list"`
	}
	if err := json.Unmarshal(result.Results[0].Writes[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(payload.ProductIDs, []int64{5000000000000001}) || len(payload.Creatives) != 1 || payload.Creatives[0].ProductID != payload.ProductIDs[0] {
		t.Fatalf("create payload expanded beyond verified target products: %#v", payload)
	}
}

func TestRoutedDuplicateConflictDoesNotSilentlyDiscardRowSettings(t *testing.T) {
	left, right := routingTestItem(0, "测试商品"), routingTestItem(1, "测试商品")
	right.Business = "different-business"
	resolver := &routedLinkResolver{items: []domainqianchuan.BatchItem{left, right}, prepared: map[string]domain.ResolvedWorkLink{
		left.WorkURL: {AwemeItemID: "6000000000000001"}, right.WorkURL: {AwemeItemID: "6000000000000001"},
	}}
	_, err := resolver.ResolveLinks(context.Background(), applicationworkmetadata.ResolveRequest{URLs: []string{left.WorkURL, right.WorkURL}, Concurrency: 1})
	if !errors.Is(err, domainqianchuan.ErrDuplicateItemConflict) {
		t.Fatalf("duplicate settings silently discarded: %v", err)
	}
}

func TestAutomaticRoutingRetainsDuplicateRowsUntilSettingsAreChecked(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		service, _, _, links, writer := routingTestService()
		links.products = map[string][]string{"6000000000000001": {batchProductID}}
		items := []domainqianchuan.BatchItem{
			{InputIndex: 0, WorkURL: "https://www.douyin.com/video/6000000000000001"},
			{InputIndex: 1, WorkURL: "https://www.douyin.com/video/6000000000000001"},
		}
		if conflict {
			items[1].Business = "different-business"
		}
		result, err := service.BatchWorks(context.Background(), BatchWorksCommand{Items: items})
		if err != nil || len(result.Pending) != 0 || len(result.Batches) != 1 || writer.createCalls != 0 {
			t.Fatalf("duplicate routing lost rows: result=%#v err=%v", result, err)
		}
		child := result.Batches[0]
		if conflict {
			if child.Status != "preflight_failed" || !errors.Is(child.Cause, domainqianchuan.ErrDuplicateItemConflict) {
				t.Fatalf("conflicting duplicate produced a snapshot: %#v", child)
			}
		} else if child.Result == nil || child.Result.PreflightID == "" || len(child.Result.Skipped) != 1 || child.Result.Skipped[0].InputIndex != 1 {
			t.Fatalf("identical duplicate was not deduped inside its child: %#v", child)
		}
	}
}

func TestRoutedCreatorRecoveryRequiresExactUnambiguousVisibleIdentity(t *testing.T) {
	for _, variant := range []string{"exact", "ambiguous", "incomplete", "missing"} {
		t.Run(variant, func(t *testing.T) {
			reader := &routingTestReader{variant: variant}
			result, err := (WorkVerifier{Reader: reader}).Verify(context.Background(), WorkVerificationRequest{
				AdvertiserID: batchAdvertiserID, AccessToken: "token-" + batchAdvertiserID, ProductIDs: []string{batchProductID},
				Works: []WorkInput{{InputIndex: 0, AwemeItemID: "6000000000000001", CreatorName: "用户明确提供的昵称", OwnerHint: &OwnerHint{AwemeShowID: batchVisibleID}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if variant == "exact" {
				if len(result.Matched) != 1 || result.Matched[0].CreatorName != "用户明确提供的昵称" {
					t.Fatalf("explicit naming hint was lost: %#v", result)
				}
				return
			}
			want := map[string]string{"ambiguous": "creator_identity_ambiguous", "incomplete": "creator_query_incomplete", "missing": "creator_identity_not_found"}[variant]
			if len(result.Matched) != 0 || len(result.Skipped) != 1 || result.Skipped[0].Reason != want {
				t.Fatalf("bad recovery: %#v", result)
			}
		})
	}
}

func routingTestItem(index int, product string) domainqianchuan.BatchItem {
	return domainqianchuan.BatchItem{InputIndex: index, WorkURL: fmt.Sprintf("https://www.douyin.com/video/%d", 6000000000000001+index), ProductName: product, CreatorDouyinID: batchVisibleID, CreatorName: "用户指定昵称"}
}

func routingTestConfig() map[string]any {
	config := commandProductConfig()
	config["qianchuan_product_templates"].(map[string]any)["qcpt_second"] = routingTestTemplate("qcpt_second", routeAdvertiserB, routeProductB, "商品乙")
	return config
}

func routingTestTemplate(id, advertiserID, productID, name string) map[string]any {
	template := commandProductConfig()["qianchuan_product_templates"].(map[string]any)["qcpt_command"].(map[string]any)
	template["template_id"], template["display_name"] = id, id
	template["bindings"] = map[string]any{"channel": "qianchuan", "advertiser_id": advertiserID, "product_name": name, "product_short_name": name, "product_ids": []any{productID}}
	return template
}

func routingTestService() (CommandService, *routingTestTokens, *routingTestReader, *routingTestLinks, *routingTestWriter) {
	tokens, reader, links, writer := &routingTestTokens{}, &routingTestReader{}, &routingTestLinks{}, &routingTestWriter{}
	return CommandService{Config: commandConfigReader{config: routingTestConfig()}, Tokens: tokens, Links: links, Verifier: WorkVerifier{Reader: reader},
		Batch: BatchService{Reader: reader, Writer: writer, Reconciler: commandNoPlanFinder{}}, Journals: &commandJournalStore{}}, tokens, reader, links, writer
}

type routingTestWriter struct {
	commandWriter
	addCalls int
}

func (writer *routingTestWriter) AddMaterials(context.Context, portqianchuan.MaterialWriteRequest) (portqianchuan.WriteResult, error) {
	writer.addCalls++
	return portqianchuan.WriteResult{}, errors.New("unexpected material write during routing")
}

type routingTestTokens struct {
	mu             sync.Mutex
	queries        []authapplication.TokenQuery
	failAdvertiser string
}

func (tokens *routingTestTokens) Ensure(_ context.Context, query authapplication.TokenQuery) (authapplication.TokenLease, error) {
	tokens.mu.Lock()
	defer tokens.mu.Unlock()
	tokens.queries = append(tokens.queries, query)
	if query.AdvertiserID == tokens.failAdvertiser {
		return authapplication.TokenLease{}, errors.New("fixture private token error")
	}
	return authapplication.TokenLease{Channel: "qianchuan", AuthorizationID: "auth-" + query.AdvertiserID, AccessToken: "token-" + query.AdvertiserID}, nil
}

type routingTestReader struct {
	commandReader
	mu         sync.Mutex
	wrongScope string
	variant    string
}

func (reader *routingTestReader) FetchAuthorizedCreators(ctx context.Context, request portqianchuan.AuthorizedCreatorPageRequest) (domainqianchuan.AuthorizedCreatorPage, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if request.SearchKeyword != batchVisibleID || request.AccessToken != "token-"+request.AdvertiserID {
		reader.wrongScope = "authorization query crossed advertiser/token scope"
	}
	if scope, ok := requestcontrol.Authorization(ctx); ok && scope.AuthorizationID != "auth-"+request.AdvertiserID {
		reader.wrongScope = "authorization context crossed account scope"
	}
	if reader.variant == "incomplete" {
		return domainqianchuan.AuthorizedCreatorPage{}, errors.New("fixture response unavailable")
	}
	rows := []domainqianchuan.AuthorizedCreator{{AwemeID: batchCreatorID, VisibleID: batchVisibleID, Name: "官方标签不得代替命名昵称"}}
	if reader.variant == "ambiguous" {
		rows = append(rows, domainqianchuan.AuthorizedCreator{AwemeID: "4000000000000002", VisibleID: batchVisibleID})
	}
	if reader.variant == "missing" {
		rows[0].VisibleID = "some-other-visible-id"
	}
	return domainqianchuan.AuthorizedCreatorPage{Rows: rows, PageInfo: domainqianchuan.PageInfo{Page: request.Page, TotalPages: 1, TotalNumber: len(rows)}}, nil
}
func (reader *routingTestReader) FetchCreatorVideos(_ context.Context, request portqianchuan.CreatorVideoPageRequest) (domainqianchuan.CreatorVideoPage, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	wantProduct := batchProductID
	if request.AdvertiserID == routeAdvertiserB {
		wantProduct = routeProductB
	}
	if request.AccessToken != "token-"+request.AdvertiserID || request.ProductID != "" && request.ProductID != wantProduct || request.AwemeID != batchCreatorID {
		reader.wrongScope = "video/product query crossed advertiser scope"
	}
	rows := []domainqianchuan.CreatorVideo{}
	for _, id := range request.AwemeItemIDs {
		rows = append(rows, domainqianchuan.CreatorVideo{AwemeItemID: id, ImageMode: "VIDEO_LARGE", MaterialID: "material-" + id, Title: "fixture"})
	}
	return domainqianchuan.CreatorVideoPage{Rows: rows}, nil
}

type routingTestLinks struct {
	linkCalls, metadataCalls int
	products                 map[string][]string
}

func (links *routingTestLinks) Resolve(ctx context.Context, request applicationworkmetadata.ResolveRequest) (applicationworkmetadata.ResolveResult, error) {
	result, err := links.ResolveLinks(ctx, request)
	if err != nil {
		return result, err
	}
	return links.ResolveMetadata(ctx, result, request.Concurrency)
}
func (links *routingTestLinks) ResolveLinks(_ context.Context, request applicationworkmetadata.ResolveRequest) (applicationworkmetadata.ResolveResult, error) {
	links.linkCalls++
	result := applicationworkmetadata.ResolveResult{}
	seen := map[string]bool{}
	for index, url := range request.URLs {
		id := url[strings.LastIndex(url, "/")+1:]
		if seen[id] {
			result.Skipped = append(result.Skipped, domain.SkippedWorkLink{InputIndex: index, InputURL: url, AwemeItemID: id, Reason: "duplicate_input"})
			continue
		}
		seen[id] = true
		result.Resolved = append(result.Resolved, domain.ResolvedWorkLink{InputIndex: index, InputURL: url, AwemeItemID: id})
	}
	return result, nil
}
func (links *routingTestLinks) ResolveMetadata(_ context.Context, result applicationworkmetadata.ResolveResult, _ int) (applicationworkmetadata.ResolveResult, error) {
	links.metadataCalls++
	for index := range result.Resolved {
		row := &result.Resolved[index]
		row.OwnerHint = &domain.WorkOwnerHint{AwemeID: batchCreatorID, AwemeShowID: batchVisibleID}
		row.CreatorName = "F2 昵称"
		for _, product := range links.products[row.AwemeItemID] {
			row.ProductHints = append(row.ProductHints, domain.WorkProductHint{ProductID: product})
		}
	}
	return result, nil
}
