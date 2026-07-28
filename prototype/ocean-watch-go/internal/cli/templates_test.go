package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type templateStoreSpy struct {
	reads  int
	config map[string]any
	err    error
}

func (store *templateStoreSpy) Read(context.Context) (map[string]any, error) {
	store.reads++
	return store.config, store.err
}

func (store *templateStoreSpy) ReadWithRevision(ctx context.Context) (map[string]any, string, error) {
	config, err := store.Read(ctx)
	return config, "revision", err
}

func (store *templateStoreSpy) CompareAndSwap(context.Context, string, map[string]any) error {
	return nil
}

func TestRunTemplatesListUsesOneLocalRead(t *testing.T) {
	store := &templateStoreSpy{config: map[string]any{}}
	stdout := new(bytes.Buffer)
	code := RunTemplates(
		context.Background(), "list", []string{"--channel", "all"},
		store, "/synthetic/config.json", stdout,
	)
	if code != 0 {
		t.Fatalf("got exit %d: %s", code, stdout.String())
	}
	if store.reads != 1 {
		t.Fatalf("config read %d times, want 1", store.reads)
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["source"] != "local_config" || result["config"] != "/synthetic/config.json" {
		t.Fatalf("unexpected template envelope: %#v", result)
	}
}

func TestRunTemplatesShowMapsNotFoundToConfigurationError(t *testing.T) {
	store := &templateStoreSpy{config: map[string]any{}}
	stdout := new(bytes.Buffer)
	code := RunTemplates(
		context.Background(), "show",
		[]string{"--channel", "qianchuan", "--template", "missing"},
		store, "/synthetic/config.json", stdout,
	)
	if code != 2 {
		t.Fatalf("got exit %d: %s", code, stdout.String())
	}
	var result ErrorEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Error.Code != "configuration_error" || result.Error.Message != "Qianchuan product template not found" {
		t.Fatalf("unexpected error envelope: %#v", result)
	}
	if result.Error.Details["selector"] != "missing" {
		t.Fatalf("selector detail was lost: %#v", result)
	}
}

func TestTemplateActionErrorPreservesPythonExitClasses(t *testing.T) {
	tests := []struct {
		action   string
		code     string
		exitCode int
	}{
		{action: "migrate", code: "unexpected_error", exitCode: 1},
		{action: "set-copy", code: "unexpected_error", exitCode: 1},
		{action: "validate", code: "configuration_error", exitCode: 2},
		{action: "delete", code: "configuration_error", exitCode: 2},
	}
	for _, test := range tests {
		t.Run(test.action, func(t *testing.T) {
			mapped := templateActionError(test.action, errors.New("synthetic failure"))
			if mapped.Code != test.code || mapped.ExitCode != test.exitCode {
				t.Fatalf("unexpected mapping: %#v", mapped)
			}
		})
	}
}
