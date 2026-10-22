CREATE TABLE operation_states (
    operation_id TEXT NOT NULL,
    operation_version INTEGER NOT NULL CHECK (operation_version > 0),
    status TEXT NOT NULL CHECK (status IN ('draft', 'preparing', 'preflight_ready', 'awaiting_approval', 'approved', 'executing', 'blocked', 'cancelled', 'expired', 'succeeded', 'failed', 'partially_succeeded', 'unknown', 'reconciling')),
    snapshot_digest TEXT NOT NULL CHECK (length(snapshot_digest) = 71 AND substr(snapshot_digest, 1, 7) = 'sha256:'),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (operation_id, operation_version),
    FOREIGN KEY (operation_id, operation_version) REFERENCES operation_versions(operation_id, version)
) STRICT;

CREATE TABLE approvals (
    id TEXT PRIMARY KEY,
    operation_id TEXT NOT NULL,
    operation_version INTEGER NOT NULL CHECK (operation_version > 0),
    snapshot_digest TEXT NOT NULL CHECK (length(snapshot_digest) = 71 AND substr(snapshot_digest, 1, 7) = 'sha256:'),
    scope_digest TEXT NOT NULL CHECK (length(scope_digest) = 71 AND substr(scope_digest, 1, 7) = 'sha256:'),
    provider_id TEXT NOT NULL,
    adapter_id TEXT NOT NULL,
    host_version TEXT NOT NULL,
    evidence_hash TEXT NOT NULL CHECK (length(evidence_hash) = 71 AND substr(evidence_hash, 1, 7) = 'sha256:'),
    nonce_hash TEXT NOT NULL UNIQUE CHECK (length(nonce_hash) = 71 AND substr(nonce_hash, 1, 7) = 'sha256:'),
    source_reference TEXT NOT NULL,
    issued_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    consumed_at TEXT,
    status TEXT NOT NULL CHECK (status IN ('valid', 'consumed', 'expired', 'rejected')),
    UNIQUE (operation_id, operation_version, id),
    FOREIGN KEY (operation_id, operation_version) REFERENCES operation_versions(operation_id, version)
) STRICT;

CREATE TABLE execution_leases (
    operation_id TEXT NOT NULL,
    operation_version INTEGER NOT NULL CHECK (operation_version > 0),
    owner_id TEXT NOT NULL,
    fencing_epoch INTEGER NOT NULL CHECK (fencing_epoch > 0),
    acquired_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    renewed_at TEXT NOT NULL,
    PRIMARY KEY (operation_id, operation_version),
    FOREIGN KEY (operation_id, operation_version) REFERENCES operation_versions(operation_id, version)
) STRICT;

CREATE TABLE operation_items (
    operation_id TEXT NOT NULL,
    operation_version INTEGER NOT NULL CHECK (operation_version > 0),
    item_id TEXT NOT NULL,
    group_id TEXT NOT NULL,
    action TEXT NOT NULL,
    target_id TEXT NOT NULL,
    expected_digest TEXT NOT NULL CHECK (length(expected_digest) = 71 AND substr(expected_digest, 1, 7) = 'sha256:'),
    payload_digest TEXT NOT NULL CHECK (length(payload_digest) = 71 AND substr(payload_digest, 1, 7) = 'sha256:'),
    status TEXT NOT NULL CHECK (status IN ('pending', 'intent', 'not_sent', 'acknowledged', 'unknown')),
    dispatch_key TEXT NOT NULL CHECK (length(dispatch_key) = 71 AND substr(dispatch_key, 1, 7) = 'sha256:'),
    failure_code TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (operation_id, operation_version, item_id),
    UNIQUE (operation_id, operation_version, dispatch_key),
    FOREIGN KEY (operation_id, operation_version) REFERENCES operation_versions(operation_id, version)
) STRICT;

CREATE TABLE dispatch_attempts (
    operation_id TEXT NOT NULL,
    operation_version INTEGER NOT NULL CHECK (operation_version > 0),
    item_id TEXT NOT NULL,
    attempt_no INTEGER NOT NULL CHECK (attempt_no > 0),
    dispatch_key TEXT NOT NULL CHECK (length(dispatch_key) = 71 AND substr(dispatch_key, 1, 7) = 'sha256:'),
    state TEXT NOT NULL CHECK (state IN ('intent', 'not_sent', 'acknowledged', 'unknown')),
    official_request_id TEXT NOT NULL,
    started_at TEXT NOT NULL,
    finished_at TEXT,
    response_digest TEXT CHECK (response_digest IS NULL OR (length(response_digest) = 71 AND substr(response_digest, 1, 7) = 'sha256:')),
    PRIMARY KEY (operation_id, operation_version, item_id, attempt_no),
    FOREIGN KEY (operation_id, operation_version, item_id) REFERENCES operation_items(operation_id, operation_version, item_id)
) STRICT;

CREATE INDEX operation_states_status ON operation_states(status, updated_at);
CREATE INDEX dispatch_attempts_state ON dispatch_attempts(state, started_at);

CREATE TRIGGER operation_state_transition
BEFORE UPDATE OF status ON operation_states
WHEN NOT (
    OLD.status = NEW.status OR
    (OLD.status = 'draft' AND NEW.status IN ('preparing', 'blocked', 'cancelled')) OR
    (OLD.status = 'preparing' AND NEW.status IN ('preflight_ready', 'blocked', 'cancelled')) OR
    (OLD.status = 'preflight_ready' AND NEW.status IN ('awaiting_approval', 'blocked', 'cancelled', 'expired')) OR
    (OLD.status = 'awaiting_approval' AND NEW.status IN ('approved', 'blocked', 'cancelled', 'expired')) OR
    (OLD.status = 'approved' AND NEW.status IN ('executing', 'cancelled', 'expired')) OR
    (OLD.status = 'executing' AND NEW.status IN ('succeeded', 'failed', 'partially_succeeded', 'unknown')) OR
    (OLD.status IN ('unknown', 'partially_succeeded') AND NEW.status = 'reconciling') OR
    (OLD.status = 'reconciling' AND NEW.status IN ('succeeded', 'failed', 'partially_succeeded', 'unknown'))
)
BEGIN
    SELECT RAISE(ABORT, 'invalid operation state transition');
END;

CREATE TRIGGER immutable_operation_state_identity
BEFORE UPDATE OF operation_id, operation_version, snapshot_digest, created_at ON operation_states
BEGIN
    SELECT RAISE(ABORT, 'operation state identity is immutable');
END;

CREATE TRIGGER immutable_operation_state_delete
BEFORE DELETE ON operation_states
BEGIN
    SELECT RAISE(ABORT, 'operation state is immutable');
END;

CREATE TRIGGER immutable_approval_identity
BEFORE UPDATE OF id, operation_id, operation_version, snapshot_digest, scope_digest, provider_id, adapter_id, host_version, evidence_hash, nonce_hash, source_reference, issued_at, expires_at ON approvals
BEGIN
    SELECT RAISE(ABORT, 'approval identity is immutable');
END;

CREATE TRIGGER immutable_approval_delete
BEFORE DELETE ON approvals
BEGIN
    SELECT RAISE(ABORT, 'approval is immutable');
END;

CREATE TRIGGER immutable_lease_identity
BEFORE UPDATE OF operation_id, operation_version, acquired_at ON execution_leases
BEGIN
    SELECT RAISE(ABORT, 'execution lease identity is immutable');
END;

CREATE TRIGGER immutable_item_identity
BEFORE UPDATE OF operation_id, operation_version, item_id, group_id, action, target_id, expected_digest, payload_digest, dispatch_key ON operation_items
BEGIN
    SELECT RAISE(ABORT, 'operation item identity is immutable');
END;

CREATE TRIGGER immutable_attempt_identity
BEFORE UPDATE OF operation_id, operation_version, item_id, attempt_no, dispatch_key, started_at ON dispatch_attempts
BEGIN
    SELECT RAISE(ABORT, 'dispatch attempt identity is immutable');
END;
