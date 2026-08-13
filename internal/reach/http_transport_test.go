package reach

// Requirement: REQ-REACH-001. Feature: messaging.delivery. GitHub: #12.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestSignedWebhookUsesTimestampNonceHMACAndRejectsReplay(t *testing.T) {
	secret := []byte("01234567890123456789012345678901")
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	nonces := NewMemoryNonceStore()
	var firstHeader http.Header
	var firstBody []byte
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(io.LimitReader(request.Body, maximumProviderBody))
		firstHeader, firstBody = request.Header.Clone(), body
		if err := VerifyWebhookSignature(request.Context(), request.Header, body, secret, now, nonces); err != nil {
			return response(http.StatusUnauthorized, ""), nil
		}
		return response(http.StatusNoContent, ""), nil
	})}
	transport := &HTTPTransport{client: client, now: func() time.Time { return now }}
	result := transport.Send(context.Background(), Endpoint{URL: "https://hooks.example.test/reach"}, Provider{Kind: ProviderWebhook}, Message{
		ID: "message-one", SourceKind: "manual", Subject: "Alert", Body: "Safe plain text", CreatedAt: now,
	}, secret)
	if !result.Succeeded || result.ErrorCode != "" {
		t.Fatalf("signed webhook failed: %#v", result)
	}
	if firstHeader.Get(webhookTimestampHeader) == "" || firstHeader.Get(webhookNonceHeader) == "" || !strings.HasPrefix(firstHeader.Get(webhookSignatureHeader), "v1=") {
		t.Fatalf("missing signing headers: %#v", firstHeader)
	}
	if err := VerifyWebhookSignature(context.Background(), firstHeader, firstBody, secret, now, nonces); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected nonce replay rejection, got %v", err)
	}
	if err := VerifyWebhookSignature(context.Background(), firstHeader, firstBody, secret, now.Add(6*time.Minute), NewMemoryNonceStore()); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected stale timestamp rejection, got %v", err)
	}
}

func TestHTTPProviderAdaptersProduceBoundedProviderSpecificRequests(t *testing.T) {
	for _, test := range []struct {
		kind     ProviderKind
		secret   []byte
		wantBody string
		wantAuth string
		region   string
	}{
		{kind: ProviderGmail, secret: []byte("oauth-token-0123456789"), wantBody: `"raw":`, wantAuth: "Bearer oauth-token-0123456789"},
		{kind: ProviderOutlook, secret: []byte(`{"accessToken":"oauth-token-0123456789"}`), wantBody: `"toRecipients":`, wantAuth: "Bearer oauth-token-0123456789"},
		{kind: ProviderTeams, secret: []byte("oauth-token-0123456789"), wantBody: `"contentType":"text"`, wantAuth: "Bearer oauth-token-0123456789"},
		{kind: ProviderSES, secret: []byte(`{"accessKeyId":"AKIAEXAMPLE","secretAccessKey":"secret-secret-secret-secret"}`), wantBody: `"FromEmailAddress":`, wantAuth: "AWS4-HMAC-SHA256", region: "us-east-1"},
	} {
		t.Run(string(test.kind), func(t *testing.T) {
			var capturedBody, authorization string
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				body, _ := io.ReadAll(io.LimitReader(request.Body, maximumProviderBody))
				capturedBody, authorization = string(body), request.Header.Get("Authorization")
				return response(http.StatusAccepted, ""), nil
			})}
			transport := &HTTPTransport{client: client, now: func() time.Time { return time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC) }}
			message := Message{ID: "message-one", SourceKind: "manual", Subject: "Alert", Body: "Safe plain text", CreatedAt: time.Now(),
				Recipients: []Recipient{{Kind: RecipientEmail, Address: "owner@example.test"}, {Kind: RecipientChannel, Address: "operations"}}}
			result := transport.Send(context.Background(), Endpoint{URL: "https://provider.example.test/send", Region: test.region}, Provider{Kind: test.kind, Sender: "sender@example.test"}, message, test.secret)
			if !result.Succeeded || !strings.Contains(capturedBody, test.wantBody) || !strings.Contains(authorization, test.wantAuth) {
				t.Fatalf("adapter request result=%#v auth=%q body=%s", result, authorization, capturedBody)
			}
		})
	}
}

func TestHTTPTransportClassifiesFailuresWithoutLeakingProviderBodies(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusServiceUnavailable, "secret provider response with credentials"), nil
	})}
	transport := &HTTPTransport{client: client, now: time.Now}
	result := transport.Send(context.Background(), Endpoint{URL: "https://provider.example.test/send"}, Provider{Kind: ProviderWebhook}, Message{ID: "message", SourceKind: "manual", Subject: "A", Body: "B", CreatedAt: time.Now()}, []byte("01234567890123456789012345678901"))
	if !result.Retryable || result.ErrorCode != "provider_unavailable" || strings.Contains(result.ErrorCode, "secret") {
		t.Fatalf("unsafe provider failure classification: %#v", result)
	}
}

func TestHTTPProviderConnectionTestsUseConfiguredRoutesAndRedactedResults(t *testing.T) {
	for _, test := range []struct {
		kind   ProviderKind
		secret []byte
		region string
	}{
		{kind: ProviderGmail, secret: []byte("oauth-token-0123456789")},
		{kind: ProviderOutlook, secret: []byte("oauth-token-0123456789")},
		{kind: ProviderTeams, secret: []byte("oauth-token-0123456789")},
		{kind: ProviderSES, secret: []byte(`{"accessKeyId":"AKIAEXAMPLE","secretAccessKey":"secret-secret-secret-secret"}`), region: "us-east-1"},
		{kind: ProviderWebhook, secret: []byte("01234567890123456789012345678901")},
	} {
		t.Run(string(test.kind), func(t *testing.T) {
			var method, target string
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				method, target = request.Method, request.URL.String()
				return response(http.StatusNoContent, "provider-internal-body"), nil
			})}
			transport := &HTTPTransport{client: client, now: func() time.Time { return time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC) }}
			result := transport.Test(context.Background(), Endpoint{URL: "https://provider.example.test/send", TestURL: "https://provider.example.test/test", Region: test.region}, Provider{Kind: test.kind}, test.secret)
			if !result.Succeeded || method != http.MethodGet || target != "https://provider.example.test/test" {
				t.Fatalf("connection test result=%#v method=%q target=%q", result, method, target)
			}
		})
	}
}

func TestHTTPProviderConnectionTestNeverFallsBackToSendRoute(t *testing.T) {
	called := false
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return response(http.StatusNoContent, ""), nil
	})}
	transport := &HTTPTransport{client: client, now: time.Now}
	result := transport.Test(context.Background(), Endpoint{URL: "https://provider.example.test/send"}, Provider{Kind: ProviderGmail}, []byte("oauth-token-0123456789"))
	if result.Succeeded || result.ErrorCode != "test_endpoint_unavailable" || called {
		t.Fatalf("unsafe connection-test fallback result=%#v called=%t", result, called)
	}
}
