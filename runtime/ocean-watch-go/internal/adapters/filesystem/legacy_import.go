package filesystem

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/legacy"
)

type LegacyJournalReader struct{ Root string }

func (reader LegacyJournalReader) ReadLegacyJournal(ctx context.Context, recordID string) ([]byte, error) {
	if ctx == nil || !legacy.ValidRecordID(recordID) {
		return nil, legacy.ErrUnsafeJournal
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(reader.Root) || filepath.Clean(reader.Root) != reader.Root || reader.Root == string(filepath.Separator) {
		return nil, legacy.ErrUnsafeJournal
	}
	root, err := openLegacyDirectory(reader.Root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	name := strings.TrimPrefix(recordID, "runs/")
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > legacy.MaxJournalBytes {
		return nil, legacy.ErrUnsafeJournal
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, legacy.ErrUnsafeJournal
	}
	payload, err := io.ReadAll(io.LimitReader(file, legacy.MaxJournalBytes+1))
	if err != nil {
		return nil, err
	}
	if len(payload) > legacy.MaxJournalBytes {
		return nil, legacy.ErrUnsafeJournal
	}
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	current, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !os.SameFile(opened, current) || opened.Size() != after.Size() || !opened.ModTime().Equal(after.ModTime()) || !current.Mode().IsRegular() {
		return nil, legacy.ErrSourceChanged
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return payload, nil
}

func openLegacyDirectory(path string) (*os.Root, error) {
	volume := filepath.VolumeName(path)
	root, err := os.OpenRoot(volume + string(filepath.Separator))
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimPrefix(strings.TrimPrefix(path, volume), string(filepath.Separator)), string(filepath.Separator))
	parts = append(parts, "runs")
	for _, part := range parts {
		info, err := root.Lstat(part)
		if err != nil {
			return nil, errors.Join(err, root.Close())
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, errors.Join(legacy.ErrUnsafeJournal, root.Close())
		}
		next, err := root.OpenRoot(part)
		if err != nil {
			return nil, errors.Join(err, root.Close())
		}
		opened, statErr := next.Stat(".")
		closeErr := root.Close()
		if statErr != nil || closeErr != nil || !os.SameFile(info, opened) {
			return nil, errors.Join(legacy.ErrUnsafeJournal, statErr, closeErr, next.Close())
		}
		root = next
	}
	return root, nil
}
