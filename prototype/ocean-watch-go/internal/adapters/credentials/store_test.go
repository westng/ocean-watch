package credentials

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
)

func TestCredentialCompatibilityIdentifiers(t *testing.T) {
	if Service != "ads-plan-monitor" || LegacyAccount != "oceanengine-oauth" {
		t.Fatalf("legacy credential identity changed: service=%q account=%q", Service, LegacyAccount)
	}
	for _, test := range []struct {
		channel, authorizationID string
		appAccount               string
		authorizationAccount     string
	}{
		{
			channel: "marketing", authorizationID: "marketing_fixture",
			appAccount:           "oceanengine-app-marketing",
			authorizationAccount: "oceanengine-auth-marketing-marketing_fixture-r2",
		},
		{
			channel: "qianchuan", authorizationID: "qianchuan_fixture",
			appAccount:           "oceanengine-app-qianchuan",
			authorizationAccount: "oceanengine-auth-qianchuan-qianchuan_fixture-r2",
		},
	} {
		appAccount, err := domain.AppCredentialAccount(test.channel)
		if err != nil || appAccount != test.appAccount {
			t.Fatalf("%s app credential = %q, %v", test.channel, appAccount, err)
		}
		authorizationAccount, err := domain.AuthorizationCredentialAccount(test.channel, test.authorizationID, 2)
		if err != nil || authorizationAccount != test.authorizationAccount {
			t.Fatalf("%s authorization credential = %q, %v", test.channel, authorizationAccount, err)
		}
	}
}

func TestCredentialBackendSelectionMatrix(t *testing.T) {
	found := func(command string) (string, error) { return "/fixture/" + command, nil }
	missing := func(string) (string, error) { return "", errors.New("missing") }
	emptyEnvironment := func(string) string { return "" }
	fallbackEnvironment := func(name string) string {
		if name == InsecureFileFallbackEnv {
			return "1"
		}
		return ""
	}
	for _, test := range []struct {
		name, goos, want string
		getenv           func(string) string
		lookPath         func(string) (string, error)
	}{
		{"macOS Keychain", "darwin", BackendMacOSKeychain, emptyEnvironment, found},
		{"Windows DPAPI", "windows", BackendWindowsDPAPI, emptyEnvironment, missing},
		{"Linux Secret Service", "linux", BackendLinuxSecretService, emptyEnvironment, found},
		{"explicit fallback", "linux", BackendFileFallback, fallbackEnvironment, missing},
		{"unavailable", "linux", BackendUnavailable, emptyEnvironment, missing},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := backendName(test.goos, test.getenv, test.lookPath); got != test.want {
				t.Fatalf("backend = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCredentialBackendsUsePythonServiceAndAccountNames(t *testing.T) {
	account := "oceanengine-auth-qianchuan-fixture-r3"
	if got, want := macOSReadArguments(account), []string{
		"find-generic-password", "-s", "ads-plan-monitor", "-a", account, "-w",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("macOS lookup = %v, want %v", got, want)
	}
	if got, want := macOSDeleteArguments(account), []string{
		"delete-generic-password", "-s", "ads-plan-monitor", "-a", account,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("macOS delete = %v, want %v", got, want)
	}
	if got, want := macOSWriteArguments(account, "fixture-payload"), []string{
		"add-generic-password", "-U", "-s", "ads-plan-monitor", "-a", account,
		"-w", "fixture-payload",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("macOS write = %v, want %v", got, want)
	}
	if got, want := linuxReadArguments(account), []string{
		"lookup", "service", "ads-plan-monitor", "account", account,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Linux lookup = %v, want %v", got, want)
	}
	if got, want := linuxWriteArguments(account), []string{
		"store", "--label", "Ads Plan Monitor OceanEngine OAuth",
		"service", "ads-plan-monitor", "account", account,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Linux write = %v, want %v", got, want)
	}
	store := Store{Root: "/fixture/root"}
	if got := store.filePath(account, ".dpapi"); got != filepath.Join("/fixture/root", account+".dpapi") {
		t.Fatalf("Windows credential path = %q", got)
	}
	if got := store.filePath(LegacyAccount, ".json"); got != filepath.Join("/fixture/root", "credentials.json") {
		t.Fatalf("legacy fallback path = %q", got)
	}
}
