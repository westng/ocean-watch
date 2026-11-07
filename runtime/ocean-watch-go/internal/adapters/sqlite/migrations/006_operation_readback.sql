ALTER TABLE dispatch_attempts ADD COLUMN object_id TEXT NOT NULL DEFAULT '';

CREATE TABLE operation_readbacks (
    operation_id TEXT NOT NULL,
    operation_version INTEGER NOT NULL,
    item_id TEXT NOT NULL,
    object_id TEXT NOT NULL,
    evidence_digest TEXT NOT NULL CHECK (length(evidence_digest)=71 AND substr(evidence_digest,1,7)='sha256:'),
    observed_at TEXT NOT NULL,
    PRIMARY KEY (operation_id, operation_version, item_id),
    FOREIGN KEY (operation_id, operation_version, item_id) REFERENCES operation_items(operation_id, operation_version, item_id)
) STRICT;
