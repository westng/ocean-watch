package filesystem

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"
)

type RefreshLocker struct {
	StateRoot string
	Timeout   time.Duration
}

func (locker RefreshLocker) Acquire(ctx context.Context, channel, authorizationID string) (func() error, error) {
	if channel != "marketing" && channel != "qianchuan" {
		return nil, errors.New("unsupported refresh lock channel")
	}
	if strings.TrimSpace(authorizationID) == "" || strings.ContainsAny(authorizationID, "/\\\x00\r\n") {
		return nil, errors.New("invalid refresh lock authorization_id")
	}
	lock, err := AcquireLock(
		ctx,
		filepath.Join(locker.StateRoot, "refresh-locks", channel+"-"+authorizationID+".lock"),
		locker.Timeout,
	)
	if err != nil {
		return nil, err
	}
	return lock.Release, nil
}
