package postgresutil

import (
	"context"
	"database/sql"
)

// WithSchemaMigrationLock serializes runtime schema changes for one PostgreSQL
// search path while allowing separate installations and test schemas to proceed.
func WithSchemaMigrationLock(ctx context.Context, db *sql.DB, migrate func(context.Context, *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('asterrouter:schema-migration:' || current_schema(), 0))`); err != nil {
		return err
	}
	if err := migrate(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}
