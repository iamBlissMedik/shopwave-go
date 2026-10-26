package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver for goose
	"github.com/pressly/goose/v3"
)

// Migrations are embedded into the binary, so a deployed build can migrate its
// own database without shipping SQL files alongside it.
//
//go:embed migrations/*.sql
var migrations embed.FS

// Migrate runs a goose command ("up", "down", "status", "reset", "version", …)
// against the database at url using the embedded migrations.
func Migrate(ctx context.Context, url, command string, args ...string) error {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.RunContext(ctx, command, db, "migrations", args...)
}
