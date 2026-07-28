//go:build !windows

package filesystem

import "os"

func replaceFile(source, target string) error {
	return os.Rename(source, target)
}
