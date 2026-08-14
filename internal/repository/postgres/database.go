// Package postgres provides the first durable repository adapter for
// StewardMesh. DynamoDB adapters must conform to the provider-neutral
// interfaces in internal/repository.
// Requirement: REQ-FOUNDATION-001.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/stdlib"
)

func Open(ctx context.Context, databaseURL string) (*sql.DB, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, errors.New("database URL is required")
	}
	configuration, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return nil, errors.New("database configuration is invalid")
	}
	database := stdlib.OpenDB(*configuration, stdlib.OptionAfterConnect(func(_ context.Context, connection *pgx.Conn) error {
		registerPortablePostgresTypes(connection.TypeMap())
		return nil
	}))
	database.SetMaxOpenConns(20)
	database.SetMaxIdleConns(5)
	database.SetConnMaxIdleTime(5 * time.Minute)
	database.SetConnMaxLifetime(30 * time.Minute)
	if err := database.PingContext(ctx); err != nil {
		database.Close()
		return nil, errors.New("connect to PostgreSQL: connection failed")
	}
	return database, nil
}

func registerPortablePostgresTypes(typeMap *pgtype.Map) {
	typeMap.RegisterType(&pgtype.Type{
		Name: "timestamptz", OID: pgtype.TimestamptzOID,
		Codec: &pgtype.TimestamptzCodec{ScanLocation: time.UTC},
	})
}
