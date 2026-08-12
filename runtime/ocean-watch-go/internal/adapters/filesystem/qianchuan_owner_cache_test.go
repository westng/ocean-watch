package filesystem

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	applicationqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans/qianchuan"
)

func TestQianchuanOwnerHintCacheMatchesStableSchemaTTLAndLock(t *testing.T) {
	now := time.Date(2026, time.July, 26, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "cache", "qianchuan-work-owners.json")
	fixture := map[string]any{
		"schema_version": 1,
		"advertisers": map[string]any{
			"1000000000000001": map[string]any{
				"6000000000000001": map[string]any{
					"aweme_id": "4000000000000001", "aweme_show_id": "visible-1",
					"updated_at": now.AddDate(0, 0, -29).Format(time.RFC3339),
				},
				"6000000000000002": map[string]any{
					"aweme_id": "4000000000000002", "aweme_show_id": nil,
					"updated_at": now.AddDate(0, 0, -31).Format(time.RFC3339),
				},
			},
		},
	}
	payload, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := AtomicWritePrivateFile(path, append(payload, '\n')); err != nil {
		t.Fatal(err)
	}
	cache := QianchuanOwnerHintCache{Path: path, Now: func() time.Time { return now }}
	hints, err := cache.Load(context.Background(), "1000000000000001", []string{
		"6000000000000001", "6000000000000002",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hints) != 1 || hints["6000000000000001"].AwemeShowID != "visible-1" {
		t.Fatalf("TTL or stable schema compatibility changed: %#v", hints)
	}
	stored, err := cache.Store(context.Background(), "1000000000000001", map[string]applicationqianchuan.OwnerHint{
		"6000000000000003": {AwemeID: "4000000000000003", AwemeShowID: "visible-3"},
	})
	if err != nil || stored != 1 {
		t.Fatalf("store result=%d err=%v", stored, err)
	}
	if _, err := os.Stat(path + ".lock"); err != nil {
		t.Fatalf("stable lock suffix is missing: %v", err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(written, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["schema_version"] != float64(1) {
		t.Fatalf("cache schema changed: %#v", decoded)
	}
	reloaded, err := cache.Load(context.Background(), "1000000000000001", []string{"6000000000000003"})
	if err != nil || reloaded["6000000000000003"].AwemeID != "4000000000000003" {
		t.Fatalf("Go cache did not round-trip through shared schema: %#v err=%v", reloaded, err)
	}
}

func TestQianchuanOwnerHintCacheRejectsMalformedDataWithoutWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "qianchuan-work-owners.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"advertisers":`), 0o600); err != nil {
		t.Fatal(err)
	}
	cache := QianchuanOwnerHintCache{Path: path}
	if _, err := cache.Load(context.Background(), "1000000000000001", []string{"6000000000000001"}); err == nil {
		t.Fatal("malformed cache was silently treated as a verified hint")
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Store(context.Background(), "1000000000000001", map[string]applicationqianchuan.OwnerHint{
		"6000000000000001": {AwemeID: "4000000000000001"},
	}); err == nil {
		t.Fatal("malformed cache was overwritten without surfacing a warning")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatalf("malformed cache changed: err=%v before=%q after=%q", err, before, after)
	}
}
