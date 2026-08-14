package storage

// Requirement: REQ-STORAGE-001. Feature: storage.blobs.

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var objectKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,127}/[a-f0-9]{32}$`)

type LocalBlobStore struct {
	root       string
	maxSize    int64
	signingKey []byte
	now        func() time.Time
}

var _ ObjectStore = (*LocalBlobStore)(nil)

func NewLocalBlobStore(root string, maxSize int64) (*LocalBlobStore, error) {
	root = strings.TrimSpace(root)
	if root == "" || maxSize <= 0 {
		return nil, errors.New("blob root and positive max size are required")
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve blob root: %w", err)
	}
	if err := os.MkdirAll(absoluteRoot, 0o750); err != nil {
		return nil, fmt.Errorf("create blob root: %w", err)
	}
	rootInfo, err := os.Lstat(absoluteRoot)
	if err != nil {
		return nil, fmt.Errorf("inspect blob root: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return nil, errors.New("blob root must be a directory, not a symbolic link")
	}
	signingKey := make([]byte, 32)
	if _, err := rand.Read(signingKey); err != nil {
		return nil, fmt.Errorf("create local download signing key: %w", err)
	}
	return &LocalBlobStore{root: absoluteRoot, maxSize: maxSize, signingKey: signingKey, now: time.Now}, nil
}

func (*LocalBlobStore) Provider() string { return "local" }

func (s *LocalBlobStore) MaximumBytes() int64 { return s.maxSize }

func (s *LocalBlobStore) Put(ctx context.Context, key, _ string, content io.Reader) (StoredObject, error) {
	if ctx == nil || content == nil {
		return StoredObject{}, ErrInvalidInput
	}
	path, err := s.safePath(key)
	if err != nil {
		return StoredObject{}, err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return StoredObject{}, fmt.Errorf("create blob directory: %w", err)
	}
	if err := s.verifyDirectory(directory); err != nil {
		return StoredObject{}, err
	}
	temporary, err := os.CreateTemp(directory, ".upload-*")
	if err != nil {
		return StoredObject{}, fmt.Errorf("create temporary blob: %w", err)
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o640); err != nil {
		return StoredObject{}, fmt.Errorf("protect temporary blob: %w", err)
	}
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(temporary, hash), readerWithContext(ctx, io.LimitReader(content, s.maxSize+1)))
	if err != nil {
		return StoredObject{}, fmt.Errorf("write blob: %w", err)
	}
	if written > s.maxSize {
		return StoredObject{}, ErrTooLarge
	}
	if err := temporary.Sync(); err != nil {
		return StoredObject{}, fmt.Errorf("sync blob: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return StoredObject{}, fmt.Errorf("close blob: %w", err)
	}
	// Linking within the same directory creates the final name atomically and
	// never replaces an existing object. The temporary name is removed below.
	if err := os.Link(temporaryPath, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return StoredObject{}, ErrConflict
		}
		return StoredObject{}, fmt.Errorf("commit blob: %w", err)
	}
	if err := os.Remove(temporaryPath); err != nil {
		_ = os.Remove(path)
		return StoredObject{}, fmt.Errorf("remove temporary blob name: %w", err)
	}
	committed = true
	return StoredObject{SizeBytes: written, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func (s *LocalBlobStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}
	path, err := s.safePath(key)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("inspect blob: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, ErrIntegrity
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("open blob: %w", err)
	}
	return &contextReadCloser{Reader: readerWithContext(ctx, file), Closer: file}, nil
}

func (s *LocalBlobStore) Delete(_ context.Context, key string) error {
	path, err := s.safePath(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete blob: %w", err)
	}
	return nil
}

// Local downloads use a process-local HMAC grant in addition to the active
// Guard session. Restarting the process invalidates every outstanding grant.
func (s *LocalBlobStore) AuthorizeDownload(_ context.Context, key, _ string, ttl time.Duration) (ObjectDownloadAuthorization, error) {
	if !objectKeyPattern.MatchString(key) || ttl < time.Minute || ttl > 15*time.Minute {
		return ObjectDownloadAuthorization{}, ErrInvalidInput
	}
	expires := strconv.FormatInt(s.now().UTC().Add(ttl).Unix(), 10)
	mac := hmac.New(sha256.New, s.signingKey)
	_, _ = io.WriteString(mac, key+"\n"+expires)
	return ObjectDownloadAuthorization{Token: expires + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))}, nil
}

func (s *LocalBlobStore) ValidateDownload(_ context.Context, key, token string) error {
	parts := strings.Split(token, ".")
	if !objectKeyPattern.MatchString(key) || len(parts) != 2 {
		return ErrInvalidInput
	}
	expires, err := strconv.ParseInt(parts[0], 10, 64)
	provided, decodeErr := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || decodeErr != nil || len(provided) != sha256.Size {
		return ErrInvalidInput
	}
	mac := hmac.New(sha256.New, s.signingKey)
	_, _ = io.WriteString(mac, key+"\n"+parts[0])
	if !hmac.Equal(provided, mac.Sum(nil)) || s.now().UTC().Unix() >= expires {
		return ErrInvalidInput
	}
	return nil
}

func (s *LocalBlobStore) safePath(key string) (string, error) {
	if !objectKeyPattern.MatchString(key) || filepath.Separator != '/' && strings.ContainsRune(key, filepath.Separator) {
		return "", ErrInvalidInput
	}
	path := filepath.Join(s.root, filepath.FromSlash(key))
	relative, err := filepath.Rel(s.root, path)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", ErrInvalidInput
	}
	return path, nil
}

func (s *LocalBlobStore) verifyDirectory(directory string) error {
	relative, err := filepath.Rel(s.root, directory)
	if err != nil {
		return ErrIntegrity
	}
	current := s.root
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return ErrIntegrity
		}
	}
	return nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

type contextReadCloser struct {
	io.Reader
	io.Closer
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
