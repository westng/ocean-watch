//go:build windows

package sqlite

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func owned(path string, info os.FileInfo) bool {
	return ownedWindowsPath(path, info, false)
}

func privateDirectory(path string, info os.FileInfo) bool {
	return info != nil && info.IsDir() && ownedWindowsPath(path, info, true)
}

func ownedWindowsPath(path string, info os.FileInfo, protectedDirectory bool) bool {
	if path == "" || info == nil || (!info.Mode().IsRegular() && !info.IsDir()) {
		return false
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	handle, err := windows.CreateFile(name, windows.READ_CONTROL|windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return false
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		windows.CloseHandle(handle)
		return false
	}
	defer file.Close()
	var attributes windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &attributes); err != nil ||
		attributes.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return false
	}
	actual, err := file.Stat()
	if err != nil || (!actual.Mode().IsRegular() && !actual.IsDir()) || !os.SameFile(info, actual) ||
		(protectedDirectory && !actual.IsDir()) {
		return false
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return false
	}
	descriptor, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false
	}
	if !privateWindowsDescriptor(descriptor, user.User.Sid) {
		return false
	}
	if protectedDirectory {
		control, _, err := descriptor.Control()
		return err == nil && control&windows.SE_DACL_PROTECTED != 0
	}
	return true
}

func privateWindowsDescriptor(descriptor *windows.SECURITY_DESCRIPTOR, currentUser *windows.SID) bool {
	if descriptor == nil || !descriptor.IsValid() || currentUser == nil || !currentUser.IsValid() {
		return false
	}
	owner, _, err := descriptor.Owner()
	if err != nil || !trustedWindowsStateSID(owner, currentUser) {
		return false
	}
	control, _, err := descriptor.Control()
	if err != nil || control&windows.SE_DACL_PRESENT == 0 {
		return false
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil {
		return false
	}
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil || ace == nil {
			return false
		}
		switch ace.Header.AceType {
		case windows.ACCESS_ALLOWED_ACE_TYPE, windows.ACCESS_DENIED_ACE_TYPE:
		default:
			return false
		}
		const sidOffset = int(unsafe.Offsetof(windows.ACCESS_ALLOWED_ACE{}.SidStart))
		if int(ace.Header.AceSize) < sidOffset+8 || ace.Header.AceFlags & ^uint8(windows.VALID_INHERIT_FLAGS) != 0 {
			return false
		}
		data := unsafe.Slice((*byte)(unsafe.Pointer(ace)), int(ace.Header.AceSize))
		if sidOffset+8+4*int(data[sidOffset+1]) != len(data) {
			return false
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !trustedWindowsStateSID(sid, currentUser) {
			return false
		}
	}
	return true
}

// Elevated processes can create SQLite sidecars with Administrators as their
// default owner even when they inherit this directory's private DACL. Owners
// and ACEs use the same trusted principals; arbitrary users remain forbidden.
func trustedWindowsStateSID(sid, currentUser *windows.SID) bool {
	return sid != nil && sid.IsValid() && (sid.Equals(currentUser) ||
		sid.IsWellKnown(windows.WinLocalSystemSid) || sid.IsWellKnown(windows.WinBuiltinAdministratorsSid))
}
