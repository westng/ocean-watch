//go:build windows

package filesystem

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func replaceFile(source, target string) error {
	sourcePath, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	targetPath, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	err = windows.MoveFileEx(sourcePath, targetPath, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return os.Rename(source, target)
	}
	return err
}
