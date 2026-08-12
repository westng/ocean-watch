package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"testing"

	domaintemplates "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/templates"
)

type authorizationMigratorStub struct {
	result  map[string]any
	err     error
	confirm bool
	calls   int
}

func (stub *authorizationMigratorStub) Migrate(_ context.Context, confirm bool) (map[string]any, error) {
	stub.calls++
	stub.confirm = confirm
	return stub.result, stub.err
}

func TestRunAuthorizationMigrationPreservesSuccessAndConfirmationContracts(t *testing.T) {
	stub := &authorizationMigratorStub{result: map[string]any{
		"activation": "schema_v2_active", "migration_id": "fixture-migration-id",
	}}
	stdout := new(bytes.Buffer)
	code := RunAuthorizationMigration(
		context.Background(), []string{"--config", "fixture.json", "--confirm-remove-legacy-materials"},
		stub, "/fixture/config.json", stdout,
	)
	if code != 0 || stub.calls != 1 || !stub.confirm {
		t.Fatalf("unexpected execution: code=%d calls=%d confirm=%v output=%s", code, stub.calls, stub.confirm, stdout.String())
	}
	decoded := decodeSingleJSONObject(t, stdout.Bytes())
	if !reflect.DeepEqual(decoded, stub.result) {
		t.Fatalf("success payload changed: %#v", decoded)
	}
}

func TestRunAuthorizationMigrationMapsLegacyMaterialsAndUnexpectedErrors(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		err      error
		code     int
		errorKey string
	}{
		{
			name: "legacy materials", err: &domaintemplates.LegacyMaterialError{Templates: []string{"fixture"}},
			code: 2, errorKey: "legacy_material_selection_requires_confirmation",
		},
		{name: "unexpected", err: errors.New("injected failure"), code: 1, errorKey: "unexpected_error"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			stdout := new(bytes.Buffer)
			code := RunAuthorizationMigration(
				context.Background(), nil, &authorizationMigratorStub{err: testCase.err},
				"/fixture/config.json", stdout,
			)
			if code != testCase.code {
				t.Fatalf("exit code = %d, want %d", code, testCase.code)
			}
			decoded := decodeSingleJSONObject(t, stdout.Bytes())
			if testCase.code == 2 {
				if decoded["error_code"] != testCase.errorKey || decoded["required_flag"] != "--confirm-remove-legacy-materials" {
					t.Fatalf("legacy error payload changed: %#v", decoded)
				}
			} else {
				errorBody := decoded["error"].(map[string]any)
				if errorBody["code"] != testCase.errorKey {
					t.Fatalf("unexpected error payload changed: %#v", decoded)
				}
			}
		})
	}
}

func TestRunAuthorizationMigrationRejectsArgumentsBeforeMigration(t *testing.T) {
	stub := &authorizationMigratorStub{}
	stdout := new(bytes.Buffer)
	if code := RunAuthorizationMigration(
		context.Background(), []string{"--unknown"}, stub, "/fixture/config.json", stdout,
	); code != 2 {
		t.Fatalf("exit code = %d", code)
	}
	if stub.calls != 0 {
		t.Fatal("invalid arguments invoked migration")
	}
	decoded := decodeSingleJSONObject(t, stdout.Bytes())
	if decoded["error"].(map[string]any)["code"] != "invalid_arguments" {
		t.Fatalf("unexpected invalid-argument payload: %#v", decoded)
	}
}

func decodeSingleJSONObject(t *testing.T, payload []byte) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(payload))
	var result map[string]any
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		t.Fatalf("stdout contains more than one JSON value: %v", err)
	}
	return result
}
