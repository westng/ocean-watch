package qianchuan

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	sharedplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
	domainqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
	portqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/qianchuan"
)

func TestMutationReconciliation(t *testing.T) {
	t.Run("removal maps nested material IDs and skips unsupported rows", testRemoveMaterialClassification)
	t.Run("removal chunks one hundred plus one and verifies deleted", testRemoveMaterialSubmission)
	t.Run("settings read back every row and isolate partial failure", testQianchuanMutationReadback)
	t.Run("settings serialize the same advertiser", testQianchuanMutationSerialization)
}

func testRemoveMaterialClassification(t *testing.T) {
	reader := newQianchuanMutationReader()
	reader.materials = []domainqianchuan.PlanMaterial{
		fixturePlanMaterial("6001", "7001", "CUSTOM", "DELIVERY_OK"),
		fixturePlanMaterial("6002", "7002", "SMART", "DELIVERY_OK"),
		fixturePlanMaterial("6003", "7003", "CUSTOM", "DELIVERY_OK"),
		fixturePlanMaterial("6003", "7004", "CUSTOM", "DELIVERY_OK"),
		fixturePlanMaterial("6004", "7005", "CUSTOM", "DELETED"),
	}
	result, err := (RemoveExecutor{Reader: reader}).Execute(context.Background(), RemoveCommand{
		AdvertiserID: batchAdvertiserID, ReadAccessToken: batchToken, AdID: batchPlanID,
		Works: []RemoveWork{
			{InputIndex: 0, AwemeItemID: "6001"},
			{InputIndex: 1, AwemeItemID: "6002"},
			{InputIndex: 2, AwemeItemID: "6003"},
			{InputIndex: 3, AwemeItemID: "6004"},
			{InputIndex: 4, AwemeItemID: "6005"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != "dry_run" || result.Endpoint != DeleteMaterialsEndpoint || result.RiskNotice != DeleteRiskNotice {
		t.Fatalf("removal preview contract changed: %#v", result)
	}
	wantStatuses := []string{"would_delete", "skipped", "failed", "already_deleted", "skipped"}
	wantReasons := []string{"", "unsupported_material_select_type", "ambiguous_material_match", "", "work_not_in_plan"}
	for index := range wantStatuses {
		if result.Results[index].Status != wantStatuses[index] || result.Results[index].Reason != wantReasons[index] {
			t.Fatalf("removal classification row %d changed: %#v", index, result.Results[index])
		}
	}
	if result.Results[0].MaterialID != "7001" || result.Results[0].AwemeItemID == result.Results[0].MaterialID ||
		!reflect.DeepEqual(result.Results[2].CandidateMaterialIDs, []string{"7003", "7004"}) {
		t.Fatalf("nested material identity mapping changed: %#v", result.Results)
	}
	if result.ExitCode != 1 {
		t.Fatalf("ambiguous removal did not fail closed: %#v", result)
	}

	credentials := &mutationCredentialProvider{}
	_, err = (RemoveExecutor{Guard: sharedplans.GuardedExecutor{
		Credentials: credentials, Locks: &mutationSerialLocker{},
	}}).Execute(context.Background(), RemoveCommand{
		AdvertiserID: batchAdvertiserID, AdID: batchPlanID, Submit: true,
		Works: []RemoveWork{{AwemeItemID: "6001"}},
	})
	if err == nil || credentials.callCount() != 0 {
		t.Fatalf("unconfirmed delete crossed credential boundary: err=%v calls=%d", err, credentials.callCount())
	}
}

func testRemoveMaterialSubmission(t *testing.T) {
	reader := newQianchuanMutationReader()
	works := make([]RemoveWork, 0, 101)
	for index := 1; index <= 101; index++ {
		itemID := fmt.Sprintf("600000000000%04d", index)
		materialID := fmt.Sprintf("700000000000%04d", index)
		works = append(works, RemoveWork{InputIndex: index - 1, AwemeItemID: itemID})
		reader.materials = append(reader.materials, fixturePlanMaterial(itemID, materialID, "CUSTOM", "DELIVERY_OK"))
	}
	writer := &removeMutationWriter{reader: reader, loseFirstResponse: true}
	credentials := &mutationCredentialProvider{}
	locks := &mutationSerialLocker{}
	executor := RemoveExecutor{
		Guard:  sharedplans.GuardedExecutor{Credentials: credentials, Locks: locks},
		Reader: reader, Writer: writer,
	}
	command := RemoveCommand{
		AdvertiserID: batchAdvertiserID, AdID: batchPlanID,
		Submit: true, ConfirmDelete: true, Works: works,
	}
	first, err := executor.Execute(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if first.ExitCode != 0 || len(first.Results) != 101 || len(first.Batches) != 2 ||
		!reflect.DeepEqual(writer.batchSizes(), []int{100, 1}) {
		t.Fatalf("delete batching changed: result=%#v batches=%v", first, writer.batchSizes())
	}
	if first.Batches[0].DispatchState != domainplans.DispatchUnknown ||
		first.Results[0].Status != "reconciled" || first.Results[100].Status != "deleted" {
		t.Fatalf("delete unknown reconciliation changed: first_batch=%#v rows=%#v", first.Batches[0], first.Results)
	}
	for index, batch := range writer.materialIDs() {
		for _, materialID := range batch {
			if materialID == works[min(index*100, 100)].AwemeItemID {
				t.Fatal("delete writer received aweme_item_id instead of nested material_id")
			}
			if len(materialID) < 4 || materialID[:4] != "7000" {
				t.Fatalf("delete writer received unexpected identifier %q", materialID)
			}
		}
	}
	writeCalls := writer.callCount()
	second, err := executor.Execute(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if writer.callCount() != writeCalls || second.Counts["already_deleted"] != 101 || second.ExitCode != 0 {
		t.Fatalf("delete rerun was not idempotent: result=%#v before=%d after=%d", second, writeCalls, writer.callCount())
	}
}

func testQianchuanMutationReadback(t *testing.T) {
	credentials := &mutationCredentialProvider{}
	invalid := MutationCommand{
		AdvertiserID: batchAdvertiserID, Submit: true,
		Kind: portqianchuan.MutationBudget, AdIDs: []string{"9223372036854775808"}, Value: "500",
	}
	_, err := (MutationExecutor{Guard: sharedplans.GuardedExecutor{
		Credentials: credentials, Locks: &mutationSerialLocker{},
	}}).Execute(context.Background(), invalid)
	if err == nil || credentials.callCount() != 0 {
		t.Fatalf("out-of-range Qianchuan ID crossed credentials: err=%v calls=%d", err, credentials.callCount())
	}
	deleteCommand := MutationCommand{
		AdvertiserID: batchAdvertiserID, Submit: true,
		Kind: portqianchuan.MutationStatus, AdIDs: []string{"2001"}, Status: "DELETE",
	}
	_, err = (MutationExecutor{Guard: sharedplans.GuardedExecutor{
		Credentials: credentials, Locks: &mutationSerialLocker{},
	}}).Execute(context.Background(), deleteCommand)
	if err == nil || credentials.callCount() != 0 {
		t.Fatalf("unconfirmed plan DELETE crossed credentials: err=%v calls=%d", err, credentials.callCount())
	}

	reader := newQianchuanMutationReader()
	reader.details["2001"] = fixturePlanDetail("2001", "ENABLE", "400", "1.5")
	reader.details["2002"] = fixturePlanDetail("2002", "ENABLE", "400", "1.5")
	writer := &planMutationWriter{reader: reader}
	executor := MutationExecutor{
		Guard: sharedplans.GuardedExecutor{
			Credentials: credentials, Locks: &mutationSerialLocker{},
		},
		Reader: reader, Writer: writer,
	}
	status, err := executor.Execute(context.Background(), MutationCommand{
		AdvertiserID: batchAdvertiserID, Submit: true,
		Kind: portqianchuan.MutationStatus, AdIDs: []string{"2001"}, Status: "DISABLE",
	})
	if err != nil {
		t.Fatal(err)
	}
	if status.ExitCode != 0 || status.Rows[0].Status != "completed" || status.Rows[0].Observed != "DISABLE" {
		t.Fatalf("status readback changed: %#v", status)
	}

	writer.setRowErrors(map[string]portqianchuan.RowError{
		"2002": {ObjectID: "2002", Code: "40000", Message: "synthetic row rejection"},
	})
	budget, err := executor.Execute(context.Background(), MutationCommand{
		AdvertiserID: batchAdvertiserID, Submit: true,
		Kind: portqianchuan.MutationBudget, AdIDs: []string{"2001", "2002"}, Value: "500.00",
	})
	if err != nil {
		t.Fatal(err)
	}
	if budget.ExitCode != 1 || budget.SuccessCount != 1 || budget.FailureCount != 1 ||
		budget.Rows[0].Status != "completed" || budget.Rows[0].Observed != "500" ||
		budget.Rows[1].Status != "failed" || budget.Rows[1].OfficialError != "40000: synthetic row rejection" {
		t.Fatalf("partial budget mutation changed: %#v", budget)
	}

	writer.setRowErrors(nil)
	writer.setUnknown(true)
	roi, err := executor.Execute(context.Background(), MutationCommand{
		AdvertiserID: batchAdvertiserID, Submit: true,
		Kind: portqianchuan.MutationROI, AdIDs: []string{"2001", "2002"}, Value: "1.75",
		DeepExternalAction: "AD_CONVERT_TYPE_LIVE_PAY_ROI",
	})
	if err != nil {
		t.Fatal(err)
	}
	if roi.ExitCode != 0 || roi.DispatchState != domainplans.DispatchUnknown ||
		roi.Rows[0].Status != "reconciled" || roi.Rows[1].Status != "reconciled" ||
		roi.Rows[0].Observed != "1.75" || roi.Rows[1].Observed != "1.75" {
		t.Fatalf("unknown ROI mutation was not reconciled: %#v", roi)
	}
	if writer.callCount() != 3 || reader.detailCallCount() != 5 {
		t.Fatalf("mutation writes or per-row readbacks changed: writes=%d reads=%d", writer.callCount(), reader.detailCallCount())
	}
}

func testQianchuanMutationSerialization(t *testing.T) {
	reader := newQianchuanMutationReader()
	reader.details["2001"] = fixturePlanDetail("2001", "ENABLE", "400", "1.5")
	writer := &planMutationWriter{reader: reader, delay: 25 * time.Millisecond}
	locker := &mutationSerialLocker{}
	executor := MutationExecutor{
		Guard: sharedplans.GuardedExecutor{
			Credentials: &mutationCredentialProvider{}, Locks: locker,
		},
		Reader: reader, Writer: writer,
	}
	start := make(chan struct{})
	results := make(chan MutationResult, 2)
	errorsChannel := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			result, err := executor.Execute(context.Background(), MutationCommand{
				AdvertiserID: batchAdvertiserID, Submit: true,
				Kind: portqianchuan.MutationBudget, AdIDs: []string{"2001"}, Value: "500",
			})
			results <- result
			errorsChannel <- err
		}()
	}
	close(start)
	for range 2 {
		if err := <-errorsChannel; err != nil {
			t.Fatal(err)
		}
		if result := <-results; result.ExitCode != 0 {
			t.Fatalf("serialized mutation failed: %#v", result)
		}
	}
	if writer.maxActiveCount() != 1 || locker.maxActiveCount() != 1 || locker.acquireCount() != 2 {
		t.Fatalf("same-advertiser Qianchuan writes overlapped: writer=%d locker=%d acquires=%d", writer.maxActiveCount(), locker.maxActiveCount(), locker.acquireCount())
	}
}

type qianchuanMutationReader struct {
	mu          sync.Mutex
	materials   []domainqianchuan.PlanMaterial
	details     map[string]domainqianchuan.PlanDetail
	detailCalls int
}

func newQianchuanMutationReader() *qianchuanMutationReader {
	return &qianchuanMutationReader{details: map[string]domainqianchuan.PlanDetail{}}
}

func (*qianchuanMutationReader) FetchProducts(context.Context, portqianchuan.ProductPageRequest) (domainqianchuan.ProductPage, error) {
	return domainqianchuan.ProductPage{}, errors.New("unexpected product query")
}

func (*qianchuanMutationReader) FetchPlans(context.Context, portqianchuan.PlanPageRequest) (domainqianchuan.PlanPage, error) {
	return domainqianchuan.PlanPage{}, errors.New("unexpected plan query")
}

func (reader *qianchuanMutationReader) FetchPlanDetail(
	_ context.Context,
	request portqianchuan.PlanDetailRequest,
) (domainqianchuan.PlanDetail, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	reader.detailCalls++
	detail, ok := reader.details[request.AdID]
	if !ok {
		return domainqianchuan.PlanDetail{}, errors.New("fixture plan detail not found")
	}
	return detail, nil
}

func (reader *qianchuanMutationReader) FetchPlanMaterials(
	_ context.Context,
	request portqianchuan.MaterialPageRequest,
) (domainqianchuan.MaterialPage, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	total := len(reader.materials)
	if total == 0 {
		return domainqianchuan.MaterialPage{
			Rows:     []domainqianchuan.PlanMaterial{},
			PageInfo: domainqianchuan.PageInfo{Page: 1, TotalPages: 0, TotalNumber: 0},
		}, nil
	}
	totalPages := (total + 99) / 100
	start := (request.Page - 1) * 100
	end := min(start+100, total)
	return domainqianchuan.MaterialPage{
		Rows: append([]domainqianchuan.PlanMaterial(nil), reader.materials[start:end]...),
		PageInfo: domainqianchuan.PageInfo{
			Page: request.Page, TotalPages: totalPages, TotalNumber: total,
		},
	}, nil
}

func (*qianchuanMutationReader) FetchAuthorizedCreators(context.Context, portqianchuan.AuthorizedCreatorPageRequest) (domainqianchuan.AuthorizedCreatorPage, error) {
	return domainqianchuan.AuthorizedCreatorPage{}, errors.New("unexpected creator query")
}

func (*qianchuanMutationReader) FetchCreatorVideos(context.Context, portqianchuan.CreatorVideoPageRequest) (domainqianchuan.CreatorVideoPage, error) {
	return domainqianchuan.CreatorVideoPage{}, errors.New("unexpected creator video query")
}

func (reader *qianchuanMutationReader) detailCallCount() int {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	return reader.detailCalls
}

type removeMutationWriter struct {
	mu                sync.Mutex
	reader            *qianchuanMutationReader
	loseFirstResponse bool
	batches           [][]string
}

func (*removeMutationWriter) CreatePlan(context.Context, portqianchuan.CreatePlanRequest) (portqianchuan.WriteResult, error) {
	return portqianchuan.WriteResult{}, errors.New("unexpected plan create")
}

func (*removeMutationWriter) AddMaterials(context.Context, portqianchuan.MaterialWriteRequest) (portqianchuan.WriteResult, error) {
	return portqianchuan.WriteResult{}, errors.New("unexpected material add")
}

func (writer *removeMutationWriter) DeleteMaterials(
	_ context.Context,
	request portqianchuan.DeleteMaterialsRequest,
) (portqianchuan.WriteResult, error) {
	writer.mu.Lock()
	writer.batches = append(writer.batches, append([]string(nil), request.MaterialIDs...))
	loseResponse := writer.loseFirstResponse
	writer.loseFirstResponse = false
	call := len(writer.batches)
	writer.mu.Unlock()
	set := map[string]struct{}{}
	for _, materialID := range request.MaterialIDs {
		set[materialID] = struct{}{}
	}
	writer.reader.mu.Lock()
	for index := range writer.reader.materials {
		if _, exists := set[writer.reader.materials[index].MaterialID]; exists {
			writer.reader.materials[index].MaterialStatus = "DELETED"
		}
	}
	writer.reader.mu.Unlock()
	result := portqianchuan.WriteResult{RequestID: fmt.Sprintf("delete-%d", call)}
	if loseResponse {
		return result, &domainplans.DispatchFailure{
			State: domainplans.DispatchUnknown, Cause: errors.New("synthetic delete response loss after apply"),
		}
	}
	return result, nil
}

func (*removeMutationWriter) UpdatePlan(context.Context, portqianchuan.MutationRequest) (portqianchuan.WriteResult, error) {
	return portqianchuan.WriteResult{}, errors.New("unexpected plan mutation")
}

func (writer *removeMutationWriter) batchSizes() []int {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	result := make([]int, len(writer.batches))
	for index, batch := range writer.batches {
		result[index] = len(batch)
	}
	return result
}

func (writer *removeMutationWriter) materialIDs() [][]string {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	result := make([][]string, len(writer.batches))
	for index, batch := range writer.batches {
		result[index] = append([]string(nil), batch...)
	}
	return result
}

func (writer *removeMutationWriter) callCount() int {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return len(writer.batches)
}

type planMutationWriter struct {
	mu        sync.Mutex
	reader    *qianchuanMutationReader
	rowErrors map[string]portqianchuan.RowError
	unknown   bool
	delay     time.Duration
	calls     int
	active    int
	maxActive int
}

func (*planMutationWriter) CreatePlan(context.Context, portqianchuan.CreatePlanRequest) (portqianchuan.WriteResult, error) {
	return portqianchuan.WriteResult{}, errors.New("unexpected plan create")
}

func (*planMutationWriter) AddMaterials(context.Context, portqianchuan.MaterialWriteRequest) (portqianchuan.WriteResult, error) {
	return portqianchuan.WriteResult{}, errors.New("unexpected material add")
}

func (*planMutationWriter) DeleteMaterials(context.Context, portqianchuan.DeleteMaterialsRequest) (portqianchuan.WriteResult, error) {
	return portqianchuan.WriteResult{}, errors.New("unexpected material delete")
}

func (writer *planMutationWriter) UpdatePlan(
	_ context.Context,
	request portqianchuan.MutationRequest,
) (portqianchuan.WriteResult, error) {
	writer.mu.Lock()
	writer.calls++
	call := writer.calls
	writer.active++
	if writer.active > writer.maxActive {
		writer.maxActive = writer.active
	}
	rowErrors := make(map[string]portqianchuan.RowError, len(writer.rowErrors))
	for key, value := range writer.rowErrors {
		rowErrors[key] = value
	}
	unknown, delay := writer.unknown, writer.delay
	writer.mu.Unlock()
	if delay != 0 {
		time.Sleep(delay)
	}
	writer.reader.mu.Lock()
	for _, adID := range request.AdIDs {
		if _, failed := rowErrors[adID]; failed {
			continue
		}
		detail := writer.reader.details[adID]
		switch request.Kind {
		case portqianchuan.MutationStatus:
			detail.OptStatus = request.Status
		case portqianchuan.MutationBudget:
			value := domain.MustDecimal(request.Value)
			detail.Budget = &value
		case portqianchuan.MutationROI:
			value := domain.MustDecimal(request.Value)
			detail.ROI2Goal = &value
		}
		writer.reader.details[adID] = detail
	}
	writer.reader.mu.Unlock()
	writer.mu.Lock()
	writer.active--
	writer.mu.Unlock()
	errorsList := make([]portqianchuan.RowError, 0, len(rowErrors))
	for _, adID := range request.AdIDs {
		if rowError, exists := rowErrors[adID]; exists {
			errorsList = append(errorsList, rowError)
		}
	}
	result := portqianchuan.WriteResult{RequestID: fmt.Sprintf("mutation-%d", call), RowErrors: errorsList}
	if unknown {
		return result, &domainplans.DispatchFailure{
			State: domainplans.DispatchUnknown, Cause: errors.New("synthetic mutation response loss after apply"),
		}
	}
	if len(errorsList) != 0 {
		return result, &domainplans.DispatchFailure{
			State: domainplans.DispatchAcknowledged, Cause: errors.New("synthetic partial mutation failure"),
		}
	}
	return result, nil
}

func (writer *planMutationWriter) setRowErrors(values map[string]portqianchuan.RowError) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.rowErrors = values
}

func (writer *planMutationWriter) setUnknown(value bool) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.unknown = value
}

func (writer *planMutationWriter) callCount() int {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.calls
}

func (writer *planMutationWriter) maxActiveCount() int {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.maxActive
}

type mutationCredentialProvider struct {
	mu    sync.Mutex
	calls int
}

func (provider *mutationCredentialProvider) AccessToken(
	context.Context,
	domainplans.Channel,
	string,
	string,
) (sharedplans.CredentialLease, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.calls++
	return sharedplans.CredentialLease{AuthorizationID: "fixture-authorization", AccessToken: batchToken}, nil
}

func (provider *mutationCredentialProvider) callCount() int {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.calls
}

type mutationSerialLocker struct {
	mu        sync.Mutex
	statsMu   sync.Mutex
	active    int
	maxActive int
	acquires  int
}

func (locker *mutationSerialLocker) Acquire(
	ctx context.Context,
	scope domainplans.WriteScope,
) (func() error, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if scope.Channel != domainplans.ChannelQianchuan ||
		(scope.LockFamily != domainplans.LockPlanSettings && scope.LockFamily != domainplans.LockQianchuanWorks) {
		return nil, errors.New("unexpected Qianchuan mutation lock scope")
	}
	locker.mu.Lock()
	locker.statsMu.Lock()
	locker.acquires++
	locker.active++
	if locker.active > locker.maxActive {
		locker.maxActive = locker.active
	}
	locker.statsMu.Unlock()
	return func() error {
		locker.statsMu.Lock()
		locker.active--
		locker.statsMu.Unlock()
		locker.mu.Unlock()
		return nil
	}, nil
}

func (locker *mutationSerialLocker) acquireCount() int {
	locker.statsMu.Lock()
	defer locker.statsMu.Unlock()
	return locker.acquires
}

func (locker *mutationSerialLocker) maxActiveCount() int {
	locker.statsMu.Lock()
	defer locker.statsMu.Unlock()
	return locker.maxActive
}

func fixturePlanMaterial(itemID, materialID, selectType, status string) domainqianchuan.PlanMaterial {
	return domainqianchuan.PlanMaterial{
		AwemeItemID: itemID, MaterialID: materialID, MaterialType: "VIDEO",
		MaterialSelectType: selectType, MaterialStatus: status,
	}
}

func fixturePlanDetail(adID, status, budget, roi string) domainqianchuan.PlanDetail {
	budgetValue, roiValue := domain.MustDecimal(budget), domain.MustDecimal(roi)
	return domainqianchuan.PlanDetail{
		AdID: adID, OptStatus: status, Budget: &budgetValue, ROI2Goal: &roiValue,
	}
}
