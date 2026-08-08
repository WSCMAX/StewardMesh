package storage

import (
	"context"
	"io"
)

type BlobStore interface {
	Put(ctx context.Context, key string, content io.Reader) error
	Open(ctx context.Context, key string) (io.ReadCloser, error)
}
