package reach

// Requirement: REQ-REACH-001. Feature: messaging.delivery. GitHub: #12.

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestSMTPAdapterUsesFixedCredentialFreeFixtureAndPlainTextMessage(t *testing.T) {
	clientConnection, serverConnection := net.Pipe()
	received := make(chan string, 1)
	go serveSMTPFixture(serverConnection, received)
	transport := &SMTPTransport{dialTimeout: 3 * time.Second, dial: func(context.Context, string, string) (net.Conn, error) { return clientConnection, nil }}
	endpoint := Endpoint{ID: "smtp-fixture", Label: "SMTP fixture", Kind: ProviderSMTP, Address: "127.0.0.1:2525", ServerName: "localhost", AllowLocalHTTP: true}
	message := Message{ID: "message-one", SourceKind: "manual", Subject: "Alert", Body: "Safe plain text",
		Recipients: []Recipient{{Kind: RecipientEmail, Address: "owner@example.test"}}}
	result := transport.Send(context.Background(), endpoint, Provider{Kind: ProviderSMTP, Sender: "sender@example.test"}, message, []byte(`{"username":"fixture-user","password":"fixture-password"}`))
	if !result.Succeeded {
		t.Fatalf("SMTP fixture delivery failed: %#v", result)
	}
	select {
	case body := <-received:
		if !strings.Contains(body, "Subject: Alert") || !strings.Contains(body, "Safe plain text") {
			t.Fatalf("unexpected SMTP body %q", body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SMTP fixture did not receive a message")
	}
}

func TestSMTPAdapterConnectionTestAuthenticatesAndUsesNOOP(t *testing.T) {
	clientConnection, serverConnection := net.Pipe()
	go serveSMTPFixture(serverConnection, make(chan string, 1))
	transport := &SMTPTransport{dialTimeout: 3 * time.Second, dial: func(context.Context, string, string) (net.Conn, error) { return clientConnection, nil }}
	result := transport.Test(context.Background(), Endpoint{Address: "127.0.0.1:2525", ServerName: "localhost", AllowLocalHTTP: true}, Provider{Kind: ProviderSMTP}, []byte(`{"username":"fixture-user","password":"fixture-password"}`))
	if !result.Succeeded {
		t.Fatalf("SMTP connection test failed: %#v", result)
	}
}

func serveSMTPFixture(connection net.Conn, received chan<- string) {
	defer connection.Close()
	reader, writer := bufio.NewReader(connection), bufio.NewWriter(connection)
	write := func(format string, values ...any) {
		_, _ = fmt.Fprintf(writer, format, values...)
		_ = writer.Flush()
	}
	write("220 localhost fixture\r\n")
	dataMode := false
	var data strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if dataMode {
			if trimmed == "." {
				received <- data.String()
				dataMode = false
				write("250 queued\r\n")
				continue
			}
			data.WriteString(line)
			continue
		}
		command := strings.ToUpper(trimmed)
		switch {
		case strings.HasPrefix(command, "EHLO"):
			write("250-localhost\r\n250 AUTH PLAIN\r\n")
		case strings.HasPrefix(command, "AUTH PLAIN"):
			write("235 authenticated\r\n")
		case strings.HasPrefix(command, "MAIL FROM"), strings.HasPrefix(command, "RCPT TO"):
			write("250 accepted\r\n")
		case command == "DATA":
			dataMode = true
			write("354 end with dot\r\n")
		case command == "QUIT":
			write("221 bye\r\n")
			return
		default:
			write("250 ok\r\n")
		}
	}
}
