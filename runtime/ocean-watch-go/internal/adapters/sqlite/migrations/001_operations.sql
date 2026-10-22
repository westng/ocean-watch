CREATE TABLE schema_migrations (
    version INTEGER PRIMARY KEY CHECK (version > 0),
    checksum TEXT NOT NULL,
    applied_at TEXT NOT NULL
) STRICT;

CREATE TABLE operations (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    channel TEXT NOT NULL CHECK (channel IN ('marketing', 'qianchuan', 'star_map')),
    subject_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('preflight_ready')),
    current_version INTEGER NOT NULL CHECK (current_version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
) STRICT;

CREATE TABLE operation_versions (
    operation_id TEXT NOT NULL REFERENCES operations(id),
    version INTEGER NOT NULL CHECK (version > 0),
    operation_schema_version INTEGER NOT NULL CHECK (operation_schema_version = 1),
    runtime_version TEXT NOT NULL,
    input_digest TEXT NOT NULL,
    snapshot_digest TEXT NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    PRIMARY KEY (operation_id, version)
) STRICT;

CREATE TABLE preflight_snapshots (
    operation_id TEXT NOT NULL,
    operation_version INTEGER NOT NULL,
    payload_json TEXT NOT NULL CHECK (json_valid(payload_json) AND json_type(payload_json) = 'object'),
    PRIMARY KEY (operation_id, operation_version),
    FOREIGN KEY (operation_id, operation_version) REFERENCES operation_versions(operation_id, version)
) STRICT;

CREATE TRIGGER immutable_operation_identity
BEFORE UPDATE OF id, kind, channel, subject_id, created_at ON operations
BEGIN
    SELECT RAISE(ABORT, 'operation identity is immutable');
END;

CREATE TRIGGER immutable_operation_versions_update
BEFORE UPDATE ON operation_versions
BEGIN
    SELECT RAISE(ABORT, 'operation version is immutable');
END;

CREATE TRIGGER immutable_operation_versions_delete
BEFORE DELETE ON operation_versions
BEGIN
    SELECT RAISE(ABORT, 'operation version is immutable');
END;

CREATE TRIGGER immutable_preflight_update
BEFORE UPDATE ON preflight_snapshots
BEGIN
    SELECT RAISE(ABORT, 'preflight snapshot is immutable');
END;

CREATE TRIGGER immutable_preflight_delete
BEFORE DELETE ON preflight_snapshots
BEGIN
    SELECT RAISE(ABORT, 'preflight snapshot is immutable');
END;
