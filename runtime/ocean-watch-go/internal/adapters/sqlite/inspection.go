package sqlite

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/state"
)

type Inspector struct {
	Path string
}

func (inspector Inspector) Inspect(ctx context.Context) (state.Inspection, error) {
	if ctx == nil {
		return state.Inspection{}, errors.New("state context is required")
	}
	if err := ctx.Err(); err != nil {
		return state.Inspection{}, err
	}
	path, err := preparePath(inspector.Path, false)
	if errors.Is(err, ErrNotInitialized) {
		return state.Inspection{Status: "not_initialized", Evidence: "filesystem_metadata", NextAction: "initialize_state_explicitly"}, nil
	}
	if err != nil {
		return state.Inspection{}, err
	}
	for _, sidecar := range []struct{ suffix, evidence string }{
		{"-wal", "wal_present_header_not_current"},
		{"-journal", "rollback_journal_present_recovery_unverified"},
		{"-shm", "shared_memory_without_wal_unverified"},
	} {
		if _, err := os.Lstat(path + sidecar.suffix); err == nil {
			return state.Inspection{Status: "unverified", Evidence: sidecar.evidence, NextAction: "validate_state_explicitly"}, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return state.Inspection{}, err
		}
	}
	file, err := os.Open(path)
	if err != nil {
		return state.Inspection{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return state.Inspection{}, err
	}
	if info.Size() == 0 {
		return state.Inspection{Status: "not_initialized", Evidence: "empty_database_file", NextAction: "initialize_state_explicitly"}, nil
	}
	var header [100]byte
	if _, err := io.ReadFull(file, header[:]); err != nil {
		return state.Inspection{Status: "blocked", Evidence: "invalid_database_header", NextAction: "inspect_state_explicitly"}, nil
	}
	if string(header[:16]) != "SQLite format 3\x00" || int(binary.BigEndian.Uint32(header[68:72])) != applicationID {
		return state.Inspection{Status: "blocked", Evidence: "unrecognized_database_header", NextAction: "inspect_state_explicitly"}, nil
	}
	result := state.Inspection{
		Status: "unverified", SchemaVersion: int(binary.BigEndian.Uint32(header[60:64])),
		Evidence: "main_file_header_only", NextAction: "validate_state_explicitly",
	}
	if result.SchemaVersion != SchemaVersion {
		result.Status = "blocked"
		result.NextAction = "use_compatible_runtime"
	}
	return result, nil
}
