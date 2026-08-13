package reach

// Requirement: REQ-REACH-001. Feature: messaging.delivery. GitHub: #12.

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

type SMTPTransport struct {
	dialTimeout time.Duration
	dial        func(context.Context, string, string) (net.Conn, error)
}

type smtpCredential struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (t *SMTPTransport) Test(ctx context.Context, endpoint Endpoint, _ Provider, secret []byte) DeliveryResult {
	client, credential, result := t.connect(ctx, endpoint, secret)
	if result.ErrorCode != "" {
		return result
	}
	defer client.Close()
	if credential.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", credential.Username, credential.Password, endpoint.ServerName)); err != nil {
			return permanent("provider_authentication_failed")
		}
	}
	if err := client.Noop(); err != nil {
		return retryable("provider_unavailable")
	}
	_ = client.Quit()
	return DeliveryResult{Succeeded: true}
}

func (t *SMTPTransport) Send(ctx context.Context, endpoint Endpoint, provider Provider, message Message, secret []byte) DeliveryResult {
	client, credential, result := t.connect(ctx, endpoint, secret)
	if result.ErrorCode != "" {
		return result
	}
	defer client.Close()
	if credential.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", credential.Username, credential.Password, endpoint.ServerName)); err != nil {
			return permanent("provider_authentication_failed")
		}
	}
	if err := client.Mail(provider.Sender); err != nil {
		return permanent("provider_rejected")
	}
	recipients := 0
	for _, recipient := range message.Recipients {
		if recipient.Kind != RecipientEmail {
			continue
		}
		if err := client.Rcpt(recipient.Address); err != nil {
			return permanent("recipient_rejected")
		}
		recipients++
	}
	if recipients == 0 {
		return permanent("recipient_invalid")
	}
	writer, err := client.Data()
	if err != nil {
		return retryable("provider_unavailable")
	}
	var content bytes.Buffer
	_, _ = fmt.Fprintf(&content, "From: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s", provider.Sender, message.Subject, message.Body)
	if _, err := writer.Write(content.Bytes()); err != nil {
		_ = writer.Close()
		return retryable("provider_unavailable")
	}
	if err := writer.Close(); err != nil {
		return retryable("provider_unavailable")
	}
	if err := client.Quit(); err != nil {
		return retryable("provider_unavailable")
	}
	return DeliveryResult{Succeeded: true}
}

func (t *SMTPTransport) connect(ctx context.Context, endpoint Endpoint, secret []byte) (*smtp.Client, smtpCredential, DeliveryResult) {
	credential, err := decodeSMTPCredential(secret)
	if err != nil {
		return nil, smtpCredential{}, permanent("credential_invalid")
	}
	timeout := t.dialTimeout
	if timeout <= 0 || timeout > 30*time.Second {
		timeout = 10 * time.Second
	}
	dial := t.dial
	if dial == nil {
		dialer := &net.Dialer{Timeout: timeout}
		dial = dialer.DialContext
	}
	connection, err := dial(ctx, "tcp", endpoint.Address)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, smtpCredential{}, permanent("request_canceled")
		}
		return nil, smtpCredential{}, retryable("provider_unavailable")
	}
	client, err := smtp.NewClient(connection, endpoint.ServerName)
	if err != nil {
		_ = connection.Close()
		return nil, smtpCredential{}, retryable("provider_unavailable")
	}
	if endpoint.RequireTLS {
		if err := client.StartTLS(&tls.Config{ServerName: endpoint.ServerName, MinVersion: tls.VersionTLS12}); err != nil {
			_ = client.Close()
			return nil, smtpCredential{}, permanent("provider_tls_required")
		}
	}
	return client, credential, DeliveryResult{}
}

func decodeSMTPCredential(secret []byte) (smtpCredential, error) {
	var credential smtpCredential
	decoder := json.NewDecoder(bytes.NewReader(secret))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&credential); err != nil {
		return smtpCredential{}, errors.New("invalid credential")
	}
	credential.Username, credential.Password = strings.TrimSpace(credential.Username), strings.TrimSpace(credential.Password)
	if credential.Username == "" || len(credential.Username) > 320 || len(credential.Password) < 8 || len(credential.Password) > 16<<10 || strings.ContainsAny(credential.Username+credential.Password, "\r\n") {
		return smtpCredential{}, errors.New("invalid credential")
	}
	return credential, nil
}
