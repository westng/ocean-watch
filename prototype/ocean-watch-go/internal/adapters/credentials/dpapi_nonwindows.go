//go:build !windows

package credentials

import "errors"

func (store Store) readWindows(string) (map[string]any, error) {
	return nil, errors.New("Windows DPAPI is unavailable on this platform")
}

func (store Store) writeWindows(string, []byte) error {
	return errors.New("Windows DPAPI is unavailable on this platform")
}
