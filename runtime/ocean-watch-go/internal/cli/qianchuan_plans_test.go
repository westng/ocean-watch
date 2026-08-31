package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application"
	applicationmarketing "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans/marketing"
	applicationqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans/qianchuan"
	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
)

const qianchuanCLIPayload = `{"advertiser_id":1000000000000001,"aweme_id":4000000000000001,"name":"cli-plan","marketing_goal":"VIDEO_PROM_GOODS","product_ids":[5000000000000001],"delivery_setting":{"smart_bid_type":"SMART_BID_CUSTOM","roi2_goal":1.7,"budget":5000}}`

func TestParseQianchuanCreateReadsPayloadFromStdin(t *testing.T) {
	options, command, err := parseQianchuanCreateOptions(
		[]string{"--payload-file", "-", "--advertiser-id", "1000000000000001", "--submit", "--out", " result.json "},
		strings.NewReader("  "+qianchuanCLIPayload+"\n"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(command.Payload) != qianchuanCLIPayload || !command.Submit ||
		command.AdvertiserID != "1000000000000001" || options.out != "result.json" {
		t.Fatalf("stdin payload contract changed: options=%#v command=%#v", options, command)
	}
}

func TestParseQianchuanBatchReadsRuntimePlanNameFields(t *testing.T) {
	options, command, err := parseQianchuanBatchOptions([]string{
		"--plan-template", " qcpt-test ",
		"--work-url", " https://www.douyin.com/video/6000000000000001 ",
		"--plan-type", " 随手po ",
		"--business", " 刘研 ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if options.planType != "随手po" || options.business != "刘研" || len(command.Items) != 1 ||
		command.Items[0].PlanType != "随手po" || command.Items[0].Business != "刘研" {
		t.Fatalf("runtime plan-name fields changed: options=%#v command=%#v", options, command)
	}
}

func TestParseQianchuanBatchRowsProducesStructuredItems(t *testing.T) {
	items := parseQianchuanBatchItems([]string{
		"[https://v.douyin.com/bad/:code](https://v.douyin.com/abc/)\t真人口播营销\t测试负责人",
		"4.87 口令 https://v.douyin.com/xyz/ 复制打开\t9386\t暖身,口播\t刘研",
		"https://v.douyin.com/only/",
	}, "", "")
	if len(items) != 3 || items[0].InputIndex != 0 || items[0].WorkURL != "https://v.douyin.com/abc/" ||
		items[0].PlanType != "真人口播营销" || items[0].Business != "测试负责人" ||
		items[1].PlanType != "暖身,口播" || items[1].Business != "刘研" ||
		items[2].PlanType != "" || items[2].Business != "" {
		t.Fatalf("batch row parsing changed: %#v", items)
	}
}

func TestParseQianchuanBatchPreflightIDIsSubmitOnlyAndExclusive(t *testing.T) {
	options, command, err := parseQianchuanBatchOptions([]string{
		"--preflight-id", " qianchuan-preflight-fixture ", "--submit",
	})
	if err != nil {
		t.Fatal(err)
	}
	if options.preflightID != "qianchuan-preflight-fixture" ||
		command.PreflightID != "qianchuan-preflight-fixture" || !command.Submit {
		t.Fatalf("preflight submit parsing changed: options=%#v command=%#v", options, command)
	}
	for _, args := range [][]string{
		{"--preflight-id", "qianchuan-preflight-fixture"},
		{"--preflight-id", "qianchuan-preflight-fixture", "--submit", "--plan-template", "fixture"},
		{"--preflight-id", "qianchuan-preflight-fixture", "--submit", "--work-url", "https://v.douyin.com/fixture/"},
		{"--preflight-id", "qianchuan-preflight-fixture", "--submit", "--plan-type", "随手po"},
		{"--preflight-id", "qianchuan-preflight-fixture", "--submit", "--business", "测试负责人"},
	} {
		if _, _, err := parseQianchuanBatchOptions(args); err == nil {
			t.Fatalf("invalid preflight argument combination was accepted: %v", args)
		}
	}
}

func TestRunnerRoutesQianchuanPlanActionsWithoutMarketing(t *testing.T) {
	manifest := qianchuanPlanGoManifest(t, "plans create-qianchuan")
	qianchuan := &qianchuanPlanCommandServiceStub{createResult: applicationqianchuan.CreateCommandResult{
		Mode: "dry_run", Channel: "qianchuan", Status: "ready", Endpoint: applicationqianchuan.CreateEndpoint,
	}}
	marketing := &marketingCreateServiceStub{result: applicationmarketing.CreateResult{Mode: "dry_run", Status: "ready"}}
	stdout := new(bytes.Buffer)
	root := t.TempDir()
	runner := Runner{
		Routes: manifest, Stdout: stdout,
		Cwd: root, UserHome: root, Getenv: func(string) string { return "" },
		MarketingPlans: MarketingPlanRuntime{CreateService: marketing},
		QianchuanPlans: QianchuanPlanRuntime{Commands: qianchuan},
	}
	code := runner.Execute(context.Background(), []string{
		"plans", "create-qianchuan", "--payload-json", qianchuanCLIPayload,
	})
	if code != 0 || qianchuan.createCalls != 1 || marketing.calls != 0 {
		t.Fatalf("Qianchuan plan route crossed channel boundary: exit=%d qc=%d marketing=%d output=%s",
			code, qianchuan.createCalls, marketing.calls, stdout.String())
	}
	if qianchuan.lastCreate.ConfigPath == "" || !filepath.IsAbs(qianchuan.lastCreate.ConfigPath) {
		t.Fatalf("Qianchuan create config path is not absolute: %#v", qianchuan.lastCreate)
	}
}

func TestRunnerKeepsMarketingCreateOutOfQianchuanRuntime(t *testing.T) {
	manifest := qianchuanPlanGoManifest(t, "plans create")
	qianchuan := &qianchuanPlanCommandServiceStub{}
	marketing := &marketingCreateServiceStub{result: applicationmarketing.CreateResult{Mode: "dry_run", Status: "ready"}}
	stdout := new(bytes.Buffer)
	root := t.TempDir()
	runner := Runner{
		Routes: manifest, Stdout: stdout,
		Cwd: root, UserHome: root, Getenv: func(string) string { return "" },
		MarketingPlans: MarketingPlanRuntime{CreateService: marketing},
		QianchuanPlans: QianchuanPlanRuntime{Commands: qianchuan},
	}
	code := runner.Execute(context.Background(), []string{
		"plans", "create", "--advertiser-id", "1001", "--plan-template", "marketing-template", "--video-id", "2001",
	})
	if code != 0 || marketing.calls != 1 || len(qianchuan.calls) != 0 {
		t.Fatalf("Marketing plan route crossed into Qianchuan: exit=%d marketing=%d qc=%v output=%s",
			code, marketing.calls, qianchuan.calls, stdout.String())
	}
}

func TestRunnerRoutesQianchuanMutationBeforeReadRuntime(t *testing.T) {
	manifest := qianchuanPlanGoManifest(t, "qc-plans update-budget")
	mutations := &qianchuanMutationServiceStub{result: applicationqianchuan.MutationResult{
		Mode: "dry_run", Channel: "qianchuan", Operation: "budget",
		Endpoint:     "/v1.0/qianchuan/uni_promotion/ad/budget/update/",
		AdvertiserID: "1000000000000001",
	}}
	stdout := new(bytes.Buffer)
	root := t.TempDir()
	runner := Runner{
		Routes: manifest, Stdout: stdout,
		Cwd: root, UserHome: root, Getenv: func(string) string { return "" },
		QianchuanPlans: QianchuanPlanRuntime{Mutations: mutations},
	}
	code := runner.Execute(context.Background(), []string{
		"qc-plans", "update-budget", "--advertiser-id", "1000000000000001",
		"--ad-id", "2000000000000001", "--value", "5000",
	})
	if code != 0 || mutations.calls != 1 {
		t.Fatalf("Qianchuan mutation did not use write runtime: exit=%d calls=%d output=%s",
			code, mutations.calls, stdout.String())
	}
	if mutations.last.Kind != "budget" || !reflect.DeepEqual(mutations.last.AdIDs, []string{"2000000000000001"}) {
		t.Fatalf("Qianchuan mutation arguments changed: %#v", mutations.last)
	}
}

func TestRunnerWritesQianchuanPartialResultBeforeNonzeroExit(t *testing.T) {
	manifest := qianchuanPlanGoManifest(t, "plans create-qianchuan")
	service := &qianchuanPlanCommandServiceStub{
		createResult: applicationqianchuan.CreateCommandResult{
			Mode: "submit", Channel: "qianchuan", Status: "unknown",
			Endpoint: applicationqianchuan.CreateEndpoint, FailureStage: "plan_reconciliation",
			DispatchState: domainplans.DispatchUnknown, ExitCode: 1,
		},
		createErr: errors.New("synthetic reconciliation failure"),
	}
	root := t.TempDir()
	out := filepath.Join(root, "partial.json")
	stdout := new(bytes.Buffer)
	runner := Runner{
		Routes: manifest, Stdout: stdout,
		Cwd: root, UserHome: root, Getenv: func(string) string { return "" },
		QianchuanPlans: QianchuanPlanRuntime{Commands: service},
	}
	code := runner.Execute(context.Background(), []string{
		"plans", "create-qianchuan", "--payload-json", qianchuanCLIPayload, "--submit", "--out", out,
	})
	if code != 1 || service.createCalls != 1 {
		t.Fatalf("partial result exit=%d calls=%d output=%s", code, service.createCalls, stdout.String())
	}
	written, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(written, stdout.Bytes()) || !strings.Contains(stdout.String(), `"status": "unknown"`) ||
		strings.Contains(stdout.String(), `"ok": false`) {
		t.Fatalf("partial result was replaced or output diverged: stdout=%s file=%s", stdout.String(), written)
	}
}

func TestQianchuanWriteCommandsUseGoByDefault(t *testing.T) {
	for _, command := range []string{
		"plans create-qianchuan", "plans batch-qianchuan-works", "plans remove-qianchuan-work",
		"qc-plans update-status", "qc-plans update-budget", "qc-plans update-roi",
	} {
		runtime, ok := application.DefaultRouteManifest().RouteFor(command)
		if !ok || runtime != application.RuntimeGo {
			t.Fatalf("default production route for %s = %q, want Go", command, runtime)
		}
	}
}

func qianchuanPlanGoManifest(t *testing.T, commands ...string) application.RouteManifest {
	t.Helper()
	routes := application.DefaultRouteManifest().Snapshot()
	for _, command := range commands {
		routes[command] = application.RuntimeGo
	}
	manifest, err := application.NewRouteManifest(6, routes)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

type qianchuanPlanCommandServiceStub struct {
	calls        []string
	createCalls  int
	lastCreate   applicationqianchuan.CreatePlanCommand
	createResult applicationqianchuan.CreateCommandResult
	createErr    error
	batchResult  applicationqianchuan.BatchCommandResult
	batchErr     error
	removeResult applicationqianchuan.RemoveResult
	removeErr    error
}

func (stub *qianchuanPlanCommandServiceStub) CreatePlan(
	_ context.Context,
	command applicationqianchuan.CreatePlanCommand,
) (applicationqianchuan.CreateCommandResult, error) {
	stub.calls = append(stub.calls, "create")
	stub.createCalls++
	stub.lastCreate = command
	return stub.createResult, stub.createErr
}

func (stub *qianchuanPlanCommandServiceStub) BatchWorks(
	_ context.Context,
	_ applicationqianchuan.BatchWorksCommand,
) (applicationqianchuan.BatchCommandResult, error) {
	stub.calls = append(stub.calls, "batch")
	return stub.batchResult, stub.batchErr
}

func (stub *qianchuanPlanCommandServiceStub) RemoveWorks(
	_ context.Context,
	_ applicationqianchuan.RemoveWorksCommand,
) (applicationqianchuan.RemoveResult, error) {
	stub.calls = append(stub.calls, "remove")
	return stub.removeResult, stub.removeErr
}

type qianchuanMutationServiceStub struct {
	calls  int
	last   applicationqianchuan.MutationCommand
	result applicationqianchuan.MutationResult
	err    error
}

func (stub *qianchuanMutationServiceStub) Execute(
	_ context.Context,
	command applicationqianchuan.MutationCommand,
) (applicationqianchuan.MutationResult, error) {
	stub.calls++
	stub.last = command
	return stub.result, stub.err
}
