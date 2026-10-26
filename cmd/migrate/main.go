// Command migrate applies the embedded database migrations.
//
//	go run ./cmd/migrate up        # apply all pending migrations
//	go run ./cmd/migrate down      # roll back the latest migration
//	go run ./cmd/migrate status    # list applied / pending migrations
//	go run ./cmd/migrate reset     # roll back everything
//
// Any goose command works; see https://github.com/pressly/goose.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/joho/godotenv"

	"github.com/iamBlissMedik/shopwave-go/internal/database"
)

func main() {
	_ = godotenv.Load()

	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: migrate <up|down|status|reset|version|...> [args]")
		os.Exit(2)
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is required")
		os.Exit(1)
	}
	if err := database.Migrate(context.Background(), url, os.Args[1], os.Args[2:]...); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}
