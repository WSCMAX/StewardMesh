package main

// Requirements: REQ-API-001, SEC-MCP-001. Feature: integrations.protocols. GitHub: #14.

import "testing"

func TestGRPCTransportRequiresTLSOffLoopback(t *testing.T) {
	if _, err := grpcTransportOptions("127.0.0.1:9090", "", ""); err != nil {
		t.Fatalf("loopback plaintext should remain available for local adapters: %v", err)
	}
	if _, err := grpcTransportOptions("0.0.0.0:9090", "", ""); err == nil {
		t.Fatal("non-loopback plaintext gRPC listener was accepted")
	}
	if _, err := grpcTransportOptions("127.0.0.1", "", ""); err == nil {
		t.Fatal("gRPC address without port was accepted")
	}
}
