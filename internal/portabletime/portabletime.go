// Package portabletime defines the timestamp representation used by portable
// records. PostgreSQL stores timestamptz values with microsecond precision, so
// portable instants must use UTC and must not contain sub-microsecond data.
package portabletime

// Requirements: REQ-EXCHANGE-001. Feature: migration.packages.

import (
	"fmt"
	"time"
)

// Normalize returns value in UTC, truncated to PostgreSQL's microsecond
// precision. Callers should normalize timestamps before they become owned
// domain state, rather than while exporting existing state.
func Normalize(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}

// Max returns the later of two normalized instants. It lets ordinary writes
// remain monotonic when they update state imported from a faster or
// intentionally future-dated source clock.
func Max(left, right time.Time) time.Time {
	left = Normalize(left)
	right = Normalize(right)
	if left.Before(right) {
		return right
	}
	return left
}

// IsCanonical reports whether value can round-trip through PostgreSQL without
// changing its instant or UTC representation.
func IsCanonical(value time.Time) bool {
	return value.Location() == time.UTC && value.Nanosecond()%int(time.Microsecond) == 0
}

// ParseCanonical parses an RFC 3339 instant and rejects representations that
// are not the exact UTC, microsecond-safe form emitted by FormatCanonical.
func ParseCanonical(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse portable instant: %w", err)
	}
	if !IsCanonical(parsed) || parsed.Format(time.RFC3339Nano) != value {
		return time.Time{}, fmt.Errorf("parse portable instant: value is not canonical")
	}
	return parsed, nil
}

// FormatCanonical formats a canonical portable instant. It deliberately
// rejects legacy values that would change when converted to UTC or truncated.
func FormatCanonical(value time.Time) (string, error) {
	if !IsCanonical(value) {
		return "", fmt.Errorf("format portable instant: value is not canonical")
	}
	return value.Format(time.RFC3339Nano), nil
}
