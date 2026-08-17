package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/requestcontrol"
)

func TestEveryCommandHasRequestBudgetProfile(t *testing.T) {
	for _, command := range contracts.Commands {
		profile, err := commandRequestBudgetProfile(command)
		if err != nil {
			t.Fatalf("%s has no request budget: %v", command.Name(), err)
		}
		if profile.unbounded {
			continue
		}
		if profile.limit < 0 || profile.limit > mutationCommandRequestLimit {
			t.Fatalf("%s request budget is invalid: %d", command.Name(), profile.limit)
		}
	}
}

func TestLocalCommandFamiliesHaveZeroNetworkBudget(t *testing.T) {
	for _, name := range []string{
		"setup doctor", "accounts list", "accounts add", "accounts remove",
		"accounts enable", "accounts disable", "runs list", "runs show",
		"templates list", "templates show", "qc-templates list",
	} {
		command, ok := commandByName(name)
		if !ok {
			t.Fatalf("fixture command is missing: %s", name)
		}
		profile, err := commandRequestBudgetProfile(command)
		if err != nil || profile.unbounded || profile.limit != 0 {
			t.Fatalf("%s network budget = %#v, %v; want bounded 0", name, profile, err)
		}
	}
}

func TestQianchuanBatchCommandsHaveUnboundedRequestCounter(t *testing.T) {
	for _, name := range []string{"plans batch-qianchuan-works", "plans remove-qianchuan-work"} {
		command, ok := commandByName(name)
		if !ok {
			t.Fatalf("fixture command is missing: %s", name)
		}
		profile, err := commandRequestBudgetProfile(command)
		if err != nil || !profile.unbounded {
			t.Fatalf("%s request profile = %#v, %v; want unbounded", name, profile, err)
		}
	}
}

func TestQianchuanBatchProfileCountsPastFormerLimit(t *testing.T) {
	command, ok := commandByName("plans batch-qianchuan-works")
	if !ok {
		t.Fatal("fixture command is missing")
	}
	profile, err := commandRequestBudgetProfile(command)
	if err != nil || !profile.unbounded {
		t.Fatalf("unexpected request profile: %#v, %v", profile, err)
	}
	ctx, budget, _, err := requestcontrol.PrepareUnboundedCommandContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for range 600 {
		if err := requestcontrol.ReserveAttempt(ctx); err != nil {
			t.Fatalf("batch request counter rejected an attempt: %v", err)
		}
	}
	if snapshot := budget.Snapshot(); snapshot != (requestcontrol.BudgetSnapshot{Used: 600, Unbounded: true}) {
		t.Fatalf("unexpected batch request snapshot: %#v", snapshot)
	}
}

func TestRunnerObserverReportsZeroNetworkUseForAccountList(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	config := `{"managed_account_schema_version":1,"managed_accounts":{"marketing":[{"advertiser_id":"1000000000000001","name":"Fixture","enabled":true}],"qianchuan":[]}}`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	var command string
	var budget requestcontrol.BudgetSnapshot
	var metrics requestcontrol.MetricsSnapshot
	runner := Runner{
		Routes: application.DefaultRouteManifest(), Stdout: new(bytes.Buffer),
		Cwd: root, UserHome: root, Getenv: func(string) string { return "" },
		RequestObserver: func(observed string, budgetSnapshot requestcontrol.BudgetSnapshot, metricsSnapshot requestcontrol.MetricsSnapshot) {
			command, budget, metrics = observed, budgetSnapshot, metricsSnapshot
		},
	}
	if exitCode := runner.Execute(context.Background(), []string{"accounts", "list", "--config", configPath}); exitCode != 0 {
		t.Fatalf("accounts list exit = %d", exitCode)
	}
	if command != "accounts list" || budget != (requestcontrol.BudgetSnapshot{}) || metrics != (requestcontrol.MetricsSnapshot{}) {
		t.Fatalf("unexpected request snapshot: command=%q budget=%#v metrics=%#v", command, budget, metrics)
	}
}

func commandByName(name string) (contracts.Command, bool) {
	for _, command := range contracts.Commands {
		if command.Name() == name {
			return command, true
		}
	}
	return contracts.Command{}, false
}
