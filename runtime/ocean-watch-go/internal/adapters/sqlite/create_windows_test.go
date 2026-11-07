//go:build windows

package sqlite

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestStateWindowsSecurityAttributes(t *testing.T) {
	attributes, err := stateWindowsSecurityAttributes()
	if err != nil {
		t.Fatal(err)
	}
	if attributes.Length != uint32(unsafe.Sizeof(windows.SecurityAttributes{})) || attributes.InheritHandle != 0 {
		t.Fatal("unexpected security attributes layout or inheritable handle")
	}
	assertStateWindowsDescriptor(t, attributes.SecurityDescriptor, true)
}

func TestCreateStateDirectoryWindows(t *testing.T) {
	user := ownershipTestUser(t)
	parent := filepath.Join(t.TempDir(), "existing")
	createOwnershipFixture(t, parent, true, "O:"+user.String()+"D:P(A;OICI;FA;;;"+user.String()+")(A;OICI;FA;;;WD)")
	before := stateWindowsTestDescriptor(t, parent).String()
	path := filepath.Join(parent, "new", "state")
	if err := createStateDirectory(path); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{filepath.Dir(path), path} {
		info, err := os.Lstat(directory)
		if err != nil {
			t.Fatal(err)
		}
		if !privateDirectory(directory, info) {
			t.Fatalf("created directory is not private: %s", directory)
		}
		assertStateWindowsDescriptor(t, stateWindowsTestDescriptor(t, directory), true)
	}
	if err := createStateDirectory(parent); err != nil {
		t.Fatal(err)
	}
	if err := createStateDirectory(path); err != nil {
		t.Fatal(err)
	}
	if after := stateWindowsTestDescriptor(t, parent).String(); after != before {
		t.Fatal("existing parent ACL was modified")
	}
	filePath := filepath.Join(path, "file")
	if err := createStateFile(filePath); err != nil {
		t.Fatal(err)
	}
	if err := createStateDirectory(filePath); err == nil {
		t.Fatal("file was accepted as an existing directory")
	}
}

func TestCreateStateFileWindows(t *testing.T) {
	user := ownershipTestUser(t)
	parent := filepath.Join(t.TempDir(), "existing")
	createOwnershipFixture(t, parent, true, "O:"+user.String()+"D:P(A;OICI;FA;;;"+user.String()+")(A;OICI;FA;;;WD)")
	path := filepath.Join(parent, "state.db")
	if err := createStateFile(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Size() != 0 || !owned(path, info) {
		t.Fatal("created database is not a private empty regular file")
	}
	assertStateWindowsDescriptor(t, stateWindowsTestDescriptor(t, path), true)
	payload := []byte("preserve existing contents")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := createStateFile(path); !errors.Is(err, os.ErrExist) {
		t.Fatalf("existing database error = %v, want ErrExist", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != string(payload) {
		t.Fatal("existing database contents were changed")
	}
}

func TestCreateStateWindowsInheritedSidecars(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "state")
	if err := createStateDirectory(directory); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		t.Run(suffix, func(t *testing.T) {
			path := filepath.Join(directory, "state.db"+suffix)
			file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			info, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			if !owned(path, info) {
				t.Fatalf("trusted inherited sidecar ACL was rejected: %s", stateWindowsTestDescriptor(t, path).String())
			}
			descriptor := stateWindowsTestDescriptor(t, path)
			assertStateWindowsDescriptor(t, descriptor, false)
			dacl, _, err := descriptor.DACL()
			if err != nil {
				t.Fatal(err)
			}
			for index := uint32(0); index < uint32(dacl.AceCount); index++ {
				var ace *windows.ACCESS_ALLOWED_ACE
				if err := windows.GetAce(dacl, index, &ace); err != nil {
					t.Fatal(err)
				}
				if ace.Header.AceFlags&windows.INHERITED_ACE == 0 {
					t.Fatal("sidecar ACE was not inherited")
				}
			}
		})
	}
}

func TestCreateStateWindowsRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "state")
	if err := createStateDirectory(directory); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(directory, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	if err := createStateDirectory(link); err == nil {
		t.Fatal("existing directory symlink was accepted")
	}
	path := filepath.Join(directory, "state.db")
	if err := createStateFile(path); err != nil {
		t.Fatal(err)
	}
	fileLink := filepath.Join(root, "file-link")
	if err := os.Symlink(path, fileLink); err != nil {
		t.Fatal(err)
	}
	if err := createStateFile(fileLink); !errors.Is(err, os.ErrExist) {
		t.Fatalf("existing symlink error = %v, want ErrExist", err)
	}
}

func stateWindowsTestDescriptor(t *testing.T, path string) *windows.SECURITY_DESCRIPTOR {
	t.Helper()
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	return descriptor
}

func assertStateWindowsDescriptor(t *testing.T, descriptor *windows.SECURITY_DESCRIPTOR, protected bool) {
	t.Helper()
	user := ownershipTestUser(t)
	if !privateWindowsDescriptor(descriptor, user) {
		t.Fatal("security descriptor is not private")
	}
	control, _, err := descriptor.Control()
	if err != nil {
		t.Fatal(err)
	}
	if got := control&windows.SE_DACL_PROTECTED != 0; got != protected {
		t.Fatalf("DACL protected = %v, want %v", got, protected)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if dacl.AceCount != 3 {
		t.Fatalf("ACE count = %d, want 3", dacl.AceCount)
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		t.Fatal(err)
	}
	administrators, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]int{user.String(): 1}
	expected[system.String()]++
	expected[administrators.String()]++
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil {
			t.Fatal(err)
		}
		const fullControl = windows.STANDARD_RIGHTS_REQUIRED | windows.SYNCHRONIZE | 0x1ff
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Mask != fullControl {
			t.Fatal("expected full-control allow ACE")
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String()
		if expected[sid] == 0 {
			t.Fatal("unexpected or duplicate allowed principal")
		}
		expected[sid]--
		if protected && ace.Header.AceFlags&(windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE) !=
			windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE {
			t.Fatal("created DACL does not propagate to both files and directories")
		}
	}
}
