package postgres_test

// Requirements: REQ-FOUNDATION-001, REQ-EXCHANGE-001. Feature: migration.packages.

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/repository/postgres"
)

func TestOpenScansTimestamptzAsUTCMicroseconds(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}

	database, err := postgres.Open(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	input := time.Date(2026, time.August, 13, 12, 34, 56, 123456789, time.FixedZone("source", -5*60*60))
	var got time.Time
	if err := database.QueryRowContext(context.Background(), `SELECT $1::timestamptz`, input).Scan(&got); err != nil {
		t.Fatal(err)
	}
	want := input.UTC().Truncate(time.Microsecond)
	if got.Location() != time.UTC || !got.Equal(want) || got.Nanosecond() != want.Nanosecond() {
		t.Fatalf("timestamptz scan = %v (%v); want %v (%v)", got, got.Location(), want, want.Location())
	}
}
