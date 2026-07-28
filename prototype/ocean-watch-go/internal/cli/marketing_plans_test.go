package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application"
	sharedplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/plans"
	applicationmarketing "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/plans/marketing"
	domainplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/plans"
	portmarketing "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/ports/marketing"
)

func TestParseMarketingCreateOptionsMapsStableCommands(t *testing.T) {
	tests := []struct {
		name            string
		action          string
		args            []string
		kind            applicationmarketing.PrepareKind
		videoIDs        []string
		itemIDs         []string
		onlinePreflight bool
		promotionOnly   bool
		wantBudget      string
		wantBid         string
		wantROI         string
	}{
		{
			name: "uploaded dry-run remains offline", action: "create",
			args: []string{
				"--advertiser-id", "1001", "--plan-template", "upload-template",
				"--video-id", "2001,2002", "--budget", "5000.25", "--bid", "1.25",
				"--roi-goal", "1.70", "--promotion-only", "--project-id", "3001",
			},
			kind: applicationmarketing.PrepareUpload, videoIDs: []string{"2001", "2002"},
			promotionOnly: true, wantBudget: "5000.25", wantBid: "1.25", wantROI: "1.70",
		},
		{
			name: "creator dry-run uses current authorization", action: "create-creator",
			args: []string{
				"--advertiser-id", "1001", "--plan-template", "creator-template",
				"--item-id", "4001", "--item-id", "4002",
			},
			kind: applicationmarketing.PrepareCreator, itemIDs: []string{"4001", "4002"},
			onlinePreflight: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, request, err := parseMarketingCreateOptions(test.action, test.args)
			if err != nil {
				t.Fatal(err)
			}
			if request.Kind != test.kind || !slices.Equal(request.VideoIDs, test.videoIDs) ||
				!slices.Equal(request.ItemIDs, test.itemIDs) ||
				request.OnlinePreflight != test.onlinePreflight || request.PromotionOnly != test.promotionOnly {
				t.Fatalf("unexpected create request: %#v", request)
			}
			for label, value := range map[string]struct {
				got  any
				want string
			}{
				"budget": {request.Budget, test.wantBudget},
				"bid":    {request.CPABid, test.wantBid},
				"roi":    {request.ROIGoal, test.wantROI},
			} {
				if value.want == "" {
					if value.got != nil {
						t.Fatalf("%s = %#v, want nil", label, value.got)
					}
					continue
				}
				number, ok := value.got.(json.Number)
				if !ok || number.String() != value.want {
					t.Fatalf("%s = %#v, want exact %s", label, value.got, value.want)
				}
			}
		})
	}
}

func TestRunnerMarketingCreateUsesSharedServiceAndPreservesFailureResult(t *testing.T) {
	routes := application.DefaultRouteManifest().Snapshot()
	routes["plans create"] = application.RuntimeGo
	manifest, err := application.NewRouteManifest(6, routes)
	if err != nil {
		t.Fatal(err)
	}
	service := &marketingCreateServiceStub{
		result: applicationmarketing.CreateResult{
			Mode: "submit", Status: "failed", Error: "promotion rejected",
			ProjectID: "2001", FailureStage: "promotion_create",
		},
		err: errors.New("promotion rejected"),
	}
	stdout := new(bytes.Buffer)
	runner := Runner{
		Routes: manifest, Fallback: &fallbackSpy{code: 99}, Stdout: stdout,
		Cwd: t.TempDir(), UserHome: t.TempDir(), Getenv: func(string) string { return "" },
		MarketingPlans: MarketingPlanRuntime{CreateService: service},
	}
	code := runner.Execute(context.Background(), []string{
		"plans", "create", "--advertiser-id", "1001", "--plan-template", "upload-template",
		"--video-id", "4001", "--submit",
	})
	if code != 1 || service.calls != 1 {
		t.Fatalf("failure exit = %d, calls = %d: %s", code, service.calls, stdout.String())
	}
	result := decodeSingleJSONObject(t, stdout.Bytes())
	if result["project_id"] != "2001" || result["failure_stage"] != "promotion_create" ||
		result["error"] != "promotion rejected" {
		t.Fatalf("resumable create result was replaced: %#v", result)
	}
}

func TestMarketingCreateRejectsCrossChannelBeforeService(t *testing.T) {
	service := &marketingCreateServiceStub{}
	stdout := new(bytes.Buffer)
	runner := Runner{MarketingPlans: MarketingPlanRuntime{CreateService: service}}
	code := runner.runMarketingPlan(
		context.Background(), "create",
		[]string{"--channel", "qianchuan", "--plan-template", "template", "--video-id", "1001"},
		t.TempDir(), nil, stdout,
	)
	if code != 2 || service.calls != 0 {
		t.Fatalf("cross-channel create crossed service boundary: exit=%d calls=%d output=%s", code, service.calls, stdout.String())
	}
}

func TestMarketingCreatorBatchRejectsUnmanagedJournalBeforeManifestRead(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	outsideJournal := filepath.Join(t.TempDir(), "creator-batch-fixture.json")
	stdout := new(bytes.Buffer)
	service := &marketingCreatorBatchServiceStub{}
	runner := Runner{
		UserHome:       t.TempDir(),
		MarketingPlans: MarketingPlanRuntime{CreatorBatchService: service},
	}
	code := runner.runMarketingCreatorBatch(
		context.Background(),
		[]string{
			"--jobs-file", filepath.Join(t.TempDir(), "missing-manifest.json"),
			"--journal", outsideJournal,
		},
		stateRoot, nil, stdout,
	)
	if code != 2 || service.calls != 0 {
		t.Fatalf("unmanaged journal exit=%d calls=%d output=%s", code, service.calls, stdout.String())
	}
	result := decodeSingleJSONObject(t, stdout.Bytes())
	errorBody := result["error"].(map[string]any)
	if errorBody["code"] != "invalid_batch_journal" ||
		strings.Contains(errorBody["message"].(string), "jobs file") {
		t.Fatalf("manifest was reached before journal rejection: %#v", result)
	}
}

func TestParseMarketingCreatorBatchOptionsMapsStableArguments(t *testing.T) {
	options, err := parseMarketingCreatorBatchOptions([]string{
		"--config", " config.json ",
		"--jobs-file", " jobs.json ",
		"--concurrency", "7",
		"--journal", " journal.json ",
		"--preflight",
		"--include-payloads",
		"--out", " result.json ",
		"--channel", " marketing ",
		"--auth-account-id", " auth-1 ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if options.configPath != "config.json" || options.jobsFile != "jobs.json" ||
		options.concurrency != 7 || options.journal != "journal.json" ||
		!options.preflight || options.submit || !options.includePayload ||
		options.out != "result.json" || options.channel != "marketing" ||
		options.authAccountID != "auth-1" {
		t.Fatalf("unexpected creator batch options: %#v", options)
	}
}

func TestMarketingCreatorBatchRejectsManagedRootSymbolicLinkBeforeManifestRead(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(stateRoot, "runs")); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	journal := filepath.Join(stateRoot, "runs", "creator-batch-fixture.json")
	stdout := new(bytes.Buffer)
	service := &marketingCreatorBatchServiceStub{}
	runner := Runner{
		UserHome:       t.TempDir(),
		MarketingPlans: MarketingPlanRuntime{CreatorBatchService: service},
	}
	code := runner.runMarketingCreatorBatch(
		context.Background(),
		[]string{
			"--jobs-file", filepath.Join(t.TempDir(), "missing-manifest.json"),
			"--journal", journal,
		},
		stateRoot, nil, stdout,
	)
	if code != 2 || service.calls != 0 {
		t.Fatalf("linked root exit=%d calls=%d output=%s", code, service.calls, stdout.String())
	}
	result := decodeSingleJSONObject(t, stdout.Bytes())
	if result["error"].(map[string]any)["code"] != "invalid_batch_journal" {
		t.Fatalf("unexpected linked-root result: %#v", result)
	}
}

func TestMarketingCreatorBatchReturnsManagedJournalPath(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	journal := filepath.Join(stateRoot, "runs", "creator-batch-fixture.json")
	manifest := filepath.Join(t.TempDir(), "jobs.json")
	if err := os.WriteFile(manifest, []byte(`{"jobs":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout := new(bytes.Buffer)
	service := &marketingCreatorBatchServiceStub{result: applicationmarketing.CreatorBatchResult{
		Mode: "preflight", BatchID: "fixture", RunID: "creator-batch-fixture", JournalUsed: true,
		Counts: map[string]int{}, Rows: []applicationmarketing.CreatorBatchRow{},
	}}
	runner := Runner{
		UserHome:       t.TempDir(),
		MarketingPlans: MarketingPlanRuntime{CreatorBatchService: service},
	}
	code := runner.runMarketingCreatorBatch(
		context.Background(),
		[]string{"--jobs-file", manifest, "--journal", journal, "--preflight"},
		stateRoot, nil, stdout,
	)
	if code != 0 || service.calls != 1 {
		t.Fatalf("managed journal exit=%d calls=%d output=%s", code, service.calls, stdout.String())
	}
	if service.last.RunID != "creator-batch-fixture" || string(service.last.ManifestPayload) != `{"jobs":[]}` {
		t.Fatalf("creator batch arguments changed: %#v", service.last)
	}
	result := decodeSingleJSONObject(t, stdout.Bytes())
	if result["journal"] != journal {
		t.Fatalf("journal output = %#v, want %q", result["journal"], journal)
	}
}

func TestParseMarketingUploadBatchOptionsMapsFrozenContract(t *testing.T) {
	options, request, err := parseMarketingUploadBatchOptions([]string{
		"--config", " config.json ",
		"--accounts", "1001,1002", "--accounts", "1003",
		"--account-template", "1001=template-one", "--account-template", "1002=template-two",
		"--account-template", "1003=template-three",
		"--date", "2026-07-26", "--filename", " Drink.MP4 ",
		"--material-date", "7.26", "--product-name", " fixture product ",
		"--product-id", "3001", "--budget", "5000.25", "--bid", "1.25", "--roi-goal", "1.70",
		"--videos-per-unit", "5", "--max-videos", "12", "--start-index", "3",
		"--account-concurrency", "4", "--group-concurrency", "2", "--cover-concurrency", "6",
		"--cover-attempts", "4", "--cover-wait-sec", "0", "--page-size", "80",
		"--ad-get-batch-size", "40", "--no-validate-ad-get", "--no-skip-missing-cover",
		"--include-payloads", "--submit", "--out", " result.json ",
		"--channel", "marketing", "--auth-account-id", "9001",
	})
	if err != nil {
		t.Fatal(err)
	}
	if options.configPath != "config.json" || options.out != "result.json" ||
		!reflect.DeepEqual(request.Accounts, []string{"1001,1002", "1003"}) ||
		!reflect.DeepEqual(request.AccountTemplates, []string{
			"1001=template-one", "1002=template-two", "1003=template-three",
		}) || request.Date != "2026-07-26" || request.Filename != "Drink.MP4" ||
		request.MaterialDate != "7.26" || request.ProductName != "fixture product" || request.ProductID != "3001" ||
		request.VideosPerUnit != 5 || request.MaxVideos != 12 || request.StartIndex != 3 ||
		request.AccountConcurrency != 4 || request.GroupConcurrency != 2 || request.CoverConcurrency != 6 ||
		request.CoverAttempts != 4 || request.CoverWait != 0 || !request.CoverWaitSet ||
		request.PageSize != 80 || request.AdGetBatchSize != 40 || request.ValidateAdGet || request.SkipMissingCover ||
		!request.IncludePayloads || !request.Submit || request.Channel != "marketing" || request.AuthAccountID != "9001" {
		t.Fatalf("unexpected Marketing upload batch request: options=%#v request=%#v", options, request)
	}
	for label, value := range map[string]struct {
		got  any
		want string
	}{
		"budget": {request.Budget, "5000.25"},
		"bid":    {request.CPABid, "1.25"},
		"roi":    {request.ROIGoal, "1.70"},
	} {
		number, ok := value.got.(json.Number)
		if !ok || number.String() != value.want {
			t.Fatalf("%s = %#v, want exact %s", label, value.got, value.want)
		}
	}
}

func TestParseMarketingUploadBatchBooleanDefaultsAndValidation(t *testing.T) {
	_, request, err := parseMarketingUploadBatchOptions(nil)
	if err != nil {
		t.Fatal(err)
	}
	if request.Date != "today" || request.StartIndex != 1 || request.AccountConcurrency != 2 ||
		request.GroupConcurrency != 2 || request.CoverConcurrency != 4 || request.CoverAttempts != 8 ||
		request.CoverWait != 2*time.Second || !request.CoverWaitSet || request.PageSize != 100 ||
		request.AdGetBatchSize != 50 || !request.ValidateAdGet || !request.SkipMissingCover {
		t.Fatalf("Marketing upload batch defaults changed: %#v", request)
	}
	for _, args := range [][]string{
		{"--videos-per-unit", "0"}, {"--max-videos", "0"}, {"--start-index", "0"},
		{"--cover-attempts", "0"}, {"--cover-wait-sec", "NaN"},
		{"--validate-ad-get=false"}, {"--channel", "qianchuan"},
	} {
		if _, _, err := parseMarketingUploadBatchOptions(args); err == nil {
			t.Fatalf("invalid upload batch arguments were accepted: %#v", args)
		}
	}
}

func TestRunnerMarketingUploadBatchPreservesResultAndExitCode(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	outPath := filepath.Join(t.TempDir(), "upload-result.json")
	stdout := new(bytes.Buffer)
	service := &marketingUploadBatchServiceStub{result: applicationmarketing.UploadBatchResult{
		Mode: "submit", GeneratedAt: "2026-07-26T12:00:00", Config: "/fixture/config.json",
		Accounts: []applicationmarketing.UploadBatchAccount{{
			AdvertiserID: "1001", Status: "completed_with_errors", FailedGroupCount: 1,
		}},
		Totals:   applicationmarketing.UploadBatchTotals{AccountCount: 1, FailedGroupCount: 1},
		ExitCode: 1,
	}}
	runner := Runner{
		Cwd: t.TempDir(), UserHome: t.TempDir(), Getenv: func(string) string { return "" },
		MarketingPlans: MarketingPlanRuntime{UploadBatchService: service},
	}
	code := runner.runMarketingUploadBatch(
		context.Background(),
		[]string{"--config", "/fixture/config.json", "--accounts", "1001", "--plan-template", "template", "--submit", "--out", outPath},
		stateRoot, nil, stdout,
	)
	if code != 1 || service.calls != 1 || !service.last.Submit ||
		!reflect.DeepEqual(service.last.Accounts, []string{"1001"}) || service.last.PlanTemplate != "template" ||
		service.last.ConfigPath != "/fixture/config.json" {
		t.Fatalf("upload batch runner changed: exit=%d calls=%d request=%#v output=%s", code, service.calls, service.last, stdout.String())
	}
	written, err := os.ReadFile(outPath)
	if err != nil || !bytes.Equal(written, stdout.Bytes()) {
		t.Fatalf("upload batch --out differs from stdout: %v", err)
	}
	result := decodeSingleJSONObject(t, stdout.Bytes())
	if result["mode"] != "submit" || result["totals"].(map[string]any)["failed_group_count"] != float64(1) {
		t.Fatalf("upload batch partial result changed: %#v", result)
	}
}

func TestMarketingUploadBatchRejectsCrossChannelBeforeService(t *testing.T) {
	service := &marketingUploadBatchServiceStub{}
	stdout := new(bytes.Buffer)
	runner := Runner{MarketingPlans: MarketingPlanRuntime{UploadBatchService: service}}
	code := runner.runMarketingUploadBatch(
		context.Background(), []string{"--channel", "qianchuan"}, t.TempDir(), nil, stdout,
	)
	if code != 2 || service.calls != 0 {
		t.Fatalf("cross-channel upload batch crossed service boundary: exit=%d calls=%d output=%s", code, service.calls, stdout.String())
	}
}

func TestParseMarketingMutationOptionsMapsStableCommands(t *testing.T) {
	tests := []struct {
		action string
		args   []string
		kind   portmarketing.MutationKind
		ids    []string
		status string
		value  string
	}{
		{
			action: "update-project-status",
			args:   []string{"--advertiser-id", "1001", "--project-id", "2001,2002", "--status", "DISABLE"},
			kind:   portmarketing.MutationProjectStatus, ids: []string{"2001", "2002"}, status: "DISABLE",
		},
		{
			action: "update-promotion-status",
			args:   []string{"--advertiser-id", "1001", "--promotion-id", "3001", "--status", "ENABLE"},
			kind:   portmarketing.MutationPromotionStatus, ids: []string{"3001"}, status: "ENABLE",
		},
		{
			action: "update-budget",
			args:   []string{"--advertiser-id", "1001", "--promotion-id", "3001", "--value", "5000"},
			kind:   portmarketing.MutationPromotionBudget, ids: []string{"3001"}, value: "5000",
		},
		{
			action: "update-bid",
			args:   []string{"--advertiser-id", "1001", "--promotion-id", "3001", "--value", "1.5"},
			kind:   portmarketing.MutationPromotionBid, ids: []string{"3001"}, value: "1.5",
		},
		{
			action: "update-roi",
			args:   []string{"--advertiser-id", "1001", "--project-id", "2001", "--value", "1.7"},
			kind:   portmarketing.MutationProjectROI, ids: []string{"2001"}, value: "1.7",
		},
	}
	for _, test := range tests {
		t.Run(test.action, func(t *testing.T) {
			_, command, err := parseMarketingMutationOptions(test.action, test.args)
			if err != nil {
				t.Fatal(err)
			}
			if command.Kind != test.kind || !reflect.DeepEqual(command.ObjectIDs, test.ids) ||
				command.Status != test.status || command.Value != test.value {
				t.Fatalf("unexpected command mapping: %#v", command)
			}
		})
	}
}

func TestRunnerMarketingMutationDryRunNeedsNoRuntimeDependencies(t *testing.T) {
	routes := application.DefaultRouteManifest().Snapshot()
	routes["plans update-budget"] = application.RuntimeGo
	manifest, err := application.NewRouteManifest(6, routes)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	outPath := filepath.Join(root, "preview.json")
	stdout := new(bytes.Buffer)
	fallback := &fallbackSpy{code: 99}
	runner := Runner{
		Routes: manifest, Fallback: fallback, Stdout: stdout,
		Cwd: root, UserHome: root, Getenv: func(string) string { return "" },
	}
	code := runner.Execute(context.Background(), []string{
		"plans", "update-budget", "--advertiser-id", "1001",
		"--promotion-id", "3001", "--value", "5000", "--out", outPath,
	})
	if code != 0 {
		t.Fatalf("dry-run exit = %d: %s", code, stdout.String())
	}
	written, err := os.ReadFile(outPath)
	if err != nil || !bytes.Equal(written, stdout.Bytes()) {
		t.Fatalf("--out differs from stdout: %v", err)
	}
	result := decodeSingleJSONObject(t, stdout.Bytes())
	if result["mode"] != "dry_run" || result["channel"] != "marketing" ||
		result["endpoint"] != "/v3.0/promotion/budget/update/" {
		t.Fatalf("unexpected dry-run result: %#v", result)
	}
	if fallback.args != nil {
		t.Fatalf("Go Shadow route invoked Python fallback: %v", fallback.args)
	}
}

func TestRunnerMarketingMutationReturnsPartialFailureExitCode(t *testing.T) {
	routes := application.DefaultRouteManifest().Snapshot()
	routes["plans update-project-status"] = application.RuntimeGo
	manifest, err := application.NewRouteManifest(6, routes)
	if err != nil {
		t.Fatal(err)
	}
	service := &marketingMutationServiceStub{result: applicationmarketing.MutationResult{
		Mode: "submit", Channel: "marketing", Operation: portmarketing.MutationProjectStatus,
		Endpoint: "/v3.0/project/status/update/", AdvertiserID: "1001", Submitted: true,
		Rows: []applicationmarketing.MutationRow{
			{ObjectID: "2001", Status: "completed", Target: "DISABLE"},
			{ObjectID: "2002", Status: "failed", Target: "DISABLE", OfficialError: "fixture rejection"},
		},
		SuccessCount: 1, FailureCount: 1, ExitCode: 1,
	}}
	stdout := new(bytes.Buffer)
	runner := Runner{
		Routes: manifest, Fallback: &fallbackSpy{code: 99}, Stdout: stdout,
		Cwd: t.TempDir(), UserHome: t.TempDir(), Getenv: func(string) string { return "" },
		MarketingPlans: MarketingPlanRuntime{MutationService: service},
	}
	code := runner.Execute(context.Background(), []string{
		"plans", "update-project-status", "--advertiser-id", "1001",
		"--project-id", "2001", "--project-id", "2002", "--status", "DISABLE", "--submit",
	})
	if code != 1 || service.calls != 1 {
		t.Fatalf("partial failure exit = %d, service calls = %d: %s", code, service.calls, stdout.String())
	}
	result := decodeSingleJSONObject(t, stdout.Bytes())
	if result["success_count"] != float64(1) || result["failure_count"] != float64(1) {
		t.Fatalf("partial result changed: %#v", result)
	}
}

func TestMarketingMutationInvalidIDStopsBeforeCredentials(t *testing.T) {
	credentials := &marketingCredentialSpy{}
	stdout := new(bytes.Buffer)
	runner := Runner{MarketingPlans: MarketingPlanRuntime{Credentials: credentials}}
	code := runner.runMarketingPlan(
		context.Background(), "update-budget",
		[]string{"--advertiser-id", "bad", "--promotion-id", "3001", "--value", "5000", "--submit"},
		t.TempDir(), nil, stdout,
	)
	if code != 1 || credentials.calls != 0 {
		t.Fatalf("invalid ID crossed credential boundary: exit=%d calls=%d output=%s", code, credentials.calls, stdout.String())
	}
	if decodeSingleJSONObject(t, stdout.Bytes())["ok"] != false {
		t.Fatalf("invalid ID did not return JSON error: %s", stdout.String())
	}
}

func TestDefaultRouteKeepsMarketingPlansOnPython(t *testing.T) {
	for _, command := range []string{
		"plans create", "plans create-creator", "plans batch-upload", "plans batch-creator",
		"plans update-project-status", "plans update-promotion-status", "plans update-budget",
		"plans update-bid", "plans update-roi",
	} {
		runtime, ok := application.DefaultRouteManifest().RouteFor(command)
		if !ok || runtime != application.RuntimePython {
			t.Fatalf("default %s route = %q, want Python", command, runtime)
		}
	}
}

type marketingCreateServiceStub struct {
	result applicationmarketing.CreateResult
	err    error
	calls  int
	last   applicationmarketing.CreateRequest
}

func (stub *marketingCreateServiceStub) Execute(
	_ context.Context,
	request applicationmarketing.CreateRequest,
) (applicationmarketing.CreateResult, error) {
	stub.calls++
	stub.last = request
	return stub.result, stub.err
}

type marketingMutationServiceStub struct {
	result applicationmarketing.MutationResult
	err    error
	calls  int
}

type marketingCreatorBatchServiceStub struct {
	result applicationmarketing.CreatorBatchResult
	err    error
	calls  int
	last   applicationmarketing.CreatorBatchRequest
}

type marketingUploadBatchServiceStub struct {
	result applicationmarketing.UploadBatchResult
	err    error
	calls  int
	last   applicationmarketing.UploadBatchRequest
}

func (stub *marketingUploadBatchServiceStub) Execute(
	_ context.Context,
	request applicationmarketing.UploadBatchRequest,
) (applicationmarketing.UploadBatchResult, error) {
	stub.calls++
	stub.last = request
	return stub.result, stub.err
}

func (stub *marketingCreatorBatchServiceStub) Execute(
	_ context.Context,
	request applicationmarketing.CreatorBatchRequest,
) (applicationmarketing.CreatorBatchResult, error) {
	stub.calls++
	stub.last = request
	return stub.result, stub.err
}

func (stub *marketingMutationServiceStub) Execute(
	context.Context,
	applicationmarketing.MutationCommand,
) (applicationmarketing.MutationResult, error) {
	stub.calls++
	return stub.result, stub.err
}

type marketingCredentialSpy struct {
	calls int
}

func (spy *marketingCredentialSpy) AccessToken(
	context.Context,
	domainplans.Channel,
	string,
	string,
) (sharedplans.CredentialLease, error) {
	spy.calls++
	return sharedplans.CredentialLease{}, errors.New("credentials must not be reached")
}
