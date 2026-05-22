package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrNotFound is returned by repositories when a row is missing.
var ErrNotFound = errors.New("not found")

// Querier is the minimal database surface used by every repository.
// Both *pgxpool.Pool and pgx.Tx satisfy it, which lets the same repository
// type participate in pool-level queries or transactional flows without
// any code duplication.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
