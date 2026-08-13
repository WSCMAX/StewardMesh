package reach

// Requirement: REQ-REACH-001. Feature: messaging.delivery. GitHub: #12.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsv4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

const (
	webhookTimestampHeader = "X-StewardMesh-Timestamp"
	webhookNonceHeader     = "X-StewardMesh-Nonce"
	webhookSignatureHeader = "X-StewardMesh-Signature"
	maximumProviderBody    = 64 << 10
)

type HTTPTransport struct {
	client *http.Client
	now    func() time.Time
}

func (t *HTTPTransport) Test(ctx context.Context, endpoint Endpoint, provider Provider, secret []byte) DeliveryResult {
	// Send routes are deliberately never used as implicit health checks: most
	// provider send endpoints reject GET, while a POST would create an external
	// side effect. Operators must select a separate deployment-owned account or
	// health route for every HTTP provider they intend to test.
	if endpoint.TestURL == "" {
		return permanent("test_endpoint_unavailable")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.TestURL, nil)
	if err != nil {
		return permanent("endpoint_invalid")
	}
	if result := t.authorize(request, endpoint, provider, nil, secret); result.ErrorCode != "" {
		return result
	}
	return t.execute(request)
}

func (t *HTTPTransport) Send(ctx context.Context, endpoint Endpoint, provider Provider, message Message, secret []byte) DeliveryResult {
	if !compatibleRecipientsForMessage(provider.Kind, endpoint, message.Recipients) {
		return permanent("recipient_invalid")
	}
	body, contentType, err := encodeProviderMessage(provider, message)
	if err != nil {
		return permanent("message_invalid")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.URL, bytes.NewReader(body))
	if err != nil {
		return permanent("endpoint_invalid")
	}
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("User-Agent", "StewardMesh-Reach/1")
	request.Header.Set("Idempotency-Key", message.ID)
	if result := t.authorize(request, endpoint, provider, body, secret); result.ErrorCode != "" {
		return result
	}
	return t.execute(request)
}

func (t *HTTPTransport) authorize(request *http.Request, endpoint Endpoint, provider Provider, body, secret []byte) DeliveryResult {
	switch provider.Kind {
	case ProviderGmail, ProviderOutlook, ProviderTeams:
		token, err := oauthAccessToken(secret)
		if err != nil {
			return permanent("credential_invalid")
		}
		request.Header.Set("Authorization", "Bearer "+token)
	case ProviderWebhook:
		if len(secret) < 32 {
			return permanent("credential_invalid")
		}
		now := t.now
		if now == nil {
			now = func() time.Time { return time.Now().UTC() }
		}
		timestamp := strconv.FormatInt(now().UTC().Unix(), 10)
		nonceBytes := make([]byte, 16)
		if _, err := rand.Read(nonceBytes); err != nil {
			return retryable("nonce_unavailable")
		}
		nonce := hex.EncodeToString(nonceBytes)
		request.Header.Set(webhookTimestampHeader, timestamp)
		request.Header.Set(webhookNonceHeader, nonce)
		request.Header.Set(webhookSignatureHeader, "v1="+webhookMAC(secret, timestamp, nonce, body))
	case ProviderSES:
		var credentials struct {
			AccessKeyID     string `json:"accessKeyId"`
			SecretAccessKey string `json:"secretAccessKey"`
			SessionToken    string `json:"sessionToken,omitempty"`
		}
		decoder := json.NewDecoder(bytes.NewReader(secret))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&credentials); err != nil || strings.TrimSpace(credentials.AccessKeyID) == "" || len(credentials.SecretAccessKey) < 16 {
			return permanent("credential_invalid")
		}
		digest := sha256.Sum256(body)
		signer := awsv4.NewSigner()
		now := time.Now().UTC()
		if t.now != nil {
			now = t.now().UTC()
		}
		err := signer.SignHTTP(request.Context(), aws.Credentials{
			AccessKeyID: credentials.AccessKeyID, SecretAccessKey: credentials.SecretAccessKey, SessionToken: credentials.SessionToken,
		}, request, hex.EncodeToString(digest[:]), "ses", endpoint.Region, now)
		if err != nil {
			return permanent("credential_invalid")
		}
	}
	return DeliveryResult{}
}

func (t *HTTPTransport) execute(request *http.Request) DeliveryResult {
	if t == nil || t.client == nil {
		return retryable("transport_unavailable")
	}
	response, err := t.client.Do(request)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return permanent("request_canceled")
		}
		return retryable("provider_unavailable")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maximumProviderBody))
	switch {
	case response.StatusCode >= 200 && response.StatusCode < 300:
		return DeliveryResult{Succeeded: true}
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500:
		return retryable("provider_unavailable")
	case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
		return permanent("provider_authentication_failed")
	default:
		return permanent("provider_rejected")
	}
}

func encodeProviderMessage(provider Provider, message Message) ([]byte, string, error) {
	emails := make([]map[string]any, 0, len(message.Recipients))
	addresses := make([]string, 0, len(message.Recipients))
	for _, recipient := range message.Recipients {
		if recipient.Kind == RecipientEmail {
			emails = append(emails, map[string]any{"emailAddress": map[string]string{"address": recipient.Address}})
			addresses = append(addresses, recipient.Address)
		}
	}
	switch provider.Kind {
	case ProviderGmail:
		if len(addresses) == 0 {
			return nil, "", ErrInvalidInput
		}
		raw := "From: " + provider.Sender + "\r\nTo: " + strings.Join(addresses, ", ") + "\r\nSubject: " + message.Subject + "\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + message.Body
		body, err := json.Marshal(map[string]string{"raw": base64.RawURLEncoding.EncodeToString([]byte(raw))})
		return body, "application/json", err
	case ProviderOutlook:
		body, err := json.Marshal(map[string]any{"message": map[string]any{
			"subject":      message.Subject,
			"body":         map[string]string{"contentType": "Text", "content": message.Body},
			"toRecipients": emails,
		}, "saveToSentItems": true})
		return body, "application/json", err
	case ProviderTeams:
		body, err := json.Marshal(map[string]any{"body": map[string]string{"contentType": "text", "content": message.Subject + "\n\n" + message.Body}})
		return body, "application/json", err
	case ProviderSES:
		if len(addresses) == 0 {
			return nil, "", ErrInvalidInput
		}
		body, err := json.Marshal(map[string]any{
			"FromEmailAddress": provider.Sender,
			"Destination":      map[string]any{"ToAddresses": addresses},
			"Content": map[string]any{"Simple": map[string]any{
				"Subject": map[string]string{"Data": message.Subject, "Charset": "UTF-8"},
				"Body":    map[string]any{"Text": map[string]string{"Data": message.Body, "Charset": "UTF-8"}},
			}},
		})
		return body, "application/json", err
	case ProviderWebhook:
		body, err := json.Marshal(map[string]any{
			"id": message.ID, "sourceKind": message.SourceKind, "sourceId": message.SourceID,
			"subject": message.Subject, "body": message.Body, "recipients": message.Recipients,
			"createdAt": message.CreatedAt.UTC().Format(time.RFC3339),
		})
		return body, "application/json", err
	default:
		return nil, "", ErrInvalidInput
	}
}

func oauthAccessToken(secret []byte) (string, error) {
	value := strings.TrimSpace(string(secret))
	if strings.HasPrefix(value, "{") {
		var object struct {
			AccessToken string `json:"accessToken"`
		}
		decoder := json.NewDecoder(strings.NewReader(value))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&object); err != nil {
			return "", errors.New("invalid credential")
		}
		value = strings.TrimSpace(object.AccessToken)
	}
	if len(value) < 16 || len(value) > 16<<10 || strings.ContainsAny(value, "\r\n") {
		return "", errors.New("invalid credential")
	}
	return value, nil
}

func permanent(code string) DeliveryResult { return DeliveryResult{ErrorCode: code} }

func retryable(code string) DeliveryResult { return DeliveryResult{Retryable: true, ErrorCode: code} }

func webhookMAC(secret []byte, timestamp, nonce string, body []byte) string {
	digest := sha256.Sum256(body)
	mac := hmac.New(sha256.New, secret)
	_, _ = fmt.Fprintf(mac, "%s\n%s\n%s", timestamp, nonce, hex.EncodeToString(digest[:]))
	return hex.EncodeToString(mac.Sum(nil))
}

type NonceStore interface {
	Use(context.Context, string, time.Time) (bool, error)
}

type MemoryNonceStore struct {
	mu    sync.Mutex
	items map[string]time.Time
}

func NewMemoryNonceStore() *MemoryNonceStore {
	return &MemoryNonceStore{items: map[string]time.Time{}}
}

func (s *MemoryNonceStore) Use(ctx context.Context, nonce string, expiresAt time.Time) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// The expiration is derived from the verified signed timestamp. Using the
	// same clock domain keeps replay behavior deterministic under test clocks
	// and avoids trusting an unrelated wall-clock read here.
	now := expiresAt.Add(-5 * time.Minute)
	for candidate, expiration := range s.items {
		if !expiration.After(now) {
			delete(s.items, candidate)
		}
	}
	if _, exists := s.items[nonce]; exists {
		return false, nil
	}
	s.items[nonce] = expiresAt
	return true, nil
}

// VerifyWebhookSignature is the receiving-side reference verifier for the
// Reach signing contract. It rejects stale timestamps and reused nonces before
// a payload is acted upon.
func VerifyWebhookSignature(ctx context.Context, header http.Header, body, secret []byte, now time.Time, nonces NonceStore) error {
	if len(secret) < 32 || nonces == nil {
		return ErrInvalidInput
	}
	timestampValue := strings.TrimSpace(header.Get(webhookTimestampHeader))
	nonce := strings.TrimSpace(header.Get(webhookNonceHeader))
	signature := strings.TrimSpace(header.Get(webhookSignatureHeader))
	if !regexpHex32.MatchString(nonce) || !strings.HasPrefix(signature, "v1=") {
		return ErrInvalidInput
	}
	seconds, err := strconv.ParseInt(timestampValue, 10, 64)
	if err != nil {
		return ErrInvalidInput
	}
	signedAt := time.Unix(seconds, 0).UTC()
	if delta := now.UTC().Sub(signedAt); delta < -5*time.Minute || delta > 5*time.Minute {
		return ErrInvalidInput
	}
	expected, err := hex.DecodeString(webhookMAC(secret, timestampValue, nonce, body))
	if err != nil {
		return ErrInvalidInput
	}
	actual, err := hex.DecodeString(strings.TrimPrefix(signature, "v1="))
	if err != nil || len(actual) != len(expected) || subtle.ConstantTimeCompare(actual, expected) != 1 {
		return ErrInvalidInput
	}
	used, err := nonces.Use(ctx, nonce, signedAt.Add(5*time.Minute))
	if err != nil {
		return err
	}
	if !used {
		return ErrConflict
	}
	return nil
}

var regexpHex32 = regexp.MustCompile(`^[a-f0-9]{32}$`)
