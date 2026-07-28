package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application"
)

func TestRunWorkMetadataSetRedactsEndpointAndPreservesConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"future":{"preserved":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout := new(bytes.Buffer)
	service := application.WorkMetadata{Store: filesystem.ConfigStore{Path: path}, Path: path}
	code := RunWorkMetadata(
		context.Background(),
		[]string{"--config", path, "--endpoint", "https://metadata.example.test/api?version=1"},
		service, true, stdout,
	)
	if code != 0 {
		t.Fatalf("got exit %d: %s", code, stdout.String())
	}
	if bytes.Contains(stdout.Bytes(), []byte("metadata.example.test")) {
		t.Fatalf("endpoint leaked to stdout: %s", stdout.String())
	}
	var result application.WorkMetadataStatus
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Configured || result.Endpoint == nil || *result.Endpoint != "<configured locally>" {
		t.Fatalf("unexpected redacted status: %#v", result)
	}
	stored, err := (filesystem.ConfigStore{Path: path}).Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stored["future"].(map[string]any)["preserved"] != true {
		t.Fatal("unrelated config was lost")
	}
}

func TestRunWorkMetadataMissingConfigIsStableError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	stdout := new(bytes.Buffer)
	service := application.WorkMetadata{Store: filesystem.ConfigStore{Path: path}, Path: path}
	if code := RunWorkMetadata(context.Background(), []string{"--config", path}, service, false, stdout); code != 2 {
		t.Fatalf("got exit %d: %s", code, stdout.String())
	}
	var result ErrorEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Error.Code != "configuration_error" || result.Error.Details["config"] != path {
		t.Fatalf("unexpected error: %#v", result)
	}
}
