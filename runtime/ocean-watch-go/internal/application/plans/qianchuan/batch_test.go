package qianchuan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	sharedplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
	domainqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
	portqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/qianchuan"
)

const (
	batchAdvertiserID = "1000000000000001"
	batchCreatorID    = "4000000000000001"
	batchVisibleID    = "creator-visible"
	batchProductID    = "5000000000000001"
	batchPlanID       = "2000000000000001"
	batchToken        = "TEST_QIANCHUAN_BATCH_TOKEN_DO_NOT_USE"
)

func TestBatchWorkIdempotencyAndPresentation(t *testing.T) {
	t.Run("verification targets one creator and batches works by fifty", testBatchVerificationLimits)
	t.Run("owner hints require targeted verification without broad fallback", testBatchOwnerHintVerification)
	t.Run("multiple creator plans are filtered by verified work products", testBatchPlanProductDisambiguation)
	t.Run("unknown append reconciles and rerun writes nothing", testBatchAppendIdempotency)
}

func TestBatchPlanNameRendersTemplateAndWeightedLimit(t *testing.T) {
	service := BatchService{Now: func() time.Time {
		return time.Date(2026, 8, 4, 12, 30, 45, 0, time.Local)
	}}
	creator := domainqianchuan.AuthorizedCreator{
		AwemeID: batchCreatorID, VisibleID: batchVisibleID, Name: "达人甲",
	}
	request := normalizedBatchRequest{BatchRequest: BatchRequest{
		ProductName:      "测试商品官方全称",
		ProductShortName: "测试商品",
		PlanNameTemplate: "{creator_name}_{douyin_id}_{aweme_id}_{product_name}_{date}_{time}_{datetime}",
	}}
	group := batchGroup{creator: creator, works: []VerifiedWork{{CreatorName: "达人甲"}}}
	if got, err := service.planName(request, group); err != nil || got != "达人甲_creator-visible_4000000000000001_测试商品官方全称_20260804_123045_20260804123045" {
		t.Fatalf("custom plan name rendered as %q", got)
	}
	request.PlanNameTemplate = ""
	request.PlanType, request.Business = "随手po", "刘研"
	if got, err := service.planName(request, group); err != nil || got != "8.4-达人甲-测试商品-随手po-刘研" {
		t.Fatalf("default plan name rendered as %q", got)
	}
	request.PlanType = ""
	if got, err := service.planName(request, group); err != nil || got != "8.4-达人甲-测试商品-刘研" {
		t.Fatalf("missing runtime plan type rendered as %q: %v", got, err)
	}
	request.PlanType, request.Business = "随手po", ""
	if got, err := service.planName(request, group); err != nil || got != "8.4-达人甲-测试商品-随手po" {
		t.Fatalf("missing runtime business rendered as %q: %v", got, err)
	}
	request.PlanNameTemplate = "{product_name}-{creator_name}-{datetime}"
	if got, err := service.planName(request, group); err != nil || got != "测试商品官方全称-达人甲-20260804123045" {
		t.Fatalf("legacy plan name rendered as %q", got)
	}
	request.PlanNameTemplate = "{creator_name}"
	creator.Name = strings.Repeat("达", 60)
	group.creator = creator
	group.works[0].CreatorName = creator.Name
	if got, err := service.planName(request, group); err != nil || got != strings.Repeat("达", 50) {
		t.Fatalf("weighted plan-name limit changed: %q", got)
	}
	creator.Name = "达人🧀甲"
	group.creator = creator
	group.works[0].CreatorName = creator.Name
	if got, err := service.planName(request, group); err != nil || got != "达人甲" {
		t.Fatalf("plan-name emoji was not removed: %q", got)
	}
	creator.Name = "🧀✨"
	group.creator = creator
	group.works[0].CreatorName = creator.Name
	if _, err := service.planName(request, group); err == nil {
		t.Fatal("plan name empty after sanitation was accepted")
	}
	request.PlanNameTemplate = "{douyin_id}"
	creator.VisibleID = ""
	group.creator = creator
	if _, err := service.planName(request, group); err == nil {
		t.Fatal("empty rendered plan name was accepted")
	}
}

func testBatchVerificationLimits(t *testing.T) {
	reader := &batchVerificationReader{
		unauthorizedID: batchWorkID(55),
		mismatchID:     batchWorkID(54),
	}
	inputs := make([]WorkInput, 0, 57)
	for index := 1; index <= 55; index++ {
		inputs = append(inputs, WorkInput{
			InputIndex: index - 1, InputURL: fmt.Sprintf("https://www.douyin.com/video/%s", batchWorkID(index)),
			AwemeItemID: batchWorkID(index), OwnerHint: &OwnerHint{AwemeID: batchCreatorID, AwemeShowID: batchVisibleID},
		})
	}
	inputs = append(inputs,
		WorkInput{InputIndex: 55, InputURL: "duplicate", AwemeItemID: batchWorkID(1)},
		WorkInput{InputIndex: 56, InputURL: "invalid", AwemeItemID: "not-an-id"},
	)

	result, err := (WorkVerifier{Reader: reader}).Verify(context.Background(), WorkVerificationRequest{
		AdvertiserID: batchAdvertiserID, AccessToken: batchToken,
		ProductIDs: []string{batchProductID}, Works: inputs,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AuthorizedCreatorScanCount != 0 || result.AuthorizedCreatorPageCount != 0 ||
		reader.authorizedCalls != 1 || reader.broadCalls != 0 ||
		result.OwnershipQueryCount != 2 || result.ProductQueryCount != 2 {
		t.Fatalf("verification call budget changed: result=%#v reader=%#v", result, reader)
	}
	if !reflect.DeepEqual(reader.ownershipBatchSizes, []int{50, 5}) ||
		!reflect.DeepEqual(reader.productBatchSizes, []int{50, 4}) {
		t.Fatalf("work query batches changed: ownership=%v product=%v", reader.ownershipBatchSizes, reader.productBatchSizes)
	}
	if len(result.Matched) != 53 || len(result.Skipped) != 4 {
		t.Fatalf("verification classification changed: matched=%d skipped=%#v", len(result.Matched), result.Skipped)
	}
	reasons := map[string]int{}
	for _, skipped := range result.Skipped {
		reasons[skipped.Reason]++
	}
	wantReasons := map[string]int{
		"invalid_work_id": 1, "duplicate_input": 1,
		"creator_work_mismatch": 1, "product_mismatch": 1,
	}
	if !reflect.DeepEqual(reasons, wantReasons) {
		t.Fatalf("verification skip reasons changed: got=%v want=%v", reasons, wantReasons)
	}
}

func testBatchOwnerHintVerification(t *testing.T) {
	workID := batchWorkID(1)
	t.Run("verified hint avoids broad scan", func(t *testing.T) {
		reader := &hintVerificationReader{
			targetedCreator: domainqianchuan.AuthorizedCreator{
				AwemeID: batchCreatorID, VisibleID: "renamed-visible-id", Name: "fixture-creator",
			},
			actualCreatorID: batchCreatorID,
		}
		result, err := (WorkVerifier{Reader: reader}).Verify(context.Background(), WorkVerificationRequest{
			AdvertiserID: batchAdvertiserID, AccessToken: batchToken, ProductIDs: []string{batchProductID},
			Works: []WorkInput{{
				InputIndex: 0, AwemeItemID: workID,
				OwnerHint: &OwnerHint{AwemeID: batchCreatorID, AwemeShowID: batchVisibleID},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if reader.targetedCalls != 1 || reader.lastSearchKeyword != batchVisibleID ||
			reader.broadCalls != 0 || len(result.Matched) != 1 ||
			result.AuthorizedCreatorScanCount != 0 || result.OwnerHintSummary.Verified != 1 ||
			result.OwnerHintSummary.Stale != 0 || result.OwnerHintSummary.BroadScanWorkCount != 0 {
			t.Fatalf("verified hint crossed the broad-scan boundary: reader=%#v result=%#v", reader, result)
		}
		if !reflect.DeepEqual(reader.ownershipCreatorIDs, []string{batchCreatorID}) ||
			!reflect.DeepEqual(reader.productCreatorIDs, []string{batchCreatorID}) {
			t.Fatalf("verified hint used unexpected creator queries: ownership=%v product=%v",
				reader.ownershipCreatorIDs, reader.productCreatorIDs)
		}
	})

	t.Run("numeric only hint skips without wrong search parameter", func(t *testing.T) {
		reader := &hintVerificationReader{
			targetedCreator: domainqianchuan.AuthorizedCreator{
				AwemeID: batchCreatorID, VisibleID: batchVisibleID, Name: "fixture-creator",
			},
			actualCreatorID: batchCreatorID,
		}
		result, err := (WorkVerifier{Reader: reader}).Verify(context.Background(), WorkVerificationRequest{
			AdvertiserID: batchAdvertiserID, AccessToken: batchToken, ProductIDs: []string{batchProductID},
			Works: []WorkInput{{
				InputIndex: 0, AwemeItemID: workID,
				OwnerHint: &OwnerHint{AwemeID: batchCreatorID},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if reader.targetedCalls != 0 || reader.broadCalls != 0 || len(result.Matched) != 0 ||
			result.OwnerHintSummary.Eligible != 0 || result.OwnerHintSummary.AuthorizedHintQueryCount != 0 ||
			result.OwnerHintSummary.BroadScanWorkCount != 0 || len(result.Skipped) != 1 ||
			result.Skipped[0].Reason != "missing_creator_show_id" {
			t.Fatalf("numeric-only hint used an invalid authorization search: reader=%#v result=%#v", reader, result)
		}
	})

	t.Run("stale hint skips without scanning another creator", func(t *testing.T) {
		actualCreatorID := "4000000000000002"
		reader := &hintVerificationReader{
			targetedCreator: domainqianchuan.AuthorizedCreator{
				AwemeID: batchCreatorID, VisibleID: batchVisibleID, Name: "stale-creator",
			},
			actualCreatorID: actualCreatorID,
		}
		result, err := (WorkVerifier{Reader: reader}).Verify(context.Background(), WorkVerificationRequest{
			AdvertiserID: batchAdvertiserID, AccessToken: batchToken, ProductIDs: []string{batchProductID},
			Works: []WorkInput{{
				InputIndex: 0, AwemeItemID: workID,
				OwnerHint: &OwnerHint{AwemeID: batchCreatorID, AwemeShowID: batchVisibleID},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if reader.targetedCalls != 1 || reader.broadCalls != 0 || len(result.Matched) != 0 ||
			result.AuthorizedCreatorScanCount != 0 || result.OwnerHintSummary.Verified != 0 ||
			result.OwnerHintSummary.Stale != 1 || result.OwnerHintSummary.BroadScanWorkCount != 0 ||
			len(result.Skipped) != 1 || result.Skipped[0].Reason != "creator_work_mismatch" {
			t.Fatalf("stale hint escaped the targeted-only boundary: reader=%#v result=%#v", reader, result)
		}
		if !reflect.DeepEqual(reader.ownershipCreatorIDs, []string{batchCreatorID}) ||
			len(reader.productCreatorIDs) != 0 {
			t.Fatalf("stale hint escaped creator verification: ownership=%v product=%v",
				reader.ownershipCreatorIDs, reader.productCreatorIDs)
		}
	})

	t.Run("missing hint skips without official requests", func(t *testing.T) {
		reader := &hintVerificationReader{}
		result, err := (WorkVerifier{Reader: reader}).Verify(context.Background(), WorkVerificationRequest{
			AdvertiserID: batchAdvertiserID, AccessToken: batchToken, ProductIDs: []string{batchProductID},
			Works: []WorkInput{{InputIndex: 0, AwemeItemID: workID}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if reader.targetedCalls != 0 || reader.broadCalls != 0 || len(result.Matched) != 0 ||
			len(result.Skipped) != 1 || result.Skipped[0].Reason != "missing_creator_uid" {
			t.Fatalf("missing hint performed an official request: reader=%#v result=%#v", reader, result)
		}
	})
}

func testBatchAppendIdempotency(t *testing.T) {
	reader := &batchStateReader{materials: map[string]domainqianchuan.PlanMaterial{}}
	writer := &batchStateWriter{reader: reader, loseFirstResponse: true}
	finder := batchExistingPlanFinder{}
	credentials := &batchCredentialProvider{}
	locks := &batchLocker{}
	service := BatchService{
		Guard:  sharedplans.GuardedExecutor{Credentials: credentials, Locks: locks},
		Reader: reader, Writer: writer, Reconciler: finder,
	}
	request := BatchRequest{
		AdvertiserID: batchAdvertiserID, Submit: true,
		TemplateID: "fixture-template", TemplateName: "fixture-template-name", ProductName: "fixture-product",
		TemplatePayload: batchTemplatePayload(), Works: batchVerifiedWorks(101),
		Skipped:       []SkippedWork{{InputIndex: 102, Reason: "invalid_work_id", Message: "fixture skip"}},
		QueryFailures: []WorkQueryFailure{{AwemeID: "4999999999999999", Message: "fixture query failure"}},
	}

	first, err := service.Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.ExitCode != 0 || len(first.Results) != 1 || first.Results[0].Status != "appended" {
		t.Fatalf("first append result changed: %#v", first)
	}
	if !reflect.DeepEqual(writer.batchSizes, []int{100, 1}) || writer.addCalls != 2 {
		t.Fatalf("material add chunking changed: calls=%d batches=%v", writer.addCalls, writer.batchSizes)
	}
	if len(first.Results[0].Writes) != 2 || first.Results[0].Writes[0].Status != "reconciled" ||
		first.Results[0].Writes[0].Reconciliation != "applied" ||
		first.Results[0].Writes[0].DispatchState != domainplans.DispatchUnknown {
		t.Fatalf("unknown append reconciliation changed: %#v", first.Results[0].Writes)
	}
	assertBatchPresentation(t, first.Presentation, request.Works)
	if !reflect.DeepEqual(first.Presentation.DetailsOutsideTable, []string{"skipped", "query_failures", "failed_results"}) {
		t.Fatalf("batch details moved into the table: %#v", first.Presentation.DetailsOutsideTable)
	}

	writesAfterFirst := writer.addCalls
	second, err := service.Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if writer.addCalls != writesAfterFirst || len(second.Results) != 1 ||
		second.Results[0].Status != "already_present" || len(second.Results[0].AlreadyPresent) != 101 {
		t.Fatalf("identical rerun was not idempotent: result=%#v writes_before=%d writes_after=%d", second, writesAfterFirst, writer.addCalls)
	}
	assertBatchPresentation(t, second.Presentation, request.Works)
	if credentials.calls != 2 || locks.calls != 2 || locks.releases != 2 {
		t.Fatalf("submit guard counts changed: credentials=%d locks=%d releases=%d", credentials.calls, locks.calls, locks.releases)
	}
}

func testBatchPlanProductDisambiguation(t *testing.T) {
	const otherProductID = "5000000000000002"
	const otherPlanID = "2000000000000002"
	finder := &batchMultiplePlanFinder{}
	service := BatchService{
		Reader:     &batchStateReader{materials: map[string]domainqianchuan.PlanMaterial{}},
		Reconciler: finder,
	}
	request := BatchRequest{
		AdvertiserID: batchAdvertiserID, ReadAccessToken: batchToken,
		TemplateID: "fixture-template", TemplateName: "fixture-template-name", ProductName: "fixture-product",
		TemplatePayload: json.RawMessage(`{"advertiser_id":1000000000000001,"marketing_goal":"VIDEO_PROM_GOODS","product_ids":[5000000000000001,5000000000000002],"delivery_setting":{"smart_bid_type":"SMART_BID_CUSTOM","roi2_goal":1.75,"budget":5000,"video_schedule_type":"SCHEDULE_FROM_NOW"}}`),
		Works:           batchVerifiedWorks(1),
	}

	result, err := service.Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(finder.targets) != 1 || !reflect.DeepEqual(finder.targets[0].ProductIDs, []string{batchProductID}) {
		t.Fatalf("reconciliation used template products instead of verified work products: %#v", finder.targets)
	}
	if result.ExitCode != 0 || len(result.Results) != 1 || result.Results[0].Status != "would_append" ||
		result.Results[0].AdID != batchPlanID {
		t.Fatalf("product disambiguation selected the wrong action: %#v", result)
	}
	if result.Results[0].AdID == otherPlanID || !reflect.DeepEqual(result.Results[0].ProductIDs, []string{batchProductID}) {
		t.Fatalf("product disambiguation selected the mismatched plan: %#v", result.Results[0])
	}
}

func assertBatchPresentation(t *testing.T, presentation domain.Presentation, works []VerifiedWork) {
	t.Helper()
	wantColumns := []domain.PresentationColumn{
		{Field: "plan_id", Label: "计划ID"},
		{Field: "creator_nickname", Label: "达人昵称"},
		{Field: "product_id", Label: "商品ID"},
		{Field: "material_id", Label: "素材ID"},
		{Field: "material_title", Label: "素材标题"},
	}
	if !presentation.Required || presentation.AllowColumnOmission || presentation.AllowColumnReordering ||
		!reflect.DeepEqual(presentation.Columns, wantColumns) {
		t.Fatalf("mandatory batch presentation contract changed: %#v", presentation)
	}
	var expected strings.Builder
	expected.WriteString("| 计划ID | 达人昵称 | 商品ID | 素材ID | 素材标题 |\n")
	expected.WriteString("| --- | --- | --- | --- | --- |")
	for _, work := range works {
		expected.WriteString("\n| ")
		expected.WriteString(batchPlanID)
		expected.WriteString(" | fixture-creator | ")
		expected.WriteString(batchProductID)
		expected.WriteString(" | ")
		expected.WriteString(work.Material.MaterialID)
		expected.WriteString(" | ")
		expected.WriteString(work.Material.Title)
		expected.WriteString(" |")
	}
	if presentation.RenderedMarkdown != expected.String() {
		t.Fatalf("batch presentation bytes changed:\n--- got ---\n%s\n--- want ---\n%s", presentation.RenderedMarkdown, expected.String())
	}
	encoded, err := json.Marshal(presentation)
	if err != nil {
		t.Fatal(err)
	}
	var replayed domain.Presentation
	if err := json.Unmarshal(encoded, &replayed); err != nil {
		t.Fatal(err)
	}
	if replayed.RenderedMarkdown != presentation.RenderedMarkdown {
		t.Fatal("presentation replay changed rendered_markdown bytes")
	}
}

type batchVerificationReader struct {
	authorizedCalls     int
	broadCalls          int
	unauthorizedID      string
	mismatchID          string
	ownershipBatchSizes []int
	productBatchSizes   []int
}

type hintVerificationReader struct {
	targetedCreator     domainqianchuan.AuthorizedCreator
	actualCreatorID     string
	targetedCalls       int
	lastSearchKeyword   string
	broadCalls          int
	ownershipCreatorIDs []string
	productCreatorIDs   []string
}

func (*hintVerificationReader) FetchProducts(context.Context, portqianchuan.ProductPageRequest) (domainqianchuan.ProductPage, error) {
	return domainqianchuan.ProductPage{}, errors.New("unexpected product query")
}

func (*hintVerificationReader) FetchPlans(context.Context, portqianchuan.PlanPageRequest) (domainqianchuan.PlanPage, error) {
	return domainqianchuan.PlanPage{}, errors.New("unexpected plan query")
}

func (*hintVerificationReader) FetchPlanDetail(context.Context, portqianchuan.PlanDetailRequest) (domainqianchuan.PlanDetail, error) {
	return domainqianchuan.PlanDetail{}, errors.New("unexpected plan detail query")
}

func (*hintVerificationReader) FetchPlanMaterials(context.Context, portqianchuan.MaterialPageRequest) (domainqianchuan.MaterialPage, error) {
	return domainqianchuan.MaterialPage{}, errors.New("unexpected material query")
}

func (reader *hintVerificationReader) FetchAuthorizedCreators(
	_ context.Context,
	request portqianchuan.AuthorizedCreatorPageRequest,
) (domainqianchuan.AuthorizedCreatorPage, error) {
	rows := []domainqianchuan.AuthorizedCreator{}
	if request.SearchKeyword != "" {
		reader.targetedCalls++
		reader.lastSearchKeyword = request.SearchKeyword
		if request.SearchKeyword != batchVisibleID {
			return domainqianchuan.AuthorizedCreatorPage{}, errors.New("unexpected targeted creator query")
		}
		rows = append(rows, reader.targetedCreator)
	} else {
		reader.broadCalls++
		rows = append(rows, reader.targetedCreator)
		if reader.actualCreatorID != reader.targetedCreator.AwemeID {
			rows = append(rows, domainqianchuan.AuthorizedCreator{
				AwemeID: reader.actualCreatorID, VisibleID: "actual-visible", Name: "actual-creator",
			})
		}
	}
	return domainqianchuan.AuthorizedCreatorPage{
		Rows:     rows,
		PageInfo: domainqianchuan.PageInfo{Page: 1, TotalPages: 1, TotalNumber: len(rows)},
	}, nil
}

func (reader *hintVerificationReader) FetchCreatorVideos(
	_ context.Context,
	request portqianchuan.CreatorVideoPageRequest,
) (domainqianchuan.CreatorVideoPage, error) {
	if request.ProductID == "" {
		reader.ownershipCreatorIDs = append(reader.ownershipCreatorIDs, request.AwemeID)
	} else {
		reader.productCreatorIDs = append(reader.productCreatorIDs, request.AwemeID)
	}
	if request.AwemeID != reader.actualCreatorID {
		return domainqianchuan.CreatorVideoPage{Rows: []domainqianchuan.CreatorVideo{}}, nil
	}
	rows := make([]domainqianchuan.CreatorVideo, 0, len(request.AwemeItemIDs))
	for _, itemID := range request.AwemeItemIDs {
		rows = append(rows, domainqianchuan.CreatorVideo{
			AwemeItemID: itemID, ImageMode: "VIDEO_LARGE", MaterialID: "material-" + itemID,
			Title: "title-" + itemID,
		})
	}
	return domainqianchuan.CreatorVideoPage{Rows: rows}, nil
}

func (*batchVerificationReader) FetchProducts(context.Context, portqianchuan.ProductPageRequest) (domainqianchuan.ProductPage, error) {
	return domainqianchuan.ProductPage{}, errors.New("unexpected product query")
}

func (*batchVerificationReader) FetchPlans(context.Context, portqianchuan.PlanPageRequest) (domainqianchuan.PlanPage, error) {
	return domainqianchuan.PlanPage{}, errors.New("unexpected plan query")
}

func (*batchVerificationReader) FetchPlanDetail(context.Context, portqianchuan.PlanDetailRequest) (domainqianchuan.PlanDetail, error) {
	return domainqianchuan.PlanDetail{}, errors.New("unexpected plan detail query")
}

func (*batchVerificationReader) FetchPlanMaterials(context.Context, portqianchuan.MaterialPageRequest) (domainqianchuan.MaterialPage, error) {
	return domainqianchuan.MaterialPage{}, errors.New("unexpected material query")
}

func (reader *batchVerificationReader) FetchAuthorizedCreators(
	_ context.Context,
	request portqianchuan.AuthorizedCreatorPageRequest,
) (domainqianchuan.AuthorizedCreatorPage, error) {
	reader.authorizedCalls++
	if request.SearchKeyword == "" {
		reader.broadCalls++
		return domainqianchuan.AuthorizedCreatorPage{}, errors.New("unexpected broad creator query")
	}
	if request.SearchKeyword != batchVisibleID {
		return domainqianchuan.AuthorizedCreatorPage{}, errors.New("unexpected targeted creator")
	}
	return domainqianchuan.AuthorizedCreatorPage{
		Rows: []domainqianchuan.AuthorizedCreator{
			{AwemeID: batchCreatorID, VisibleID: batchVisibleID, Name: "fixture-creator"},
		},
		PageInfo: domainqianchuan.PageInfo{Page: 1, TotalPages: 1, TotalNumber: 1},
	}, nil
}

func (reader *batchVerificationReader) FetchCreatorVideos(
	_ context.Context,
	request portqianchuan.CreatorVideoPageRequest,
) (domainqianchuan.CreatorVideoPage, error) {
	if request.AwemeID != batchCreatorID || len(request.AwemeItemIDs) > WorkQueryBatchSize {
		return domainqianchuan.CreatorVideoPage{}, errors.New("work query escaped the usable creator or official batch limit")
	}
	if request.ProductID == "" {
		reader.ownershipBatchSizes = append(reader.ownershipBatchSizes, len(request.AwemeItemIDs))
	} else {
		reader.productBatchSizes = append(reader.productBatchSizes, len(request.AwemeItemIDs))
	}
	rows := make([]domainqianchuan.CreatorVideo, 0, len(request.AwemeItemIDs))
	for _, itemID := range request.AwemeItemIDs {
		if request.ProductID == "" && itemID == reader.unauthorizedID {
			continue
		}
		if request.ProductID != "" && itemID == reader.mismatchID {
			continue
		}
		rows = append(rows, domainqianchuan.CreatorVideo{
			AwemeItemID: itemID, ImageMode: "VIDEO_LARGE", MaterialID: "material-" + itemID,
			Title: "title-" + itemID,
		})
	}
	return domainqianchuan.CreatorVideoPage{Rows: rows}, nil
}

type batchStateReader struct {
	materials map[string]domainqianchuan.PlanMaterial
}

func (*batchStateReader) FetchProducts(context.Context, portqianchuan.ProductPageRequest) (domainqianchuan.ProductPage, error) {
	return domainqianchuan.ProductPage{}, errors.New("unexpected product query")
}

func (*batchStateReader) FetchPlans(context.Context, portqianchuan.PlanPageRequest) (domainqianchuan.PlanPage, error) {
	return domainqianchuan.PlanPage{}, errors.New("unexpected plan query")
}

func (*batchStateReader) FetchPlanDetail(context.Context, portqianchuan.PlanDetailRequest) (domainqianchuan.PlanDetail, error) {
	return domainqianchuan.PlanDetail{}, errors.New("unexpected plan detail query")
}

func (reader *batchStateReader) FetchPlanMaterials(
	context.Context,
	portqianchuan.MaterialPageRequest,
) (domainqianchuan.MaterialPage, error) {
	rows := make([]domainqianchuan.PlanMaterial, 0, len(reader.materials))
	for index := 1; index <= 101; index++ {
		if row, ok := reader.materials[batchWorkID(index)]; ok {
			rows = append(rows, row)
		}
	}
	return domainqianchuan.MaterialPage{
		Rows:     rows,
		PageInfo: domainqianchuan.PageInfo{Page: 1, TotalPages: 1, TotalNumber: len(rows)},
	}, nil
}

func (*batchStateReader) FetchAuthorizedCreators(context.Context, portqianchuan.AuthorizedCreatorPageRequest) (domainqianchuan.AuthorizedCreatorPage, error) {
	return domainqianchuan.AuthorizedCreatorPage{}, errors.New("unexpected creator query")
}

func (*batchStateReader) FetchCreatorVideos(context.Context, portqianchuan.CreatorVideoPageRequest) (domainqianchuan.CreatorVideoPage, error) {
	return domainqianchuan.CreatorVideoPage{}, errors.New("unexpected video query")
}

type batchStateWriter struct {
	reader            *batchStateReader
	loseFirstResponse bool
	addCalls          int
	batchSizes        []int
}

func (*batchStateWriter) CreatePlan(context.Context, portqianchuan.CreatePlanRequest) (portqianchuan.WriteResult, error) {
	return portqianchuan.WriteResult{}, errors.New("unexpected plan create")
}

func (writer *batchStateWriter) AddMaterials(
	_ context.Context,
	request portqianchuan.MaterialWriteRequest,
) (portqianchuan.WriteResult, error) {
	var payload struct {
		Creatives []struct {
			Videos []struct {
				AwemeItemID json.Number `json:"aweme_item_id"`
			} `json:"video_material"`
		} `json:"multi_product_creative_list"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(request.Payload)))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return portqianchuan.WriteResult{}, err
	}
	ids := []string{}
	for _, creative := range payload.Creatives {
		for _, video := range creative.Videos {
			ids = append(ids, string(video.AwemeItemID))
		}
	}
	writer.addCalls++
	writer.batchSizes = append(writer.batchSizes, len(ids))
	for _, itemID := range ids {
		writer.reader.materials[itemID] = domainqianchuan.PlanMaterial{
			MaterialID: "material-" + itemID, AwemeItemID: itemID,
			MaterialType: "VIDEO", MaterialSelectType: "CUSTOM", MaterialStatus: "DELIVERY_OK",
		}
	}
	if writer.loseFirstResponse {
		writer.loseFirstResponse = false
		return portqianchuan.WriteResult{}, &domainplans.DispatchFailure{
			State: domainplans.DispatchUnknown, Cause: errors.New("synthetic response loss after apply"),
		}
	}
	return portqianchuan.WriteResult{RequestID: fmt.Sprintf("add-%d", writer.addCalls)}, nil
}

func (*batchStateWriter) DeleteMaterials(context.Context, portqianchuan.DeleteMaterialsRequest) (portqianchuan.WriteResult, error) {
	return portqianchuan.WriteResult{}, errors.New("unexpected material delete")
}

func (*batchStateWriter) UpdatePlan(context.Context, portqianchuan.MutationRequest) (portqianchuan.WriteResult, error) {
	return portqianchuan.WriteResult{}, errors.New("unexpected plan mutation")
}

type batchExistingPlanFinder struct{}

func (batchExistingPlanFinder) FindCurrentPlans(
	_ context.Context,
	request CurrentPlanRequest,
) (CurrentPlanResult, error) {
	return CurrentPlanResult{Matches: map[string][]ExistingPlan{
		batchCreatorID: {{
			AdID: batchPlanID, Name: "fixture-plan", Status: "DISABLE",
			AwemeID: batchCreatorID, ProductIDs: []string{batchProductID},
		}},
	}}, nil
}

type batchMultiplePlanFinder struct {
	targets []CreatorTarget
}

func (finder *batchMultiplePlanFinder) FindCurrentPlans(
	_ context.Context,
	request CurrentPlanRequest,
) (CurrentPlanResult, error) {
	finder.targets = append([]CreatorTarget(nil), request.Targets...)
	return CurrentPlanResult{Matches: map[string][]ExistingPlan{
		batchCreatorID: {
			{
				AdID: batchPlanID, Name: "matching-plan", Status: "DISABLE",
				AwemeID: batchCreatorID, ProductIDs: []string{batchProductID},
			},
			{
				AdID: "2000000000000002", Name: "other-product-plan", Status: "DISABLE",
				AwemeID: batchCreatorID, ProductIDs: []string{"5000000000000002"},
			},
		},
	}}, nil
}

type batchCredentialProvider struct {
	calls int
}

func (provider *batchCredentialProvider) AccessToken(
	context.Context,
	domainplans.Channel,
	string,
	string,
) (sharedplans.CredentialLease, error) {
	provider.calls++
	return sharedplans.CredentialLease{AuthorizationID: "fixture-authorization", AccessToken: batchToken}, nil
}

type batchLocker struct {
	calls    int
	releases int
}

func (locker *batchLocker) Acquire(context.Context, domainplans.WriteScope) (func() error, error) {
	locker.calls++
	return func() error {
		locker.releases++
		return nil
	}, nil
}

func batchTemplatePayload() json.RawMessage {
	return json.RawMessage(`{"advertiser_id":1000000000000001,"marketing_goal":"VIDEO_PROM_GOODS","product_ids":[5000000000000001],"delivery_setting":{"smart_bid_type":"SMART_BID_CUSTOM","roi2_goal":1.75,"budget":5000,"video_schedule_type":"SCHEDULE_FROM_NOW"}}`)
}

func batchVerifiedWorks(count int) []VerifiedWork {
	result := make([]VerifiedWork, 0, count)
	for index := 1; index <= count; index++ {
		itemID := batchWorkID(index)
		result = append(result, VerifiedWork{
			InputIndex: index - 1, InputURL: "https://www.douyin.com/video/" + itemID,
			AwemeItemID: itemID, CreatorName: "fixture-creator",
			Creator: domainqianchuan.AuthorizedCreator{
				AwemeID: batchCreatorID, VisibleID: batchVisibleID, Name: "fixture-creator",
			},
			Material: domainqianchuan.CreatorVideo{
				AwemeItemID: itemID, ImageMode: "VIDEO_LARGE", MaterialID: "material-" + itemID,
				Title: "title-" + itemID,
			},
			MatchedProductIDs: []string{batchProductID},
		})
	}
	return result
}

func batchWorkID(index int) string {
	return fmt.Sprintf("600000000000%04d", index)
}
