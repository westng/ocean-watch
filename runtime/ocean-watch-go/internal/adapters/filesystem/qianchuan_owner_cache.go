package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	applicationqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans/qianchuan"
)

type QianchuanOwnerHintCache struct {
	Path        string
	LockTimeout time.Duration
	Now         func() time.Time
}

type qianchuanOwnerCacheDocument struct {
	SchemaVersion int                                            `json:"schema_version"`
	Advertisers   map[string]map[string]qianchuanOwnerCacheEntry `json:"advertisers"`
}

type qianchuanOwnerCacheEntry struct {
	AwemeID     string  `json:"aweme_id"`
	AwemeShowID *string `json:"aweme_show_id"`
	UpdatedAt   string  `json:"updated_at"`
}

func (cache QianchuanOwnerHintCache) Load(
	ctx context.Context,
	advertiserID string,
	itemIDs []string,
) (map[string]applicationqianchuan.OwnerHint, error) {
	if ctx == nil {
		return nil, errors.New("owner hint cache context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	advertiserID = strings.TrimSpace(advertiserID)
	if !positiveCacheID(advertiserID) {
		return nil, errors.New("owner hint cache advertiser_id is invalid")
	}
	document, err := cache.read()
	if err != nil {
		return nil, err
	}
	cutoff := cache.now().AddDate(0, 0, -applicationqianchuan.OwnerHintCacheTTLDays)
	rows := document.Advertisers[advertiserID]
	result := map[string]applicationqianchuan.OwnerHint{}
	for _, itemID := range uniqueCacheIDs(itemIDs) {
		row, ok := rows[itemID]
		if !ok || !positiveCacheID(row.AwemeID) {
			continue
		}
		updatedAt, ok := parseOwnerHintTimestamp(row.UpdatedAt)
		if !ok || updatedAt.Before(cutoff) {
			continue
		}
		showID := ""
		if row.AwemeShowID != nil {
			showID = strings.TrimSpace(*row.AwemeShowID)
		}
		result[itemID] = applicationqianchuan.OwnerHint{AwemeID: row.AwemeID, AwemeShowID: showID}
	}
	return result, nil
}

func (cache QianchuanOwnerHintCache) Store(
	ctx context.Context,
	advertiserID string,
	hints map[string]applicationqianchuan.OwnerHint,
) (int, error) {
	if ctx == nil {
		return 0, errors.New("owner hint cache context is required")
	}
	advertiserID = strings.TrimSpace(advertiserID)
	if !positiveCacheID(advertiserID) {
		return 0, errors.New("owner hint cache advertiser_id is invalid")
	}
	normalized := map[string]applicationqianchuan.OwnerHint{}
	for itemID, hint := range hints {
		itemID = strings.TrimSpace(itemID)
		hint.AwemeID = strings.TrimSpace(hint.AwemeID)
		hint.AwemeShowID = strings.TrimSpace(hint.AwemeShowID)
		if positiveCacheID(itemID) && positiveCacheID(hint.AwemeID) {
			normalized[itemID] = hint
		}
	}
	if len(normalized) == 0 {
		return 0, nil
	}
	path, err := cache.path()
	if err != nil {
		return 0, err
	}
	lock, err := AcquireLock(ctx, path+".lock", cache.LockTimeout)
	if err != nil {
		return 0, err
	}
	defer func() { _ = lock.Release() }()
	document, err := cache.read()
	if err != nil {
		return 0, err
	}
	if document.Advertisers == nil {
		document.Advertisers = map[string]map[string]qianchuanOwnerCacheEntry{}
	}
	rows := document.Advertisers[advertiserID]
	if rows == nil {
		rows = map[string]qianchuanOwnerCacheEntry{}
		document.Advertisers[advertiserID] = rows
	}
	timestamp := cache.now().Format(time.RFC3339Nano)
	for itemID, hint := range normalized {
		var showID *string
		if hint.AwemeShowID != "" {
			value := hint.AwemeShowID
			showID = &value
		}
		rows[itemID] = qianchuanOwnerCacheEntry{
			AwemeID: hint.AwemeID, AwemeShowID: showID, UpdatedAt: timestamp,
		}
	}
	pruneOwnerHintRows(rows, applicationqianchuan.OwnerHintCacheMaxEntries)
	payload, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return 0, fmt.Errorf("encode Qianchuan owner hint cache: %w", err)
	}
	payload = append(payload, '\n')
	if err := AtomicWritePrivateFile(path, payload); err != nil {
		return 0, fmt.Errorf("write Qianchuan owner hint cache: %w", err)
	}
	return len(normalized), nil
}

func (cache QianchuanOwnerHintCache) read() (qianchuanOwnerCacheDocument, error) {
	path, err := cache.path()
	if err != nil {
		return qianchuanOwnerCacheDocument{}, err
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return emptyQianchuanOwnerCache(), nil
	}
	if err != nil {
		return qianchuanOwnerCacheDocument{}, fmt.Errorf("read Qianchuan owner hint cache: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return qianchuanOwnerCacheDocument{}, fmt.Errorf("stat Qianchuan owner hint cache: %w", err)
	}
	if !info.Mode().IsRegular() {
		return qianchuanOwnerCacheDocument{}, errors.New("Qianchuan owner hint cache must be a regular file")
	}
	decoder := json.NewDecoder(io.LimitReader(file, 16<<20))
	var document qianchuanOwnerCacheDocument
	if err := decoder.Decode(&document); err != nil {
		return qianchuanOwnerCacheDocument{}, fmt.Errorf("decode Qianchuan owner hint cache: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return qianchuanOwnerCacheDocument{}, errors.New("Qianchuan owner hint cache contains trailing JSON")
	}
	if document.SchemaVersion != applicationqianchuan.OwnerHintCacheSchemaVersion || document.Advertisers == nil {
		return emptyQianchuanOwnerCache(), nil
	}
	return document, nil
}

func (cache QianchuanOwnerHintCache) path() (string, error) {
	path := filepath.Clean(strings.TrimSpace(cache.Path))
	if path == "." || path == string(filepath.Separator) || filepath.Base(path) != "qianchuan-work-owners.json" {
		return "", errors.New("Qianchuan owner hint cache path is invalid")
	}
	return path, nil
}

func (cache QianchuanOwnerHintCache) now() time.Time {
	now := time.Now().UTC()
	if cache.Now != nil {
		now = cache.Now().UTC()
	}
	return now
}

func emptyQianchuanOwnerCache() qianchuanOwnerCacheDocument {
	return qianchuanOwnerCacheDocument{
		SchemaVersion: applicationqianchuan.OwnerHintCacheSchemaVersion,
		Advertisers:   map[string]map[string]qianchuanOwnerCacheEntry{},
	}
}

func uniqueCacheIDs(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if !positiveCacheID(value) {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func positiveCacheID(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return value != "0"
}

func parseOwnerHintTimestamp(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			if parsed.Location() == time.Local {
				parsed = parsed.UTC()
			}
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func pruneOwnerHintRows(rows map[string]qianchuanOwnerCacheEntry, maximum int) {
	if len(rows) <= maximum {
		return
	}
	type retainedRow struct {
		itemID    string
		updatedAt time.Time
	}
	ordered := make([]retainedRow, 0, len(rows))
	for itemID, row := range rows {
		updatedAt, _ := parseOwnerHintTimestamp(row.UpdatedAt)
		ordered = append(ordered, retainedRow{itemID: itemID, updatedAt: updatedAt})
	}
	sort.Slice(ordered, func(left, right int) bool {
		if ordered[left].updatedAt.Equal(ordered[right].updatedAt) {
			return ordered[left].itemID < ordered[right].itemID
		}
		return ordered[left].updatedAt.After(ordered[right].updatedAt)
	})
	for _, row := range ordered[maximum:] {
		delete(rows, row.itemID)
	}
}
