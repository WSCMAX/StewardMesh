package main

// Authenticated all-domain gRPC runtime. Requirements: REQ-API-001, SEC-MCP-001.
// Feature: integrations.protocols. GitHub: #14.

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/maxlemke/stewardmesh/internal/application"
	"github.com/maxlemke/stewardmesh/internal/config"
	"github.com/maxlemke/stewardmesh/internal/grpcapi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/tap"
)

const (
	defaultProtectedAddress = "127.0.0.1:9090"
	defaultPublicAddress    = "127.0.0.1:9091"
	maximumHeaderBytes      = 16 << 10
	maximumProtectedStreams = uint32(8)
	maximumPublicStreams    = uint32(4)
	maximumProcessRPCs      = 12
	maximumProtectedRPCs    = 8
	maximumPublicRPCs       = 4
	maximumRPCDuration      = 5 * time.Minute
	connectionTimeout       = 5 * time.Second
	gracefulStopTimeout     = 10 * time.Second
)

type grpcRuntimeConfig struct {
	protectedAddress string
	publicAddress    string
	certificateFile  string
	keyFile          string
}

type namedServeResult struct {
	listener string
	err      error
}

type grpcRuntime struct {
	protected *grpc.Server
	public    *grpc.Server
	health    *health.Server
}

// checkOnlyHealth exposes the standard unary health probe without the
// unauthenticated long-lived Watch stream. Watch would otherwise let a handful
// of remote clients retain every public pre-decode permit and starve Guard
// bootstrap/login. List is intentionally unavailable because this process
// publishes only the overall serving state.
type checkOnlyHealth struct {
	healthpb.UnimplementedHealthServer
	server *health.Server
}

func (service checkOnlyHealth) Check(ctx context.Context, request *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	return service.server.Check(ctx, request)
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil {
		logger.Error("run StewardMesh gRPC server", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) (returnErr error) {
	if ctx == nil {
		return errors.New("gRPC runtime context is required")
	}
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	runtimeConfig := loadGRPCRuntimeConfig()
	if err := validateRemoteGRPCConfiguration(runtimeConfig, cfg); err != nil {
		return err
	}
	protectedTransport, err := grpcTransportOptions(runtimeConfig.protectedAddress, runtimeConfig.certificateFile, runtimeConfig.keyFile)
	if err != nil {
		return fmt.Errorf("configure STEWARDMESH_GRPC_ADDR: %w", err)
	}
	publicTransport, err := grpcTransportOptions(runtimeConfig.publicAddress, runtimeConfig.certificateFile, runtimeConfig.keyFile)
	if err != nil {
		return fmt.Errorf("configure STEWARDMESH_GRPC_PUBLIC_ADDR: %w", err)
	}
	if runtimeConfig.protectedAddress == runtimeConfig.publicAddress {
		return errors.New("STEWARDMESH_GRPC_ADDR and STEWARDMESH_GRPC_PUBLIC_ADDR must be different")
	}

	protectedListener, err := net.Listen("tcp", runtimeConfig.protectedAddress)
	if err != nil {
		return fmt.Errorf("listen for protected gRPC: %w", err)
	}
	defer protectedListener.Close()
	publicListener, err := net.Listen("tcp", runtimeConfig.publicAddress)
	if err != nil {
		return fmt.Errorf("listen for public gRPC: %w", err)
	}
	defer publicListener.Close()

	setup, cancelSetup := context.WithTimeout(ctx, 20*time.Second)
	app, applicationErr := application.New(setup, cfg, application.Options{RunMigrations: true})
	cancelSetup()
	bootstrapTokenConfigured := strings.TrimSpace(cfg.BootstrapToken) != ""
	scrubConfigSecrets(&cfg)
	if applicationErr != nil {
		return fmt.Errorf("initialize application: %w", applicationErr)
	}
	defer func() {
		if closeErr := app.Close(); closeErr != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close application: %w", closeErr))
		}
	}()
	if !loopbackListener(runtimeConfig.publicAddress) && !bootstrapTokenConfigured {
		check, cancelCheck := context.WithTimeout(ctx, 5*time.Second)
		bootstrapRequired, _, statusErr := app.Guard().BootstrapStatus(check)
		cancelCheck()
		if statusErr != nil {
			return fmt.Errorf("verify remote gRPC bootstrap state: %w", statusErr)
		}
		if err := validateRemoteBootstrapConfiguration(runtimeConfig.publicAddress, bootstrapTokenConfigured, bootstrapRequired); err != nil {
			return err
		}
	}

	runtime, err := newGRPCRuntime(app, cfg.AllowedOrigin, cfg.SessionCookieSecure, protectedTransport, publicTransport)
	if err != nil {
		return fmt.Errorf("initialize all-domain gRPC runtime: %w", err)
	}
	logger.Info(
		"StewardMesh all-domain gRPC server started",
		"protected_addr", protectedListener.Addr().String(),
		"public_addr", publicListener.Addr().String(),
		"organization_id", app.Organization().ID,
	)
	if err := runtime.serve(ctx, protectedListener, publicListener); err != nil {
		return err
	}
	return nil
}

func loadGRPCRuntimeConfig() grpcRuntimeConfig {
	protectedAddress := strings.TrimSpace(os.Getenv("STEWARDMESH_GRPC_ADDR"))
	if protectedAddress == "" {
		protectedAddress = defaultProtectedAddress
	}
	publicAddress := strings.TrimSpace(os.Getenv("STEWARDMESH_GRPC_PUBLIC_ADDR"))
	if publicAddress == "" {
		publicAddress = defaultPublicAddress
	}
	return grpcRuntimeConfig{
		protectedAddress: protectedAddress,
		publicAddress:    publicAddress,
		certificateFile:  strings.TrimSpace(os.Getenv("STEWARDMESH_GRPC_TLS_CERT_FILE")),
		keyFile:          strings.TrimSpace(os.Getenv("STEWARDMESH_GRPC_TLS_KEY_FILE")),
	}
}

func validateRemoteGRPCConfiguration(runtimeConfig grpcRuntimeConfig, cfg config.Config) error {
	if loopbackListener(runtimeConfig.protectedAddress) && loopbackListener(runtimeConfig.publicAddress) {
		return nil
	}
	origin, err := url.Parse(strings.TrimSpace(cfg.AllowedOrigin))
	if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" {
		return errors.New("a non-loopback gRPC listener requires an HTTPS STEWARDMESH_ALLOWED_ORIGIN")
	}
	if !cfg.SessionCookieSecure {
		return errors.New("a non-loopback gRPC listener requires secure session cookies")
	}
	return nil
}

func validateRemoteBootstrapConfiguration(publicAddress string, bootstrapTokenConfigured, bootstrapRequired bool) error {
	if loopbackListener(publicAddress) || bootstrapTokenConfigured || !bootstrapRequired {
		return nil
	}
	return errors.New("a non-loopback public gRPC listener requires STEWARDMESH_BOOTSTRAP_TOKEN until administrator setup is complete")
}

func newGRPCRuntime(app *application.Application, allowedOrigin string, sessionCookieSecure bool, protectedTransport, publicTransport []grpc.ServerOption) (*grpcRuntime, error) {
	if app == nil || app.Handler() == nil || app.Guard() == nil {
		return nil, errors.New("initialized application, handler, and Guard service are required")
	}
	gateway, err := grpcapi.New(app.Handler(), grpcapi.Options{
		AllowedOrigin:       allowedOrigin,
		SessionCookieSecure: sessionCookieSecure,
		OrganizationID:      app.Organization().ID,
		Guard:               app.Guard(),
		Vault:               app.Vault(),
	})
	if err != nil {
		return nil, err
	}

	globalLimiter := newRPCLimiter(maximumProcessRPCs)
	protectedLimiter := newRPCLimiter(maximumProtectedRPCs)
	publicLimiter := newRPCLimiter(maximumPublicRPCs)
	codec := gateway.TransportCodec()

	protectedOptions := append([]grpc.ServerOption(nil), protectedTransport...)
	protectedOptions = append(protectedOptions,
		grpc.MaxRecvMsgSize(grpcapi.MaximumMessageBytes),
		grpc.MaxSendMsgSize(grpcapi.MaximumMessageBytes),
		grpc.MaxHeaderListSize(maximumHeaderBytes),
		grpc.MaxConcurrentStreams(maximumProtectedStreams),
		grpc.ConnectionTimeout(connectionTimeout),
		grpc.KeepaliveParams(serverKeepaliveParameters()),
		grpc.KeepaliveEnforcementPolicy(serverKeepalivePolicy()),
		grpc.InTapHandle(limitedTap(gateway.TapHandle, globalLimiter, protectedLimiter)),
		grpc.ChainUnaryInterceptor(releaseRPCPermitUnary),
		grpc.ChainStreamInterceptor(releaseRPCPermitStream),
		grpc.ForceServerCodec(codec),
	)
	protectedServer := grpc.NewServer(protectedOptions...)
	if err := gateway.RegisterProtected(protectedServer); err != nil {
		return nil, err
	}

	publicOptions := append([]grpc.ServerOption(nil), publicTransport...)
	publicOptions = append(publicOptions,
		grpc.MaxRecvMsgSize(grpcapi.MaximumPublicMessageBytes),
		grpc.MaxSendMsgSize(grpcapi.MaximumPublicMessageBytes),
		grpc.MaxHeaderListSize(maximumHeaderBytes),
		grpc.MaxConcurrentStreams(maximumPublicStreams),
		grpc.ConnectionTimeout(connectionTimeout),
		grpc.KeepaliveParams(serverKeepaliveParameters()),
		grpc.KeepaliveEnforcementPolicy(serverKeepalivePolicy()),
		grpc.InTapHandle(limitedTap(nil, globalLimiter, publicLimiter)),
		grpc.ChainUnaryInterceptor(releaseRPCPermitUnary),
		grpc.ChainStreamInterceptor(releaseRPCPermitStream),
		grpc.ForceServerCodec(codec),
	)
	publicServer := grpc.NewServer(publicOptions...)
	if err := gateway.RegisterPublic(publicServer); err != nil {
		return nil, err
	}
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	healthpb.RegisterHealthServer(publicServer, checkOnlyHealth{server: healthServer})
	return &grpcRuntime{protected: protectedServer, public: publicServer, health: healthServer}, nil
}

func (runtime *grpcRuntime) serve(ctx context.Context, protectedListener, publicListener net.Listener) error {
	if runtime == nil || runtime.protected == nil || runtime.public == nil || runtime.health == nil || protectedListener == nil || publicListener == nil {
		return errors.New("complete gRPC runtime and listeners are required")
	}
	serveDone := make(chan namedServeResult, 2)
	go func() {
		serveDone <- namedServeResult{listener: "protected", err: runtime.protected.Serve(protectedListener)}
	}()
	go func() { serveDone <- namedServeResult{listener: "public", err: runtime.public.Serve(publicListener)} }()
	runtime.health.Resume()

	select {
	case <-ctx.Done():
		runtime.shutdown()
		return nil
	case result := <-serveDone:
		runtime.shutdown()
		if result.err == nil {
			return fmt.Errorf("%s gRPC listener stopped unexpectedly", result.listener)
		}
		return fmt.Errorf("%s gRPC listener stopped: %w", result.listener, result.err)
	}
}

func (runtime *grpcRuntime) shutdown() {
	if runtime == nil {
		return
	}
	if runtime.health != nil {
		runtime.health.Shutdown()
	}
	stopped := make(chan struct{}, 2)
	for _, server := range []*grpc.Server{runtime.protected, runtime.public} {
		if server == nil {
			stopped <- struct{}{}
			continue
		}
		go func(server *grpc.Server) {
			server.GracefulStop()
			stopped <- struct{}{}
		}(server)
	}
	timer := time.NewTimer(gracefulStopTimeout)
	defer timer.Stop()
	for completed := 0; completed < 2; completed++ {
		select {
		case <-stopped:
		case <-timer.C:
			if runtime.protected != nil {
				runtime.protected.Stop()
			}
			if runtime.public != nil {
				runtime.public.Stop()
			}
			return
		}
	}
}

func grpcTransportOptions(address, certificateFile, keyFile string) ([]grpc.ServerOption, error) {
	if err := validateGRPCAddress(address); err != nil {
		return nil, err
	}
	certificateFile, keyFile = strings.TrimSpace(certificateFile), strings.TrimSpace(keyFile)
	if certificateFile == "" && keyFile == "" {
		if !loopbackListener(address) {
			return nil, errors.New("a non-loopback gRPC listener requires TLS certificate and key files")
		}
		return nil, nil
	}
	if certificateFile == "" || keyFile == "" {
		return nil, errors.New("both gRPC TLS certificate and key files are required")
	}
	certificate, err := tls.LoadX509KeyPair(certificateFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load gRPC TLS key pair: %w", err)
	}
	configuration := &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}}
	return []grpc.ServerOption{grpc.Creds(credentials.NewTLS(configuration))}, nil
}

func validateGRPCAddress(address string) error {
	_, rawPort, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil || rawPort == "" {
		return errors.New("gRPC address must contain a host and numeric port")
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil || port < 0 || port > 65535 {
		return errors.New("gRPC address must contain a host and numeric port")
	}
	return nil
}

func loopbackListener(address string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func scrubConfigSecrets(cfg *config.Config) {
	if cfg == nil {
		return
	}
	cfg.DatabaseURL = ""
	cfg.CacheURL = ""
	cfg.CacheKeySecret = ""
	cfg.OIDCClientSecret = ""
	cfg.OIDCTransactionSecret = ""
	cfg.BootstrapToken = ""
	cfg.EntraClientSecret = ""
	cfg.SailPointClientSecret = ""
	cfg.GrouperPassword = ""
	cfg.GrouperBearerToken = ""
	cfg.PeopleSoftPassword = ""
	cfg.PeopleSoftBearerToken = ""
	cfg.S3AccessKeyID = ""
	cfg.S3SecretAccessKey = ""
	cfg.S3SessionToken = ""
}

func serverKeepaliveParameters() keepalive.ServerParameters {
	return keepalive.ServerParameters{
		MaxConnectionIdle:     5 * time.Minute,
		MaxConnectionAge:      30 * time.Minute,
		MaxConnectionAgeGrace: 30 * time.Second,
		Time:                  2 * time.Minute,
		Timeout:               20 * time.Second,
	}
}

func serverKeepalivePolicy() keepalive.EnforcementPolicy {
	return keepalive.EnforcementPolicy{MinTime: time.Minute, PermitWithoutStream: false}
}

type rpcLimiter struct {
	permits chan struct{}
}

func newRPCLimiter(limit int) *rpcLimiter {
	return &rpcLimiter{permits: make(chan struct{}, limit)}
}

func (limiter *rpcLimiter) acquire() (func(), bool) {
	if limiter == nil || cap(limiter.permits) == 0 {
		return func() {}, false
	}
	select {
	case limiter.permits <- struct{}{}:
		return func() { <-limiter.permits }, true
	default:
		return func() {}, false
	}
}

type rpcPermitKey struct{}

type rpcPermit struct {
	once     sync.Once
	releases []func()
	cancel   context.CancelFunc
}

func (permit *rpcPermit) release() {
	if permit == nil {
		return
	}
	permit.once.Do(func() {
		for index := len(permit.releases) - 1; index >= 0; index-- {
			permit.releases[index]()
		}
		if permit.cancel != nil {
			permit.cancel()
		}
	})
}

func limitedTap(next tap.ServerInHandle, limiters ...*rpcLimiter) tap.ServerInHandle {
	return func(ctx context.Context, information *tap.Info) (context.Context, error) {
		releases := make([]func(), 0, len(limiters))
		for _, limiter := range limiters {
			release, ok := limiter.acquire()
			if !ok {
				for index := len(releases) - 1; index >= 0; index-- {
					releases[index]()
				}
				return ctx, status.Error(codes.ResourceExhausted, "gRPC concurrency limit reached")
			}
			releases = append(releases, release)
		}
		limitedContext, cancel := context.WithTimeout(ctx, maximumRPCDuration)
		permit := &rpcPermit{releases: releases, cancel: cancel}
		limitedContext = context.WithValue(limitedContext, rpcPermitKey{}, permit)
		if next != nil {
			forwarded, err := next(limitedContext, information)
			if err != nil {
				permit.release()
				return forwarded, err
			}
			limitedContext = forwarded
			if current, ok := limitedContext.Value(rpcPermitKey{}).(*rpcPermit); !ok || current != permit {
				limitedContext = context.WithValue(limitedContext, rpcPermitKey{}, permit)
			}
		}
		go func() {
			<-limitedContext.Done()
			permit.release()
		}()
		return limitedContext, nil
	}
}

func releaseRPCPermit(ctx context.Context) {
	permit, _ := ctx.Value(rpcPermitKey{}).(*rpcPermit)
	permit.release()
}

func releaseRPCPermitUnary(ctx context.Context, request any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	defer releaseRPCPermit(ctx)
	return handler(ctx, request)
}

func releaseRPCPermitStream(server any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	defer releaseRPCPermit(stream.Context())
	return handler(server, stream)
}
