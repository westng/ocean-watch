package filesystem

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	qianchuanMinimumRequestInterval = 250 * time.Millisecond
	qianchuanDefaultCooldown        = 5 * time.Second
	qianchuanMaximumRetryAfter      = 30 * time.Second
)

type QianchuanRequestController struct {
	Root            string
	MinimumInterval time.Duration
	DefaultCooldown time.Duration
	Now             func() time.Time
	Sleep           func(context.Context, time.Duration) error
}

type qianchuanRequestState struct {
	NextRequestAt float64 `json:"next_request_at"`
	CooldownUntil float64 `json:"cooldown_until"`
}

func (controller QianchuanRequestController) Acquire(
	ctx context.Context,
	advertiserID string,
) (func(error, *http.Response) error, error) {
	if ctx == nil {
		return nil, errors.New("Qianchuan request context is required")
	}
	advertiserID = strings.TrimSpace(advertiserID)
	if !positiveDecimalID(advertiserID) {
		return nil, errors.New("Qianchuan request advertiser_id is invalid")
	}
	root := filepath.Clean(controller.Root)
	if root == "." || root == string(filepath.Separator) {
		return nil, errors.New("Qianchuan request state root is invalid")
	}
	directory := filepath.Join(root, "request-control")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create Qianchuan request-control directory: %w", err)
	}
	if info, err := os.Lstat(directory); err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, errors.New("Qianchuan request-control root must be a managed directory")
	}
	statePath := filepath.Join(directory, "qianchuan-"+advertiserID+".json")
	lockPath := strings.TrimSuffix(statePath, ".json") + ".lock"
	if info, statErr := os.Lstat(lockPath); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, errors.New("Qianchuan request-control lock must be a regular managed file")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	}
	lock, err := AcquireLock(ctx, lockPath, 0)
	if err != nil {
		return nil, err
	}
	state, err := readQianchuanRequestState(statePath)
	if err != nil {
		_ = lock.Release()
		return nil, err
	}
	now := controller.now()
	waitUntil := state.NextRequestAt
	if state.CooldownUntil > waitUntil {
		waitUntil = state.CooldownUntil
	}
	if delay := durationUntil(waitUntil, now); delay > 0 {
		if err := controller.sleep(ctx, delay); err != nil {
			_ = lock.Release()
			return nil, err
		}
		now = controller.now()
	}
	state.NextRequestAt = unixSeconds(now.Add(controller.minimumInterval()))
	if err := writeQianchuanRequestState(statePath, state); err != nil {
		_ = lock.Release()
		return nil, err
	}
	return func(requestErr error, response *http.Response) error {
		state, readErr := readQianchuanRequestState(statePath)
		if readErr == nil && qianchuanRateLimited(requestErr, response) {
			cooldown := controller.defaultCooldown()
			if retryAfter := boundedRetryAfter(response, controller.now()); retryAfter > cooldown {
				cooldown = retryAfter
			}
			until := unixSeconds(controller.now().Add(cooldown))
			if until > state.CooldownUntil {
				state.CooldownUntil = until
				readErr = writeQianchuanRequestState(statePath, state)
			}
		}
		releaseErr := lock.Release()
		if readErr != nil {
			return readErr
		}
		return releaseErr
	}, nil
}

func (controller QianchuanRequestController) minimumInterval() time.Duration {
	if controller.MinimumInterval > 0 {
		return controller.MinimumInterval
	}
	return qianchuanMinimumRequestInterval
}

func (controller QianchuanRequestController) defaultCooldown() time.Duration {
	if controller.DefaultCooldown > 0 {
		return controller.DefaultCooldown
	}
	return qianchuanDefaultCooldown
}

func (controller QianchuanRequestController) now() time.Time {
	if controller.Now != nil {
		return controller.Now().UTC()
	}
	return time.Now().UTC()
}

func (controller QianchuanRequestController) sleep(ctx context.Context, delay time.Duration) error {
	if controller.Sleep != nil {
		return controller.Sleep(ctx, delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func readQianchuanRequestState(path string) (qianchuanRequestState, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return qianchuanRequestState{}, nil
	}
	if err != nil {
		return qianchuanRequestState{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return qianchuanRequestState{}, errors.New("Qianchuan request state must be a regular managed file")
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return qianchuanRequestState{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var state qianchuanRequestState
	if err := decoder.Decode(&state); err != nil {
		return qianchuanRequestState{}, errors.New("Qianchuan request state is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return qianchuanRequestState{}, errors.New("Qianchuan request state is invalid")
	}
	if math.IsNaN(state.NextRequestAt) || math.IsInf(state.NextRequestAt, 0) ||
		math.IsNaN(state.CooldownUntil) || math.IsInf(state.CooldownUntil, 0) ||
		state.NextRequestAt < 0 || state.CooldownUntil < 0 {
		return qianchuanRequestState{}, errors.New("Qianchuan request state is invalid")
	}
	return state, nil
}

func writeQianchuanRequestState(path string, state qianchuanRequestState) error {
	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	return AtomicWritePrivateFile(path, payload)
}

func qianchuanRateLimited(requestErr error, response *http.Response) bool {
	if response != nil && response.StatusCode == http.StatusTooManyRequests {
		return true
	}
	return requestErr != nil && strings.Contains(requestErr.Error(), "40100")
}

func boundedRetryAfter(response *http.Response, now time.Time) time.Duration {
	if response == nil {
		return 0
	}
	value := strings.TrimSpace(response.Header.Get("Retry-After"))
	if value == "" {
		return 0
	}
	var delay time.Duration
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		delay = time.Duration(seconds) * time.Second
	} else if when, err := http.ParseTime(value); err == nil {
		delay = when.Sub(now)
	}
	if delay < 0 {
		return 0
	}
	if delay > qianchuanMaximumRetryAfter {
		return qianchuanMaximumRetryAfter
	}
	return delay
}

func positiveDecimalID(value string) bool {
	if value == "" || len(value) > 64 || value[0] == '0' {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func unixSeconds(value time.Time) float64 {
	return float64(value.UnixNano()) / float64(time.Second)
}

func durationUntil(target float64, now time.Time) time.Duration {
	seconds := target - unixSeconds(now)
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds * float64(time.Second))
}
