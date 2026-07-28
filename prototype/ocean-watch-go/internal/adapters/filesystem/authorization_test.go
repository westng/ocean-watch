package filesystem

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAuthorizationStoreCommitsPythonCompatibleGeneration(t *testing.T) {
	root := t.TempDir()
	store := AuthorizationStore{Root: root}
	state := map[string]any{
		"generation": json.Number("1"),
		"future":     "中文<&字段",
		"authorizations": map[string]any{
			"fixture_auth": map[string]any{"token_revision": json.Number("1")},
		},
		"account_index":    map[string]any{},
		"advertiser_index": map[string]any{"1000000000000001": []any{"fixture_auth"}},
	}
	if err := store.CommitChannel(context.Background(), "marketing", state); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadChannel(context.Background(), "marketing")
	if err != nil {
		t.Fatal(err)
	}
	if loaded["future"] != "中文<&字段" {
		t.Fatalf("unknown field was lost: %#v", loaded)
	}
	current, err := readJSONObject(filepath.Join(root, "channels", "marketing", "current.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := readJSONObject(filepath.Join(root, "channels", "marketing", "manifest-1.json"))
	if err != nil {
		t.Fatal(err)
	}
	digest, err := authorizationManifestChecksum(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if current["sha256"] != digest {
		t.Fatalf("checksum mismatch: %#v", current)
	}
	if info, err := os.Stat(filepath.Join(root, "channels", "marketing", "current.json")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("current pointer permissions: %v, %v", info, err)
	}
}

func TestAuthorizationStoreRejectsCrossChannelAndCorruptPointer(t *testing.T) {
	store := AuthorizationStore{Root: t.TempDir()}
	if err := store.CommitChannel(context.Background(), "unknown", map[string]any{}); err == nil {
		t.Fatal("expected unsupported channel error")
	}
	root := filepath.Join(store.Root, "channels", "qianchuan")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWritePrivateJSON(filepath.Join(root, "manifest-1.json"), map[string]any{"generation": 1}); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWritePrivateJSON(filepath.Join(root, "current.json"), map[string]any{
		"schema_version": 2, "generation": 1, "sha256": "wrong",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadChannel(context.Background(), "qianchuan"); err == nil {
		t.Fatal("expected checksum failure")
	}
}

func TestAuthorizationStoreReturnsSanitizedRevisionSummaryWithoutCredentials(t *testing.T) {
	root := t.TempDir()
	store := AuthorizationStore{Root: root}
	state := map[string]any{
		"generation": json.Number("7"),
		"authorizations": map[string]any{
			"auth_fixture": map[string]any{
				"token_revision":           json.Number("4"),
				"pending_account_sync":     true,
				"account_discovery_issues": []any{map[string]any{"code": "fixture_partial"}},
				"advertiser_ids":           []any{"1000000000000001"},
				"authorized_accounts":      []any{map[string]any{"account_id": "9000000000000001"}},
				"future_secret_like_field": "must-not-be-projected",
			},
		},
		"account_index":    map[string]any{"9000000000000001": "auth_fixture"},
		"advertiser_index": map[string]any{"1000000000000001": []any{"auth_fixture"}},
	}
	if err := store.CommitChannel(context.Background(), "marketing", state); err != nil {
		t.Fatal(err)
	}
	summary, err := store.ReadChannel(context.Background(), "marketing")
	if err != nil {
		t.Fatal(err)
	}
	if summary.AuthorizationCount != 1 || summary.AuthorizedAccountCount != 1 || summary.Generation != 7 ||
		summary.PendingAccountSyncCount != 1 || summary.PartialAccountDiscoveryCount != 1 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	if len(summary.Authorizations) != 1 || summary.Authorizations[0].TokenRevision != 4 ||
		summary.Authorizations[0].AccountDiscoveryComplete {
		t.Fatalf("unexpected authorization row: %#v", summary.Authorizations)
	}
}
