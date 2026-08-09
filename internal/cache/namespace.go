package cache

// Requirement: REQ-PLATFORM-VALKEY-001.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const maximumNamespaceSegmentBytes = 128

// Namespace builds deployment-, schema-, and organization-scoped keys.
// Variable dimensions are hashed so cache tooling and logs do not directly
// embed account names, client addresses, filters, or other input values.
type Namespace struct {
	prefix         string
	schemaVersion  string
	organizationID string
}

// NewNamespace creates a validated cache key namespace.
func NewNamespace(prefix, schemaVersion, organizationID string) (Namespace, error) {
	if !validNamespaceSegment(prefix) {
		return Namespace{}, fmt.Errorf("invalid cache namespace prefix")
	}
	if !validNamespaceSegment(schemaVersion) {
		return Namespace{}, fmt.Errorf("invalid cache namespace schema version")
	}
	if !validNamespaceSegment(organizationID) {
		return Namespace{}, fmt.Errorf("invalid cache namespace organization ID")
	}
	return Namespace{
		prefix:         prefix,
		schemaVersion:  schemaVersion,
		organizationID: organizationID,
	}, nil
}

// Key builds a stable key and SHA-256 hashes every variable dimension. Hashing
// prevents accidental raw-value disclosure but is not a confidentiality
// boundary; callers must HMAC low-entropy sensitive identifiers before use.
func (n Namespace) Key(resource string, dimensions ...string) (string, error) {
	if !validNamespaceSegment(n.prefix) ||
		!validNamespaceSegment(n.schemaVersion) ||
		!validNamespaceSegment(n.organizationID) {
		return "", fmt.Errorf("invalid cache namespace: %w", ErrInvalidKey)
	}
	if !validNamespaceSegment(resource) {
		return "", fmt.Errorf("invalid cache key resource: %w", ErrInvalidKey)
	}
	var key strings.Builder
	key.WriteString(n.prefix)
	key.WriteByte(':')
	key.WriteString(n.schemaVersion)
	key.WriteString(":org:")
	key.WriteString(n.organizationID)
	key.WriteByte(':')
	key.WriteString(resource)
	for _, dimension := range dimensions {
		if dimension == "" {
			return "", fmt.Errorf("cache key dimensions must not be empty: %w", ErrInvalidKey)
		}
		digest := sha256.Sum256([]byte(dimension))
		key.WriteByte(':')
		key.WriteString(hex.EncodeToString(digest[:]))
	}
	if key.Len() > maximumKeyBytes {
		return "", ErrInvalidKey
	}
	return key.String(), nil
}

func validNamespaceSegment(value string) bool {
	if len(value) == 0 || len(value) > maximumNamespaceSegmentBytes {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			character == '-' || character == '_' || character == '.' {
			continue
		}
		return false
	}
	return true
}
