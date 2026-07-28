package filesystem

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const pythonLockScript = `
import sys
from ocean_watch.core.process_lock import ProcessLock

mode, path = sys.argv[1:]
with ProcessLock(path, timeout=0.2):
    if mode == "hold":
        print("locked", flush=True)
        sys.stdin.readline()
`

func TestCrossRuntimeLockMutualExclusion(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "config.json.lock")
	goLock, err := AcquireLock(context.Background(), lockPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	contender := pythonLockCommand(t, "contend", lockPath)
	if output, err := contender.CombinedOutput(); err == nil {
		_ = goLock.Release()
		t.Fatalf("Python acquired a Go-held lock: %s", output)
	}
	if err := goLock.Release(); err != nil {
		t.Fatal(err)
	}

	holder := pythonLockCommand(t, "hold", lockPath)
	stdin, err := holder.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := holder.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	holder.Stderr = &stderr
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	released := false
	defer func() {
		if !released && holder.Process != nil {
			_ = holder.Process.Kill()
			_ = holder.Wait()
		}
	}()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "locked" {
		t.Fatalf("Python lock holder did not start: line=%q err=%v stderr=%s", line, err, stderr.String())
	}
	if lock, err := AcquireLock(context.Background(), lockPath, 100*time.Millisecond); err == nil {
		_ = lock.Release()
		t.Fatal("Go acquired a Python-held lock")
	}
	_, _ = io.WriteString(stdin, "release\n")
	_ = stdin.Close()
	if err := holder.Wait(); err != nil {
		t.Fatalf("Python lock holder failed: %v: %s", err, stderr.String())
	}
	released = true
}

func pythonLockCommand(t *testing.T, mode, path string) *exec.Cmd {
	t.Helper()
	python, prefix := findPython(t)
	arguments := append(prefix, "-c", pythonLockScript, mode, path)
	command := exec.Command(python, arguments...)
	command.Env = append(os.Environ(), "PYTHONPATH="+pythonSourceRoot(t))
	return command
}

func findPython(t *testing.T) (string, []string) {
	t.Helper()
	candidates := []struct {
		name   string
		prefix []string
	}{
		{name: "python3"},
		{name: "python"},
	}
	if runtime.GOOS == "windows" {
		candidates = append([]struct {
			name   string
			prefix []string
		}{{name: "py", prefix: []string{"-3"}}}, candidates...)
	}
	for _, candidate := range candidates {
		if path, err := exec.LookPath(candidate.name); err == nil {
			return path, candidate.prefix
		}
	}
	t.Fatal("a supported Python 3 interpreter is required for cross-runtime lock tests")
	return "", nil
}

func pythonSourceRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve repository root")
	}
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "../../../../.."))
	return filepath.Join(repositoryRoot, "skills", "ads-plan-monitor", "src")
}
