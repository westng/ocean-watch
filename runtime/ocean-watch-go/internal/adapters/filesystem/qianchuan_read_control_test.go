package filesystem

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestQianchuanReadCapacitySurvivesProcessCrash(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.CommandContext(ctx, executable, "-test.run=^TestQianchuanReadProcessHelper$")
	child.Env = append(os.Environ(), "OW_QC_READ_CHILD_ROOT="+root)
	output, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(output).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		if line != "read-ready\n" {
			t.Fatalf("child failed to acquire read lease: %q", line)
		}
	case <-ctx.Done():
		t.Fatal("child did not acquire its read lease")
	}
	controller := QianchuanRequestController{Root: root, MinimumInterval: time.Millisecond}
	release, err := controller.AcquireRead(ctx, "123")
	if err != nil {
		t.Fatal(err)
	}
	defer release(nil, nil)
	blocked, cancelBlocked := context.WithTimeout(ctx, 80*time.Millisecond)
	_, err = controller.AcquireRead(blocked, "123")
	cancelBlocked()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("parent and child did not share capacity: %v", err)
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	recovered, err := controller.AcquireRead(ctx, "123")
	if err != nil {
		t.Fatalf("crashed child leaked a read slot or legacy shared lock: %v", err)
	}
	_ = recovered(nil, nil)
}

func TestQianchuanReadProcessHelper(t *testing.T) {
	root := os.Getenv("OW_QC_READ_CHILD_ROOT")
	if root == "" {
		return
	}
	controller := QianchuanRequestController{Root: root, MinimumInterval: time.Millisecond}
	if _, err := controller.AcquireRead(context.Background(), "123"); err != nil {
		t.Fatal(err)
	}
	fmt.Println("read-ready")
	// The parent kills this process without a Go-level release callback.
	time.Sleep(10 * time.Minute)
}

func TestQianchuanReadsOverlapAndShareCapacityAcrossControllers(t *testing.T) {
	root := t.TempDir()
	first := QianchuanRequestController{Root: root, MinimumInterval: time.Millisecond}
	second := QianchuanRequestController{Root: root, MinimumInterval: time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	releaseA, err := first.AcquireRead(ctx, "123")
	if err != nil {
		t.Fatal(err)
	}
	defer releaseA(nil, nil)
	// The first response is still open when the second read is admitted.
	releaseB, err := second.AcquireRead(ctx, "123")
	if err != nil {
		t.Fatal(err)
	}
	defer releaseB(nil, nil)
	blocked, cancelBlocked := context.WithTimeout(ctx, 80*time.Millisecond)
	defer cancelBlocked()
	if _, err := first.AcquireRead(blocked, "123"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("third read exceeded shared capacity: %v", err)
	}
	if err := releaseA(nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := releaseA(nil, nil); err != nil {
		t.Fatalf("duplicate release failed: %v", err)
	}
	releaseC, err := second.AcquireRead(ctx, "123")
	if err != nil {
		t.Fatalf("cancelled waiter or duplicate release leaked capacity: %v", err)
	}
	defer releaseC(nil, nil)
}

func TestQianchuanReadsRemainExclusiveWithLegacyRequests(t *testing.T) {
	controller := QianchuanRequestController{Root: t.TempDir(), MinimumInterval: time.Millisecond}
	paths, err := controller.requestPaths(context.Background(), "123")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := AcquireLock(context.Background(), paths.legacyLock, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Release()
	blocked, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if _, err := controller.AcquireRead(blocked, "123"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("new read overlapped legacy request: %v", err)
	}
	if err := legacy.Release(); err != nil {
		t.Fatal(err)
	}
	releaseRead, err := controller.AcquireRead(context.Background(), "123")
	if err != nil {
		t.Fatal(err)
	}
	defer releaseRead(nil, nil)
	lock, acquired, err := TryAcquireLock(paths.legacyLock)
	if err != nil {
		t.Fatal(err)
	}
	if acquired {
		_ = lock.Release()
		t.Fatal("legacy request overlapped open new response")
	}
	if err := releaseRead(nil, nil); err != nil {
		t.Fatal(err)
	}
	lock, acquired, err = TryAcquireLock(paths.legacyLock)
	if err != nil || !acquired {
		t.Fatalf("legacy compatibility lock leaked: acquired=%v err=%v", acquired, err)
	}
	_ = lock.Release()
}

func TestQianchuanWaitingReadRechecksRateLimitWithoutHoldingStateLock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var clock atomic.Int64
	clock.Store(int64(100 * time.Second))
	delays := make(chan time.Duration, 2)
	wake := make(chan struct{})
	controller := QianchuanRequestController{
		Root: t.TempDir(), Now: func() time.Time { return time.Unix(0, clock.Load()) },
		Sleep: func(ctx context.Context, delay time.Duration) error {
			select {
			case delays <- delay:
			case <-ctx.Done():
				return ctx.Err()
			}
			select {
			case <-wake:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
	releaseFirst, err := controller.AcquireRead(ctx, "123")
	if err != nil {
		t.Fatal(err)
	}
	defer releaseFirst(nil, nil)
	type outcome struct {
		release func(error, *http.Response) error
		err     error
	}
	ready := make(chan outcome, 1)
	go func() {
		release, err := controller.AcquireRead(ctx, "123")
		ready <- outcome{release: release, err: err}
	}()
	expectDelay := func(wanted time.Duration) {
		t.Helper()
		select {
		case delay := <-delays:
			if delay != wanted {
				t.Fatalf("read wait=%v, want %v", delay, wanted)
			}
		case <-ctx.Done():
			t.Fatal("waiting read did not reach admission")
		}
	}
	expectDelay(250 * time.Millisecond)
	paths, _ := controller.requestPaths(ctx, "123")
	lock, acquired, err := TryAcquireLock(paths.admission)
	if err != nil || !acquired {
		t.Fatalf("sleep held the exclusive state lock: acquired=%v err=%v", acquired, err)
	}
	_ = lock.Release()
	if err := releaseFirst(errors.New("business error 40100"), nil); err != nil {
		t.Fatal(err)
	}
	clock.Store(int64(100*time.Second + 250*time.Millisecond))
	wake <- struct{}{}
	expectDelay(4750 * time.Millisecond)
	clock.Store(int64(105 * time.Second))
	wake <- struct{}{}
	select {
	case result := <-ready:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if err := result.release(nil, nil); err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("read did not resume after shared cooldown")
	}
	state, err := readQianchuanRequestState(paths.state)
	if err != nil || state.NextRequestAt != 105.25 || state.CooldownUntil != 105 {
		t.Fatalf("shared timing contract changed: %#v err=%v", state, err)
	}
}

func TestQianchuanReadAdmissionFailsClosedAndReleasesLocks(t *testing.T) {
	controller := QianchuanRequestController{Root: t.TempDir()}
	paths, err := controller.requestPaths(context.Background(), "123")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.state, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.AcquireRead(context.Background(), "123"); err == nil {
		t.Fatal("read accepted corrupt state")
	}
	if err := os.Remove(paths.state); err != nil {
		t.Fatal(err)
	}
	release, err := controller.Acquire(context.Background(), "123")
	if err != nil {
		t.Fatalf("failed read leaked compatibility or intent lock: %v", err)
	}
	_ = release(nil, nil)
	for _, name := range []string{"qianchuan-123-admission.lock", "qianchuan-123-write-intent.lock", "qianchuan-123-read-0.lock"} {
		path := filepath.Join(controller.Root, "request-control", name)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(paths.state, path); err != nil {
			t.Fatal(err)
		}
		if _, err := controller.AcquireRead(context.Background(), "123"); err == nil {
			t.Fatalf("accepted symlink lock %s", name)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
}
