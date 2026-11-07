package approvalbroker

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/sqlite"
	application "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/operations"
	domain "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/operations"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/approval"
)

func testSnapshot(t *testing.T) domain.Snapshot {
	t.Helper()
	payload := `{"items":[{"item_id":"item-1","group_id":"group-1","action":"append","target_id":"9007199254740993123","payload_digest":"sha256:` + strings.Repeat("1", 64) + `","expected_digest":"sha256:` + strings.Repeat("2", 64) + `","material_count":1,"changes":[{"field":"素材数量","before":"0","after":"1"}]}]}`
	digest, err := domain.Digest([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Second)
	return domain.Snapshot{OperationID: "op_broker_test", Version: 1, Kind: "qianchuan_batch", Channel: "qianchuan",
		SubjectID: "9007199254740993123", RuntimeVersion: "test", SchemaVersion: domain.SchemaVersion,
		InputDigest: digest, SnapshotDigest: digest, CreatedAt: now, ExpiresAt: now.Add(time.Hour), Payload: []byte(payload)}
}

func testRequest(t *testing.T, snapshot domain.Snapshot) approval.Request {
	t.Helper()
	expires := time.Now().UTC().Add(time.Minute)
	summary, err := domain.NewConfirmationSummary(snapshot, expires)
	if err != nil {
		t.Fatal(err)
	}
	return approval.Request{OperationID: snapshot.OperationID, Version: snapshot.Version, SnapshotDigest: snapshot.SnapshotDigest,
		ScopeDigest: summary.ScopeDigest, ExpiresAt: expires, Summary: summary}
}

func testService(t *testing.T, exchange exchangeFunc) (application.Service, *Broker, *sqlite.Store, domain.Snapshot) {
	t.Helper()
	store, err := sqlite.Initialize(context.Background(), sqlite.Options{Path: filepath.Join(t.TempDir(), "state", "state.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	snapshot := testSnapshot(t)
	if err := store.SavePreflight(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	broker := &Broker{now: time.Now, exchange: exchange}
	verifier, err := approval.NewVerifier(broker)
	if err != nil {
		t.Fatal(err)
	}
	return application.Service{Repository: store, Snapshots: store, Provider: broker, Evidence: broker, Verifier: verifier}, broker, store, snapshot
}

func TestConfirmationConsumesApprovalOnceAndPersistsDispatchFacts(t *testing.T) {
	ctx := context.Background()
	service, _, store, snapshot := testService(t, func(context.Context, approval.Request) error { return nil })
	result, err := service.Confirm(ctx, snapshot.OperationID, 1, snapshot.SnapshotDigest)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(result)
	for _, forbidden := range []string{"nonce", "evidence", "source_reference", "challenge"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("private approval material leaked: %s", forbidden)
		}
	}
	request := application.ExecuteRequest{OperationID: snapshot.OperationID, Version: 1, SnapshotDigest: snapshot.SnapshotDigest,
		ApprovalID: result.ApprovalID, OwnerID: "worker-1", TTL: time.Minute}
	execution, err := service.Execute(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, request); err == nil {
		t.Fatal("approval was consumed twice")
	}
	intent := domain.DispatchIntentRequest{OperationID: snapshot.OperationID, Version: 1, ItemID: "item-1", GroupID: "group-1",
		Action: "append", TargetID: snapshot.SubjectID, PayloadDigest: "sha256:" + strings.Repeat("1", 64),
		ExpectedDigest: "sha256:" + strings.Repeat("2", 64), OwnerID: "worker-1", FencingEpoch: execution.Lease.FencingEpoch}
	attempt, err := service.BeginDispatch(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.CompleteDispatch(ctx, domain.DispatchOutcome{OperationID: snapshot.OperationID, Version: 1,
		ItemID: intent.ItemID, DispatchKey: attempt.DispatchKey, State: domain.DispatchUnknown,
		OwnerID: "worker-1", FencingEpoch: execution.Lease.FencingEpoch}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.BeginDispatch(ctx, intent); err == nil {
		t.Fatal("unknown dispatch was blindly replayed")
	}
	state, err := store.GetOperationState(ctx, snapshot.OperationID, 1)
	if err != nil || state.Status != domain.StatusUnknown {
		t.Fatalf("unknown result lost: %+v, %v", state, err)
	}
}

func TestConfirmationRevalidatesVersionAfterUserDecision(t *testing.T) {
	service, broker, store, snapshot := testService(t, nil)
	broker.exchange = func(ctx context.Context, request approval.Request) error {
		next := snapshot
		next.Version = 2
		next.CreatedAt = time.Now().UTC()
		return store.SavePreflight(ctx, next)
	}
	if _, err := service.Confirm(context.Background(), snapshot.OperationID, 1, snapshot.SnapshotDigest); !errors.Is(err, domain.ErrOperationNotExecutable) {
		t.Fatalf("stale decision was accepted: %v", err)
	}
	if broker.pending != nil {
		t.Fatal("failed confirmation retained a private approval")
	}
}

func TestNewVersionInvalidatesAlreadySavedApprovalBeforeClaim(t *testing.T) {
	ctx := context.Background()
	service, _, store, snapshot := testService(t, func(context.Context, approval.Request) error { return nil })
	result, err := service.Confirm(ctx, snapshot.OperationID, 1, snapshot.SnapshotDigest)
	if err != nil {
		t.Fatal(err)
	}
	// Re-confirmation of an already approved version cannot open another UI
	// and leave a rejection ambiguously coexisting with the first approval.
	if _, err := service.Confirm(ctx, snapshot.OperationID, 1, snapshot.SnapshotDigest); err == nil {
		t.Fatal("approved version requested a second decision")
	}
	next := snapshot
	next.Version = 2
	next.CreatedAt = time.Now().UTC()
	if err := store.SavePreflight(ctx, next); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, application.ExecuteRequest{OperationID: snapshot.OperationID, Version: 1,
		SnapshotDigest: snapshot.SnapshotDigest, ApprovalID: result.ApprovalID, OwnerID: "worker-1", TTL: time.Minute}); !errors.Is(err, domain.ErrOperationNotExecutable) {
		t.Fatalf("obsolete version claimed execution: %v", err)
	}
}

func TestSavedApprovalSurvivesRestartAndOnlyOneConnectionClaimsIt(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state", "state.db")
	store, err := sqlite.Initialize(ctx, sqlite.Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := testSnapshot(t)
	if err := store.SavePreflight(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	broker := &Broker{now: time.Now, exchange: func(context.Context, approval.Request) error { return nil }}
	verifier, _ := approval.NewVerifier(broker)
	service := application.Service{Repository: store, Snapshots: store, Provider: broker, Evidence: broker, Verifier: verifier}
	confirmed, err := service.Confirm(ctx, snapshot.OperationID, 1, snapshot.SnapshotDigest)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	first, err := sqlite.Open(ctx, sqlite.Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := sqlite.Open(ctx, sqlite.Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	start, results := make(chan struct{}), make(chan error, 2)
	for index, repository := range []*sqlite.Store{first, second} {
		go func() {
			<-start
			_, err := (application.Service{Repository: repository}).Execute(ctx, application.ExecuteRequest{
				OperationID: snapshot.OperationID, Version: 1, SnapshotDigest: snapshot.SnapshotDigest,
				ApprovalID: confirmed.ApprovalID, OwnerID: []string{"first-worker", "second-worker"}[index], TTL: time.Minute,
			})
			results <- err
		}()
	}
	close(start)
	granted := 0
	for index := 0; index < 2; index++ {
		if err := <-results; err == nil {
			granted++
		}
	}
	if granted != 1 {
		t.Fatalf("independent state connections granted %d executions", granted)
	}
}

func TestBrokerSerializesRequestsAndDiscardsUndeliveredDecision(t *testing.T) {
	request := testRequest(t, testSnapshot(t))
	entered, finish := make(chan struct{}), make(chan struct{})
	broker := &Broker{now: time.Now, exchange: func(ctx context.Context, request approval.Request) error {
		close(entered)
		<-finish
		return nil
	}}
	done := make(chan error, 1)
	go func() { done <- broker.Request(context.Background(), request) }()
	<-entered
	if err := broker.Request(context.Background(), request); !errors.Is(err, ErrBusy) {
		t.Errorf("concurrent request = %v", err)
	}
	close(finish)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	broker.Discard(request)
	if _, err := broker.Receive(context.Background(), request); err == nil {
		t.Fatal("discarded decision remained available")
	}
}

func TestBrokerOwnsTheReviewWhileDialogIsOpen(t *testing.T) {
	request := testRequest(t, testSnapshot(t))
	broker := &Broker{now: time.Now, exchange: func(ctx context.Context, frozen approval.Request) error {
		request.Summary.Items[0].Changes[0].After = "999"
		*request.Summary.Items[0].MaterialCount = 999
		if frozen.Summary.Items[0].Changes[0].After != "1" || *frozen.Summary.Items[0].MaterialCount != 1 {
			t.Fatal("caller mutation changed the displayed approval")
		}
		return approval.ErrNotApproved
	}}
	if err := broker.Request(context.Background(), request); !errors.Is(err, approval.ErrNotApproved) {
		t.Fatal(err)
	}
}

func TestBrokerRejectsForgeryReplayRejectionAndExpiry(t *testing.T) {
	ctx := context.Background()
	request := testRequest(t, testSnapshot(t))
	broker := &Broker{now: time.Now, exchange: func(context.Context, approval.Request) error { return nil }}
	verifier, _ := approval.NewVerifier(broker)
	if _, err := verifier.Verify(ctx, approval.Evidence{ProviderID: providerID, AdapterID: adapterID, HostVersion: protocolID}); err == nil {
		t.Fatal("provider labels approved a fabricated event")
	}
	if err := broker.Request(ctx, request); err != nil {
		t.Fatal(err)
	}
	evidence, err := broker.Receive(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := broker.Receive(ctx, request); err == nil {
		t.Fatal("evidence was delivered twice")
	}
	for _, change := range []func(*approval.Evidence){
		func(e *approval.Evidence) { e.Nonce = strings.Repeat("0", 64) },
		func(e *approval.Evidence) { e.OperationVersion++ },
		func(e *approval.Evidence) { e.ProviderID = "model" },
		func(e *approval.Evidence) { e.ScopeDigest = "sha256:" + strings.Repeat("0", 64) },
	} {
		forged := evidence
		change(&forged)
		if _, err := verifier.Verify(ctx, forged); err == nil {
			t.Fatal("altered evidence was accepted")
		}
	}
	if _, err := verifier.Verify(ctx, evidence); err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.Verify(ctx, evidence); err == nil {
		t.Fatal("event was replayed")
	}
	broker.exchange = func(context.Context, approval.Request) error { return approval.ErrNotApproved }
	if err := broker.Request(ctx, request); !errors.Is(err, approval.ErrNotApproved) || broker.pending != nil {
		t.Fatalf("rejection created evidence: %v", err)
	}
	broker.exchange = func(context.Context, approval.Request) error { return nil }
	broker.now = func() time.Time { return request.ExpiresAt }
	if err := broker.Request(ctx, request); !errors.Is(err, domain.ErrApprovalExpired) || broker.pending != nil {
		t.Fatalf("expired request created evidence: %v", err)
	}
}

func TestConfirmationRejectsUnavailableProviderAndIncompleteReviewBeforeInteraction(t *testing.T) {
	called := false
	service, _, store, snapshot := testService(t, func(context.Context, approval.Request) error { called = true; return nil })
	service.Provider = nil
	if _, err := service.Confirm(context.Background(), snapshot.OperationID, 1, snapshot.SnapshotDigest); !errors.Is(err, approval.ErrVerifierUnavailable) || called {
		t.Fatalf("unavailable provider interacted: %v", err)
	}
	service.Provider = service.Evidence.(*Broker)
	next := snapshot
	next.Version = 2
	next.Payload = []byte(`{"jobs":{}}`)
	next.SnapshotDigest, _ = domain.Digest(next.Payload)
	if err := store.SavePreflight(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Confirm(context.Background(), next.OperationID, 2, next.SnapshotDigest); !errors.Is(err, domain.ErrConfirmationSummary) || called {
		t.Fatalf("legacy journal was approved: %v", err)
	}
}
