package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
)

var runIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,127}$`)

type RunStore struct {
	Root string
}

func ResolveRunsRoot(getenv func(string) string, userHome string) string {
	return filepath.Join(CodexHome(getenv, userHome), "ads-plan-monitor", "state", "runs")
}

func (store RunStore) List(ctx context.Context, limit int) ([]domain.RunSummary, error) {
	if limit < 1 || limit > 500 {
		return nil, errors.New("limit must be between 1 and 500")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	rootInfo, err := os.Lstat(store.Root)
	if errors.Is(err, os.ErrNotExist) {
		return []domain.RunSummary{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read runs root: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return nil, errors.New("run journal root must be a managed directory")
	}
	root, err := os.OpenRoot(store.Root)
	if err != nil {
		return nil, fmt.Errorf("open runs root: %w", err)
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil, fmt.Errorf("list run journals: %w", err)
	}
	rows := make([]domain.RunSummary, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		runID := strings.TrimSuffix(name, ".json")
		summary, _, readErr := readRun(root, name, runID)
		if readErr != nil {
			readable := false
			rows = append(rows, domain.RunSummary{RunID: runID, Readable: &readable})
			continue
		}
		rows = append(rows, summary)
	}
	sort.SliceStable(rows, func(left, right int) bool {
		return rows[left].UpdatedAt > rows[right].UpdatedAt
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

func (store RunStore) Show(ctx context.Context, runID string) (domain.RunSummary, domain.RunJournal, error) {
	runID = strings.TrimSpace(runID)
	if !runIDPattern.MatchString(runID) {
		return domain.RunSummary{}, domain.RunJournal{}, errors.New("run_id contains unsupported characters")
	}
	select {
	case <-ctx.Done():
		return domain.RunSummary{}, domain.RunJournal{}, ctx.Err()
	default:
	}
	rootInfo, err := os.Lstat(store.Root)
	if errors.Is(err, os.ErrNotExist) {
		return domain.RunSummary{}, domain.RunJournal{}, &RunNotFoundError{RunID: runID}
	}
	if err != nil {
		return domain.RunSummary{}, domain.RunJournal{}, fmt.Errorf("read runs root: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return domain.RunSummary{}, domain.RunJournal{}, errors.New("run journal root must be a managed directory")
	}
	root, err := os.OpenRoot(store.Root)
	if err != nil {
		return domain.RunSummary{}, domain.RunJournal{}, fmt.Errorf("open runs root: %w", err)
	}
	defer root.Close()
	return readRun(root, runID+".json", runID)
}

type RunNotFoundError struct {
	RunID string
}

func (err *RunNotFoundError) Error() string {
	return "run not found"
}

type RunUnreadableError struct {
	RunID string
	Cause error
}

func (err *RunUnreadableError) Error() string {
	return "run journal is unreadable"
}

func (err *RunUnreadableError) Unwrap() error {
	return err.Cause
}

type RunSchemaError struct {
	RunID string
}

func (err *RunSchemaError) Error() string {
	return "run journal has an invalid schema"
}

func readRun(root *os.Root, name, runID string) (domain.RunSummary, domain.RunJournal, error) {
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return domain.RunSummary{}, domain.RunJournal{}, &RunNotFoundError{RunID: runID}
	}
	if err != nil {
		return domain.RunSummary{}, domain.RunJournal{}, &RunUnreadableError{RunID: runID, Cause: err}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return domain.RunSummary{}, domain.RunJournal{}, errors.New("run journal symbolic links are not supported")
	}
	if !info.Mode().IsRegular() {
		return domain.RunSummary{}, domain.RunJournal{}, &RunUnreadableError{RunID: runID, Cause: errors.New("not a regular file")}
	}
	file, err := root.Open(name)
	if err != nil {
		return domain.RunSummary{}, domain.RunJournal{}, &RunUnreadableError{RunID: runID, Cause: err}
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return domain.RunSummary{}, domain.RunJournal{}, &RunUnreadableError{RunID: runID, Cause: err}
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return domain.RunSummary{}, domain.RunJournal{}, &RunUnreadableError{RunID: runID, Cause: err}
	}
	data, ok := decoded.(map[string]any)
	if !ok {
		return domain.RunSummary{}, domain.RunJournal{}, &RunSchemaError{RunID: runID}
	}
	jobsValue, exists := data["jobs"]
	if !exists {
		jobsValue = map[string]any{}
	}
	jobs, ok := jobsValue.(map[string]any)
	if !ok {
		return domain.RunSummary{}, domain.RunJournal{}, &RunSchemaError{RunID: runID}
	}
	statusCounts := map[string]int{}
	for _, value := range jobs {
		job, ok := value.(map[string]any)
		if !ok {
			return domain.RunSummary{}, domain.RunJournal{}, &RunSchemaError{RunID: runID}
		}
		status := stringValue(job["status"])
		if status == "" {
			status = "unknown"
		}
		statusCounts[status]++
	}
	updatedAt := float64(info.ModTime().Unix()) + float64(info.ModTime().Nanosecond())/float64(time.Second)
	summary := domain.RunSummary{
		RunID: runID, Kind: runKind(runID), SchemaVersion: data["schema_version"],
		CreatedAt: data["created_at"], Fingerprint: data["fingerprint"],
		JobCount: len(jobs), StatusCounts: statusCounts, UpdatedAt: updatedAt,
	}
	return summary, domain.RunJournal{Data: data}, nil
}

func runKind(runID string) string {
	if index := strings.LastIndex(runID, "-"); index >= 0 {
		return runID[:index]
	}
	return runID
}
