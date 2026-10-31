//go:build !windows

package credentials

import (
	"context"
	"errors"
)

func readSystemWindows(context.Context, string) (map[string]any, error) {
	return nil, errors.New("Windows Credential Manager is unavailable")
}

func writeSystemWindows(context.Context, string, []byte) error {
	return errors.New("Windows Credential Manager is unavailable")
}
