package marketing

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	sharedplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/plans"
	domainplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/plans"
	portmarketing "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/ports/marketing"
)

func TestProjectPromotionTransaction(t *testing.T) {
	t.Run("success uses returned project", func(t *testing.T) {
		writer := &writerFixture{projectID: "2001", promotionID: "3001"}
		result, err := fixtureExecutor(writer, nil).Execute(context.Background(), fixtureRequest())
		if err != nil {
			t.Fatal(err)
		}
		if result.Status != "completed" || result.ProjectID != "2001" || result.PromotionID != "3001" {
			t.Fatalf("unexpected result: %#v", result)
		}
		if !reflect.DeepEqual(writer.calls, []string{"project", "promotion:2001"}) {
			t.Fatalf("unexpected calls: %#v", writer.calls)
		}
	})

	t.Run("promotion failure preserves project and resumes promotion only", func(t *testing.T) {
		writer := &writerFixture{projectID: "2001", promotionErr: acknowledgedError("promotion rejected")}
		result, err := fixtureExecutor(writer, nil).Execute(context.Background(), fixtureRequest())
		if err == nil || result.FailureStage != "promotion_create" || result.ProjectID != "2001" {
			t.Fatalf("expected resumable failure: result=%#v err=%v", result, err)
		}
		resumeWriter := &writerFixture{promotionID: "3001"}
		request := fixtureRequest()
		request.ResumeProjectID = result.ProjectID
		resumed, err := fixtureExecutor(resumeWriter, nil).Execute(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		if resumed.PromotionID != "3001" || !reflect.DeepEqual(resumeWriter.calls, []string{"promotion:2001"}) {
			t.Fatalf("project was recreated during resume: result=%#v calls=%#v", resumed, resumeWriter.calls)
		}
	})

	t.Run("project failure stops before promotion", func(t *testing.T) {
		writer := &writerFixture{projectErr: acknowledgedError("project rejected")}
		result, err := fixtureExecutor(writer, nil).Execute(context.Background(), fixtureRequest())
		if err == nil || result.FailureStage != "project_create" || !reflect.DeepEqual(writer.calls, []string{"project"}) {
			t.Fatalf("unexpected failure: result=%#v calls=%#v err=%v", result, writer.calls, err)
		}
	})
}

func TestUnknownWriteReconciliation(t *testing.T) {
	t.Run("unique project continues without replay", func(t *testing.T) {
		writer := &writerFixture{projectErr: unknownError(), promotionID: "3001"}
		reconciler := &reconcilerFixture{projectIDs: []string{"2001"}}
		result, err := fixtureExecutor(writer, reconciler).Execute(context.Background(), fixtureRequest())
		if err != nil {
			t.Fatal(err)
		}
		if result.ProjectID != "2001" || result.PromotionID != "3001" || writer.projectCalls != 1 {
			t.Fatalf("unknown project was not safely reconciled: result=%#v writer=%#v", result, writer)
		}
	})

	t.Run("not applied project stops without replay", func(t *testing.T) {
		writer := &writerFixture{projectErr: unknownError()}
		result, err := fixtureExecutor(writer, &reconcilerFixture{}).Execute(context.Background(), fixtureRequest())
		if err == nil || result.Reconciliation == nil || result.Reconciliation.State != domainplans.ReconciliationNotApplied || writer.projectCalls != 1 || writer.promotionCalls != 0 {
			t.Fatalf("unexpected not-applied result: result=%#v writer=%#v err=%v", result, writer, err)
		}
	})

	t.Run("ambiguous project blocks later writes", func(t *testing.T) {
		writer := &writerFixture{projectErr: unknownError()}
		reconciler := &reconcilerFixture{projectIDs: []string{"2001", "2002"}}
		result, err := fixtureExecutor(writer, reconciler).Execute(context.Background(), fixtureRequest())
		if err == nil || result.Reconciliation == nil || result.Reconciliation.State != domainplans.ReconciliationAmbiguous || writer.promotionCalls != 0 {
			t.Fatalf("ambiguous result did not stop: result=%#v writer=%#v err=%v", result, writer, err)
		}
	})

	t.Run("unique promotion completes without replay", func(t *testing.T) {
		writer := &writerFixture{projectID: "2001", promotionErr: unknownError()}
		reconciler := &reconcilerFixture{promotionIDs: []string{"3001"}}
		result, err := fixtureExecutor(writer, reconciler).Execute(context.Background(), fixtureRequest())
		if err != nil || result.PromotionID != "3001" || writer.promotionCalls != 1 {
			t.Fatalf("promotion reconciliation failed: result=%#v writer=%#v err=%v", result, writer, err)
		}
	})

	t.Run("pre-dispatch failure is not reconciled", func(t *testing.T) {
		writer := &writerFixture{projectErr: &domainplans.DispatchFailure{State: domainplans.DispatchNotSent, Cause: errors.New("budget exhausted")}}
		reconciler := &reconcilerFixture{projectIDs: []string{"2001"}}
		result, err := fixtureExecutor(writer, reconciler).Execute(context.Background(), fixtureRequest())
		if err == nil || reconciler.projectCalls != 0 || result.DispatchState != domainplans.DispatchNotSent {
			t.Fatalf("pre-dispatch failure was reconciled: result=%#v reconciler=%#v err=%v", result, reconciler, err)
		}
	})
}

func fixtureExecutor(writer portmarketing.PlanWriter, reconciler portmarketing.PlanReconciler) Executor {
	return Executor{
		Guard: sharedplans.GuardedExecutor{
			Credentials: fixtureCredentials{}, Locks: fixtureLock{},
		},
		Writer: writer, Reconciler: reconciler,
	}
}

func fixtureRequest() Request {
	return Request{
		AdvertiserID: "1001", Submit: true,
		ProjectPayload:   mustJSON(map[string]any{"advertiser_id": 1001, "name": "project-stable"}),
		PromotionPayload: mustJSON(map[string]any{"advertiser_id": 1001, "name": "promotion-stable", "project_id": "{{project_id}}"}),
	}
}

func mustJSON(value any) json.RawMessage {
	payload, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return payload
}

type fixtureCredentials struct{}

func (fixtureCredentials) AccessToken(context.Context, domainplans.Channel, string, string) (sharedplans.CredentialLease, error) {
	return sharedplans.CredentialLease{AuthorizationID: "fixture-auth", AccessToken: "fixture-token"}, nil
}

type fixtureLock struct{}

func (fixtureLock) Acquire(context.Context, domainplans.WriteScope) (func() error, error) {
	return func() error { return nil }, nil
}

type writerFixture struct {
	projectID      string
	promotionID    string
	projectErr     error
	promotionErr   error
	projectCalls   int
	promotionCalls int
	calls          []string
	promotionBody  json.RawMessage
}

func (fixture *writerFixture) CreateProject(context.Context, portmarketing.ProjectCreateRequest) (portmarketing.CreateResult, error) {
	fixture.projectCalls++
	fixture.calls = append(fixture.calls, "project")
	return portmarketing.CreateResult{ObjectID: fixture.projectID, RequestID: "project-request"}, fixture.projectErr
}

func (fixture *writerFixture) CreatePromotion(_ context.Context, request portmarketing.PromotionCreateRequest) (portmarketing.CreateResult, error) {
	fixture.promotionCalls++
	fixture.calls = append(fixture.calls, "promotion:"+request.ProjectID)
	fixture.promotionBody = append(json.RawMessage(nil), request.Payload...)
	return portmarketing.CreateResult{ObjectID: fixture.promotionID, RequestID: "promotion-request"}, fixture.promotionErr
}

type reconcilerFixture struct {
	projectIDs     []string
	promotionIDs   []string
	projectCalls   int
	promotionCalls int
}

func (fixture *reconcilerFixture) FindProjects(context.Context, portmarketing.ProjectReconciliationRequest) ([]string, error) {
	fixture.projectCalls++
	return append([]string(nil), fixture.projectIDs...), nil
}

func (fixture *reconcilerFixture) FindPromotions(context.Context, portmarketing.PromotionReconciliationRequest) ([]string, error) {
	fixture.promotionCalls++
	return append([]string(nil), fixture.promotionIDs...), nil
}

func unknownError() error {
	return &domainplans.DispatchFailure{State: domainplans.DispatchUnknown, Cause: errors.New("response lost after dispatch")}
}

func acknowledgedError(message string) error {
	return &domainplans.DispatchFailure{State: domainplans.DispatchAcknowledged, Cause: errors.New(message)}
}
