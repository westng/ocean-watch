//go:build !darwin

package credentials

import (
	"context"
	"errors"
)

func (SystemStore) readSystemMacOS(context.Context, string) (map[string]any, error) {
	return nil, errors.New("macOS Keychain is unavailable")
}

func (SystemStore) writeSystemMacOS(context.Context, string, []byte) error {
	return errors.New("macOS Keychain is unavailable")
}
