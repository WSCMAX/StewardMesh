package guard

// Requirement: SEC-GUARD-001.

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argon2MemoryKiB   uint32 = 19 * 1024
	argon2Iterations  uint32 = 2
	argon2Parallelism uint8  = 1
	argon2SaltBytes          = 16
	argon2KeyBytes    uint32 = 32
)

type Argon2idHasher struct{}

func NewArgon2idHasher() Argon2idHasher {
	return Argon2idHasher{}
}

func (Argon2idHasher) Hash(password string) (string, error) {
	if password == "" || len(password) > 1024 {
		return "", errors.New("password length is invalid")
	}
	salt := make([]byte, argon2SaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", errors.New("generate password salt")
	}
	passwordBytes := []byte(password)
	key := argon2.IDKey(passwordBytes, salt, argon2Iterations, argon2MemoryKiB, argon2Parallelism, argon2KeyBytes)
	clear(passwordBytes)
	encoded := fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argon2MemoryKiB,
		argon2Iterations,
		argon2Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
	clear(key)
	return encoded, nil
}

func (Argon2idHasher) Verify(password, encodedHash string) (bool, bool, error) {
	parameters, salt, expected, err := parseArgon2idHash(encodedHash)
	if err != nil {
		return false, false, err
	}
	if len(password) > 1024 {
		return false, false, nil
	}
	passwordBytes := []byte(password)
	actual := argon2.IDKey(
		passwordBytes,
		salt,
		parameters.iterations,
		parameters.memoryKiB,
		parameters.parallelism,
		uint32(len(expected)),
	)
	clear(passwordBytes)
	matches := subtle.ConstantTimeCompare(actual, expected) == 1
	clear(actual)
	needsRehash := parameters.memoryKiB != argon2MemoryKiB ||
		parameters.iterations != argon2Iterations ||
		parameters.parallelism != argon2Parallelism ||
		len(expected) != int(argon2KeyBytes)
	return matches, needsRehash, nil
}

type argon2idParameters struct {
	memoryKiB   uint32
	iterations  uint32
	parallelism uint8
}

func parseArgon2idHash(encoded string) (argon2idParameters, []byte, []byte, error) {
	if len(encoded) > 1024 {
		return argon2idParameters{}, nil, nil, errors.New("password hash is too large")
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return argon2idParameters{}, nil, nil, errors.New("password hash format is invalid")
	}
	var version int
	if count, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || count != 1 || version != argon2.Version {
		return argon2idParameters{}, nil, nil, errors.New("password hash version is invalid")
	}
	var parameters argon2idParameters
	if count, err := fmt.Sscanf(
		parts[3],
		"m=%d,t=%d,p=%d",
		&parameters.memoryKiB,
		&parameters.iterations,
		&parameters.parallelism,
	); err != nil || count != 3 {
		return argon2idParameters{}, nil, nil, errors.New("password hash parameters are invalid")
	}
	if parameters.memoryKiB < 8*1024 || parameters.memoryKiB > 1024*1024 ||
		parameters.iterations < 1 || parameters.iterations > 10 ||
		parameters.parallelism < 1 || parameters.parallelism > 16 {
		return argon2idParameters{}, nil, nil, errors.New("password hash parameters are outside safe bounds")
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil || len(salt) < 16 || len(salt) > 64 {
		return argon2idParameters{}, nil, nil, errors.New("password hash salt is invalid")
	}
	expected, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil || len(expected) < 16 || len(expected) > 64 {
		return argon2idParameters{}, nil, nil, errors.New("password hash key is invalid")
	}
	return parameters, salt, expected, nil
}
