package approvalbroker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativeDialogScriptCompilesWithoutOpeningUI(t *testing.T) {
	if _, err := os.Stat("/usr/bin/osacompile"); err != nil {
		t.Skip("AppleScript compiler is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/usr/bin/osacompile", "-o", filepath.Join(t.TempDir(), "dialog.scpt"))
	command.Env = dialogEnvironment()
	command.Stdin = strings.NewReader(dialogScript)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("native UI script did not compile: %v: %s", err, output)
	}
}
