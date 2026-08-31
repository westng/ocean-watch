package filesystem

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
)

type AdvertiserLockStore struct {
	Root    string
	Timeout time.Duration
}

func (store AdvertiserLockStore) Acquire(
	ctx context.Context,
	scope domainplans.WriteScope,
) (func() error, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	root := filepath.Clean(store.Root)
	if root == "." || root == string(filepath.Separator) {
		return nil, errors.New("advertiser lock root is invalid")
	}
	name, err := advertiserLockName(scope)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create advertiser lock root: %w", err)
	}
	if _, err := validateManagedDirectory(root, "advertiser lock state root"); err != nil {
		return nil, err
	}
	locks, err := openManagedStateSubdirectory(
		root, "locks", "advertiser locks root", true,
	)
	if err != nil {
		return nil, err
	}
	defer locks.Close()
	lock, err := acquireManagedLockAt(
		ctx, locks, name, filepath.Join(root, "locks", name), "advertiser lock", store.Timeout,
	)
	if err != nil {
		return nil, err
	}
	return lock.Release, nil
}

func advertiserLockName(scope domainplans.WriteScope) (string, error) {
	switch scope.LockFamily {
	case domainplans.LockMarketingPlans:
		return "marketing-plans-" + scope.AdvertiserID + ".lock", nil
	case domainplans.LockQianchuanWorks:
		return "qianchuan-advertiser-" + scope.AdvertiserID + ".lock", nil
	case domainplans.LockPlanSettings:
		if scope.Channel == domainplans.ChannelQianchuan {
			return "qianchuan-advertiser-" + scope.AdvertiserID + ".lock", nil
		}
		return string(scope.Channel) + "-plan-settings-" + scope.AdvertiserID + ".lock", nil
	default:
		return "", errors.New("unsupported advertiser lock family")
	}
}

type OperationJournalStore struct {
	Root        string
	LockTimeout time.Duration
}

func (store OperationJournalStore) Load(
	ctx context.Context,
	runID string,
) (domainplans.Journal, error) {
	if err := domainplans.ValidateJournalID(runID); err != nil {
		return domainplans.Journal{}, err
	}
	if err := ctx.Err(); err != nil {
		return domainplans.Journal{}, err
	}
	root, name, err := store.openRunsRoot(runID, false)
	if err != nil {
		return domainplans.Journal{}, err
	}
	defer root.Close()
	info, err := root.Lstat(name)
	if err != nil {
		return domainplans.Journal{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return domainplans.Journal{}, errors.New("operation journal must be a regular managed file")
	}
	file, err := root.Open(name)
	if err != nil {
		return domainplans.Journal{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	var journal domainplans.Journal
	if err := decoder.Decode(&journal); err != nil {
		return domainplans.Journal{}, fmt.Errorf("decode operation journal: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return domainplans.Journal{}, errors.New("operation journal contains trailing JSON")
	}
	if _, err := domainplans.NewJournal(
		journal.Fingerprint, journal.Jobs, parseJournalTime(journal.CreatedAt),
	); err != nil {
		return domainplans.Journal{}, err
	}
	return journal, nil
}

func (store OperationJournalStore) Save(
	ctx context.Context,
	runID string,
	journal domainplans.Journal,
) error {
	if err := domainplans.ValidateJournalID(runID); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := domainplans.NewJournal(
		journal.Fingerprint, journal.Jobs, parseJournalTime(journal.CreatedAt),
	); err != nil {
		return err
	}
	payload, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	if containsSensitiveJournalData(payload) {
		return errors.New("operation journal contains a forbidden credential field")
	}
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	root, name, err := store.openRunsRoot(runID, true)
	if err != nil {
		return err
	}
	defer root.Close()
	if info, statErr := root.Lstat(name); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errors.New("operation journal must be a regular managed file")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	return atomicWritePrivateJSONAt(root, name, value)
}

func (store OperationJournalStore) List(
	ctx context.Context,
	prefix string,
) ([]domainplans.JournalRecord, error) {
	prefix = strings.TrimSpace(prefix)
	if err := domainplans.ValidateJournalID(strings.TrimSuffix(prefix, "-")); err != nil || !strings.HasSuffix(prefix, "-") {
		return nil, errors.New("operation journal prefix is invalid")
	}
	root, err := store.openRunsDirectory(false)
	if errors.Is(err, os.ErrNotExist) {
		return []domainplans.JournalRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil, fmt.Errorf("list operation journals: %w", err)
	}
	records := make([]domainplans.JournalRecord, 0)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".json") {
			continue
		}
		runID := strings.TrimSuffix(name, ".json")
		if err := domainplans.ValidateJournalID(runID); err != nil {
			return nil, fmt.Errorf("managed operation journal has an invalid run_id: %w", err)
		}
		info, err := root.Lstat(name)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, errors.New("operation journal must be a regular managed file")
		}
		file, err := root.Open(name)
		if err != nil {
			return nil, err
		}
		journal, decodeErr := decodeOperationJournal(file)
		closeErr := file.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("decode operation journal %s: %w", runID, decodeErr)
		}
		if closeErr != nil {
			return nil, closeErr
		}
		records = append(records, domainplans.JournalRecord{RunID: runID, Journal: journal})
	}
	sort.Slice(records, func(left, right int) bool { return records[left].RunID < records[right].RunID })
	return records, nil
}

func (store OperationJournalStore) AcquireScope(
	ctx context.Context,
	scopeFingerprint string,
) (func() error, error) {
	scopeFingerprint = strings.TrimSpace(scopeFingerprint)
	decoded, err := hex.DecodeString(scopeFingerprint)
	if err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != scopeFingerprint {
		return nil, errors.New("operation journal scope fingerprint is invalid")
	}
	stateRoot, _, err := store.managedRoots()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create operation journal state root: %w", err)
	}
	if _, err := validateManagedDirectory(stateRoot, "operation journal state root"); err != nil {
		return nil, err
	}
	locks, err := openManagedStateSubdirectory(
		stateRoot, "locks", "operation journal locks root", true,
	)
	if err != nil {
		return nil, err
	}
	defer locks.Close()
	name := "marketing-upload-scope-" + scopeFingerprint + ".lock"
	lock, err := acquireManagedLockAt(
		ctx, locks, name, filepath.Join(stateRoot, "locks", name),
		"operation journal scope lock", store.LockTimeout,
	)
	if err != nil {
		return nil, err
	}
	return lock.Release, nil
}

func acquireManagedLockAt(
	ctx context.Context,
	root *os.Root,
	name string,
	displayPath string,
	label string,
	timeout time.Duration,
) (*FileLock, error) {
	if info, err := root.Lstat(name); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s must be a regular managed file", label)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return acquireLockAt(ctx, root, name, displayPath, timeout)
}

func (store OperationJournalStore) ManagedPath(runID string) (string, error) {
	if err := domainplans.ValidateJournalID(runID); err != nil {
		return "", err
	}
	_, runsRoot, err := store.managedRoots()
	if err != nil {
		return "", err
	}
	if err := validateManagedRunsRoot(store.Root); err != nil {
		return "", err
	}
	path := filepath.Join(runsRoot, runID+".json")
	if info, statErr := os.Lstat(path); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", errors.New("operation journal must be a regular managed file")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", statErr
	}
	return path, nil
}

func (store OperationJournalStore) RunIDFromManagedPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("operation journal path is required")
	}
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	_, runsRoot, err := store.managedRoots()
	if err != nil {
		return "", err
	}
	parentRelation, err := filepath.Rel(runsRoot, filepath.Dir(absolute))
	if err != nil || parentRelation != "." {
		return "", errors.New("--journal must name a Plugin-managed file under state/runs")
	}
	if filepath.Ext(absolute) != ".json" {
		return "", errors.New("--journal must use a .json file name")
	}
	runID := strings.TrimSuffix(filepath.Base(absolute), ".json")
	if err := domainplans.ValidateJournalID(runID); err != nil {
		return "", err
	}
	managed, err := store.ManagedPath(runID)
	if err != nil {
		return "", err
	}
	managedRelation, err := filepath.Rel(managed, absolute)
	if err != nil || managedRelation != "." {
		return "", errors.New("--journal must name a Plugin-managed file under state/runs")
	}
	if _, statErr := os.Stat(runsRoot); statErr == nil {
		canonicalRoot, canonicalErr := filepath.EvalSymlinks(runsRoot)
		if canonicalErr != nil {
			return "", fmt.Errorf("resolve operation journal root: %w", canonicalErr)
		}
		canonicalParent, canonicalErr := filepath.EvalSymlinks(filepath.Dir(absolute))
		if canonicalErr != nil {
			return "", fmt.Errorf("resolve operation journal path: %w", canonicalErr)
		}
		canonicalRelation, relationErr := filepath.Rel(canonicalRoot, canonicalParent)
		if relationErr != nil || canonicalRelation != "." {
			return "", errors.New("--journal must name a Plugin-managed file under state/runs")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", statErr
	}
	return runID, nil
}

func (store OperationJournalStore) managedRoots() (string, string, error) {
	root := filepath.Clean(store.Root)
	if root == "." || root == string(filepath.Separator) {
		return "", "", errors.New("operation journal root is invalid")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", "", err
	}
	if absolute == string(filepath.Separator) {
		return "", "", errors.New("operation journal root is invalid")
	}
	return absolute, filepath.Join(absolute, "runs"), nil
}

func validateManagedRunsRoot(root string) error {
	stateRoot, runsRoot, err := (OperationJournalStore{Root: root}).managedRoots()
	if err != nil {
		return err
	}
	stateExists, err := validateManagedDirectory(stateRoot, "operation journal state root")
	if err != nil {
		return err
	}
	runsExists, err := validateManagedDirectory(runsRoot, "operation journal runs root")
	if err != nil {
		return err
	}
	if !stateExists || !runsExists {
		return nil
	}
	canonicalState, err := filepath.EvalSymlinks(stateRoot)
	if err != nil {
		return fmt.Errorf("resolve operation journal state root: %w", err)
	}
	canonicalRuns, err := filepath.EvalSymlinks(runsRoot)
	if err != nil {
		return fmt.Errorf("resolve operation journal runs root: %w", err)
	}
	relation, err := filepath.Rel(canonicalState, canonicalRuns)
	if err != nil || relation != "runs" {
		return errors.New("operation journal runs root escapes the managed state root")
	}
	return nil
}

func validateManagedDirectory(path string, label string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s: %w", label, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, fmt.Errorf("%s must be a managed directory", label)
	}
	return true, nil
}

func (store OperationJournalStore) openRunsRoot(
	runID string,
	create bool,
) (*os.Root, string, error) {
	if err := domainplans.ValidateJournalID(runID); err != nil {
		return nil, "", err
	}
	root, err := store.openRunsDirectory(create)
	if err != nil {
		return nil, "", err
	}
	return root, runID + ".json", nil
}

func (store OperationJournalStore) openRunsDirectory(create bool) (*os.Root, error) {
	stateRoot, _, err := store.managedRoots()
	if err != nil {
		return nil, err
	}
	if create {
		if err := os.MkdirAll(stateRoot, 0o700); err != nil {
			return nil, fmt.Errorf("create operation journal state root: %w", err)
		}
		_ = os.Chmod(stateRoot, 0o700)
	}
	if err := validateManagedRunsRoot(store.Root); err != nil {
		return nil, err
	}
	return openManagedStateSubdirectory(
		stateRoot, "runs", "operation journal runs root", create,
	)
}

func openManagedStateSubdirectory(
	stateRoot string,
	name string,
	label string,
	create bool,
) (*os.Root, error) {
	state, err := os.OpenRoot(stateRoot)
	if err != nil {
		return nil, fmt.Errorf("open operation journal state root: %w", err)
	}
	info, err := state.Lstat(name)
	if errors.Is(err, os.ErrNotExist) && create {
		mkdirErr := state.Mkdir(name, 0o700)
		if mkdirErr == nil || errors.Is(mkdirErr, os.ErrExist) {
			info, err = state.Lstat(name)
		} else {
			err = mkdirErr
		}
	}
	if err != nil {
		return nil, errors.Join(err, state.Close())
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, errors.Join(
			fmt.Errorf("%s must be a managed directory", label),
			state.Close(),
		)
	}
	root, err := state.OpenRoot(name)
	closeErr := state.Close()
	if err != nil {
		return nil, errors.Join(fmt.Errorf("open %s: %w", label, err), closeErr)
	}
	if closeErr != nil {
		return nil, errors.Join(closeErr, root.Close())
	}
	return root, nil
}

func decodeOperationJournal(reader io.Reader) (domainplans.Journal, error) {
	decoder := json.NewDecoder(reader)
	var journal domainplans.Journal
	if err := decoder.Decode(&journal); err != nil {
		return domainplans.Journal{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return domainplans.Journal{}, errors.New("operation journal contains trailing JSON")
	}
	if _, err := domainplans.NewJournal(
		journal.Fingerprint, journal.Jobs, parseJournalTime(journal.CreatedAt),
	); err != nil {
		return domainplans.Journal{}, err
	}
	return journal, nil
}

func atomicWritePrivateJSONAt(root *os.Root, name string, value map[string]any) error {
	buffer := new(bytes.Buffer)
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("encode private JSON: %w", err)
	}
	return atomicWritePrivateBytesAt(root, name, buffer.Bytes())
}

func atomicWritePrivateBytesAt(root *os.Root, name string, payload []byte) error {
	var temporary *os.File
	var temporaryName string
	for attempt := 0; attempt < 16; attempt++ {
		random := make([]byte, 8)
		if _, err := rand.Read(random); err != nil {
			return fmt.Errorf("create operation journal temporary name: %w", err)
		}
		temporaryName = "." + name + "." + hex.EncodeToString(random) + ".tmp"
		file, err := root.OpenFile(temporaryName, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("create operation journal temporary file: %w", err)
		}
		temporary = file
		break
	}
	if temporary == nil {
		return errors.New("create operation journal temporary file: name collision")
	}
	keep := false
	defer func() {
		_ = temporary.Close()
		if !keep {
			_ = root.Remove(temporaryName)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(payload); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := root.Rename(temporaryName, name); err != nil {
		return fmt.Errorf("replace operation journal atomically: %w", err)
	}
	keep = true
	if err := root.Chmod(name, 0o600); err != nil {
		return err
	}
	written, err := root.ReadFile(name)
	if err != nil {
		return fmt.Errorf("verify operation journal write: %w", err)
	}
	if !bytes.Equal(written, payload) {
		return errors.New("operation journal write verification failed")
	}
	if directory, err := root.Open("."); err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}

func parseJournalTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}

func containsSensitiveJournalData(payload []byte) bool {
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return true
	}
	return containsSensitiveValue(value)
}

func containsSensitiveValue(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "access_token", "refresh_token", "app_secret", "secret", "auth_code", "authorization":
				return true
			}
			if containsSensitiveValue(item) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if containsSensitiveValue(item) {
				return true
			}
		}
	}
	return false
}
