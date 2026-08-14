package portabletime_test

// Requirements: REQ-EXCHANGE-001. Feature: migration.packages.

import (
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/portabletime"
)

func TestNormalizeUsesUTCMicrosecondPrecision(t *testing.T) {
	t.Parallel()

	zone := time.FixedZone("source", -6*60*60)
	input := time.Date(2026, time.August, 13, 12, 34, 56, 123456789, zone)
	want := time.Date(2026, time.August, 13, 18, 34, 56, 123456000, time.UTC)

	got := portabletime.Normalize(input)
	if !got.Equal(want) || got.Location() != time.UTC || got.Nanosecond() != want.Nanosecond() {
		t.Fatalf("Normalize() = %v (%v); want %v (%v)", got, got.Location(), want, want.Location())
	}
}

func TestCanonicalRoundTrip(t *testing.T) {
	t.Parallel()

	values := []string{
		"2026-08-13T18:34:56Z",
		"2026-08-13T18:34:56.1Z",
		"2026-08-13T18:34:56.123456Z",
	}
	for _, value := range values {
		value := value
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			parsed, err := portabletime.ParseCanonical(value)
			if err != nil {
				t.Fatalf("ParseCanonical() error = %v", err)
			}
			if !portabletime.IsCanonical(parsed) {
				t.Fatal("ParseCanonical() returned a noncanonical value")
			}
			formatted, err := portabletime.FormatCanonical(parsed)
			if err != nil {
				t.Fatalf("FormatCanonical() error = %v", err)
			}
			if formatted != value {
				t.Fatalf("FormatCanonical() = %q; want %q", formatted, value)
			}
		})
	}
}

func TestMaxNormalizesAndPreservesMonotonicTime(t *testing.T) {
	t.Parallel()

	clock := time.Date(2026, time.August, 13, 12, 0, 0, 123456789, time.FixedZone("clock", -5*60*60))
	future := time.Date(2027, time.August, 13, 12, 0, 0, 654321000, time.UTC)
	if got := portabletime.Max(clock, future); !got.Equal(future) || got.Location() != time.UTC {
		t.Fatalf("Max(clock, future) = %v (%v); want %v", got, got.Location(), future)
	}
	wantClock := portabletime.Normalize(clock)
	if got := portabletime.Max(clock, time.Time{}); !got.Equal(wantClock) || got.Location() != time.UTC {
		t.Fatalf("Max(clock, zero) = %v (%v); want %v", got, got.Location(), wantClock)
	}
}

func TestParseCanonicalRejectsLossyOrNoncanonicalRepresentations(t *testing.T) {
	t.Parallel()

	values := []string{
		"",
		"2026-08-13T18:34:56.123456789Z",
		"2026-08-13T18:34:56.123456000Z",
		"2026-08-13T13:34:56.123456-05:00",
		"2026-08-13T18:34:56+00:00",
		"2026-08-13t18:34:56z",
	}
	for _, value := range values {
		value := value
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			if _, err := portabletime.ParseCanonical(value); err == nil {
				t.Fatalf("ParseCanonical(%q) unexpectedly succeeded", value)
			}
		})
	}
}

func TestFormatCanonicalRejectsLossyValues(t *testing.T) {
	t.Parallel()

	canonical := time.Date(2026, time.August, 13, 18, 34, 56, 123456000, time.UTC)
	values := []time.Time{
		canonical.Add(time.Nanosecond),
		canonical.In(time.FixedZone("offset", -5*60*60)),
		time.Date(2026, time.August, 13, 18, 34, 56, 123456000, time.FixedZone("zero offset", 0)),
	}
	for _, value := range values {
		if portabletime.IsCanonical(value) {
			t.Fatalf("IsCanonical(%v, %v) = true", value, value.Location())
		}
		if _, err := portabletime.FormatCanonical(value); err == nil {
			t.Fatalf("FormatCanonical(%v, %v) unexpectedly succeeded", value, value.Location())
		}
	}
}
