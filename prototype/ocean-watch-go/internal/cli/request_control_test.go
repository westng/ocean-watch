package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/contracts"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/requestcontrol"
)

func TestEveryCommandHasBoundedRequestBudgetProfile(t *testing.T) {
	for _, command := range contracts.Commands {
		limit, err := commandRequestLimit(command)
		if err != nil {
			t.Fatalf("%s has no request budget: %v", command.Name(), err)
		}
		if limit < 0 || limit > mutationCommandRequestLimit {
			t.Fatalf("%s request budget is invalid: %d", command.Name(), limit)
		}
	}
}

func TestLocalCommandFamiliesHaveZeroNetworkBudget(t *testing.T) {
	for _, name := range []string{
		"setup doctor", "accounts list", "accounts add", "accounts remove",
		"accounts enable", "accounts disable", "runs list", "runs show",
		"templates list", "templates show", "qc-templates list", "auth migrate",
	} {
		command, ok := commandByName(name)
		if !ok {
			t.Fatalf("fixture command is missing: %s", name)
		}
		limit, err := commandRequestLimit(command)
		if err != nil || limit != 0 {
			t.Fatalf("%s network budget = %d, %v; want 0", name, limit, err)
		}
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
