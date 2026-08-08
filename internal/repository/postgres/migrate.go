package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

var migrationNamePattern = regexp.MustCompile(`^([0-9]{4})_([a-z0-9_]+)\.sql$`)

type migration struct {
	version  int64
	name     string
	checksum string
	contents string
}

// Migrate applies embedded, checksum-verified migrations under a PostgreSQL
// advisory transaction lock. It is safe to call on every process start.
func Migrate(ctx context.Context, database *sql.DB) error {
	if database == nil {
		return errors.New("database is required")
	}
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS stewardmesh_schema_migrations (
			version BIGINT PRIMARY KEY,
			name TEXT NOT NULL,
			checksum TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}

	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", int64(836_752_025)); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	for _, candidate := range migrations {
		var storedChecksum string
		err := transaction.QueryRowContext(
			ctx,
			"SELECT checksum FROM stewardmesh_schema_migrations WHERE version = $1",
			candidate.version,
		).Scan(&storedChecksum)
		switch {
		case err == nil:
			if storedChecksum != candidate.checksum {
				return fmt.Errorf("migration %04d checksum changed after application", candidate.version)
			}
			continue
		case !errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("read migration %04d: %w", candidate.version, err)
		}
		if _, err := transaction.ExecContext(ctx, candidate.contents); err != nil {
			return fmt.Errorf("apply migration %04d_%s: %w", candidate.version, candidate.name, err)
		}
		if _, err := transaction.ExecContext(
			ctx,
			"INSERT INTO stewardmesh_schema_migrations (version, name, checksum) VALUES ($1, $2, $3)",
			candidate.version,
			candidate.name,
			candidate.checksum,
		); err != nil {
			return fmt.Errorf("record migration %04d: %w", candidate.version, err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	return nil
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	result := make([]migration, 0, len(entries))
	seen := make(map[int64]string)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matches := migrationNamePattern.FindStringSubmatch(entry.Name())
		if matches == nil {
			return nil, fmt.Errorf("invalid migration filename %q", entry.Name())
		}
		version, err := strconv.ParseInt(matches[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse migration version %q: %w", matches[1], err)
		}
		if previous, exists := seen[version]; exists {
			return nil, fmt.Errorf("duplicate migration version %04d in %q and %q", version, previous, entry.Name())
		}
		contents, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", entry.Name(), err)
		}
		if strings.TrimSpace(string(contents)) == "" {
			return nil, fmt.Errorf("migration %q is empty", entry.Name())
		}
		digest := sha256.Sum256(contents)
		result = append(result, migration{
			version:  version,
			name:     matches[2],
			checksum: hex.EncodeToString(digest[:]),
			contents: string(contents),
		})
		seen[version] = entry.Name()
	}
	sort.Slice(result, func(i, j int) bool { return result[i].version < result[j].version })
	return result, nil
}
