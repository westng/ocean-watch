package filesystem

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// This is a conservative local in-flight limit, not an official API quota.
// A fixed capacity keeps all processes sharing a root on the same contract.
const qianchuanConcurrentReads = 2

type qianchuanRequestPaths struct {
	state       string
	legacyLock  string
	admission   string
	writeIntent string
	readSlots   [qianchuanConcurrentReads]string
}

func (controller QianchuanRequestController) requestPaths(ctx context.Context, advertiserID string) (qianchuanRequestPaths, error) {
	advertiserID = strings.TrimSpace(advertiserID)
	if !positiveDecimalID(advertiserID) {
		return qianchuanRequestPaths{}, errors.New("Qianchuan request advertiser_id is invalid")
	}
	return controller.requestPathsForBase(ctx, "qianchuan-"+advertiserID)
}

func (controller QianchuanRequestController) requestPathsForBase(ctx context.Context, name string) (qianchuanRequestPaths, error) {
	var paths qianchuanRequestPaths
	if ctx == nil {
		return paths, errors.New("Qianchuan request context is required")
	}
	if err := ctx.Err(); err != nil {
		return paths, err
	}
	root := filepath.Clean(controller.Root)
	if root == "." || root == string(filepath.Separator) {
		return paths, errors.New("Qianchuan request state root is invalid")
	}
	directory := filepath.Join(root, "request-control")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return paths, fmt.Errorf("create Qianchuan request-control directory: %w", err)
	}
	if info, err := os.Lstat(directory); err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return paths, errors.New("Qianchuan request-control root must be a managed directory")
	}
	base := filepath.Join(directory, name)
	paths.state, paths.legacyLock = base+".json", base+".lock"
	paths.admission, paths.writeIntent = base+"-admission.lock", base+"-write-intent.lock"
	locks := []string{paths.legacyLock, paths.admission, paths.writeIntent}
	for index := range paths.readSlots {
		paths.readSlots[index] = fmt.Sprintf("%s-read-%d.lock", base, index)
		locks = append(locks, paths.readSlots[index])
	}
	for _, path := range locks {
		if info, err := os.Lstat(path); err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				return qianchuanRequestPaths{}, errors.New("Qianchuan request-control lock must be a regular managed file")
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return qianchuanRequestPaths{}, err
		}
	}
	return paths, nil
}

// AcquireRead keeps the original lock shared until the body is closed. Older
// processes still acquire it exclusively, so old requests and writes cannot
// overlap new reads. A separate short lock protects the unchanged state JSON.
func (controller QianchuanRequestController) AcquireRead(ctx context.Context, advertiserID string) (func(error, *http.Response) error, error) {
	paths, err := controller.requestPaths(ctx, advertiserID)
	if err != nil {
		return nil, err
	}
	return controller.acquireReadPaths(ctx, paths)
}

func (controller QianchuanRequestController) acquireReadPaths(ctx context.Context, paths qianchuanRequestPaths) (func(error, *http.Response) error, error) {
	slot, err := acquireQianchuanReadSlot(ctx, paths.readSlots)
	if err != nil {
		return nil, err
	}
	intent, err := AcquireSharedLock(ctx, paths.writeIntent, 0)
	if err != nil {
		_ = slot.Release()
		return nil, err
	}
	legacy, err := AcquireSharedLock(ctx, paths.legacyLock, 0)
	intentErr := intent.Release()
	if err != nil || intentErr != nil {
		if legacy != nil {
			_ = legacy.Release()
		}
		_ = slot.Release()
		return nil, errors.Join(err, intentErr)
	}
	if err := controller.admitRead(ctx, paths); err != nil {
		_ = legacy.Release()
		_ = slot.Release()
		return nil, err
	}
	var once sync.Once
	var releaseErr error
	return func(requestErr error, response *http.Response) error {
		once.Do(func() {
			var feedbackErr error
			if qianchuanRateLimited(requestErr, response) {
				feedbackContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				defer cancel()
				feedbackErr = controller.recordReadCooldown(feedbackContext, paths, response)
			}
			releaseErr = errors.Join(feedbackErr, legacy.Release(), slot.Release())
		})
		return releaseErr
	}, nil
}

func acquireQianchuanReadSlot(ctx context.Context, paths [qianchuanConcurrentReads]string) (*FileLock, error) {
	deadline := time.Now().Add(60 * time.Second)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, path := range paths {
			lock, acquired, err := TryAcquireLock(path)
			if err != nil {
				return nil, err
			}
			if acquired {
				return lock, nil
			}
		}
		if time.Now().After(deadline) {
			return nil, errors.New("timed out waiting for Qianchuan read capacity")
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (controller QianchuanRequestController) admitRead(ctx context.Context, paths qianchuanRequestPaths) error {
	for {
		lock, err := AcquireLock(ctx, paths.admission, 0)
		if err != nil {
			return err
		}
		state, err := readQianchuanRequestState(paths.state)
		if err != nil {
			_ = lock.Release()
			return err
		}
		now := controller.now()
		waitUntil := max(state.NextRequestAt, state.CooldownUntil)
		if delay := durationUntil(waitUntil, now); delay > 0 {
			if err := lock.Release(); err != nil {
				return err
			}
			if err := controller.sleep(ctx, delay); err != nil {
				return err
			}
			continue // A rate-limit response may have extended the cooldown.
		}
		state.NextRequestAt = unixSeconds(now.Add(controller.minimumInterval()))
		writeErr := writeQianchuanRequestState(paths.state, state)
		return errors.Join(writeErr, lock.Release())
	}
}

func (controller QianchuanRequestController) recordReadCooldown(ctx context.Context, paths qianchuanRequestPaths, response *http.Response) error {
	lock, err := AcquireLock(ctx, paths.admission, 0)
	if err != nil {
		return err
	}
	state, readErr := readQianchuanRequestState(paths.state)
	if readErr == nil {
		cooldown := max(controller.defaultCooldown(), boundedRetryAfter(response, controller.now()))
		state.CooldownUntil = max(state.CooldownUntil, unixSeconds(controller.now().Add(cooldown)))
		readErr = writeQianchuanRequestState(paths.state, state)
	}
	return errors.Join(readErr, lock.Release())
}
