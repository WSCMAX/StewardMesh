package storage

// Requirement: REQ-STORAGE-001. Feature: storage.blobs.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsv4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

type fakeS3Client struct {
	put    *s3.PutObjectInput
	get    *s3.GetObjectInput
	delete *s3.DeleteObjectInput
	body   []byte
	err    error
}

func (f *fakeS3Client) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	f.put = input
	if f.err != nil {
		return nil, f.err
	}
	f.body, _ = io.ReadAll(input.Body)
	return &s3.PutObjectOutput{}, nil
}
func (f *fakeS3Client) GetObject(_ context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	f.get = input
	if f.err != nil {
		return nil, f.err
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(f.body))}, nil
}
func (f *fakeS3Client) DeleteObject(_ context.Context, input *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	f.delete = input
	return &s3.DeleteObjectOutput{}, f.err
}

type fakeS3Presigner struct {
	input   *s3.GetObjectInput
	expires time.Duration
	url     string
}

func (f *fakeS3Presigner) PresignGetObject(_ context.Context, input *s3.GetObjectInput, options ...func(*s3.PresignOptions)) (*awsv4.PresignedHTTPRequest, error) {
	f.input = input
	configured := s3.PresignOptions{}
	for _, option := range options {
		option(&configured)
	}
	f.expires = configured.Expires
	return &awsv4.PresignedHTTPRequest{URL: f.url, Method: "GET"}, nil
}

func validS3Config() S3Config {
	return S3Config{Bucket: "private-vault", Region: "us-east-1", Encryption: "AES256", MaximumBytes: 100}
}

func TestS3BlobStoreConformanceAndSecurityHeaders(t *testing.T) {
	client := &fakeS3Client{}
	presigner := &fakeS3Presigner{url: "https://private-vault.s3.example.test/object?temporary=signature"}
	store, err := newS3BlobStoreWithClients(validS3Config(), client, presigner)
	if err != nil {
		t.Fatal(err)
	}
	key := "example-org/0123456789abcdef0123456789abcdef"
	stored, err := store.Put(context.Background(), key, "text/plain", strings.NewReader("hello Vault"))
	if err != nil {
		t.Fatal(err)
	}
	if stored.SizeBytes != 11 || stored.SHA256 == "" || string(client.body) != "hello Vault" ||
		client.put.ServerSideEncryption != types.ServerSideEncryptionAes256 || aws.ToString(client.put.IfNoneMatch) != "*" ||
		aws.ToString(client.put.ChecksumSHA256) == "" || aws.ToInt64(client.put.ContentLength) != 11 {
		t.Fatalf("unexpected secure put input %#v metadata=%#v", client.put, stored)
	}
	content, err := store.Open(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	loaded, _ := io.ReadAll(content)
	_ = content.Close()
	if string(loaded) != "hello Vault" || client.get.ChecksumMode != types.ChecksumModeEnabled {
		t.Fatal("expected checksummed object read")
	}
	authorization, err := store.AuthorizeDownload(context.Background(), key, "evidence file.txt", 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(authorization.URL, "temporary=signature") || presigner.expires != 5*time.Minute ||
		!strings.Contains(aws.ToString(presigner.input.ResponseContentDisposition), "attachment") {
		t.Fatalf("unexpected authorization %#v", authorization)
	}
	if err := store.Delete(context.Background(), key); err != nil || client.delete == nil {
		t.Fatalf("expected delete, got %v", err)
	}
}

func TestS3BlobStoreRejectsUnsafeConfigurationAndLimits(t *testing.T) {
	tests := []S3Config{
		{Bucket: "private", Region: "us-east-1", Encryption: "", MaximumBytes: 100},
		{Bucket: "private", Region: "us-east-1", Encryption: "AES256", KMSKeyID: "secret-key", MaximumBytes: 100},
		{Bucket: "private", Region: "us-east-1", Encryption: "aws:kms", MaximumBytes: 100},
		{Bucket: "private", Region: "us-east-1", Encryption: "AES256", EndpointURL: "http://169.254.169.254", MaximumBytes: 100},
		{Bucket: "private", Region: "us-east-1", Encryption: "AES256", EndpointURL: "https://user:secret@example.test", MaximumBytes: 100},
		{Bucket: "private", Region: "us-east-1", Encryption: "AES256", AccessKeyID: "only-key", MaximumBytes: 100},
		{Bucket: "private", Region: "us-east-1", Encryption: "AES256", RoleARN: "arn:aws:iam::invalid:role/vault", MaximumBytes: 100},
	}
	for _, configuration := range tests {
		if err := ValidateS3Config(configuration); err == nil || strings.Contains(err.Error(), "only-key") || strings.Contains(err.Error(), "user:secret") {
			t.Fatalf("expected redacted invalid config for %#v: %v", configuration, err)
		}
	}
	client := &fakeS3Client{}
	presigner := &fakeS3Presigner{url: "https://example.test/object"}
	store, err := newS3BlobStoreWithClients(S3Config{Bucket: "private", Region: "us-east-1", Encryption: "aws:kms", KMSKeyID: "alias/vault", MaximumBytes: 4}, client, presigner)
	if err != nil {
		t.Fatal(err)
	}
	key := "example-org/0123456789abcdef0123456789abcdef"
	if _, err := store.Put(context.Background(), key, "text/plain", strings.NewReader("large")); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("expected size limit, got %v", err)
	}
	if client.put != nil {
		t.Fatal("oversized content must not reach S3")
	}
}

type fakeAPIError struct{ code string }

func (e fakeAPIError) Error() string                 { return e.code }
func (e fakeAPIError) ErrorCode() string             { return e.code }
func (e fakeAPIError) ErrorMessage() string          { return e.code }
func (e fakeAPIError) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

func TestS3BlobStoreMapsMissingAndConflictingObjects(t *testing.T) {
	client := &fakeS3Client{err: fakeAPIError{code: "NoSuchKey"}}
	store, err := newS3BlobStoreWithClients(validS3Config(), client, &fakeS3Presigner{url: "https://example.test"})
	if err != nil {
		t.Fatal(err)
	}
	key := "example-org/0123456789abcdef0123456789abcdef"
	if _, err := store.Open(context.Background(), key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
	client.err = fakeAPIError{code: "PreconditionFailed"}
	if _, err := store.Put(context.Background(), key, "text/plain", strings.NewReader("x")); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}
