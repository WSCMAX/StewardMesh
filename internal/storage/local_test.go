package storage

import (
	"context"
	"strings"
	"testing"
)

func TestLocalBlobStoreRejectsTraversal(t *testing.T) {
	store, err := NewLocalBlobStore(t.TempDir(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), "../escape", strings.NewReader("nope")); err == nil {
		t.Fatal("expected traversal key to be rejected")
	}
}
