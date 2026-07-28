package platform_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/requestcontrol"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/retry"
)

func TestCancellationAndLeak(t *testing.T) {
	baseline := stableGoroutineCount()
	root := t.TempDir()
	lockPath := filepath.Join(root, "state", "acceptance.lock")
	outputPath := filepath.Join(root, "state", "result.json")
	governor, err := requestcontrol.NewGovernor(requestcontrol.Limits{
		AuthorizationConcurrency: 1,
		EndpointConcurrency:      1,
	})
	if err != nil {
		t.Fatal(err)
	}
	scope := requestcontrol.AuthorizationScope{
		Channel: "qianchuan", AuthorizationID: "authorization-fixture",
	}

	for iteration := 0; iteration < 20; iteration++ {
		lock, lockErr := filesystem.AcquireLock(context.Background(), lockPath, time.Second)
		if lockErr != nil {
			t.Fatalf("iteration %d acquire first lock: %v", iteration, lockErr)
		}
		waitContext, cancelWait := context.WithTimeout(context.Background(), 30*time.Millisecond)
		started := time.Now()
		_, waitErr := filesystem.AcquireLock(waitContext, lockPath, time.Second)
		cancelWait()
		if !errors.Is(waitErr, context.DeadlineExceeded) {
			_ = lock.Release()
			t.Fatalf("iteration %d lock wait did not preserve deadline: %v", iteration, waitErr)
		}
		if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
			_ = lock.Release()
			t.Fatalf("iteration %d lock cancellation took %s", iteration, elapsed)
		}
		if releaseErr := lock.Release(); releaseErr != nil {
			t.Fatalf("iteration %d release first lock: %v", iteration, releaseErr)
		}
		reacquired, reacquireErr := filesystem.AcquireLock(
			context.Background(), lockPath, time.Second,
		)
		if reacquireErr != nil {
			t.Fatalf("iteration %d lock remained held: %v", iteration, reacquireErr)
		}
		if releaseErr := reacquired.Release(); releaseErr != nil {
			t.Fatalf("iteration %d release reacquired lock: %v", iteration, releaseErr)
		}

		releaseGovernor, acquireErr := governor.Acquire(
			context.Background(), scope, "qianchuan-plan-list",
		)
		if acquireErr != nil {
			t.Fatalf("iteration %d acquire governor: %v", iteration, acquireErr)
		}
		governorContext, cancelGovernor := context.WithTimeout(
			context.Background(), 20*time.Millisecond,
		)
		_, acquireErr = governor.Acquire(governorContext, scope, "qianchuan-plan-list")
		cancelGovernor()
		if !errors.Is(acquireErr, context.DeadlineExceeded) {
			releaseGovernor()
			t.Fatalf("iteration %d governor wait did not cancel: %v", iteration, acquireErr)
		}
		releaseGovernor()
		releaseAgain, acquireErr := governor.Acquire(
			context.Background(), scope, "qianchuan-plan-list",
		)
		if acquireErr != nil {
			t.Fatalf("iteration %d governor slot leaked: %v", iteration, acquireErr)
		}
		releaseAgain()

		retryContext, cancelRetry := context.WithTimeout(context.Background(), 20*time.Millisecond)
		_, retryErr := retry.Do(
			retryContext,
			retry.Policy{Delays: []time.Duration{time.Minute}},
			func(error) (bool, time.Duration) { return true, 0 },
			func(context.Context, int) (struct{}, error) {
				return struct{}{}, errors.New("temporary fixture failure")
			},
		)
		cancelRetry()
		if !errors.Is(retryErr, context.DeadlineExceeded) {
			t.Fatalf("iteration %d retry deadline was not preserved: %v", iteration, retryErr)
		}

		if writeErr := filesystem.AtomicWritePrivateFile(
			outputPath, []byte("{\"status\":\"passed\"}\n"),
		); writeErr != nil {
			t.Fatalf("iteration %d atomic write: %v", iteration, writeErr)
		}
		temporaryFiles, globErr := filepath.Glob(
			filepath.Join(filepath.Dir(outputPath), "."+filepath.Base(outputPath)+".*.tmp"),
		)
		if globErr != nil {
			t.Fatal(globErr)
		}
		if len(temporaryFiles) != 0 {
			t.Fatalf("iteration %d temporary files leaked: %#v", iteration, temporaryFiles)
		}
	}

	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("atomic output is missing: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		current := stableGoroutineCount()
		if current <= baseline+2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutines did not return to baseline: before=%d after=%d", baseline, current)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func stableGoroutineCount() int {
	runtime.GC()
	runtime.Gosched()
	return runtime.NumGoroutine()
}
