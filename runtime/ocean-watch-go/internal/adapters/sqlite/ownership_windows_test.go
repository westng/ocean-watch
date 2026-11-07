//go:build windows

package sqlite

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestOwnedWindowsACL(t *testing.T) {
	user := ownershipTestUser(t)
	allowUser := "(A;;FA;;;" + user.String() + ")"
	cases := []struct {
		name string
		dacl string
		want bool
	}{
		{name: "current user", dacl: "D:P" + allowUser, want: true},
		{name: "system and administrators", dacl: "D:P" + allowUser + "(A;;FA;;;SY)(A;;FA;;;BA)", want: true},
		{name: "trusted deny", dacl: "D:P(D;;0x1;;;SY)" + allowUser, want: true},
		{name: "everyone deny", dacl: "D:P(D;;0x1;;;WD)" + allowUser},
		{name: "unprotected", dacl: "D:" + allowUser, want: true},
		{name: "null DACL", dacl: "D:PNO_ACCESS_CONTROL"},
		{name: "everyone full control", dacl: "D:P" + allowUser + "(A;;FA;;;WD)"},
		{name: "authenticated users read", dacl: "D:P" + allowUser + "(A;;FR;;;AU)"},
		{name: "users execute", dacl: "D:P" + allowUser + "(A;;FX;;;BU)"},
		{name: "another user", dacl: "D:P" + allowUser + "(A;;FR;;;S-1-5-21-1-2-3-1001)"},
	}
	for _, directory := range []bool{true, false} {
		kind := "file"
		if directory {
			kind = "directory"
		}
		t.Run(kind, func(t *testing.T) {
			for _, test := range cases {
				t.Run(test.name, func(t *testing.T) {
					parent := filepath.Join(t.TempDir(), "private")
					if err := createStateDirectory(parent); err != nil {
						t.Fatal(err)
					}
					path := filepath.Join(parent, "fixture")
					info := createOwnershipFixture(t, path, directory, "O:"+user.String()+test.dacl)
					if got := owned(path, info); got != test.want {
						t.Fatalf("owned() = %v, want %v", got, test.want)
					}
				})
			}
		})
	}
}

func TestOwnedWindowsUsesPathSecurity(t *testing.T) {
	user := ownershipTestUser(t)
	sddl := "O:" + user.String() + "D:P(A;;FA;;;" + user.String() + ")"
	path := filepath.Join(t.TempDir(), "fixture")
	info := createOwnershipFixture(t, path, false, sddl)
	otherPath := filepath.Join(t.TempDir(), "fixture")
	otherInfo := createOwnershipFixture(t, otherPath, false, sddl)
	if !owned(path, info) {
		t.Fatal("private fixture was rejected")
	}
	if owned(path, otherInfo) {
		t.Fatal("different file with the same basename was accepted")
	}
	if owned("", info) || owned(path, nil) || owned(path+"-missing", info) {
		t.Fatal("invalid path or missing file information was accepted")
	}
	descriptor := ownershipTestDescriptor(t, sddl+"(A;;FR;;;WD)")
	dacl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	runtime.KeepAlive(descriptor)
	if owned(path, info) {
		t.Fatal("cached file information hid the newly permissive DACL")
	}
}

func TestOwnedWindowsRejectsReparsePoint(t *testing.T) {
	user := ownershipTestUser(t)
	root := t.TempDir()
	path := filepath.Join(root, "fixture")
	createOwnershipFixture(t, path, false, "O:"+user.String()+"D:P(A;;FA;;;"+user.String()+")")
	link := filepath.Join(root, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	info, err := os.Stat(link)
	if err != nil {
		t.Fatal(err)
	}
	if owned(link, info) {
		t.Fatal("reparse point was accepted using its target's file information")
	}
}

func TestPrivateWindowsDescriptor(t *testing.T) {
	user := ownershipTestUser(t)
	allowUser := "(A;;FA;;;" + user.String() + ")"
	cases := []struct {
		name string
		sddl string
		want bool
	}{
		{name: "private", sddl: "O:" + user.String() + "D:P" + allowUser, want: true},
		{name: "empty DACL", sddl: "O:" + user.String() + "D:P", want: true},
		{name: "inherited trusted", sddl: "O:" + user.String() + "D:AI(A;ID;FA;;;" + user.String() + ")(A;ID;FA;;;SY)(A;ID;FA;;;BA)", want: true},
		{name: "administrators owner", sddl: "O:BAD:AI(A;ID;FA;;;" + user.String() + ")(A;ID;FA;;;SY)(A;ID;FA;;;BA)", want: true},
		{name: "system owner", sddl: "O:SYD:AI(A;ID;FA;;;" + user.String() + ")(A;ID;FA;;;SY)(A;ID;FA;;;BA)", want: true},
		{name: "administrators owner with everyone access", sddl: "O:BAD:AI(A;ID;FA;;;" + user.String() + ")(A;ID;FR;;;WD)"},
		{name: "inherited everyone", sddl: "O:" + user.String() + "D:AI(A;ID;FA;;;" + user.String() + ")(A;ID;FR;;;WD)"},
		{name: "inherited another owner", sddl: "O:S-1-5-21-1-2-3-1001D:AI(A;ID;FA;;;" + user.String() + ")"},
		{name: "inherited null DACL", sddl: "O:" + user.String() + "D:AINO_ACCESS_CONTROL"},
		{name: "trusted inherit only", sddl: "O:" + user.String() + "D:P" + allowUser + "(A;OICIIO;FA;;;SY)", want: true},
		{name: "everyone deny", sddl: "O:" + user.String() + "D:P(D;;FR;;;WD)" + allowUser},
		{name: "another owner", sddl: "O:S-1-5-21-1-2-3-1001D:P" + allowUser},
		{name: "missing owner", sddl: "D:P" + allowUser},
		{name: "missing DACL", sddl: "O:" + user.String()},
		{name: "null DACL", sddl: "O:" + user.String() + "D:PNO_ACCESS_CONTROL"},
		{name: "unprotected", sddl: "O:" + user.String() + "D:" + allowUser, want: true},
		{name: "inherit only everyone", sddl: "O:" + user.String() + "D:P" + allowUser + "(A;OIIO;FA;;;WD)"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := privateWindowsDescriptor(ownershipTestDescriptor(t, test.sddl), user); got != test.want {
				t.Fatalf("privateWindowsDescriptor() = %v, want %v", got, test.want)
			}
		})
	}
	if privateWindowsDescriptor(nil, user) ||
		privateWindowsDescriptor(ownershipTestDescriptor(t, "O:"+user.String()+"D:P"+allowUser), nil) ||
		privateWindowsDescriptor(ownershipTestDescriptor(t, "O:"+user.String()+"D:P"+allowUser), &windows.SID{}) {
		t.Fatal("missing descriptor or token user was accepted")
	}
	for _, aceType := range []uint8{5, 9} {
		descriptor := ownershipTestDescriptor(t, "O:"+user.String()+"D:P"+allowUser)
		dacl, _, err := descriptor.DACL()
		if err != nil {
			t.Fatal(err)
		}
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, 0, &ace); err != nil {
			t.Fatal(err)
		}
		ace.Header.AceType = aceType
		if privateWindowsDescriptor(descriptor, user) {
			t.Fatalf("unsupported ACE type %d was accepted", aceType)
		}
	}
}

func TestPrivateDirectoryWindowsRequiresProtection(t *testing.T) {
	user := ownershipTestUser(t)
	for _, protected := range []bool{false, true} {
		name := "unprotected"
		dacl := "D:"
		if protected {
			name = "protected"
			dacl = "D:P"
		}
		t.Run(name, func(t *testing.T) {
			parent := filepath.Join(t.TempDir(), "private")
			if err := createStateDirectory(parent); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(parent, "fixture")
			info := createOwnershipFixture(t, path, true, "O:"+user.String()+dacl+"(A;;FA;;;"+user.String()+")")
			if !owned(path, info) {
				t.Fatal("trusted directory ACL was rejected")
			}
			if got := privateDirectory(path, info); got != protected {
				t.Fatalf("privateDirectory() = %v, want %v", got, protected)
			}
		})
	}
	path := filepath.Join(t.TempDir(), "file")
	info := createOwnershipFixture(t, path, false, "O:"+user.String()+"D:P(A;;FA;;;"+user.String()+")")
	if privateDirectory(path, info) || privateDirectory(path, nil) {
		t.Fatal("file or missing information was accepted as a private directory")
	}
}

func ownershipTestUser(t *testing.T) *windows.SID {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	return user.User.Sid
}

func ownershipTestDescriptor(t *testing.T, sddl string) *windows.SECURITY_DESCRIPTOR {
	t.Helper()
	descriptor, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatal(err)
	}
	return descriptor
}

func createOwnershipFixture(t *testing.T, path string, directory bool, sddl string) os.FileInfo {
	t.Helper()
	descriptor := ownershipTestDescriptor(t, sddl)
	attributes := windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: descriptor,
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	if directory {
		if err := windows.CreateDirectory(name, &attributes); err != nil {
			t.Fatal(err)
		}
	} else {
		handle, err := windows.CreateFile(name, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
			&attributes, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := windows.CloseHandle(handle); err != nil {
			t.Fatal(err)
		}
	}
	runtime.KeepAlive(descriptor)
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}
