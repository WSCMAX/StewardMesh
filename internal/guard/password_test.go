package guard

// Requirement: SEC-GUARD-001.

import (
	"strings"
	"testing"
)

func TestArgon2idHasherDoesNotStorePlaintext(t *testing.T) {
	hasher := NewArgon2idHasher()
	password := "correct horse battery staple"
	encoded, err := hasher.Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encoded, password) || !strings.HasPrefix(encoded, "$argon2id$") {
		t.Fatalf("unexpected password hash format %q", encoded)
	}
	matches, needsRehash, err := hasher.Verify(password, encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !matches || needsRehash {
		t.Fatalf("expected current hash to verify without rehash, matches=%t rehash=%t", matches, needsRehash)
	}
	matches, _, err = hasher.Verify("incorrect password", encoded)
	if err != nil {
		t.Fatal(err)
	}
	if matches {
		t.Fatal("expected incorrect password to fail")
	}
}

func TestArgon2idHasherRejectsUnsafeParameters(t *testing.T) {
	hasher := NewArgon2idHasher()
	_, _, err := hasher.Verify("password", "$argon2id$v=19$m=9999999,t=2,p=1$c2FsdHNhbHRzYWx0c2FsdA$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaA")
	if err == nil || !strings.Contains(err.Error(), "safe bounds") {
		t.Fatalf("expected excessive memory parameters to be rejected, got %v", err)
	}
}

func FuzzArgon2idHashParser(f *testing.F) {
	f.Add("")
	f.Add("$argon2id$v=19$m=19456,t=2,p=1$c2FsdHNhbHRzYWx0c2FsdA$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaA")
	f.Add(strings.Repeat("x", 2048))
	f.Fuzz(func(t *testing.T, encoded string) {
		_, _, _, _ = parseArgon2idHash(encoded)
	})
}
