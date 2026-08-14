package postgres

// Requirements: REQ-FOUNDATION-001, REQ-EXCHANGE-001. Feature: migration.packages.

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestPortablePostgresTimestampCodecScansUTC(t *testing.T) {
	typeMap := pgtype.NewMap()
	registerPortablePostgresTypes(typeMap)

	dataType, ok := typeMap.TypeForOID(pgtype.TimestamptzOID)
	if !ok {
		t.Fatal("timestamptz type is not registered")
	}
	codec, ok := dataType.Codec.(*pgtype.TimestamptzCodec)
	if !ok || codec.ScanLocation != time.UTC {
		t.Fatalf("timestamptz codec = %#v; want UTC scan location", dataType.Codec)
	}
}
