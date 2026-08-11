package storage

// Requirement: REQ-STORAGE-001. Feature: storage.blobs.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsv4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
)

var assumeRoleARNPattern = regexp.MustCompile(`^arn:(aws|aws-us-gov|aws-cn):iam::[0-9]{12}:role/[A-Za-z0-9+=,.@_/-]{1,512}$`)

type S3Config struct {
	Bucket          string
	Region          string
	EndpointURL     string
	ForcePathStyle  bool
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	RoleARN         string
	Encryption      string
	KMSKeyID        string
	MaximumBytes    int64
}

type s3ObjectAPI interface {
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

type s3PresignAPI interface {
	PresignGetObject(context.Context, *s3.GetObjectInput, ...func(*s3.PresignOptions)) (*awsv4.PresignedHTTPRequest, error)
}

type S3BlobStore struct {
	client     s3ObjectAPI
	presigner  s3PresignAPI
	bucket     string
	maxSize    int64
	encryption types.ServerSideEncryption
	kmsKeyID   string
}

var _ ObjectStore = (*S3BlobStore)(nil)

// NewS3BlobStore uses the AWS default credential chain for IAM roles, workload
// identity, and local profiles. Explicit credentials and STS assume-role are
// optional and never leave this constructor.
func NewS3BlobStore(ctx context.Context, configuration S3Config) (*S3BlobStore, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}
	if err := ValidateS3Config(configuration); err != nil {
		return nil, err
	}
	loadOptions := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(configuration.Region)}
	if configuration.AccessKeyID != "" {
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			configuration.AccessKeyID, configuration.SecretAccessKey, configuration.SessionToken,
		)))
	}
	awsConfiguration, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, errors.New("load S3 authentication configuration")
	}
	if configuration.RoleARN != "" {
		provider := stscreds.NewAssumeRoleProvider(sts.NewFromConfig(awsConfiguration), configuration.RoleARN)
		awsConfiguration.Credentials = aws.NewCredentialsCache(provider)
	}
	client := s3.NewFromConfig(awsConfiguration, func(options *s3.Options) {
		options.UsePathStyle = configuration.ForcePathStyle
		if configuration.EndpointURL != "" {
			options.BaseEndpoint = aws.String(configuration.EndpointURL)
		}
	})
	return newS3BlobStoreWithClients(configuration, client, s3.NewPresignClient(client))
}

func newS3BlobStoreWithClients(configuration S3Config, client s3ObjectAPI, presigner s3PresignAPI) (*S3BlobStore, error) {
	if err := ValidateS3Config(configuration); err != nil {
		return nil, err
	}
	if client == nil || presigner == nil {
		return nil, errors.New("S3 client and presigner are required")
	}
	return &S3BlobStore{
		client: client, presigner: presigner, bucket: configuration.Bucket,
		maxSize: configuration.MaximumBytes, encryption: types.ServerSideEncryption(configuration.Encryption),
		kmsKeyID: configuration.KMSKeyID,
	}, nil
}

func ValidateS3Config(configuration S3Config) error {
	configuration.Bucket = strings.TrimSpace(configuration.Bucket)
	configuration.Region = strings.TrimSpace(configuration.Region)
	if configuration.Bucket == "" || configuration.Region == "" || configuration.MaximumBytes <= 0 {
		return errors.New("S3 bucket, region, and positive maximum size are required")
	}
	if strings.ContainsAny(configuration.Bucket, "/\\\x00") || len(configuration.Bucket) > 255 {
		return errors.New("S3 bucket is invalid")
	}
	if (configuration.AccessKeyID == "") != (configuration.SecretAccessKey == "") ||
		(configuration.SessionToken != "" && configuration.AccessKeyID == "") {
		return errors.New("S3 explicit credentials require an access key and secret key")
	}
	if configuration.RoleARN != "" && !assumeRoleARNPattern.MatchString(configuration.RoleARN) {
		return errors.New("S3 assume-role ARN is invalid")
	}
	if configuration.Encryption != string(types.ServerSideEncryptionAes256) && configuration.Encryption != string(types.ServerSideEncryptionAwsKms) {
		return errors.New("S3 server-side encryption must be AES256 or aws:kms")
	}
	if configuration.Encryption == string(types.ServerSideEncryptionAwsKms) && strings.TrimSpace(configuration.KMSKeyID) == "" {
		return errors.New("S3 KMS encryption requires a KMS key id")
	}
	if configuration.Encryption != string(types.ServerSideEncryptionAwsKms) && configuration.KMSKeyID != "" {
		return errors.New("S3 KMS key id requires aws:kms encryption")
	}
	if configuration.EndpointURL != "" {
		endpoint, err := url.Parse(configuration.EndpointURL)
		if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" ||
			(endpoint.Path != "" && endpoint.Path != "/") || (endpoint.Scheme != "https" && endpoint.Scheme != "http") {
			return errors.New("S3 endpoint must be an HTTP or HTTPS origin without credentials, query, or path")
		}
		hostname := endpoint.Hostname()
		if endpoint.Scheme == "http" && !isLoopbackEndpoint(hostname) {
			return errors.New("S3 endpoints require TLS except for loopback development")
		}
		if address := net.ParseIP(hostname); address != nil &&
			(address.IsUnspecified() || address.IsMulticast() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast()) {
			return errors.New("S3 endpoint address is unsafe")
		}
	}
	return nil
}

func isLoopbackEndpoint(host string) bool {
	return strings.EqualFold(host, "localhost") || strings.EqualFold(host, "ip6-localhost") ||
		func() bool { address := net.ParseIP(host); return address != nil && address.IsLoopback() }()
}

func (*S3BlobStore) Provider() string { return "s3" }

func (s *S3BlobStore) MaximumBytes() int64 { return s.maxSize }

func (s *S3BlobStore) Put(ctx context.Context, key, mediaType string, content io.Reader) (StoredObject, error) {
	if ctx == nil || !objectKeyPattern.MatchString(key) || content == nil {
		return StoredObject{}, ErrInvalidInput
	}
	temporary, err := os.CreateTemp("", "stewardmesh-vault-*")
	if err != nil {
		return StoredObject{}, fmt.Errorf("create S3 upload buffer: %w", err)
	}
	defer func() { _ = temporary.Close(); _ = os.Remove(temporary.Name()) }()
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(temporary, hash), readerWithContext(ctx, io.LimitReader(content, s.maxSize+1)))
	if err != nil {
		return StoredObject{}, fmt.Errorf("buffer S3 upload: %w", err)
	}
	if written > s.maxSize {
		return StoredObject{}, ErrTooLarge
	}
	if _, err := temporary.Seek(0, io.SeekStart); err != nil {
		return StoredObject{}, fmt.Errorf("rewind S3 upload: %w", err)
	}
	digest := hash.Sum(nil)
	input := &s3.PutObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), Body: temporary,
		ContentLength: aws.Int64(written), ContentType: aws.String(mediaType),
		ChecksumSHA256:       aws.String(base64.StdEncoding.EncodeToString(digest)),
		ServerSideEncryption: s.encryption, IfNoneMatch: aws.String("*"),
	}
	if s.kmsKeyID != "" {
		input.SSEKMSKeyId = aws.String(s.kmsKeyID)
		input.BucketKeyEnabled = aws.Bool(true)
	}
	if _, err := s.client.PutObject(ctx, input); err != nil {
		if apiErrorCode(err) == "PreconditionFailed" || apiErrorCode(err) == "ConditionalRequestConflict" {
			return StoredObject{}, ErrConflict
		}
		return StoredObject{}, fmt.Errorf("put S3 object: %w", err)
	}
	return StoredObject{SizeBytes: written, SHA256: hex.EncodeToString(digest)}, nil
}

func (s *S3BlobStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if ctx == nil || !objectKeyPattern.MatchString(key) {
		return nil, ErrInvalidInput
	}
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key), ChecksumMode: types.ChecksumModeEnabled})
	if err != nil {
		if code := apiErrorCode(err); code == "NoSuchKey" || code == "NotFound" {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get S3 object: %w", err)
	}
	if output.Body == nil {
		return nil, ErrIntegrity
	}
	return output.Body, nil
}

func (s *S3BlobStore) Delete(ctx context.Context, key string) error {
	if ctx == nil || !objectKeyPattern.MatchString(key) {
		return ErrInvalidInput
	}
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return fmt.Errorf("delete S3 object: %w", err)
	}
	return nil
}

func (s *S3BlobStore) AuthorizeDownload(ctx context.Context, key, name string, ttl time.Duration) (ObjectDownloadAuthorization, error) {
	if ctx == nil || !objectKeyPattern.MatchString(key) || ttl < time.Minute || ttl > 15*time.Minute {
		return ObjectDownloadAuthorization{}, ErrInvalidInput
	}
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": name})
	request, err := s.presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), ResponseContentDisposition: aws.String(disposition),
	}, func(options *s3.PresignOptions) { options.Expires = ttl })
	if err != nil {
		return ObjectDownloadAuthorization{}, fmt.Errorf("authorize S3 download: %w", err)
	}
	parsed, err := url.Parse(request.URL)
	if err != nil || parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackEndpoint(parsed.Hostname())) {
		return ObjectDownloadAuthorization{}, ErrIntegrity
	}
	return ObjectDownloadAuthorization{URL: request.URL}, nil
}

// S3 downloads are validated by the provider's SigV4 URL rather than routed
// through the application content endpoint.
func (*S3BlobStore) ValidateDownload(context.Context, string, string) error { return ErrInvalidInput }

func apiErrorCode(err error) string {
	var apiError smithy.APIError
	if errors.As(err, &apiError) {
		return apiError.ErrorCode()
	}
	return ""
}
