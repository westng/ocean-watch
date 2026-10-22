package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/legacy"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/state"
)

var _ state.LegacyMappings = (*Store)(nil)

func (store *Store) ImportLegacyMapping(ctx context.Context, mapping legacy.Mapping) (legacy.Mapping, bool, error) {
	if err := stateContext(ctx); err != nil {
		return legacy.Mapping{}, false, err
	}
	if err := store.ensureIdentity(); err != nil {
		return legacy.Mapping{}, false, err
	}
	if err := mapping.Validate(); err != nil {
		return legacy.Mapping{}, false, err
	}
	digest := legacyMappingDigest(mapping)
	contentDigest := legacyContentDigest(mapping)
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return legacy.Mapping{}, false, err
	}
	defer tx.Rollback()
	var previousDigest string
	err = tx.QueryRowContext(ctx, "SELECT mapping_digest FROM legacy_mappings WHERE source_root=? AND source_record=?", mapping.Source.RootID, mapping.Source.RecordID).Scan(&previousDigest)
	if err == nil {
		if previousDigest != digest {
			return legacy.Mapping{}, false, legacy.ErrMappingConflict
		}
		previous, err := readLegacyMapping(ctx, tx, mapping.Source.RootID, mapping.Source.RecordID)
		return previous, false, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return legacy.Mapping{}, false, err
	}
	keys := legacyEvidenceKeys(mapping)
	duplicates := map[string]bool{}
	for _, key := range keys {
		rows, err := tx.QueryContext(ctx, "SELECT operation_id FROM legacy_evidence_keys WHERE evidence_key=?", key)
		if err != nil {
			return legacy.Mapping{}, false, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return legacy.Mapping{}, false, err
			}
			duplicates[id] = true
		}
		err = errors.Join(rows.Err(), rows.Close())
		if err != nil {
			return legacy.Mapping{}, false, err
		}
	}
	if len(duplicates) != 0 {
		mapping = legacy.QuarantineDuplicate(mapping)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO legacy_mappings(operation_id,version,target_schema,migration_id,decoder_version,
		source_root,source_record,source_checksum,mapping_digest,content_digest,item_count,source_operation_id,fingerprint_digest,
		runtime_version,runtime_build,runtime_digest,journal_schema,snapshot_schema,contract_id,compatible_runtime,recovery_state)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, mapping.OperationID, mapping.Version, mapping.TargetSchema, mapping.MigrationID, mapping.Decoder,
		mapping.Source.RootID, mapping.Source.RecordID, mapping.Source.Checksum, digest, contentDigest, len(mapping.Items), mapping.SourceOperationID, mapping.FingerprintDigest,
		mapping.RuntimeVersion, mapping.RuntimeBuild, mapping.RuntimeDigest, mapping.JournalSchema, mapping.SnapshotSchema, mapping.ContractID, mapping.CompatibleRuntime, mapping.State)
	if err != nil {
		return legacy.Mapping{}, false, err
	}
	for _, item := range mapping.Items {
		_, err := tx.ExecContext(ctx, `INSERT INTO legacy_items(operation_id,source_item_id,target_item_id,dispatch_key,request_id,project_id,promotion_id,ad_id,
			source_status,dispatch_state,reconciliation_state,evidence_digest,channel,advertiser_id,recovery_state,reason) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			mapping.OperationID, item.SourceItemID, item.TargetItemID, item.DispatchKey, item.RequestID, item.ProjectID, item.PromotionID, item.AdID,
			item.Status, item.DispatchState, item.ReconciliationState, item.EvidenceDigest, item.Scope.Channel, item.Scope.AdvertiserID, item.State, item.Reason)
		if err != nil {
			return legacy.Mapping{}, false, err
		}
	}
	for _, key := range keys {
		if _, err := tx.ExecContext(ctx, "INSERT INTO legacy_evidence_keys(evidence_key,operation_id) VALUES(?,?)", key, mapping.OperationID); err != nil {
			return legacy.Mapping{}, false, err
		}
	}
	for id := range duplicates {
		if _, err := tx.ExecContext(ctx, "UPDATE legacy_mappings SET recovery_state='quarantined_unknown' WHERE operation_id=?", id); err != nil {
			return legacy.Mapping{}, false, err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE legacy_items SET recovery_state='quarantined_unknown',reason='duplicate_evidence' WHERE operation_id=?", id); err != nil {
			return legacy.Mapping{}, false, err
		}
	}
	if err := store.ensureIdentity(); err != nil {
		return legacy.Mapping{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return legacy.Mapping{}, false, err
	}
	return mapping, true, nil
}

func legacyEvidenceKeys(mapping legacy.Mapping) []string {
	seen := map[string]bool{}
	add := func(kind, value string) {
		if value != legacy.Unknown {
			seen[kind+":"+legacy.Digest([]byte(value))] = true
		}
	}
	add("operation", mapping.SourceOperationID)
	for _, item := range mapping.Items {
		add("request", item.RequestID)
		add("dispatch", item.DispatchKey)
		add("business", mapping.FingerprintDigest+":"+item.SourceItemID)
		if item.Scope.Known() {
			prefix := item.Scope.Channel + ":" + item.Scope.AdvertiserID + ":"
			if item.PromotionID != legacy.Unknown {
				add("promotion", prefix+item.PromotionID)
			}
			if item.AdID != legacy.Unknown {
				add("ad", prefix+item.AdID)
			}
		}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

type legacyQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (store *Store) GetLegacyMapping(ctx context.Context, rootID, recordID string) (legacy.Mapping, error) {
	if err := stateContext(ctx); err != nil {
		return legacy.Mapping{}, err
	}
	if err := store.ensureIdentity(); err != nil {
		return legacy.Mapping{}, err
	}
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return legacy.Mapping{}, err
	}
	defer tx.Rollback()
	return readLegacyMapping(ctx, tx, rootID, recordID)
}

func readLegacyMapping(ctx context.Context, db legacyQueryer, rootID, recordID string) (legacy.Mapping, error) {
	var result legacy.Mapping
	var mappingDigest, contentDigest string
	var itemCount int
	err := db.QueryRowContext(ctx, `SELECT operation_id,version,target_schema,migration_id,decoder_version,source_root,source_record,source_checksum,mapping_digest,content_digest,item_count,
		source_operation_id,fingerprint_digest,runtime_version,runtime_build,runtime_digest,journal_schema,snapshot_schema,contract_id,compatible_runtime,recovery_state
		FROM legacy_mappings WHERE source_root=? AND source_record=?`, rootID, recordID).Scan(&result.OperationID, &result.Version, &result.TargetSchema, &result.MigrationID, &result.Decoder,
		&result.Source.RootID, &result.Source.RecordID, &result.Source.Checksum, &mappingDigest, &contentDigest, &itemCount, &result.SourceOperationID, &result.FingerprintDigest,
		&result.RuntimeVersion, &result.RuntimeBuild, &result.RuntimeDigest, &result.JournalSchema, &result.SnapshotSchema, &result.ContractID, &result.CompatibleRuntime, &result.State)
	if errors.Is(err, sql.ErrNoRows) {
		return legacy.Mapping{}, legacy.ErrMappingNotFound
	}
	if err != nil {
		return legacy.Mapping{}, err
	}
	rows, err := db.QueryContext(ctx, `SELECT source_item_id,target_item_id,dispatch_key,request_id,project_id,promotion_id,ad_id,
		source_status,dispatch_state,reconciliation_state,evidence_digest,channel,advertiser_id,recovery_state,reason
		FROM legacy_items WHERE operation_id=? ORDER BY source_item_id`, result.OperationID)
	if err != nil {
		return legacy.Mapping{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item legacy.Item
		if err := rows.Scan(&item.SourceItemID, &item.TargetItemID, &item.DispatchKey, &item.RequestID, &item.ProjectID, &item.PromotionID, &item.AdID,
			&item.Status, &item.DispatchState, &item.ReconciliationState, &item.EvidenceDigest, &item.Scope.Channel, &item.Scope.AdvertiserID, &item.State, &item.Reason); err != nil {
			return legacy.Mapping{}, err
		}
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return legacy.Mapping{}, err
	}
	if itemCount != len(result.Items) || contentDigest != legacyContentDigest(result) {
		return legacy.Mapping{}, legacy.ErrWriteBlocked
	}
	if err := result.Validate(); err != nil {
		return legacy.Mapping{}, err
	}
	if mappingDigest != legacyMappingDigest(result) && !legacyDuplicateQuarantine(result) {
		return legacy.Mapping{}, legacy.ErrWriteBlocked
	}
	return result, nil
}

func (store *Store) LegacyBlockers(ctx context.Context, scope legacy.Scope) ([]legacy.Blocker, error) {
	if !scope.Known() {
		return nil, legacy.ErrWriteBlocked
	}
	if err := stateContext(ctx); err != nil {
		return nil, err
	}
	if err := store.ensureIdentity(); err != nil {
		return nil, err
	}
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT source_root,source_record FROM legacy_mappings ORDER BY operation_id`)
	if err != nil {
		return nil, err
	}
	var sources [][2]string
	for rows.Next() {
		var source [2]string
		if err := rows.Scan(&source[0], &source[1]); err != nil {
			rows.Close()
			return nil, err
		}
		sources = append(sources, source)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	var blockers []legacy.Blocker
	for _, source := range sources {
		mapping, err := readLegacyMapping(ctx, tx, source[0], source[1])
		if err != nil {
			return nil, errors.Join(legacy.ErrWriteBlocked, err)
		}
		for _, blocker := range mapping.Blockers() {
			if blocker.Blocks(scope) {
				blockers = append(blockers, blocker)
			}
		}
	}
	return blockers, nil
}

func legacyMappingDigest(mapping legacy.Mapping) string {
	mapping.Items = append([]legacy.Item(nil), mapping.Items...)
	sort.Slice(mapping.Items, func(i, j int) bool { return mapping.Items[i].SourceItemID < mapping.Items[j].SourceItemID })
	encoded, _ := json.Marshal(mapping)
	return legacy.Digest(encoded)
}

func legacyDuplicateQuarantine(mapping legacy.Mapping) bool {
	if mapping.State != legacy.QuarantinedUnknown {
		return false
	}
	for _, item := range mapping.Items {
		if item.State != legacy.QuarantinedUnknown || item.Reason != "duplicate_evidence" {
			return false
		}
	}
	return true
}

func legacyContentDigest(mapping legacy.Mapping) string {
	mapping.Items = append([]legacy.Item(nil), mapping.Items...)
	mapping.State = ""
	for i := range mapping.Items {
		mapping.Items[i].State = ""
		mapping.Items[i].Reason = ""
	}
	sort.Slice(mapping.Items, func(i, j int) bool { return mapping.Items[i].SourceItemID < mapping.Items[j].SourceItemID })
	encoded, _ := json.Marshal(mapping)
	return legacy.Digest(encoded)
}
