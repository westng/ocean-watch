package filesystem

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	applicationqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans/qianchuan"
)

const qianchuanPlanBindingsFile = "plan-bindings.json"

type QianchuanPlanBindingStore struct {
	Root        string
	LockTimeout time.Duration
}

func (store QianchuanPlanBindingStore) Get(ctx context.Context, businessDate, groupID string) (applicationqianchuan.PlanBinding, bool, error) {
	key, err := applicationqianchuan.BindingKey(groupID, businessDate)
	if err != nil {
		return applicationqianchuan.PlanBinding{}, false, err
	}
	document, err := store.read(ctx)
	if err != nil {
		return applicationqianchuan.PlanBinding{}, false, err
	}
	binding, exists := document.Bindings[key]
	return binding, exists, nil
}

func (store QianchuanPlanBindingStore) List(ctx context.Context) ([]applicationqianchuan.PlanBinding, error) {
	document, err := store.read(ctx)
	if err != nil {
		return nil, err
	}
	return applicationqianchuan.SortedPlanBindings(document), nil
}

func (store QianchuanPlanBindingStore) Put(ctx context.Context, binding applicationqianchuan.PlanBinding) error {
	if ctx == nil {
		return errors.New("plan binding context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := applicationqianchuan.ValidatePlanBinding(binding); err != nil {
		return err
	}
	root, err := store.openDirectory(true)
	if err != nil {
		return err
	}
	defer root.Close()
	lock, err := acquireManagedLockAt(
		ctx, root, "plan-bindings.lock", filepath.Join(store.Root, "qianchuan", "plan-bindings.lock"),
		"Qianchuan plan binding lock", store.LockTimeout,
	)
	if err != nil {
		return err
	}
	defer lock.Release()
	document, err := readPlanBindingDocument(root)
	if err != nil {
		return err
	}
	if existing, exists := document.Bindings[binding.BindingKey]; exists {
		if existing.AdID != binding.AdID {
			return errors.New("plan binding key already points to another ad_id")
		}
		binding.CreatedAt = existing.CreatedAt
	}
	document.Bindings[binding.BindingKey] = binding
	buffer := new(bytes.Buffer)
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(document); err != nil {
		return fmt.Errorf("encode Qianchuan plan bindings: %w", err)
	}
	if err := atomicWritePrivateBytesAt(root, qianchuanPlanBindingsFile, buffer.Bytes()); err != nil {
		return fmt.Errorf("write Qianchuan plan bindings: %w", err)
	}
	return nil
}

func (store QianchuanPlanBindingStore) read(ctx context.Context) (applicationqianchuan.PlanBindingDocument, error) {
	if ctx == nil {
		return applicationqianchuan.PlanBindingDocument{}, errors.New("plan binding context is required")
	}
	if err := ctx.Err(); err != nil {
		return applicationqianchuan.PlanBindingDocument{}, err
	}
	root, err := store.openDirectory(false)
	if errors.Is(err, os.ErrNotExist) {
		return emptyPlanBindingDocument(), nil
	}
	if err != nil {
		return applicationqianchuan.PlanBindingDocument{}, err
	}
	defer root.Close()
	return readPlanBindingDocument(root)
}

func (store QianchuanPlanBindingStore) openDirectory(create bool) (*os.Root, error) {
	stateRoot := filepath.Clean(strings.TrimSpace(store.Root))
	if stateRoot == "." || stateRoot == string(filepath.Separator) {
		return nil, errors.New("plan binding state root is invalid")
	}
	absolute, err := filepath.Abs(stateRoot)
	if err != nil || absolute == string(filepath.Separator) {
		return nil, errors.New("plan binding state root is invalid")
	}
	if create {
		if err := os.MkdirAll(absolute, 0o700); err != nil {
			return nil, fmt.Errorf("create plan binding state root: %w", err)
		}
		if err := os.Chmod(absolute, 0o700); err != nil {
			return nil, err
		}
	}
	if _, err := validateManagedDirectory(absolute, "plan binding state root"); err != nil {
		return nil, err
	}
	return openManagedStateSubdirectory(absolute, "qianchuan", "Qianchuan plan binding root", create)
}

func readPlanBindingDocument(root *os.Root) (applicationqianchuan.PlanBindingDocument, error) {
	info, err := root.Lstat(qianchuanPlanBindingsFile)
	if errors.Is(err, os.ErrNotExist) {
		return emptyPlanBindingDocument(), nil
	}
	if err != nil {
		return applicationqianchuan.PlanBindingDocument{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return applicationqianchuan.PlanBindingDocument{}, errors.New("Qianchuan plan bindings must be a regular managed file")
	}
	file, err := root.Open(qianchuanPlanBindingsFile)
	if err != nil {
		return applicationqianchuan.PlanBindingDocument{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 16<<20))
	decoder.DisallowUnknownFields()
	var document applicationqianchuan.PlanBindingDocument
	if err := decoder.Decode(&document); err != nil {
		return applicationqianchuan.PlanBindingDocument{}, fmt.Errorf("decode Qianchuan plan bindings: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return applicationqianchuan.PlanBindingDocument{}, errors.New("Qianchuan plan bindings contain trailing JSON")
	}
	if document.SchemaVersion != applicationqianchuan.PlanBindingSchemaVersion || document.Bindings == nil {
		return applicationqianchuan.PlanBindingDocument{}, errors.New("Qianchuan plan binding schema is unsupported")
	}
	for key, binding := range document.Bindings {
		if key != binding.BindingKey {
			return applicationqianchuan.PlanBindingDocument{}, errors.New("Qianchuan plan binding map key is invalid")
		}
		if err := applicationqianchuan.ValidatePlanBinding(binding); err != nil {
			return applicationqianchuan.PlanBindingDocument{}, fmt.Errorf("validate Qianchuan plan binding: %w", err)
		}
	}
	return document, nil
}

func emptyPlanBindingDocument() applicationqianchuan.PlanBindingDocument {
	return applicationqianchuan.PlanBindingDocument{
		SchemaVersion: applicationqianchuan.PlanBindingSchemaVersion,
		Bindings:      map[string]applicationqianchuan.PlanBinding{},
	}
}
