package marketplace

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// execMigrationScript executes a complete migration file. Migration files are
// trusted repository SQL and intentionally contain multiple statements and
// dollar-quoted PL/pgSQL blocks, so they must use pgx's simple query protocol.
func execMigrationScript(ctx context.Context, pool *pgxpool.Pool, sql []byte) error {
	_, err := pool.Exec(ctx, string(sql), pgx.QueryExecModeSimpleProtocol)
	return err
}
