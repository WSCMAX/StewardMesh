package main

// Authenticated Bridge gRPC listener. Requirements: REQ-API-001, SEC-MCP-001.
// Feature: integrations.protocols. GitHub: #14.

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	stewardmeshv1 "github.com/maxlemke/stewardmesh/api/proto"
	"github.com/maxlemke/stewardmesh/internal/application"
	"github.com/maxlemke/stewardmesh/internal/bridgegrpc"
	"github.com/maxlemke/stewardmesh/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load configuration", "error", err)
		os.Exit(1)
	}
	address := strings.TrimSpace(os.Getenv("STEWARDMESH_GRPC_ADDR"))
	if address == "" {
		address = "127.0.0.1:9090"
	}
	serverOptions, err := grpcTransportOptions(address, os.Getenv("STEWARDMESH_GRPC_TLS_CERT_FILE"), os.Getenv("STEWARDMESH_GRPC_TLS_KEY_FILE"))
	if err != nil {
		logger.Error("configure gRPC transport", "error", err)
		os.Exit(1)
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		logger.Error("listen for gRPC", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	setup, cancel := context.WithTimeout(ctx, 20*time.Second)
	app, err := application.New(setup, cfg, application.Options{RunMigrations: true})
	cancel()
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
	if err != nil {
		_ = listener.Close()
		logger.Error("initialize application", "error", err)
		os.Exit(1)
	}
	defer app.Close()
	adapter, err := bridgegrpc.NewServer(app.Bridge())
	if err != nil {
		_ = listener.Close()
		logger.Error("initialize Bridge gRPC adapter", "error", err)
		os.Exit(1)
	}
	serverOptions = append(serverOptions,
		grpc.MaxRecvMsgSize(64<<10), grpc.MaxSendMsgSize(4<<20),
		grpc.ChainUnaryInterceptor(bridgegrpc.UnaryAuthenticationInterceptor(app.Bridge())),
	)
	server := grpc.NewServer(serverOptions...)
	stewardmeshv1.RegisterBridgeServiceServer(server, adapter)

	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	logger.Info("StewardMesh Bridge gRPC server started", "addr", address, "organization_id", app.Organization().ID)
	select {
	case <-ctx.Done():
		stopped := make(chan struct{})
		go func() { server.GracefulStop(); close(stopped) }()
		select {
		case <-stopped:
		case <-time.After(10 * time.Second):
			server.Stop()
		}
	case err := <-serveDone:
		if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			logger.Error("gRPC server stopped", "error", err)
			os.Exit(1)
		}
	}
}

func grpcTransportOptions(address, certificateFile, keyFile string) ([]grpc.ServerOption, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("STEWARDMESH_GRPC_ADDR must contain a host and port")
	}
	certificateFile, keyFile = strings.TrimSpace(certificateFile), strings.TrimSpace(keyFile)
	if certificateFile == "" && keyFile == "" {
		ip := net.ParseIP(host)
		if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
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
