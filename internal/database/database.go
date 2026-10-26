// Package database owns the Postgres connection pool, migrations, and the small
// set of query helpers every store shares (filters, pagination, error checks).
package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB is satisfied by both *pgxpool.Pool and pgx.Tx, so store code runs the same
// inside or outside a transaction. Begin on a pgx.Tx opens a savepoint.
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Connect opens a connection pool and verifies the database is reachable.
// Sessions run in UTC so date bucketing (dashboards) is consistent everywhere.
// Pool size etc. can be tuned via the URL, e.g. ?pool_max_conns=20.
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	cfg.ConnConfig.RuntimeParams["timezone"] = "UTC"
	// Return timestamps in UTC (JSON "…Z") regardless of the server's local zone.
	cfg.AfterConnect = func(_ context.Context, conn *pgx.Conn) error {
		conn.TypeMap().RegisterType(&pgtype.Type{
			Name: "timestamptz", OID: pgtype.TimestamptzOID,
			Codec: &pgtype.TimestamptzCodec{ScanLocation: time.UTC},
		})
		return nil
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// Postgres error codes we react to. See https://www.postgresql.org/docs/current/errcodes-appendix.html
const (
	codeUniqueViolation     = "23505"
	codeForeignKeyViolation = "23503"
)

// IsUniqueViolation reports whether err is a unique-constraint violation. If
// constraint is non-empty it must also match the violated constraint's name.
func IsUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == codeUniqueViolation &&
		(constraint == "" || pgErr.ConstraintName == constraint)
}

// IsForeignKeyViolation reports whether err is a foreign-key violation (e.g.
// deleting a row that other rows still reference).
func IsForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == codeForeignKeyViolation
}

// DailyTotals runs a query returning (day date, total bigint) rows and collects
// them into a map for timeseries.Daily.
func DailyTotals(ctx context.Context, db DB, sql string, args ...any) (map[time.Time]int64, error) {
	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	totals := make(map[time.Time]int64)
	for rows.Next() {
		var day time.Time
		var total int64
		if err := rows.Scan(&day, &total); err != nil {
			return nil, err
		}
		totals[day] = total
	}
	return totals, rows.Err()
}
