package bridge

// Requirement: SEC-MCP-001. Feature: integrations.protocols. GitHub: #14.

import (
	"crypto/sha256"
	"encoding/json"
)

func canonicalHash(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > 4<<10 {
		return nil, ErrInvalidInput
	}
	digest := sha256.Sum256(encoded)
	return append([]byte(nil), digest[:]...), nil
}
