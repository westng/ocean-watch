package cli

import (
	"bytes"
	"context"
	"testing"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application"
	applicationqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans/qianchuan"
)

func TestParseQianchuanBindingCommandsRequireCompleteIdentity(t *testing.T) {
	base := []string{
		"--advertiser-id", "1000000000000001", "--template-id", "qcpt_fixture",
		"--creator-id", "4000000000000001", "--douyin-id", "creator-visible",
		"--product-id", "5000000000000002,5000000000000001",
		"--plan-type", " 随手po ", "--business", " 测试商务 ", "--business-date", "2026-08-18",
	}
	options, audit, _, err := parseQianchuanBindingOptions("binding-audit", base)
	if err != nil {
		t.Fatal(err)
	}
	if audit.PlanType != "随手po" || audit.Business != "测试商务" || audit.BusinessDate != "2026-08-18" ||
		len(audit.ProductIDs) != 2 || options.submit {
		t.Fatalf("binding audit parse=%#v options=%#v", audit, options)
	}
	bindArgs := append(append([]string(nil), base...),
		"--group-id", "qcg_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"--ad-id", "2000000000000001", "--submit",
	)
	_, _, bind, err := parseQianchuanBindingOptions("bind", bindArgs)
	if err != nil || !bind.Submit || bind.AdID != "2000000000000001" {
		t.Fatalf("binding submit parse=%#v err=%v", bind, err)
	}
	for _, invalid := range [][]string{
		{"--advertiser-id", "1000000000000001"},
		append(append([]string(nil), base...), "--group-id", bind.GroupID),
		append(append([]string(nil), base...), "--ad-id", bind.AdID),
	} {
		if _, _, _, err := parseQianchuanBindingOptions("bind", invalid); err == nil {
			t.Fatalf("incomplete binding command was accepted: %v", invalid)
		}
	}
}

func TestRunnerRoutesBindingCommandsToLocalBindingService(t *testing.T) {
	stub := &qianchuanBindingServiceStub{
		audit: applicationqianchuan.BindingAuditResult{Mode: "audit", Channel: "qianchuan", Status: "legacy_binding_required"},
	}
	root := t.TempDir()
	stdout := new(bytes.Buffer)
	runner := Runner{
		Routes: application.DefaultRouteManifest(), Stdout: stdout, Cwd: root, UserHome: root,
		Getenv:         func(string) string { return "" },
		QianchuanPlans: QianchuanPlanRuntime{PlanBindings: stub},
	}
	code := runner.Execute(context.Background(), append([]string{"qc-plans", "binding-audit"},
		"--advertiser-id", "1000000000000001", "--template-id", "qcpt_fixture",
		"--creator-id", "4000000000000001", "--product-id", "5000000000000001",
		"--business-date", "2026-08-18",
	))
	if code != 0 || stub.auditCalls != 1 || stub.bindCalls != 0 ||
		!bytes.Contains(stdout.Bytes(), []byte(`"legacy_binding_required"`)) {
		t.Fatalf("binding audit route code=%d stub=%#v output=%s", code, stub, stdout)
	}
}

type qianchuanBindingServiceStub struct {
	auditCalls int
	bindCalls  int
	audit      applicationqianchuan.BindingAuditResult
	bind       applicationqianchuan.BindPlanResult
}

func (stub *qianchuanBindingServiceStub) Audit(_ context.Context, _ applicationqianchuan.BindingAuditCommand) (applicationqianchuan.BindingAuditResult, error) {
	stub.auditCalls++
	return stub.audit, nil
}

func (stub *qianchuanBindingServiceStub) Bind(_ context.Context, _ applicationqianchuan.BindPlanCommand) (applicationqianchuan.BindPlanResult, error) {
	stub.bindCalls++
	return stub.bind, nil
}
