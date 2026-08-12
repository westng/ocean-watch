package marketing

import (
	"context"
	"errors"
	"testing"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/configuration"
	domainmarketing "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/marketing"
)

func TestMarketingCreateServiceDryRunNeverExecutes(t *testing.T) {
	executor := &transactionExecutorStub{}
	result, err := (CreateService{
		Preparer: Preparer{Config: staticMarketingConfig{value: marketingPrepareFixture(AccountUploadSource)}},
		Executor: executor,
	}).Execute(context.Background(), CreateRequest{PrepareRequest: PrepareRequest{
		Kind: PrepareUpload, AdvertiserID: "1234567890", PlanTemplate: "upload-template",
		VideoIDs: []string{"runtime-video"}, MaterialDate: "7.26",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != "dry_run" || executor.calls != 0 || result.SubmitBlocked {
		t.Fatalf("dry-run execution boundary changed: result=%#v calls=%d", result, executor.calls)
	}
}

func TestMarketingCreateServiceBlocksBeforeExecutor(t *testing.T) {
	config := marketingPrepareFixture(AccountUploadSource)
	defaultTemplate := configuration.Object(config["default_plan_template"])
	configuration.Object(defaultTemplate["resolved_ids"])["city_ids"] = []any{}
	executor := &transactionExecutorStub{}
	result, err := (CreateService{
		Preparer: Preparer{
			Config: staticMarketingConfig{value: config}, Materials: &marketingMaterialStub{
				adVideos: []domainmarketing.VideoAsset{{ID: "video-1", Filename: "one.mp4"}},
				covers:   map[string]string{"video-1": "cover-1"},
			}, RuntimeAssets: passthroughRuntimeAssets{},
		},
		Executor: executor,
	}).Execute(context.Background(), CreateRequest{PrepareRequest: PrepareRequest{
		Kind: PrepareUpload, AdvertiserID: "1234567890", PlanTemplate: "upload-template",
		VideoIDs: []string{"video-1"}, MaterialDate: "7.26", Submit: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.SubmitBlocked || executor.calls != 0 || !containsString(result.BlockingFields, "resolved_ids.city_ids") {
		t.Fatalf("blocking fields crossed write boundary: result=%#v calls=%d", result, executor.calls)
	}
}

func TestMarketingCreateServicePromotionOnlyUsesExistingProject(t *testing.T) {
	executor := &transactionExecutorStub{result: Result{
		Status: "completed", ProjectID: "2001", PromotionID: "3001",
	}}
	result, err := (CreateService{
		Preparer: Preparer{
			Config: staticMarketingConfig{value: marketingPrepareFixture(AccountUploadSource)},
			Materials: &marketingMaterialStub{
				adVideos: []domainmarketing.VideoAsset{{ID: "video-1", Filename: "one.mp4"}},
				covers:   map[string]string{"video-1": "cover-1"},
			}, RuntimeAssets: passthroughRuntimeAssets{},
		},
		Executor: executor,
	}).Execute(context.Background(), CreateRequest{
		PrepareRequest: PrepareRequest{
			Kind: PrepareUpload, AdvertiserID: "1234567890", PlanTemplate: "upload-template",
			VideoIDs: []string{"video-1"}, MaterialDate: "7.26", ProjectID: "2001", Submit: true,
		},
		PromotionOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ProjectID != "2001" || result.PromotionID != "3001" || executor.calls != 1 ||
		executor.last.ResumeProjectID != "2001" {
		t.Fatalf("promotion-only transaction changed: result=%#v request=%#v", result, executor.last)
	}
}

func TestMarketingCreateServiceReturnsTransactionFailure(t *testing.T) {
	executor := &transactionExecutorStub{
		result: Result{Status: "failed", ProjectID: "2001", FailureStage: "promotion_create"},
		err:    errors.New("promotion rejected"),
	}
	service := CreateService{
		Preparer: Preparer{
			Config: staticMarketingConfig{value: marketingPrepareFixture(AccountUploadSource)},
			Materials: &marketingMaterialStub{
				adVideos: []domainmarketing.VideoAsset{{ID: "video-1", Filename: "one.mp4"}},
				covers:   map[string]string{"video-1": "cover-1"},
			}, RuntimeAssets: passthroughRuntimeAssets{},
		},
		Executor: executor,
	}
	result, err := service.Execute(context.Background(), CreateRequest{PrepareRequest: PrepareRequest{
		Kind: PrepareUpload, AdvertiserID: "1234567890", PlanTemplate: "upload-template",
		VideoIDs: []string{"video-1"}, MaterialDate: "7.26", Submit: true,
	}})
	if err == nil || result.ProjectID != "2001" || result.FailureStage != "promotion_create" {
		t.Fatalf("resumable transaction failure was lost: result=%#v err=%v", result, err)
	}
}

type transactionExecutorStub struct {
	result Result
	err    error
	calls  int
	last   Request
}

func (stub *transactionExecutorStub) Execute(_ context.Context, request Request) (Result, error) {
	stub.calls++
	stub.last = request
	return stub.result, stub.err
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
