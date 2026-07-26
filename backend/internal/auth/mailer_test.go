package auth

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestSMTPMailerRequiresConfiguration(t *testing.T) {
	if err := (SMTPMailer{}).Send(context.Background(), "user@example.test", "subject", "body"); err == nil {
		t.Fatal("unconfigured SMTP must fail")
	}
}

func TestSMTPMailerRejectsInjectedHeadersBeforeDial(t *testing.T) {
	mailer := SMTPMailer{Config: SMTPConfig{Host: "127.0.0.1", Port: 1, From: "sender@example.test"}}
	for _, test := range []struct {
		name    string
		from    string
		to      string
		subject string
	}{
		{name: "sender", from: "sender@example.test\r\nBcc: victim@example.test", to: "user@example.test", subject: "subject"},
		{name: "recipient", from: "sender@example.test", to: "user@example.test\r\nBcc: victim@example.test", subject: "subject"},
		{name: "subject", from: "sender@example.test", to: "user@example.test", subject: "subject\r\nBcc: victim@example.test"},
	} {
		t.Run(test.name, func(t *testing.T) {
			mailer.Config.From = test.from
			if err := mailer.Send(context.Background(), test.to, test.subject, "body"); err == nil || !strings.Contains(err.Error(), "invalid") && !strings.Contains(err.Error(), "single line") {
				t.Fatalf("Send() error = %v, want header validation error", err)
			}
		})
	}
}

func TestBuildSMTPMessageEncodesUntrustedContent(t *testing.T) {
	from, err := parseSMTPAddress("Aster Router <sender@example.test>")
	if err != nil {
		t.Fatal(err)
	}
	to, err := parseSMTPAddress("user@example.test")
	if err != nil {
		t.Fatal(err)
	}
	body := "hello\r\nBcc: victim@example.test"
	message, err := buildSMTPMessage(from, to, "verification subject", "text/html", body)
	if err != nil {
		t.Fatal(err)
	}
	encodedBody := base64.StdEncoding.EncodeToString([]byte(body))
	if strings.Contains(string(message), body) || !strings.Contains(string(message), encodedBody) {
		t.Fatalf("message body was not safely encoded: %s", message)
	}
	if !strings.Contains(string(message), "Content-Transfer-Encoding: base64") {
		t.Fatalf("message is missing base64 transfer encoding: %s", message)
	}
}

func TestSMTPMailerRejectsPlaintextServerWithoutSTARTTLS(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	commands := make(chan []string, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			commands <- nil
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		_, _ = fmt.Fprint(conn, "220 smtp.example.test ESMTP\r\n")
		reader := bufio.NewReader(conn)
		var received []string
		line, readErr := reader.ReadString('\n')
		if readErr == nil {
			received = append(received, strings.TrimSpace(line))
			_, _ = fmt.Fprint(conn, "250-smtp.example.test\r\n250 AUTH PLAIN\r\n")
			_ = conn.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
			if line, readErr = reader.ReadString('\n'); readErr == nil {
				received = append(received, strings.TrimSpace(line))
			}
		}
		commands <- received
	}()

	port := listener.Addr().(*net.TCPAddr).Port
	mailer := SMTPMailer{Config: SMTPConfig{
		Host: "127.0.0.1", Port: port, Username: "mailer", Password: "smtp-secret", From: "sender@example.test",
	}}
	err = mailer.Send(t.Context(), "user@example.test", "verification subject", "sensitive reset link")
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("Send() error = %v, want mandatory STARTTLS failure", err)
	}
	received := <-commands
	if len(received) != 1 || !strings.HasPrefix(received[0], "EHLO ") {
		t.Fatalf("plaintext SMTP commands = %#v, want only EHLO", received)
	}
}
