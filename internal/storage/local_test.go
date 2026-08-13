package storage

// Requirement: REQ-STORAGE-001. Feature: storage.blobs.

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalBlobStoreConformance(t *testing.T) {
	store, err := NewLocalBlobStore(t.TempDir(), 100)
	if err != nil {
		t.Fatal(err)
	}
	key := "example-org/0123456789abcdef0123456789abcdef"
	stored, err := store.Put(context.Background(), key, "text/plain", strings.NewReader("hello Vault"))
	if err != nil {
		t.Fatal(err)
	}
	if stored.SizeBytes != 11 || stored.SHA256 != "08e4352fdb12c6424bac7efd62a67492092252299b22ee38f1d0492862320196" {
		t.Fatalf("unexpected object metadata %#v", stored)
	}
	content, err := store.Open(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer content.Close()
	loaded, _ := io.ReadAll(content)
	if string(loaded) != "hello Vault" {
		t.Fatalf("unexpected content %q", loaded)
	}
	authorization, err := store.AuthorizeDownload(context.Background(), key, "hello.txt", 5*time.Minute)
	if err != nil || authorization.Token == "" {
		t.Fatalf("expected local download grant, got %#v %v", authorization, err)
	}
	if err := store.ValidateDownload(context.Background(), key, authorization.Token); err != nil {
		t.Fatalf("expected valid local grant: %v", err)
	}
	if err := store.ValidateDownload(context.Background(), key, authorization.Token+"tampered"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected tampered grant rejection, got %v", err)
	}
	if _, err := store.Put(context.Background(), key, "text/plain", strings.NewReader("duplicate")); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected duplicate conflict, got %v", err)
	}
	if err := store.Delete(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(context.Background(), key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found after delete, got %v", err)
	}
}

func TestLocalBlobStoreOpenHonorsCancellation(t *testing.T) {
	store, err := NewLocalBlobStore(t.TempDir(), 100)
	if err != nil {
		t.Fatal(err)
	}
	key := "example-org/0123456789abcdef0123456789abcdef"
	if _, err := store.Put(context.Background(), key, "text/plain", strings.NewReader("hello Vault")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	content, err := store.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	defer content.Close()
	if _, err := io.ReadAll(content); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled read, got %v", err)
	}
}

func TestLocalBlobStoreRejectsTraversalOversizeAndSymlinks(t *testing.T) {
	root := t.TempDir()
	store, err := NewLocalBlobStore(root, 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"../escape", "/absolute", "example-org/../escape", "example-org/not-a-random-id", "example.org/0123456789abcdef0123456789abcdef"} {
		if _, err := store.Put(context.Background(), key, "text/plain", strings.NewReader("nope")); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected key %q to be rejected, got %v", key, err)
		}
	}
	key := "example-org/0123456789abcdef0123456789abcdef"
	if _, err := store.Put(context.Background(), key, "text/plain", strings.NewReader("large")); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("expected size limit, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(key))); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("oversized partial blob was not removed")
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "example-org")); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	if _, err := store.Put(context.Background(), key, "text/plain", strings.NewReader("safe")); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("expected symbolic link to be rejected, got %v", err)
	}
}
