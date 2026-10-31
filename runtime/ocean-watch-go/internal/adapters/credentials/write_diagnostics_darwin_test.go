package credentials

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMacOSCredentialFailureDistinguishesWriteFromVerification(t *testing.T) {
	for _, test := range []struct{ name, add, reason string }{
		{"write", "exit 11", "CREDENTIAL_WRITE_FAILED"},
		{"verification", "exit 0", "CREDENTIAL_VERIFY_FAILED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			command := filepath.Join(directory, "security")
			script := "#!/bin/sh\ncase \"$1\" in\n  -i) " + test.add + " ;;\n  find-generic-password) printf '{}'; exit 0 ;;\n  *) exit 2 ;;\nesac\n"
			if err := os.WriteFile(command, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			store := Store{Root: directory, LookPath: func(string) (string, error) { return command, nil }}
			_, err := store.Write(context.Background(), "oceanengine-auth-qianchuan-fixture-r1", map[string]any{"probe": "fixture-private-secret"})
			var reason interface {
				error
				AuthorizationFailureReason() string
			}
			if err == nil || !errors.As(err, &reason) || reason.AuthorizationFailureReason() != test.reason {
				t.Fatalf("incorrect credential failure classification: %v", err)
			}
			if strings.Contains(err.Error(), "fixture-private-secret") {
				t.Fatal("credential error exposed the payload")
			}
		})
	}
}
