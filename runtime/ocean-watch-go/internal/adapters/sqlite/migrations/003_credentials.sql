CREATE TABLE credential_authorizations (
    channel TEXT NOT NULL CHECK (channel IN ('marketing', 'qianchuan', 'star_map')),
    authorization_id TEXT NOT NULL,
    credential_ref TEXT NOT NULL,
    token_revision INTEGER NOT NULL CHECK (token_revision > 0),
    state TEXT NOT NULL CHECK (state IN ('active', 'reauthorization_required')),
    updated_at TEXT NOT NULL,
    PRIMARY KEY (channel, authorization_id),
    UNIQUE (credential_ref)
) STRICT;

CREATE TABLE credential_versions (
    channel TEXT NOT NULL CHECK (channel IN ('marketing', 'qianchuan', 'star_map')),
    authorization_id TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK (revision > 0),
    credential_ref TEXT NOT NULL UNIQUE,
    credential_digest TEXT NOT NULL CHECK (credential_digest LIKE 'sha256:%' AND length(credential_digest) = 71),
    issued_at TEXT NOT NULL,
    access_expires_at TEXT NOT NULL,
    refresh_expires_at TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('candidate', 'active', 'retired')),
    PRIMARY KEY (channel, authorization_id, revision),
    FOREIGN KEY (channel, authorization_id) REFERENCES credential_authorizations(channel, authorization_id)
) STRICT;

CREATE TABLE credential_rotations (
    channel TEXT NOT NULL CHECK (channel IN ('marketing', 'qianchuan', 'star_map')),
    authorization_id TEXT NOT NULL,
    old_revision INTEGER NOT NULL CHECK (old_revision > 0),
    new_revision INTEGER NOT NULL CHECK (new_revision = old_revision + 1),
    old_ref TEXT NOT NULL,
    candidate_ref TEXT NOT NULL,
    candidate_digest TEXT,
    candidate_issued_at TEXT,
    candidate_access_expires_at TEXT,
    candidate_refresh_expires_at TEXT,
    state TEXT NOT NULL CHECK (state IN ('intent', 'candidate_stored', 'activated', 'completed', 'reauthorization_required')),
    started_at TEXT NOT NULL,
    activated_at TEXT,
    completed_at TEXT,
    failure_code TEXT,
    PRIMARY KEY (channel, authorization_id, new_revision),
    CHECK ((state IN ('intent', 'candidate_stored', 'activated', 'completed') AND failure_code IS NULL)
        OR (state = 'reauthorization_required' AND failure_code IS NOT NULL)),
    CHECK ((candidate_digest IS NULL AND candidate_issued_at IS NULL AND candidate_access_expires_at IS NULL AND candidate_refresh_expires_at IS NULL)
        OR (candidate_digest IS NOT NULL AND candidate_issued_at IS NOT NULL AND candidate_access_expires_at IS NOT NULL AND candidate_refresh_expires_at IS NOT NULL)),
    FOREIGN KEY (channel, authorization_id) REFERENCES credential_authorizations(channel, authorization_id)
) STRICT;

CREATE UNIQUE INDEX credential_rotations_pending
    ON credential_rotations(channel, authorization_id)
    WHERE state IN ('intent', 'candidate_stored', 'activated');
CREATE INDEX credential_rotations_recovery ON credential_rotations(state, channel, authorization_id);
CREATE INDEX credential_versions_active ON credential_versions(channel, authorization_id, state, revision);
