package filesystem

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	applicationqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans/qianchuan"
	domainqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
)

func TestQianchuanPlanBindingStoreIsPrivateAndPreservesDailyIdentity(t *testing.T) {
	root := t.TempDir()
	store := QianchuanPlanBindingStore{Root: root}
	first := fixturePlanBinding(t, "2026-08-18", "2000000000000001")
	if err := store.Put(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	qianchuanRoot := filepath.Join(root, "qianchuan")
	path := filepath.Join(qianchuanRoot, qianchuanPlanBindingsFile)
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(qianchuanRoot); err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("binding directory mode=%v err=%v", infoMode(info), err)
		}
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("binding file mode=%v err=%v", infoMode(info), err)
		}
	}
	loaded, exists, err := store.Get(context.Background(), first.BusinessDate, first.GroupID)
	if err != nil || !exists || loaded.AdID != first.AdID {
		t.Fatalf("binding load=%#v exists=%t err=%v", loaded, exists, err)
	}
	conflict := first
	conflict.AdID = "2000000000000002"
	if err := store.Put(context.Background(), conflict); err == nil ||
		!strings.Contains(err.Error(), "another ad_id") {
		t.Fatalf("same-day binding conflict was accepted: %v", err)
	}
	second := fixturePlanBinding(t, "2026-08-19", "2000000000000002")
	if err := store.Put(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	bindings, err := store.List(context.Background())
	if err != nil || len(bindings) != 2 || bindings[0].BusinessDate != "2026-08-18" ||
		bindings[1].BusinessDate != "2026-08-19" {
		t.Fatalf("daily binding history=%#v err=%v", bindings, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"access_token", "cookie", "https://", "v.douyin.com"} {
		if strings.Contains(strings.ToLower(string(data)), forbidden) {
			t.Fatalf("binding store contains forbidden data %q: %s", forbidden, data)
		}
	}
}

func TestQianchuanPlanBindingStoreSerializesConcurrentWriters(t *testing.T) {
	store := QianchuanPlanBindingStore{Root: t.TempDir(), LockTimeout: 2 * time.Second}
	const count = 20
	start := make(chan struct{})
	errs := make(chan error, count)
	var wait sync.WaitGroup
	for index := 1; index <= count; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			binding := fixturePlanBinding(t, fmt.Sprintf("2026-08-%02d", index), fmt.Sprintf("200000000000%04d", index))
			errs <- store.Put(context.Background(), binding)
		}()
	}
	close(start)
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	bindings, err := store.List(context.Background())
	if err != nil || len(bindings) != count {
		t.Fatalf("concurrent writes lost updates: count=%d err=%v", len(bindings), err)
	}
}

func TestQianchuanPlanBindingStoreRejectsInvalidManagedState(t *testing.T) {
	t.Run("malformed and unknown JSON", func(t *testing.T) {
		for name, payload := range map[string]string{
			"malformed": `{`,
			"unknown":   `{"schema_version":1,"bindings":{},"unexpected":true}`,
		} {
			t.Run(name, func(t *testing.T) {
				root := t.TempDir()
				directory := filepath.Join(root, "qianchuan")
				if err := os.Mkdir(directory, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, qianchuanPlanBindingsFile), []byte(payload), 0o600); err != nil {
					t.Fatal(err)
				}
				if _, err := (QianchuanPlanBindingStore{Root: root}).List(context.Background()); err == nil {
					t.Fatalf("%s binding document was accepted", name)
				}
			})
		}
	})

	t.Run("binding file symbolic link", func(t *testing.T) {
		root := t.TempDir()
		directory := filepath.Join(root, "qianchuan")
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "outside.json")
		if err := os.WriteFile(target, []byte(`{"sentinel":true}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(directory, qianchuanPlanBindingsFile)); err != nil {
			t.Skipf("symbolic links unavailable: %v", err)
		}
		store := QianchuanPlanBindingStore{Root: root}
		if _, err := store.List(context.Background()); err == nil || !strings.Contains(err.Error(), "regular managed file") {
			t.Fatalf("binding symlink was accepted: %v", err)
		}
		if err := store.Put(context.Background(), fixturePlanBinding(t, "2026-08-18", "2000000000000001")); err == nil || !strings.Contains(err.Error(), "regular managed file") {
			t.Fatalf("binding write followed symlink: %v", err)
		}
		data, err := os.ReadFile(target)
		if err != nil || string(data) != `{"sentinel":true}` {
			t.Fatalf("symlink target changed: %q err=%v", data, err)
		}
	})

	t.Run("binding directory symbolic link", func(t *testing.T) {
		root := t.TempDir()
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(root, "qianchuan")); err != nil {
			t.Skipf("symbolic links unavailable: %v", err)
		}
		store := QianchuanPlanBindingStore{Root: root}
		if err := store.Put(context.Background(), fixturePlanBinding(t, "2026-08-18", "2000000000000001")); err == nil || !strings.Contains(err.Error(), "managed directory") {
			t.Fatalf("binding directory symlink was accepted: %v", err)
		}
		if _, err := os.Stat(filepath.Join(outside, qianchuanPlanBindingsFile)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("binding escaped managed root: %v", err)
		}
	})
}

func fixturePlanBinding(t *testing.T, businessDate, adID string) applicationqianchuan.PlanBinding {
	t.Helper()
	identity, err := domainqianchuan.NewPlanGroupIdentity(
		"1000000000000001", "qcpt_fixture", "4000000000000001",
		[]string{"5000000000000001"}, "随手po", "测试商务",
	)
	if err != nil {
		t.Fatal(err)
	}
	groupID, err := domainqianchuan.GroupID(identity)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := applicationqianchuan.NewPlanBinding(
		identity, groupID, businessDate, adID,
		time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	return binding
}

func infoMode(info os.FileInfo) os.FileMode {
	if info == nil {
		return 0
	}
	return info.Mode().Perm()
}
