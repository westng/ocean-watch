package sqlite

import (
	"context"
	"database/sql"
	"errors"

	_ "modernc.org/sqlite"
)

// LoadCredentialFixture creates only the credential tables needed by focused tests.
// It intentionally does not initialize or upgrade a production state database.
func LoadCredentialFixture(ctx context.Context, db *sql.DB, migration []byte) error {
	if ctx == nil || db == nil || len(migration) == 0 {
		return errors.New("credential fixture inputs are required")
	}
	_, err := db.ExecContext(ctx, string(migration))
	return err
}
