//go:build !windows

package sqlite

import (
	"os"
	"syscall"
)

func owned(_ string, info os.FileInfo) bool {
	if info == nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

func privateDirectory(path string, info os.FileInfo) bool {
	return info != nil && info.IsDir() && info.Mode().Perm() == 0o700 && owned(path, info)
}
