//go:build windows

package sqlite

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

func createStateDirectory(path string) error {
	attributes, err := stateWindowsSecurityAttributes()
	if err != nil {
		return &os.PathError{Op: "mkdir", Path: path, Err: err}
	}
	return createWindowsDirectories(filepath.Clean(path), &attributes)
}

func createWindowsDirectories(path string, attributes *windows.SecurityAttributes) error {
	info, err := os.Lstat(path)
	if err == nil {
		if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			return nil
		}
		return &os.PathError{Op: "mkdir", Path: path, Err: windows.ERROR_ALREADY_EXISTS}
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return err
	}
	if err := createWindowsDirectories(parent, attributes); err != nil {
		return err
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return &os.PathError{Op: "mkdir", Path: path, Err: err}
	}
	err = windows.CreateDirectory(name, attributes)
	runtime.KeepAlive(attributes.SecurityDescriptor)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		info, statErr := os.Lstat(path)
		if statErr == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			return nil
		}
	}
	if err != nil {
		return &os.PathError{Op: "mkdir", Path: path, Err: err}
	}
	return nil
}

func createStateFile(path string) error {
	attributes, err := stateWindowsSecurityAttributes()
	if err != nil {
		return &os.PathError{Op: "open", Path: path, Err: err}
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return &os.PathError{Op: "open", Path: path, Err: err}
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, &attributes, windows.CREATE_NEW,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	runtime.KeepAlive(attributes.SecurityDescriptor)
	if err != nil {
		return &os.PathError{Op: "open", Path: path, Err: err}
	}
	if err := windows.CloseHandle(handle); err != nil {
		return &os.PathError{Op: "close", Path: path, Err: err}
	}
	return nil
}

func stateWindowsSecurityAttributes() (windows.SecurityAttributes, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return windows.SecurityAttributes{}, err
	}
	sid := user.User.Sid.String()
	descriptor, err := windows.SecurityDescriptorFromString("O:" + sid + "D:P(A;OICI;FA;;;" + sid +
		")(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")
	if err != nil {
		return windows.SecurityAttributes{}, err
	}
	return windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: descriptor,
	}, nil
}
