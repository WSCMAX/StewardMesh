package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type LocalBlobStore struct {
	root    string
	maxSize int64
}

func NewLocalBlobStore(root string, maxSize int64) (*LocalBlobStore, error) {
	if root == "" || maxSize <= 0 {
		return nil, errors.New("blob root and positive max size are required")
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, err
	}
	return &LocalBlobStore{root: root, maxSize: maxSize}, nil
}

func (s *LocalBlobStore) Put(ctx context.Context, key string, content io.Reader) error {
	path, err := s.safePath(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return err
	}
	defer file.Close()
	limited := io.LimitReader(content, s.maxSize+1)
	if _, err := io.Copy(file, readerWithContext(ctx, limited)); err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() > s.maxSize {
		_ = os.Remove(path)
		return errors.New("blob exceeds configured size limit")
	}
	return nil
}

func (s *LocalBlobStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	path, err := s.safePath(key)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

func (s *LocalBlobStore) safePath(key string) (string, error) {
	if key == "" || filepath.IsAbs(key) || strings.ContainsRune(key, '\x00') {
		return "", errors.New("invalid blob key")
	}
	clean := filepath.Clean(key)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("blob key escapes storage root")
	}
	return filepath.Join(s.root, clean), nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func readerWithContext(ctx context.Context, r io.Reader) io.Reader {
	return &contextReader{ctx: ctx, r: r}
}

func (r *contextReader) Read(p []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.r.Read(p)
	}
}
