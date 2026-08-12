package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestQianchuanRequestControllerReadsSharedStateAndWaitsForCooldown(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "request-control", "qianchuan-123.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		statePath,
		[]byte("{\"next_request_at\":100.25,\"cooldown_until\":103.0}\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(100, 0).UTC()
	var sleeps []time.Duration
	controller := QianchuanRequestController{
		Root: root,
		Now:  func() time.Time { return now },
		Sleep: func(_ context.Context, delay time.Duration) error {
			sleeps = append(sleeps, delay)
			now = now.Add(delay)
			return nil
		},
	}
	release, err := controller.Acquire(context.Background(), "123")
	if err != nil {
		t.Fatal(err)
	}
	if err := release(nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(sleeps) != 1 || sleeps[0] != 3*time.Second {
		t.Fatalf("shared cooldown wait = %v, want [3s]", sleeps)
	}
	state, err := readQianchuanRequestState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if state.NextRequestAt != 103.25 || state.CooldownUntil != 103.0 {
		t.Fatalf("shared state changed incompatibly: %#v", state)
	}
}

func TestQianchuanRequestControllerRecordsBoundedRateLimitCooldown(t *testing.T) {
	root := t.TempDir()
	now := time.Unix(100, 0).UTC()
	controller := QianchuanRequestController{
		Root: root,
		Now:  func() time.Time { return now },
		Sleep: func(context.Context, time.Duration) error {
			return errors.New("unexpected sleep")
		},
	}
	release, err := controller.Acquire(context.Background(), "123")
	if err != nil {
		t.Fatal(err)
	}
	response := &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{
		"Retry-After": []string{"600"},
	}}
	if err := release(nil, response); err != nil {
		t.Fatal(err)
	}
	state, err := readQianchuanRequestState(
		filepath.Join(root, "request-control", "qianchuan-123.json"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if state.CooldownUntil != 130 {
		t.Fatalf("cooldown_until = %v, want 130", state.CooldownUntil)
	}
}

func TestQianchuanRequestControllerFailsClosedOnCorruptState(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "request-control", "qianchuan-123.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (QianchuanRequestController{Root: root}).Acquire(
		context.Background(), "123",
	); err == nil || err.Error() != "Qianchuan request state is invalid" {
		t.Fatalf("corrupt state did not fail closed: %v", err)
	}
}

func TestQianchuanRequestControllerRejectsSymlinkStateDirectoryFileAndLock(t *testing.T) {
	for _, kind := range []string{"directory", "state", "lock"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			directory := filepath.Join(root, "request-control")
			target := filepath.Join(root, "target")
			if kind == "directory" {
				if err := os.Mkdir(target, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, directory); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(directory, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
				name := "qianchuan-123.json"
				if kind == "lock" {
					name = "qianchuan-123.lock"
				}
				if err := os.Symlink(target, filepath.Join(directory, name)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := (QianchuanRequestController{Root: root}).Acquire(
				context.Background(), "123",
			); err == nil {
				t.Fatal("symlink request-control state was accepted")
			}
		})
	}
}

func TestQianchuanRequestControllerRejectsUnknownAndNegativeState(t *testing.T) {
	for name, payload := range map[string]string{
		"negative next request": `{"next_request_at":-1,"cooldown_until":0}`,
		"negative cooldown":     `{"next_request_at":0,"cooldown_until":-1}`,
		"unknown field":         `{"next_request_at":0,"cooldown_until":0,"unexpected":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := readQianchuanRequestState(path); err == nil ||
				err.Error() != "Qianchuan request state is invalid" {
				t.Fatalf("invalid state did not fail closed: %v", err)
			}
		})
	}
}

func TestQianchuanRequestStateUsesStableJSONContract(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := writeQianchuanRequestState(path, qianchuanRequestState{
		NextRequestAt: 100.25, CooldownUntil: 103,
	}); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]float64
	if err := json.Unmarshal(payload, &state); err != nil {
		t.Fatal(err)
	}
	if len(state) != 2 || state["next_request_at"] != 100.25 || state["cooldown_until"] != 103 {
		t.Fatalf("stable state JSON changed: %s", payload)
	}
}
