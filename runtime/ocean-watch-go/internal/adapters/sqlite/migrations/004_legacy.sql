CREATE TABLE legacy_mappings (
    operation_id TEXT PRIMARY KEY,
    version INTEGER NOT NULL CHECK (version = 1),
    target_schema INTEGER NOT NULL CHECK (target_schema = 1),
    migration_id TEXT NOT NULL UNIQUE,
    decoder_version TEXT NOT NULL,
    source_root TEXT NOT NULL,
    source_record TEXT NOT NULL,
    source_checksum TEXT NOT NULL CHECK (length(source_checksum) = 64),
    mapping_digest TEXT NOT NULL CHECK (length(mapping_digest) = 64),
    content_digest TEXT NOT NULL CHECK (length(content_digest) = 64),
    item_count INTEGER NOT NULL CHECK (item_count BETWEEN 1 AND 2000),
    source_operation_id TEXT NOT NULL,
    fingerprint_digest TEXT NOT NULL CHECK (length(fingerprint_digest) = 64),
    runtime_version TEXT NOT NULL,
    runtime_build TEXT NOT NULL,
    runtime_digest TEXT NOT NULL,
    journal_schema TEXT NOT NULL,
    snapshot_schema TEXT NOT NULL,
    contract_id TEXT NOT NULL,
    compatible_runtime TEXT NOT NULL,
    recovery_state TEXT NOT NULL CHECK (recovery_state IN ('history_read_only', 'pending_reconciliation_read_only', 'quarantined_unknown')),
    UNIQUE (source_root, source_record)
) STRICT;

CREATE TABLE legacy_items (
    operation_id TEXT NOT NULL REFERENCES legacy_mappings(operation_id),
    source_item_id TEXT NOT NULL,
    target_item_id TEXT NOT NULL UNIQUE,
    dispatch_key TEXT NOT NULL,
    request_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    promotion_id TEXT NOT NULL,
    ad_id TEXT NOT NULL,
    source_status TEXT NOT NULL,
    dispatch_state TEXT NOT NULL,
    reconciliation_state TEXT NOT NULL,
    evidence_digest TEXT NOT NULL CHECK (length(evidence_digest) = 64),
    channel TEXT NOT NULL,
    advertiser_id TEXT NOT NULL,
    recovery_state TEXT NOT NULL CHECK (recovery_state IN ('history_read_only', 'pending_reconciliation_read_only', 'quarantined_unknown')),
    reason TEXT NOT NULL,
    PRIMARY KEY (operation_id, source_item_id)
) STRICT;

CREATE TABLE legacy_evidence_keys (
    evidence_key TEXT NOT NULL,
    operation_id TEXT NOT NULL REFERENCES legacy_mappings(operation_id),
    PRIMARY KEY (evidence_key, operation_id)
) STRICT;

CREATE INDEX legacy_items_blockers ON legacy_items(recovery_state, channel, advertiser_id);
