ALTER TABLE schema_migrations ADD COLUMN source_runtime TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE schema_migrations ADD COLUMN result TEXT NOT NULL DEFAULT 'succeeded' CHECK (result = 'succeeded');
ALTER TABLE schema_migrations ADD COLUMN backup_reference TEXT NOT NULL DEFAULT 'initial_creation';

CREATE TABLE migration_attempts (
    id TEXT PRIMARY KEY,
    from_version INTEGER NOT NULL,
    to_version INTEGER NOT NULL,
    source_runtime TEXT NOT NULL,
    backup_reference TEXT NOT NULL,
    result TEXT NOT NULL CHECK (result = 'succeeded'),
    completed_at TEXT NOT NULL
) STRICT;
