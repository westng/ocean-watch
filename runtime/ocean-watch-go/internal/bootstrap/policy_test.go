package bootstrap

import (
	"context"
	"testing"
)

func TestStartupPolicyRejectsDuplicateAndTrailingOverrides(t *testing.T) {
	for _, text := range []string{`{"mode":"read","mode":"execute","toolsets":["core"]}`, `{"mode":"read","toolsets":["core"]} {}`, `{"mode":"read","toolsets":["core"],"server_url":"https://example.invalid"}`} {
		if _, err := StartupPolicy(context.Background(), nil, text); err == nil {
			t.Fatal("invalid proxy snapshot accepted")
		}
	}
	instance, err := StartupPolicy(context.Background(), nil, `{"mode":"read","toolsets":["core","qianchuan"]}`)
	if err != nil || instance.Mode() != "read" {
		t.Fatal("valid frozen proxy snapshot failed")
	}
}
