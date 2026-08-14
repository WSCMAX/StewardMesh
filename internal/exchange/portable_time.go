package exchange

// Requirements: REQ-EXCHANGE-001. Feature: migration.packages.

import (
	"time"

	"github.com/maxlemke/stewardmesh/internal/portabletime"
)

func validatePortableInstants(minimumYear int, values ...time.Time) error {
	for _, value := range values {
		if value.IsZero() || value.Year() < minimumYear || value.Year() > 9999 {
			return ErrInvalidInput
		}
		if _, err := portabletime.FormatCanonical(value); err != nil {
			return ErrInvalidInput
		}
	}
	return nil
}

func validateOptionalPortableInstant(minimumYear int, value *time.Time) error {
	if value == nil {
		return nil
	}
	return validatePortableInstants(minimumYear, *value)
}

func parsePortableInstant(value string, minimumYear int) (time.Time, error) {
	parsed, err := portabletime.ParseCanonical(value)
	if err != nil || parsed.IsZero() || parsed.Year() < minimumYear || parsed.Year() > 9999 {
		return time.Time{}, ErrInvalidInput
	}
	return parsed, nil
}
