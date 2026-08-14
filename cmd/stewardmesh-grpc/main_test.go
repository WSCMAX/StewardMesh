package main

// Requirements: REQ-API-001, SEC-MCP-001. Feature: integrations.protocols. GitHub: #14.

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	stewardmeshv1 "github.com/maxlemke/stewardmesh/api/proto"
	"github.com/maxlemke/stewardmesh/internal/application"
	"github.com/maxlemke/stewardmesh/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/tap"
	"google.golang.org/grpc/test/bufconn"
)

func TestGRPCTransportRequiresTLSOffLoopback(t *testing.T) {
	for _, address := range []string{"127.0.0.1:9090", "[::1]:9090", "localhost:9090"} {
		if _, err := grpcTransportOptions(address, "", ""); err != nil {
			t.Fatalf("loopback plaintext %s should remain available for local adapters: %v", address, err)
		}
	}
	for _, address := range []string{"0.0.0.0:9090", ":9090", "grpc.example.test:9090"} {
		if _, err := grpcTransportOptions(address, "", ""); err == nil {
			t.Fatalf("non-loopback plaintext gRPC listener %s was accepted", address)
		}
	}
	for _, address := range []string{"127.0.0.1", "127.0.0.1:http", "127.0.0.1:65536"} {
		if _, err := grpcTransportOptions(address, "", ""); err == nil {
			t.Fatalf("invalid gRPC address %s was accepted", address)
		}
	}
	if _, err := grpcTransportOptions("127.0.0.1:9090", "certificate.pem", ""); err == nil {
		t.Fatal("incomplete gRPC TLS key pair was accepted")
	}
}

func TestRemoteGRPCConfigurationRequiresHTTPSAndSecureCookies(t *testing.T) {
	loopback := grpcRuntimeConfig{protectedAddress: defaultProtectedAddress, publicAddress: defaultPublicAddress}
	if err := validateRemoteGRPCConfiguration(loopback, config.Config{}); err != nil {
		t.Fatalf("loopback development configuration: %v", err)
	}
	remote := grpcRuntimeConfig{protectedAddress: "0.0.0.0:9090", publicAddress: "0.0.0.0:9091"}
	if err := validateRemoteGRPCConfiguration(remote, config.Config{AllowedOrigin: "http://stewardmesh.example.test"}); err == nil {
		t.Fatal("remote gRPC accepted an HTTP application origin")
	}
	if err := validateRemoteGRPCConfiguration(remote, config.Config{AllowedOrigin: "https://stewardmesh.example.test"}); err == nil {
		t.Fatal("remote gRPC accepted insecure session-cookie configuration")
	}
	if err := validateRemoteGRPCConfiguration(remote, config.Config{AllowedOrigin: "https://stewardmesh.example.test", SessionCookieSecure: true}); err != nil {
		t.Fatalf("secure remote gRPC configuration: %v", err)
	}
	if err := validateRemoteBootstrapConfiguration("0.0.0.0:9091", false, true); err == nil {
		t.Fatal("remote initial bootstrap was accepted without a deployment token")
	}
	for _, test := range []struct {
		address      string
		token, setup bool
	}{
		{address: "127.0.0.1:9091", setup: true},
		{address: "0.0.0.0:9091", token: true, setup: true},
		{address: "0.0.0.0:9091"},
	} {
		if err := validateRemoteBootstrapConfiguration(test.address, test.token, test.setup); err != nil {
			t.Fatalf("valid bootstrap exposure %#v: %v", test, err)
		}
	}
}

func TestAllDomainRuntimeSeparatesPublicAndProtectedSurfaces(t *testing.T) {
	app, cfg := newGRPCTestApplication(t)
	runtime, err := newGRPCRuntime(app, cfg.AllowedOrigin, cfg.SessionCookieSecure, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	protectedListener := listenBuffered(t)
	publicListener := listenBuffered(t)
	serverContext, stopServer := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() { serveDone <- runtime.serve(serverContext, protectedListener, publicListener) }()

	publicConnection := dialGRPC(t, "public", publicListener, insecure.NewCredentials())
	protectedConnection := dialGRPC(t, "protected", protectedListener, insecure.NewCredentials())
	callContext, cancelCalls := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelCalls()

	healthClient := healthpb.NewHealthClient(publicConnection)
	healthResponse, err := healthClient.Check(callContext, &healthpb.HealthCheckRequest{})
	if err != nil || healthResponse.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("public health response=%v err=%v", healthResponse, err)
	}
	if _, err := healthpb.NewHealthClient(protectedConnection).Check(callContext, &healthpb.HealthCheckRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("protected listener health code=%s err=%v", status.Code(err), err)
	}

	publicGuard := stewardmeshv1.NewGuardServiceClient(publicConnection)
	bootstrapStatus, err := publicGuard.GetBootstrapStatus(callContext, &stewardmeshv1.GetBootstrapStatusRequest{})
	if err != nil || !bootstrapStatus.GetRequired() {
		t.Fatalf("public bootstrap status=%#v err=%v", bootstrapStatus, err)
	}
	if _, err := stewardmeshv1.NewFoundationServiceClient(publicConnection).GetOrganization(callContext, &stewardmeshv1.GetOrganizationRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("protected method on public listener code=%s err=%v", status.Code(err), err)
	}
	oversized := &stewardmeshv1.BootstrapAdministratorRequest{
		Username: "oversized", Email: "oversized@example.test", Password: "correct horse battery staple",
		DisplayName: strings.Repeat("x", 70<<10),
	}
	if _, err := publicGuard.BootstrapAdministrator(callContext, oversized); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("oversized public request code=%s err=%v", status.Code(err), err)
	}
	headerContext := metadata.NewOutgoingContext(callContext, metadata.Pairs("x-oversized", strings.Repeat("h", maximumHeaderBytes*2)))
	if _, err := healthClient.Check(headerContext, &healthpb.HealthCheckRequest{}); err == nil {
		t.Fatal("oversized public metadata was accepted")
	}

	session, err := publicGuard.BootstrapAdministrator(callContext, &stewardmeshv1.BootstrapAdministratorRequest{
		Username: "grpc-admin", Email: "grpc-admin@example.test", DisplayName: "gRPC Administrator",
		Password: "correct horse battery staple", BootstrapToken: cfg.BootstrapToken,
	})
	if err != nil || session.GetSessionToken() == "" {
		t.Fatalf("public bootstrap session=%#v err=%v", session, err)
	}
	protectedGuard := stewardmeshv1.NewGuardServiceClient(protectedConnection)
	if _, err := protectedGuard.GetBootstrapStatus(callContext, &stewardmeshv1.GetBootstrapStatusRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("public method on protected listener code=%s err=%v", status.Code(err), err)
	}
	foundationClient := stewardmeshv1.NewFoundationServiceClient(protectedConnection)
	if _, err := foundationClient.GetOrganization(callContext, &stewardmeshv1.GetOrganizationRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unauthenticated protected method code=%s err=%v", status.Code(err), err)
	}
	authenticated := metadata.NewOutgoingContext(callContext, metadata.Pairs("authorization", "Bearer "+session.GetSessionToken()))
	organization, err := foundationClient.GetOrganization(authenticated, &stewardmeshv1.GetOrganizationRequest{})
	if err != nil || organization.GetId() != cfg.OrganizationID {
		t.Fatalf("protected organization=%#v err=%v", organization, err)
	}
	if clients, err := stewardmeshv1.NewBridgeServiceClient(protectedConnection).ListClients(authenticated, &stewardmeshv1.ListBridgeClientsRequest{}); err != nil || len(clients.GetItems()) != 0 {
		t.Fatalf("protected Bridge compatibility response=%#v err=%v", clients, err)
	}

	watchContext, stopWatch := context.WithTimeout(context.Background(), time.Second)
	healthWatch, err := healthClient.Watch(watchContext, &healthpb.HealthCheckRequest{})
	if err == nil {
		_, err = healthWatch.Recv()
	}
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("public health Watch code=%s err=%v", status.Code(err), err)
	}
	stopWatch()
	stopServer()
	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("dual-listener gRPC runtime did not stop after context cancellation")
	}
}

func TestGRPCRuntimeEnforcesTLS13(t *testing.T) {
	app, cfg := newGRPCTestApplication(t)
	certificateFile, keyFile, roots := writeTestTLSCertificate(t)
	protectedOptions, err := grpcTransportOptions("127.0.0.1:9090", certificateFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	publicOptions, err := grpcTransportOptions("127.0.0.1:9091", certificateFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := newGRPCRuntime(app, cfg.AllowedOrigin, cfg.SessionCookieSecure, protectedOptions, publicOptions)
	if err != nil {
		t.Fatal(err)
	}
	protectedListener := listenBuffered(t)
	publicListener := listenBuffered(t)
	serverContext, stopServer := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() { serveDone <- runtime.serve(serverContext, protectedListener, publicListener) }()

	legacyConnection := dialGRPC(t, "public-legacy", publicListener, credentials.NewTLS(&tls.Config{
		RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12,
	}))
	legacyContext, cancelLegacy := context.WithTimeout(context.Background(), time.Second)
	_, legacyErr := healthpb.NewHealthClient(legacyConnection).Check(legacyContext, &healthpb.HealthCheckRequest{})
	cancelLegacy()
	if legacyErr == nil {
		t.Fatal("TLS 1.2 client connected to the gRPC runtime")
	}

	modernConnection := dialGRPC(t, "public-modern", publicListener, credentials.NewTLS(&tls.Config{
		RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13,
	}))
	modernContext, cancelModern := context.WithTimeout(context.Background(), 5*time.Second)
	response, err := healthpb.NewHealthClient(modernConnection).Check(modernContext, &healthpb.HealthCheckRequest{})
	cancelModern()
	if err != nil || response.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("TLS 1.3 health response=%v err=%v", response, err)
	}
	stopServer()
	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("TLS gRPC runtime did not stop")
	}
}

func TestRPCLimiterRejectsBeforeAuthenticationAndReleases(t *testing.T) {
	global := newRPCLimiter(1)
	local := newRPCLimiter(1)
	var forwarded atomic.Int32
	tapHandle := limitedTap(func(ctx context.Context, _ *tap.Info) (context.Context, error) {
		forwarded.Add(1)
		return ctx, nil
	}, global, local)

	firstParent, cancelFirst := context.WithCancel(context.Background())
	first, err := tapHandle(firstParent, &tap.Info{FullMethodName: "/stewardmesh.v1.FoundationService/GetOrganization"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tapHandle(context.Background(), &tap.Info{}); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("saturated limiter code=%s err=%v", status.Code(err), err)
	}
	if forwarded.Load() != 1 {
		t.Fatalf("capacity rejection reached authentication %d times", forwarded.Load())
	}
	cancelFirst()

	deadline := time.Now().Add(time.Second)
	for {
		third, thirdErr := tapHandle(context.Background(), &tap.Info{})
		if thirdErr == nil {
			releaseRPCPermit(third)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("canceled RPC did not release capacity: %v", thirdErr)
		}
		time.Sleep(time.Millisecond)
	}
	releaseRPCPermit(first)

	rejecting := limitedTap(func(ctx context.Context, _ *tap.Info) (context.Context, error) {
		return ctx, status.Error(codes.Unauthenticated, "authentication is required")
	}, global, local)
	for range 2 {
		if _, err := rejecting(context.Background(), &tap.Info{}); status.Code(err) != codes.Unauthenticated {
			t.Fatalf("authentication rejection code=%s err=%v", status.Code(err), err)
		}
	}
}

func TestScrubConfigSecretsIncludesEveryConnector(t *testing.T) {
	cfg := config.Config{
		DatabaseURL: "database", CacheURL: "cache", CacheKeySecret: "cache-secret",
		OIDCClientSecret: "oidc", OIDCTransactionSecret: "transaction", BootstrapToken: "bootstrap",
		EntraClientSecret: "entra", SailPointClientSecret: "sailpoint",
		GrouperPassword: "grouper-password", GrouperBearerToken: "grouper-token",
		PeopleSoftPassword: "peoplesoft-password", PeopleSoftBearerToken: "peoplesoft-token",
		S3AccessKeyID: "s3-id", S3SecretAccessKey: "s3-secret", S3SessionToken: "s3-token",
	}
	scrubConfigSecrets(&cfg)
	if cfg.DatabaseURL != "" || cfg.CacheURL != "" || cfg.CacheKeySecret != "" || cfg.OIDCClientSecret != "" ||
		cfg.OIDCTransactionSecret != "" || cfg.BootstrapToken != "" || cfg.EntraClientSecret != "" ||
		cfg.SailPointClientSecret != "" || cfg.GrouperPassword != "" || cfg.GrouperBearerToken != "" ||
		cfg.PeopleSoftPassword != "" || cfg.PeopleSoftBearerToken != "" || cfg.S3AccessKeyID != "" ||
		cfg.S3SecretAccessKey != "" || cfg.S3SessionToken != "" {
		t.Fatalf("configuration retained a secret: %#v", cfg)
	}
}

func newGRPCTestApplication(t *testing.T) (*application.Application, config.Config) {
	t.Helper()
	cfg := config.Config{
		Addr: "127.0.0.1:8080", DataDir: t.TempDir(), BlobDir: t.TempDir(),
		StorageDriver: config.StorageDriverLocal, BlobMaximumBytes: 25 << 20, BlobDownloadTTL: 5 * time.Minute,
		RepositoryDriver: config.RepositoryDriverMemory, CacheDriver: config.CacheDriverNone,
		AllowedOrigin: "http://localhost:5173", OrganizationID: "grpc-command-test", OrganizationName: "gRPC Command Test",
		ExchangeSourceSystemID: "grpc-command-test", SessionTTL: time.Hour,
		BootstrapToken:      "grpc-command-token-0123456789abcdef",
		EntraSourceSystemID: "entra", SailPointSourceSystemID: "sailpoint",
		PeopleSoftSourceSystemID: "peoplesoft", ReachSecretPrefix: "STEWARDMESH_REACH_SECRET_",
	}
	app, err := application.New(t.Context(), cfg, application.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Error(err)
		}
	})
	return app, cfg
}

func listenBuffered(t *testing.T) *bufconn.Listener {
	t.Helper()
	listener := bufconn.Listen(36 << 20)
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

func dialGRPC(t *testing.T, target string, listener *bufconn.Listener, transport credentials.TransportCredentials) *grpc.ClientConn {
	t.Helper()
	connection, err := grpc.NewClient(
		"passthrough:///"+target,
		grpc.WithTransportCredentials(transport),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	return connection
}

func writeTestTLSCertificate(t *testing.T) (string, string, *x509.CertPool) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	certificateFile := filepath.Join(directory, "certificate.pem")
	keyFile := filepath.Join(directory, "key.pem")
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	if err := os.WriteFile(certificateFile, certificatePEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certificatePEM) {
		t.Fatal("append test TLS root")
	}
	return certificateFile, keyFile, roots
}

func TestRuntimeServeRejectsIncompleteState(t *testing.T) {
	if err := (*grpcRuntime)(nil).serve(context.Background(), nil, nil); err == nil {
		t.Fatal("incomplete runtime was accepted")
	}
	if _, err := limitedTap(func(ctx context.Context, _ *tap.Info) (context.Context, error) {
		return ctx, errors.New("denied")
	}, newRPCLimiter(1))(context.Background(), &tap.Info{}); err == nil {
		t.Fatal("tap failure was discarded")
	}
}
