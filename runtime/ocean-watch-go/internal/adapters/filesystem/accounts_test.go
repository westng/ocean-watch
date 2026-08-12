package filesystem

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

func TestAccountStoreReadIsWriteFree(t *testing.T) {
	path := writeFixtureConfig(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	book, err := (AccountStore{Path: path}).Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(book.List(nil, true)) != 1 {
		t.Fatal("unexpected enabled account count")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	afterInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || !beforeInfo.ModTime().Equal(afterInfo.ModTime()) {
		t.Fatal("account list changed local state")
	}
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Fatal("read-only account list created a lock")
	}
}

func TestAccountStorePreservesUnknownFieldsAndBackup(t *testing.T) {
	path := writeFixtureConfig(t)
	store := AccountStore{Path: path}
	_, err := store.Update(context.Background(), func(book *domain.AccountBook) error {
		_, _, updateErr := book.Upsert(domain.ManagedAccount{
			Channel: domain.Qianchuan, AdvertiserID: "1000000000000002",
			Name: "千川账户", Enabled: true,
		})
		return updateErr
	})
	if err != nil {
		t.Fatal(err)
	}
	var updated map[string]any
	decodeJSONFile(t, path, &updated)
	if updated["future_root"] != "preserved" {
		t.Fatal("root unknown field was lost")
	}
	groups := updated["managed_accounts"].(map[string]any)
	marketing := groups["marketing"].([]any)[0].(map[string]any)
	if marketing["future_record"] != "preserved" {
		t.Fatal("record unknown field was lost")
	}
	var backup map[string]any
	decodeJSONFile(t, path+".bak", &backup)
	if backup["future_root"] != "preserved" {
		t.Fatal("backup did not preserve previous state")
	}
}

func TestAccountStoreRejectsNonBooleanEnabledValues(t *testing.T) {
	for _, value := range []string{`"false"`, `null`, `0`} {
		path := filepath.Join(t.TempDir(), "config.json")
		payload := `{"managed_accounts":{"marketing":[{"advertiser_id":"1000000000000001","name":"account","enabled":` + value + `}],"qianchuan":[]}}`
		if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := (AccountStore{Path: path}).Read(context.Background()); err == nil ||
			!strings.Contains(err.Error(), "enabled must be a boolean") {
			t.Fatalf("enabled=%s produced unexpected error: %v", value, err)
		}
	}
}

func TestFileLockContenderTimesOutWithoutDeletingLockFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json.lock")
	first, err := AcquireLock(context.Background(), path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	start := time.Now()
	_, err = AcquireLock(context.Background(), path, 100*time.Millisecond)
	if err == nil {
		t.Fatal("lock contender unexpectedly succeeded")
	}
	if time.Since(start) < 90*time.Millisecond {
		t.Fatal("lock contender failed without waiting")
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatal("advisory lock file was deleted")
	}
}

func writeFixtureConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	payload := `{
  "future_root": "preserved",
  "managed_account_schema_version": 1,
  "managed_accounts": {
    "marketing": [{
      "advertiser_id": "1000000000000001",
      "name": "营销账户",
      "enabled": true,
      "future_record": "preserved"
    }],
    "qianchuan": []
  }
}
`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func decodeJSONFile(t *testing.T, path string, target any) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(content, target); err != nil {
		t.Fatal(err)
	}
}
