package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/legacy"
)

func legacyFixtureRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "runs"), 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestLegacyReaderReadOnlyAndRestrictedPaths(t *testing.T) {
	root := legacyFixtureRoot(t)
	name := filepath.Join(root, "runs", "source.json")
	payload := []byte(`{"synthetic":true}`)
	if err := os.WriteFile(name, payload, 0o400); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	reader := LegacyJournalReader{Root: root}
	got, err := reader.ReadLegacyJournal(context.Background(), "runs/source.json")
	if err != nil || string(got) != string(payload) {
		t.Fatalf("got %s err=%v", got, err)
	}
	info, err := os.Stat(name)
	if err != nil || info.Mode() != before.Mode() {
		t.Fatal("source permissions mutated")
	}
	for _, name := range []string{"../source.json", filepath.Join(root, "runs", "source.json"), "runs/../source.json", "runs/sub/source.json", `runs\source.json`} {
		if _, err := reader.ReadLegacyJournal(context.Background(), name); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "runs", "large.json"), []byte(strings.Repeat("x", legacy.MaxJournalBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadLegacyJournal(context.Background(), "runs/large.json"); err == nil {
		t.Fatal("oversize accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reader.ReadLegacyJournal(ctx, "runs/source.json"); err == nil {
		t.Fatal("cancel ignored")
	}
}

func TestLegacyReaderRejectsSymlinkFilesAndDirectories(t *testing.T) {
	root := legacyFixtureRoot(t)
	outside := legacyFixtureRoot(t)
	if err := os.WriteFile(filepath.Join(outside, "runs", "target.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "runs", "target.json"), filepath.Join(root, "runs", "link.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := (LegacyJournalReader{Root: root}).ReadLegacyJournal(context.Background(), "runs/link.json"); err == nil {
		t.Fatal("symlink file accepted")
	}
	linkedRoot := filepath.Join(root, "linked")
	if err := os.Symlink(outside, linkedRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := (LegacyJournalReader{Root: linkedRoot}).ReadLegacyJournal(context.Background(), "runs/target.json"); err == nil {
		t.Fatal("symlink root accepted")
	}
	parent := legacyFixtureRoot(t)
	if err := os.Remove(filepath.Join(parent, "runs")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "runs"), filepath.Join(parent, "runs")); err != nil {
		t.Fatal(err)
	}
	if _, err := (LegacyJournalReader{Root: parent}).ReadLegacyJournal(context.Background(), "runs/target.json"); err == nil {
		t.Fatal("symlink runs accepted")
	}
	if err := os.Mkdir(filepath.Join(root, "runs", "directory.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := (LegacyJournalReader{Root: root}).ReadLegacyJournal(context.Background(), "runs/directory.json"); err == nil {
		t.Fatal("directory accepted")
	}
}
