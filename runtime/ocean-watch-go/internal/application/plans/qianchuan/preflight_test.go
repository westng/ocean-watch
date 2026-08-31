package qianchuan

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	sharedplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans"
	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
	domainqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
	portqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/qianchuan"
)

func TestGetBatchPreflightReturnsStableSanitizedSummary(t *testing.T) {
	createdAt := time.Date(2026, 8, 16, 4, 0, 0, 0, time.UTC)
	works := batchVerifiedWorks(2)
	works[1].Creator.AwemeID = "4000000000000002"
	works[1].MatchedProductIDs = []string{"5000000000000002", batchProductID}
	firstGroupID := batchTestGroupID(t, "fixture-template", works[0])
	secondGroupID := batchTestGroupID(t, "fixture-template", works[1])
	secondBindingDigest := testBindingDigest(t, "fixture-template", works[1], secondGroupID, "2026-08-16", "2000000000000002")
	snapshot, eligible, err := prepareBatchSnapshot(BatchRequest{
		AdvertiserID: batchAdvertiserID, AuthAccountID: "private-auth-selector",
		TemplateID: "fixture-template", TemplateName: "fixture-template-name",
		ProductName: "fixture-product", ProductShortName: "fixture-short",
		TemplatePayload: json.RawMessage(`{"private":"payload"}`), Works: works,
		Skipped: []SkippedWork{{InputURL: "https://v.douyin.com/private/", Reason: "invalid"}},
	}, BatchResult{BusinessDate: "2026-08-16", Results: []BatchGroupResult{
		{GroupID: secondGroupID, AwemeID: "4000000000000002", AdID: "2000000000000002", ProductIDs: works[1].MatchedProductIDs, InputItemIDs: []string{works[1].AwemeItemID}, Status: "would_append", BindingDigest: secondBindingDigest},
		{GroupID: firstGroupID, AwemeID: batchCreatorID, ProductIDs: works[0].MatchedProductIDs, InputItemIDs: []string{works[0].AwemeItemID}, Status: "would_create"},
	}}, createdAt)
	if err != nil || !eligible {
		t.Fatalf("prepare snapshot: eligible=%t err=%v", eligible, err)
	}
	journal, err := batchPreflightJournal(snapshot, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	const preflightID = "qianchuan-preflight-20260816t040000-abcdef123456"
	tokens := &commandTokenProvider{}
	store := &commandJournalStore{journals: map[string]domainplans.Journal{preflightID: journal}}
	service := CommandService{
		Journals: store, Tokens: tokens,
		Now: func() time.Time { return createdAt.Add(time.Minute) },
	}
	summary, err := service.GetBatchPreflight(context.Background(), preflightID)
	if err != nil {
		t.Fatal(err)
	}
	if tokens.calls != 0 || summary.SchemaVersion != 2 || summary.PreflightID != preflightID || !summary.ReadyForSubmit ||
		summary.BusinessDate != "2026-08-16" ||
		summary.EligibleWorks != 2 || summary.SkippedWorks != 1 ||
		!reflect.DeepEqual(summary.ProductIDs, []string{batchProductID, "5000000000000002"}) ||
		!reflect.DeepEqual(summary.Decisions, []BatchPreflightDecision{
			{GroupID: firstGroupID, CreatorID: "4000000000000001", Action: "would_create"},
			{GroupID: secondGroupID, CreatorID: "4000000000000002", Action: "would_append", ExistingPlanID: "2000000000000002"},
		}) {
		t.Fatalf("unexpected preflight summary: %#v token_calls=%d", summary, tokens.calls)
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"private-auth-selector", "private", "v.douyin.com", "template_payload"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("preflight summary leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestGetBatchPreflightFailsClosed(t *testing.T) {
	createdAt := time.Date(2026, 8, 16, 4, 0, 0, 0, time.UTC)
	works := batchVerifiedWorks(1)
	groupID := batchTestGroupID(t, "fixture", works[0])
	snapshot, _, err := prepareBatchSnapshot(BatchRequest{
		AdvertiserID: batchAdvertiserID, TemplateID: "fixture", TemplateName: "fixture",
		TemplatePayload: batchTemplatePayload(), Works: works,
	}, BatchResult{Results: []BatchGroupResult{{
		GroupID: groupID, AwemeID: batchCreatorID, ProductIDs: works[0].MatchedProductIDs,
		InputItemIDs: []string{works[0].AwemeItemID}, Status: "would_create",
	}}}, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := batchPreflightJournal(snapshot, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	const preflightID = "qianchuan-preflight-20260816t040000-abcdef123456"
	service := CommandService{
		Journals: &commandJournalStore{journals: map[string]domainplans.Journal{preflightID: journal}},
		Now:      func() time.Time { return createdAt.Add(31 * time.Minute) },
	}
	if _, err := service.GetBatchPreflight(context.Background(), preflightID); !errors.Is(err, ErrBatchPreflightExpired) {
		t.Fatalf("expired preflight error=%v", err)
	}
	service.Journals = missingPreflightStore{}
	if _, err := service.GetBatchPreflight(context.Background(), preflightID); !errors.Is(err, ErrBatchPreflightNotFound) {
		t.Fatalf("missing preflight error=%v", err)
	}
	if _, err := service.GetBatchPreflight(context.Background(), "../private"); !errors.Is(err, ErrBatchPreflightInvalid) {
		t.Fatalf("invalid preflight error=%v", err)
	}
	other, err := domainplans.NewJournal("fixture-fingerprint", map[string]domainplans.JournalJob{
		"fixture": {Status: "prepared"},
	}, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	service.Journals = &commandJournalStore{journals: map[string]domainplans.Journal{preflightID: other}}
	service.Now = func() time.Time { return createdAt.Add(time.Minute) }
	if _, err := service.GetBatchPreflight(context.Background(), preflightID); !errors.Is(err, ErrBatchPreflightInvalid) {
		t.Fatalf("non-preflight journal error=%v", err)
	}
}

func TestLegacyBatchPreflightIsReadableButCannotBeSubmitted(t *testing.T) {
	createdAt := time.Date(2026, 8, 16, 4, 0, 0, 0, time.UTC)
	works := batchVerifiedWorks(1)
	groupID := batchTestGroupID(t, "fixture", works[0])
	journal := legacyBatchPreflightFixture(t, createdAt, groupID, works[0])
	const preflightID = "qianchuan-preflight-20260816t040000-abcdef123456"
	store := &commandJournalStore{journals: map[string]domainplans.Journal{preflightID: journal}}
	service := CommandService{
		Config: commandConfigReader{config: commandProductConfig()}, Journals: store,
		Now: func() time.Time { return createdAt.Add(time.Minute) },
	}
	summary, err := service.GetBatchPreflight(context.Background(), preflightID)
	if err != nil {
		t.Fatal(err)
	}
	if summary.SchemaVersion != 1 || summary.ReadyForSubmit || len(summary.Decisions) != 1 {
		t.Fatalf("legacy preflight summary changed: %#v", summary)
	}
	_, err = service.BatchWorks(context.Background(), BatchWorksCommand{PreflightID: preflightID, Submit: true})
	if !errors.Is(err, ErrBatchPreflightSchemaObsolete) {
		t.Fatalf("legacy preflight submit error=%v", err)
	}
}

func legacyBatchPreflightFixture(t *testing.T, createdAt time.Time, groupID string, work VerifiedWork) domainplans.Journal {
	t.Helper()
	expiresAt, err := batchPreflightExpiry(createdAt)
	if err != nil {
		t.Fatal(err)
	}
	decision := legacyBatchExpectedDecision{CreatorID: batchCreatorID, Action: "create"}
	snapshot := legacyPreparedBatchSnapshot{
		SchemaVersion: 1, CreatedAt: createdAt.Format(time.RFC3339Nano), ExpiresAt: expiresAt.Format(time.RFC3339Nano),
		AdvertiserID: batchAdvertiserID, TemplateID: "fixture", TemplateName: "fixture",
		TemplatePayload: batchTemplatePayload(), Works: []VerifiedWork{preparedVerifiedWork(work)},
		Expected: map[string]legacyBatchExpectedDecision{groupID: decision},
	}
	fingerprint, err := legacyBatchSnapshotFingerprint(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := json.Marshal(decision)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := domainplans.NewJournal(fingerprint, map[string]domainplans.JournalJob{
		groupID: {Status: "prepared", AdvertiserID: batchAdvertiserID, Extra: map[string]json.RawMessage{"expected": expected}},
	}, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	kind, _ := json.Marshal(batchPreflightKind)
	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	journal.Extra = map[string]json.RawMessage{"kind": kind, "snapshot": payload}
	return journal
}

type missingPreflightStore struct{}

func (missingPreflightStore) Load(context.Context, string) (domainplans.Journal, error) {
	return domainplans.Journal{}, os.ErrNotExist
}

func (missingPreflightStore) Save(context.Context, string, domainplans.Journal) error { return nil }

func TestBatchPreflightSnapshotLifecycle(t *testing.T) {
	createdAt := time.Date(2026, 8, 16, 4, 0, 0, 0, time.UTC)
	request := BatchRequest{
		AdvertiserID: batchAdvertiserID, AuthAccountID: "fixture-auth-account",
		TemplateID: "fixture-template", TemplateName: "fixture-template-name",
		ProductName: "fixture-product", ProductShortName: "fixture-short",
		PlanNameTemplate: "{creator_name}-{product_short_name}",
		TemplatePayload:  batchTemplatePayload(), IncludePayloads: true,
		Works:         batchVerifiedWorks(1),
		Skipped:       []SkippedWork{{InputIndex: 2, InputURL: "https://v.douyin.com/private-input/", Reason: "invalid", Message: "fixture"}},
		QueryFailures: []WorkQueryFailure{{AwemeID: batchCreatorID, Message: "raw query failure"}},
	}
	request.Works[0].Material.URL = "https://example.invalid/video"
	request.Works[0].Material.VideoCoverURL = "https://example.invalid/cover"
	groupID := batchTestGroupID(t, request.TemplateID, request.Works[0])
	result := BatchResult{BusinessDate: "2026-08-16", Results: []BatchGroupResult{{
		GroupID: groupID, AwemeID: batchCreatorID, ProductIDs: request.Works[0].MatchedProductIDs,
		InputItemIDs: []string{request.Works[0].AwemeItemID}, Status: "would_create",
	}}}

	snapshot, eligible, err := prepareBatchSnapshot(request, result, createdAt)
	if err != nil || !eligible {
		t.Fatalf("prepare snapshot: eligible=%t err=%v", eligible, err)
	}
	if snapshot.ExpiresAt != "2026-08-16T04:30:00Z" || snapshot.AuthAccountID != "fixture-auth-account" {
		t.Fatalf("snapshot expiry or authorization selector changed: %#v", snapshot)
	}
	group := snapshot.Groups[groupID]
	if len(group.Works) != 1 || group.Works[0].InputURL != "" ||
		group.Works[0].Material.URL != "" || group.Works[0].Material.VideoCoverURL != "" ||
		len(snapshot.Skipped) != 1 || snapshot.Skipped[0].InputURL != "" {
		t.Fatalf("snapshot retained unnecessary source or response data: %#v", snapshot)
	}
	journal, err := batchPreflightJournal(snapshot, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeBatchPreflight(journal, createdAt.Add(29*time.Minute))
	if err != nil || decoded.TemplateDigest != snapshot.TemplateDigest || len(decoded.Groups[groupID].Works) != 1 {
		t.Fatalf("decode snapshot: decoded=%#v err=%v", decoded, err)
	}
	if _, err := decodeBatchPreflight(journal, createdAt.Add(30*time.Minute)); err == nil ||
		!strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired snapshot was accepted: %v", err)
	}

	tampered := journal
	tampered.Jobs = map[string]domainplans.JournalJob{}
	if _, err := decodeBatchPreflight(tampered, createdAt); err == nil ||
		!strings.Contains(err.Error(), "jobs do not match") {
		t.Fatalf("tampered journal jobs were accepted: %v", err)
	}

	tampered = journal
	var extended preparedBatchSnapshot
	if err := json.Unmarshal(tampered.Extra["snapshot"], &extended); err != nil {
		t.Fatal(err)
	}
	extended.ExpiresAt = createdAt.Add(2 * time.Hour).Format(time.RFC3339Nano)
	tampered.Extra["snapshot"], err = json.Marshal(extended)
	if err != nil {
		t.Fatal(err)
	}
	tampered.Fingerprint, err = batchSnapshotFingerprint(extended)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeBatchPreflight(tampered, createdAt); err == nil ||
		!strings.Contains(err.Error(), "timestamps are invalid") {
		t.Fatalf("extended snapshot lifetime was accepted: %v", err)
	}

	tampered = journal
	raw := strings.TrimSuffix(string(tampered.Extra["snapshot"]), "}") + `,"unknown":true}`
	tampered.Extra["snapshot"] = json.RawMessage(raw)
	if _, err := decodeBatchPreflight(tampered, createdAt); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("unknown snapshot field was accepted: %v", err)
	}
}

func TestBatchPreflightExpiresAtShanghaiBusinessDayEnd(t *testing.T) {
	createdAt := time.Date(2026, 8, 16, 15, 50, 0, 0, time.UTC)
	expiresAt, err := batchPreflightExpiry(createdAt)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 8, 16, 16, 0, 0, 0, time.UTC); !expiresAt.Equal(want) {
		t.Fatalf("cross-day expiry=%s want=%s", expiresAt, want)
	}
}

func TestBatchTemplateDigestUsesCanonicalJSONNumbers(t *testing.T) {
	first := BatchRequest{
		AdvertiserID: batchAdvertiserID, TemplateID: "fixture", TemplateName: "fixture",
		ProductName: "fixture", ProductShortName: "fixture", PlanNameTemplate: "{creator_name}",
		TemplatePayload: json.RawMessage(`{"advertiser_id":1000000000000001,"budget":5000}`),
	}
	second := first
	second.TemplatePayload = json.RawMessage("{\n  \"budget\": 5000,\n  \"advertiser_id\": 1000000000000001\n}")
	if batchTemplateDigest(first) != batchTemplateDigest(second) {
		t.Fatal("semantically identical template JSON produced different digests")
	}
}

func TestV2SnapshotDigestsAreStable(t *testing.T) {
	work := batchVerifiedWorks(1)[0]
	templateDigest := batchTemplateDigest(BatchRequest{
		AdvertiserID: batchAdvertiserID, TemplateID: "fixture", TemplateName: "fixture",
		ProductName: "fixture", ProductShortName: "fixture", PlanNameTemplate: "{creator_name}",
		TemplatePayload: batchTemplatePayload(),
	})
	if templateDigest != "91acf86c01b886d5e15a0e2aab15742805fcffe38f502e1c1865cdad8a747503" {
		t.Fatalf("template digest changed: %s", templateDigest)
	}
	identity := testGroupIdentity(t, "fixture-template", work)
	groupID, err := domainqianchuan.GroupID(identity)
	if err != nil {
		t.Fatal(err)
	}
	if groupID != "qcg_77df7b7adf667b5f19f085fec206d5dab80acd9d5bb0838c13b1aab5ea0259fa" {
		t.Fatalf("group id changed: %s", groupID)
	}
	bindingDigest := testBindingDigest(t, "fixture-template", work, groupID, "2026-08-16", batchPlanID)
	if bindingDigest != "53cd026d7e2de5e6621d5cf0c12ecaa61be4719c2b6a93de7310abde9f502352" {
		t.Fatalf("binding digest changed: %s", bindingDigest)
	}
	digest, err := batchInputDigest(map[string]PreparedGroup{groupID: {
		GroupID: groupID, Identity: identity, Works: []VerifiedWork{preparedVerifiedWork(work)},
		ExpectedAction: "would_append", ExpectedAdID: batchPlanID,
		ExpectedWriteItemIDs: []string{work.AwemeItemID}, BindingDigest: strings.Repeat("a", 64), SubmitEligible: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if digest != "e35246a51e417374745ba2b3ac4931eec0064758cb0a8bb6c0d74a64c06e3d78" {
		t.Fatalf("input digest changed: %s", digest)
	}
}

func TestBatchSubmitIsolatesChangedPreflightCreator(t *testing.T) {
	const changedCreatorID = "4000000000000002"
	works := batchVerifiedWorks(1)
	changedWork := batchVerifiedWorks(1)[0]
	changedWork.InputIndex = 1
	changedWork.AwemeItemID = "6000000000000002"
	changedWork.Creator = domainqianchuan.AuthorizedCreator{
		AwemeID: changedCreatorID, VisibleID: "changed-visible", Name: "changed-creator",
	}
	changedWork.CreatorName = "changed-creator"
	changedWork.Material.AwemeItemID = changedWork.AwemeItemID
	works = append(works, changedWork)
	firstGroupID := batchTestGroupID(t, "fixture-template", works[0])
	changedGroupID := batchTestGroupID(t, "fixture-template", works[1])
	reader := &batchStateReader{materials: map[string]domainqianchuan.PlanMaterial{
		works[0].AwemeItemID: {
			MaterialID: "material-" + works[0].AwemeItemID, AwemeItemID: works[0].AwemeItemID,
			MaterialType: "VIDEO", MaterialSelectType: "CUSTOM", MaterialStatus: "DELIVERY_OK",
		},
	}}
	writer := &batchStateWriter{reader: reader}
	bindingDigest := strings.Repeat("a", 64)
	service := BatchService{
		Guard:  sharedplans.GuardedExecutor{Credentials: &batchCredentialProvider{}, Locks: &batchLocker{}},
		Reader: reader, Writer: writer,
		Reconciler: batchPreflightFinder{bindingDigest: bindingDigest, matches: map[string][]ExistingPlan{
			batchCreatorID: {{
				AdID: batchPlanID, Name: "fixture-plan", Status: "DISABLE",
				AwemeID: batchCreatorID, ProductIDs: []string{batchProductID},
			}},
			changedCreatorID: {{
				AdID: "2000000000000002", Name: "new-plan", Status: "ENABLE",
				AwemeID: changedCreatorID, ProductIDs: []string{batchProductID},
			}},
		}},
	}
	result, err := service.Execute(context.Background(), BatchRequest{
		AdvertiserID: batchAdvertiserID, Submit: true,
		TemplateID: "fixture-template", TemplateName: "fixture-template-name", ProductName: "fixture-product",
		TemplatePayload: batchTemplatePayload(), Works: works,
		PreparedGroups: map[string]PreparedGroup{
			firstGroupID: {
				GroupID: firstGroupID, Identity: testGroupIdentity(t, "fixture-template", works[0]),
				Works: []VerifiedWork{works[0]}, ExpectedAction: "noop", ExpectedAdID: batchPlanID,
				ExpectedPresentItemIDs: []string{works[0].AwemeItemID}, BindingDigest: bindingDigest, SubmitEligible: true,
			},
			changedGroupID: {
				GroupID: changedGroupID, Identity: testGroupIdentity(t, "fixture-template", works[1]),
				Works: []VerifiedWork{works[1]}, ExpectedAction: "would_create",
				ExpectedWriteItemIDs: []string{works[1].AwemeItemID}, BindingDigest: bindingDigest, SubmitEligible: true,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Results) != 2 || result.Results[0].Status != "already_present" ||
		result.Results[1].Status != "preflight_changed" || result.ExitCode != 1 || writer.addCalls != 0 {
		t.Fatalf("preflight job isolation changed: %#v", result)
	}
}

func TestBatchSubmitReadsAllGroupsBeforeAnyWrite(t *testing.T) {
	works := batchVerifiedWorks(1)
	second := batchVerifiedWorks(1)[0]
	second.InputIndex = 1
	second.AwemeItemID = "6000000000000002"
	second.Creator = domainqianchuan.AuthorizedCreator{AwemeID: "4000000000000002", VisibleID: "changed-visible", Name: "changed-creator"}
	second.CreatorName = "changed-creator"
	second.Material.AwemeItemID = second.AwemeItemID
	works = append(works, second)
	firstGroupID := batchTestGroupID(t, "fixture-template", works[0])
	secondGroupID := batchTestGroupID(t, "fixture-template", works[1])
	reader := &selectiveMaterialReader{batchStateReader: &batchStateReader{materials: map[string]domainqianchuan.PlanMaterial{}}, failAdID: "2000000000000002"}
	writer := &batchStateWriter{reader: reader.batchStateReader}
	bindingDigest := strings.Repeat("b", 64)
	result, err := (BatchService{
		Guard:  sharedplans.GuardedExecutor{Credentials: &batchCredentialProvider{}, Locks: &batchLocker{}},
		Reader: reader, Writer: writer,
		Reconciler: batchPreflightFinder{
			bindingDigest: bindingDigest,
			matches: map[string][]ExistingPlan{
				batchCreatorID:         {{AdID: batchPlanID, Name: "first", Status: "ENABLE", AwemeID: batchCreatorID, ProductIDs: []string{batchProductID}}},
				second.Creator.AwemeID: {{AdID: "2000000000000002", Name: "second", Status: "ENABLE", AwemeID: second.Creator.AwemeID, ProductIDs: []string{batchProductID}}},
			},
		},
	}).Execute(context.Background(), BatchRequest{
		AdvertiserID: batchAdvertiserID, ReadAccessToken: batchToken, Submit: true,
		TemplateID: "fixture-template", TemplateName: "fixture-template-name", ProductName: "fixture-product",
		TemplatePayload: batchTemplatePayload(), Works: works,
		PreparedGroups: map[string]PreparedGroup{
			firstGroupID: {
				GroupID: firstGroupID, Identity: testGroupIdentity(t, "fixture-template", works[0]), Works: []VerifiedWork{works[0]},
				ExpectedAction: "would_append", ExpectedAdID: batchPlanID, ExpectedWriteItemIDs: []string{works[0].AwemeItemID},
				BindingDigest: bindingDigest, SubmitEligible: true,
			},
			secondGroupID: {
				GroupID: secondGroupID, Identity: testGroupIdentity(t, "fixture-template", works[1]), Works: []VerifiedWork{works[1]},
				ExpectedAction: "would_append", ExpectedAdID: "2000000000000002", ExpectedWriteItemIDs: []string{works[1].AwemeItemID},
				BindingDigest: bindingDigest, SubmitEligible: true,
			},
		},
	})
	if err == nil || writer.addCalls != 0 || len(result.Results) != 2 {
		t.Fatalf("submit wrote before all group reads completed: result=%#v err=%v writes=%d", result, err, writer.addCalls)
	}
}

func batchTestGroupID(t *testing.T, templateID string, work VerifiedWork) string {
	t.Helper()
	identity := testGroupIdentity(t, templateID, work)
	groupID, err := domainqianchuan.GroupID(identity)
	if err != nil {
		t.Fatal(err)
	}
	return groupID
}

func testGroupIdentity(t *testing.T, templateID string, work VerifiedWork) domainqianchuan.PlanGroupIdentity {
	t.Helper()
	identity, err := domainqianchuan.NewPlanGroupIdentity(
		batchAdvertiserID, templateID, work.Creator.AwemeID,
		work.MatchedProductIDs, work.PlanType, work.Business,
	)
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func testBindingDigest(t *testing.T, templateID string, work VerifiedWork, groupID, businessDate, adID string) string {
	t.Helper()
	binding, err := NewPlanBinding(
		testGroupIdentity(t, templateID, work), groupID, businessDate, adID,
		time.Date(2026, 8, 16, 4, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := PlanBindingDigest(&binding)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

type memoryPlanBindingStore struct {
	bindings map[string]PlanBinding
}

func (store *memoryPlanBindingStore) Get(_ context.Context, businessDate, groupID string) (PlanBinding, bool, error) {
	if store.bindings == nil {
		return PlanBinding{}, false, nil
	}
	binding, exists := store.bindings[groupID+"/"+businessDate]
	return binding, exists, nil
}

func (store *memoryPlanBindingStore) List(context.Context) ([]PlanBinding, error) {
	result := make([]PlanBinding, 0, len(store.bindings))
	for _, binding := range store.bindings {
		result = append(result, binding)
	}
	return result, nil
}

func (store *memoryPlanBindingStore) Put(_ context.Context, binding PlanBinding) error {
	if store.bindings == nil {
		store.bindings = map[string]PlanBinding{}
	}
	store.bindings[binding.BindingKey] = binding
	return nil
}

type batchPreflightFinder struct {
	matches       map[string][]ExistingPlan
	bindingDigest string
}

type selectiveMaterialReader struct {
	*batchStateReader
	failAdID string
}

func (reader *selectiveMaterialReader) FetchPlanMaterials(
	ctx context.Context, request portqianchuan.MaterialPageRequest,
) (domainqianchuan.MaterialPage, error) {
	if request.AdID == reader.failAdID {
		return domainqianchuan.MaterialPage{}, errors.New("fixture material read failure")
	}
	return reader.batchStateReader.FetchPlanMaterials(ctx, request)
}

func (finder batchPreflightFinder) FindCurrentPlans(
	_ context.Context,
	request CurrentPlanRequest,
) (CurrentPlanResult, error) {
	result := CurrentPlanResult{Matches: map[string][]ExistingPlan{}, Policies: map[string]PlanMatchPolicy{}}
	for _, target := range request.Targets {
		matches := append([]ExistingPlan(nil), finder.matches[target.AwemeID]...)
		result.Matches[target.GroupID] = matches
		status := "would_create"
		if len(matches) != 0 {
			status = "bound"
		}
		result.Policies[target.GroupID] = PlanMatchPolicy{
			Status: status, Candidates: matches, BindingDigest: finder.bindingDigest,
		}
	}
	return result, nil
}
