package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestConfigStoreReadIsWriteFree(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	payload := []byte("{\n  \"unknown\": \"preserved\"\n}\n")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (ConfigStore{Path: path}).Read(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("read changed config modification time")
	}
	if _, err := os.Stat(path + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read created a process lock")
	}
}

func TestConfigStoreInitializeCreatesOnceAndForceKeepsBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	store := ConfigStore{Path: path}
	created, err := store.Initialize(context.Background(), map[string]any{"owner": "first"}, false)
	if err != nil || !created {
		t.Fatalf("initial create = %v, %v", created, err)
	}
	created, err = store.Initialize(context.Background(), map[string]any{"owner": "ignored"}, false)
	if err != nil || created {
		t.Fatalf("second create = %v, %v", created, err)
	}
	current, err := store.Read(context.Background())
	if err != nil || current["owner"] != "first" {
		t.Fatalf("existing config changed: %#v, %v", current, err)
	}
	created, err = store.Initialize(context.Background(), map[string]any{"owner": "forced"}, true)
	if err != nil || !created {
		t.Fatalf("force create = %v, %v", created, err)
	}
	backup, err := readJSON(path + ".bak")
	if err != nil || backup["owner"] != "first" {
		t.Fatalf("unexpected backup: %#v, %v", backup, err)
	}
	current, err = store.Read(context.Background())
	if err != nil || current["owner"] != "forced" {
		t.Fatalf("force config missing: %#v, %v", current, err)
	}
}

func TestConfigStorePreservesOpaqueCIDLinkStrings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	want := map[string]any{
		"links": map[string]any{
			"landing_page_url": "  custom+cid://landing?repeat=1&repeat=2&TODO=保留&encoded=%2f%2F  ",
			"open_url":         " openapp.custom://opaque?params=%7B%22url%22%3A%22a%252Fb%22%7D ",
		},
		"tracking_urls": map[string]any{
			"track_url":        []any{" https://any-host.invalid/display?kind=click&unknown=1&unknown=2 "},
			"action_track_url": []any{" custom-track://click?kind=impress&REPLACE_WITH=value&raw=%26%3D "},
		},
	}
	store := ConfigStore{Path: path}
	created, err := store.Initialize(context.Background(), want, false)
	if err != nil || !created {
		t.Fatalf("initialize = %v, %v", created, err)
	}
	got, err := store.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("opaque CID links changed after save/load: got %#v want %#v", got, want)
	}
}

func TestConfigStoreUpdatePreservesUnknownFieldsAndBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := []byte("{\n  \"unknown\": {\"nested\": true},\n  \"counter\": 1\n}\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := (ConfigStore{Path: path}).Update(
		context.Background(),
		func(config map[string]any) (any, bool, error) {
			config["counter"] = 2
			return "updated", true, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result != "updated" {
		t.Fatalf("unexpected update result: %#v", result)
	}
	updated, err := (ConfigStore{Path: path}).Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if updated["unknown"].(map[string]any)["nested"] != true {
		t.Fatal("unknown field was lost")
	}
	backup, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != string(original) {
		t.Fatal("backup is not the exact previous file")
	}
}

func TestConfigStoreUnchangedUpdateDoesNotWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	payload := []byte("{\"value\":1}\n")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if _, err := (ConfigStore{Path: path}).Update(
		context.Background(),
		func(map[string]any) (any, bool, error) { return nil, false, nil },
	); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("unchanged update rewrote config")
	}
	if _, err := os.Stat(path + ".bak"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unchanged update wrote a backup")
	}
}

func TestConfigStoreSerializesConcurrentUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{\"counter\":0}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := ConfigStore{Path: path, LockTimeout: 5 * time.Second}
	const workers = 40
	var wait sync.WaitGroup
	wait.Add(workers)
	errorsFound := make(chan error, workers)
	for range workers {
		go func() {
			defer wait.Done()
			_, err := store.Update(context.Background(), func(config map[string]any) (any, bool, error) {
				current, conversionErr := config["counter"].(json.Number).Int64()
				if conversionErr != nil {
					return nil, false, conversionErr
				}
				config["counter"] = current + 1
				return nil, true, nil
			})
			if err != nil {
				errorsFound <- err
			}
		}()
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Fatal(err)
	}
	config, err := store.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if config["counter"].(json.Number).String() != "40" {
		t.Fatalf("concurrent updates lost data: %#v", config["counter"])
	}
}

func TestConfigStoreCompareAndSwapRejectsStaleRevision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{\"value\":1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := ConfigStore{Path: path}
	current, err := store.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	revision, err := JSONRevision(current)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(context.Background(), func(config map[string]any) (any, bool, error) {
		config["value"] = 2
		return nil, true, nil
	}); err != nil {
		t.Fatal(err)
	}
	current["value"] = 3
	err = store.CompareAndSwap(context.Background(), revision, current)
	if err == nil || err.Error() != "configuration changed while this operation was running; reload and retry" {
		t.Fatalf("unexpected compare-and-swap error: %v", err)
	}
	latest, err := store.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if latest["value"].(json.Number).String() != "2" {
		t.Fatal("stale compare-and-swap overwrote current config")
	}
}

func TestJSONRevisionMatchesPythonCanonicalEncoding(t *testing.T) {
	value := map[string]any{
		"中文": "值",
		"a": []any{
			json.Number("1"),
			json.Number("1.0"),
			map[string]any{"z": false, "b": nil},
		},
		"nested": map[string]any{"x": "<tag>", "n": json.Number("1e-06")},
	}
	revision, err := JSONRevision(value)
	if err != nil {
		t.Fatal(err)
	}
	const pythonRevision = "e5f47b4b0cc46bfaec86c17001c2cf764f7bd749c0b2001dab00ded8f9df6d16"
	if revision != pythonRevision {
		t.Fatalf("Go revision %s does not match Python %s", revision, pythonRevision)
	}
}

func TestAtomicWriteReplaceFailureKeepsOriginalAndCleansTemporary(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")
	original := []byte("original\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	err := atomicWriteBytesWithReplace(path, []byte("updated\n"), func(string, string) error {
		return errors.New("injected replace failure")
	})
	if err == nil {
		t.Fatal("replace failure was ignored")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(original) {
		t.Fatal("replace failure changed original file")
	}
	matches, err := filepath.Glob(filepath.Join(directory, ".config.json.*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files were not cleaned: %#v", matches)
	}
}
