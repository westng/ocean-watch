package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestRunStoreListsSummariesAndUnreadableJournals(t *testing.T) {
	root := t.TempDir()
	valid := filepath.Join(root, "creator-batch-abc.json")
	writeRunFixture(t, valid, `{"schema_version":1,"created_at":"2026-07-25T00:00:00Z","jobs":{"one":{"status":"completed"},"two":{}}}`)
	writeRunFixture(t, filepath.Join(root, "creator-batch-bad.json"), `{"jobs":[]}`)
	modified := time.Unix(1_700_000_000, 123_000_000)
	if err := os.Chtimes(valid, modified, modified); err != nil {
		t.Fatal(err)
	}

	rows, err := (RunStore{Root: root}).List(context.Background(), 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows", len(rows))
	}
	if rows[0].RunID != "creator-batch-abc" || rows[0].Kind != "creator-batch" {
		t.Fatalf("unexpected valid summary: %#v", rows[0])
	}
	if !reflect.DeepEqual(rows[0].StatusCounts, map[string]int{"completed": 1, "unknown": 1}) {
		t.Fatalf("unexpected status counts: %#v", rows[0].StatusCounts)
	}
	if rows[1].Readable == nil || *rows[1].Readable {
		t.Fatalf("invalid journal was not marked unreadable: %#v", rows[1])
	}
}

func TestRunStoreShowRejectsTraversalAndSymbolicLinks(t *testing.T) {
	root := t.TempDir()
	store := RunStore{Root: root}
	if _, _, err := store.Show(context.Background(), "../secret"); err == nil || err.Error() != "run_id contains unsupported characters" {
		t.Fatalf("unexpected traversal error: %v", err)
	}
	target := filepath.Join(t.TempDir(), "target.json")
	writeRunFixture(t, target, `{"jobs":{}}`)
	link := filepath.Join(root, "creator-batch-link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	if _, _, err := store.Show(context.Background(), "creator-batch-link"); err == nil || err.Error() != "run journal symbolic links are not supported" {
		t.Fatalf("unexpected symbolic link error: %v", err)
	}
}

func TestRunStoreShowClassifiesNotFoundUnreadableAndInvalidSchema(t *testing.T) {
	root := t.TempDir()
	store := RunStore{Root: root}
	_, _, err := store.Show(context.Background(), "missing-run")
	var notFound *RunNotFoundError
	if !errors.As(err, &notFound) || notFound.RunID != "missing-run" {
		t.Fatalf("unexpected not-found error: %v", err)
	}
	writeRunFixture(t, filepath.Join(root, "broken-run.json"), `{`)
	_, _, err = store.Show(context.Background(), "broken-run")
	var unreadable *RunUnreadableError
	if !errors.As(err, &unreadable) {
		t.Fatalf("unexpected unreadable error: %v", err)
	}
	writeRunFixture(t, filepath.Join(root, "invalid-run.json"), `{"jobs":{"one":"invalid"}}`)
	_, _, err = store.Show(context.Background(), "invalid-run")
	var schema *RunSchemaError
	if !errors.As(err, &schema) {
		t.Fatalf("unexpected schema error: %v", err)
	}
}

func TestRunSummaryJSONMatchesReadableAndUnreadableShapes(t *testing.T) {
	root := t.TempDir()
	writeRunFixture(t, filepath.Join(root, "creator-batch-empty.json"), `{"jobs":{}}`)
	rows, err := (RunStore{Root: root}).List(context.Background(), 50)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(rows[0])
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(payload, &value); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"run_id", "kind", "schema_version", "created_at", "fingerprint", "job_count", "status_counts", "updated_at"} {
		if _, ok := value[key]; !ok {
			t.Fatalf("valid summary omitted %s: %s", key, payload)
		}
	}
}

func writeRunFixture(t *testing.T, path, payload string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
}
