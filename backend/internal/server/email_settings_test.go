package server

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/astercloud/asterrouter/backend/internal/settings"
)

func TestEmailTemplateRoutesExposeLowercaseCatalogAndIndependentUpdates(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{AdminToken: "secret"})
	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer secret")
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	defaults := request(http.MethodGet, "/api/v1/console/settings/email-templates/defaults", "")
	if defaults.Code != http.StatusOK || !strings.Contains(defaults.Body.String(), `"event":"email_verification"`) || strings.Contains(defaults.Body.String(), `"Event"`) {
		t.Fatalf("defaults status=%d body=%s", defaults.Code, defaults.Body.String())
	}

	catalogResponse := request(http.MethodGet, "/api/v1/console/settings/email-templates", "")
	if catalogResponse.Code != http.StatusOK {
		t.Fatalf("catalog status=%d body=%s", catalogResponse.Code, catalogResponse.Body.String())
	}
	var catalogEnvelope struct {
		Data settings.EmailTemplateCatalog `json:"data"`
	}
	if err := json.Unmarshal(catalogResponse.Body.Bytes(), &catalogEnvelope); err != nil {
		t.Fatal(err)
	}
	if len(catalogEnvelope.Data.Events) != 3 || len(catalogEnvelope.Data.Locales) != 2 {
		t.Fatalf("catalog = %+v", catalogEnvelope.Data)
	}

	updated := request(http.MethodPut, "/api/v1/console/settings/email-templates/quota_limit/en-US", `{"subject":"Custom {{.SiteName}}","html":"<p>{{.Limit}}</p>"}`)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"customized":true`) {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}
	loaded := request(http.MethodGet, "/api/v1/console/settings/email-templates/quota_limit/en-US", "")
	if loaded.Code != http.StatusOK || !strings.Contains(loaded.Body.String(), "Custom") {
		t.Fatalf("get status=%d body=%s", loaded.Code, loaded.Body.String())
	}
	restored := request(http.MethodPost, "/api/v1/console/settings/email-templates/quota_limit/en-US/restore", "")
	if restored.Code != http.StatusOK || !strings.Contains(restored.Body.String(), `"customized":false`) {
		t.Fatalf("restore status=%d body=%s", restored.Code, restored.Body.String())
	}
	invalid := request(http.MethodPut, "/api/v1/console/settings/email-templates/quota_limit/en-US", `{"subject":"{{.Unknown}}","html":"<p>body</p>"}`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid status=%d body=%s", invalid.Code, invalid.Body.String())
	}
	preview := request(http.MethodPost, "/api/v1/console/settings/email-templates/preview", `{"subject":"Hello {{.UserName}}","html":"<p>{{.SiteName}} {{.ActionURL}}</p>"}`)
	if preview.Code != http.StatusOK || !strings.Contains(preview.Body.String(), "Hello Enterprise User") || !strings.Contains(preview.Body.String(), "https://example.test/action") {
		t.Fatalf("preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	invalidPreview := request(http.MethodPost, "/api/v1/console/settings/email-templates/preview", `{"subject":"{{.Unknown}}","html":"<p>body</p>"}`)
	if invalidPreview.Code != http.StatusBadRequest || !strings.Contains(invalidPreview.Body.String(), `"code":1420`) {
		t.Fatalf("invalid preview status=%d body=%s", invalidPreview.Code, invalidPreview.Body.String())
	}
}

func TestEmailTemplateRoutesRequireAuthentication(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{AdminToken: "secret"})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/console/settings/email-templates", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSMTPConnectionRouteUsesRequestConfiguration(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{AdminToken: "secret"})
	body := bytes.NewBufferString(`{"smtp_host":"127.0.0.1","smtp_port":1,"smtp_username":"unsaved","smtp_password":"unsaved-secret","smtp_use_tls":false}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/console/settings/smtp/test-connection", body)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("connection test status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response httpx.Response
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Code != 1502 {
		t.Fatalf("connection test response=%+v", response)
	}
}

type capturedSMTPMessage struct {
	from string
	to   string
	raw  string
}

func TestSMTPRoutesDeliverPlainAndRenderedMessagesOverSTARTTLS(t *testing.T) {
	port, messages := startTestSMTPServer(t)
	handler := newTestHandler(t, RuntimeConfig{AdminToken: "secret"})
	post := func(path string, body map[string]any) *httptest.ResponseRecorder {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	config := map[string]any{
		"smtp_host": "127.0.0.1", "smtp_port": port, "smtp_from": "sender@example.test",
		"smtp_from_name": "AsterRouter Test", "smtp_use_tls": false,
	}

	connected := post("/api/v1/console/settings/smtp/test-connection", config)
	if connected.Code != http.StatusOK || !strings.Contains(connected.Body.String(), `"connected":true`) {
		t.Fatalf("connection status=%d body=%s", connected.Code, connected.Body.String())
	}

	plainConfig := cloneSMTPRequest(config)
	plainConfig["recipient"] = "plain@example.test"
	plain := post("/api/v1/console/settings/smtp/test", plainConfig)
	if plain.Code != http.StatusOK || !strings.Contains(plain.Body.String(), `"sent":true`) {
		t.Fatalf("plain status=%d body=%s", plain.Code, plain.Body.String())
	}
	assertSMTPMessage(t, nextSMTPMessage(t, messages), "sender@example.test", "plain@example.test", "AsterRouter SMTP test", "SMTP configuration is working.")

	templateConfig := cloneSMTPRequest(config)
	templateConfig["recipient"] = "template@example.test"
	templateConfig["subject"] = "Hello {{.UserName}}"
	templateConfig["html"] = "<p>{{.SiteName}} / {{.ActionURL}}</p>"
	template := post("/api/v1/console/settings/email-templates/test", templateConfig)
	if template.Code != http.StatusOK || !strings.Contains(template.Body.String(), `"sent":true`) {
		t.Fatalf("template status=%d body=%s", template.Code, template.Body.String())
	}
	assertSMTPMessage(t, nextSMTPMessage(t, messages), "sender@example.test", "template@example.test", "Hello Enterprise User", "<p>AsterRouter / https://example.test/action</p>")
}

func TestSMTPRoutesRejectInvalidRecipientsAndTemplates(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{AdminToken: "secret"})
	request := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	invalidRecipient := request("/api/v1/console/settings/smtp/test", `{"recipient":"not-an-email"}`)
	if invalidRecipient.Code != http.StatusBadRequest || !strings.Contains(invalidRecipient.Body.String(), `"code":1402`) {
		t.Fatalf("invalid recipient status=%d body=%s", invalidRecipient.Code, invalidRecipient.Body.String())
	}
	invalidTemplate := request("/api/v1/console/settings/email-templates/test", `{"recipient":"user@example.test","subject":"{{.Unknown}}","html":"<p>body</p>"}`)
	if invalidTemplate.Code != http.StatusBadRequest || !strings.Contains(invalidTemplate.Body.String(), `"code":1420`) {
		t.Fatalf("invalid template status=%d body=%s", invalidTemplate.Code, invalidTemplate.Body.String())
	}
}

func startTestSMTPServer(t *testing.T) (int, <-chan capturedSMTPMessage) {
	t.Helper()
	certificateSource := httptest.NewTLSServer(http.NotFoundHandler())
	serverCertificate := certificateSource.TLS.Certificates[0]
	caCertificate := certificateSource.Certificate()
	certificateSource.Close()
	caFile := t.TempDir() + "/smtp-ca.pem"
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertificate.Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSL_CERT_FILE", caFile)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	messages := make(chan capturedSMTPMessage, 4)
	go func() {
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go serveTestSMTPConnection(connection, serverCertificate, messages)
		}
	}()
	return listener.Addr().(*net.TCPAddr).Port, messages
}

func serveTestSMTPConnection(connection net.Conn, certificate tls.Certificate, messages chan<- capturedSMTPMessage) {
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(connection)
	writer := bufio.NewWriter(connection)
	write := func(value string) bool {
		if _, err := fmt.Fprint(writer, value); err != nil {
			return false
		}
		return writer.Flush() == nil
	}
	if !write("220 test-smtp ESMTP ready\r\n") {
		return
	}
	tlsActive := false
	from, to := "", ""
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		command := strings.TrimSpace(line)
		verb := strings.ToUpper(strings.Fields(command)[0])
		switch verb {
		case "EHLO", "HELO":
			if tlsActive {
				write("250-test-smtp\r\n250 8BITMIME\r\n")
			} else {
				write("250-test-smtp\r\n250 STARTTLS\r\n")
			}
		case "STARTTLS":
			if tlsActive || !write("220 2.0.0 ready to start TLS\r\n") {
				return
			}
			tlsConnection := tls.Server(connection, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
			if err := tlsConnection.Handshake(); err != nil {
				return
			}
			connection = tlsConnection
			reader = bufio.NewReader(connection)
			writer = bufio.NewWriter(connection)
			tlsActive = true
		case "MAIL":
			from = smtpCommandMailbox(command)
			write("250 2.1.0 sender accepted\r\n")
		case "RCPT":
			to = smtpCommandMailbox(command)
			write("250 2.1.5 recipient accepted\r\n")
		case "DATA":
			if from == "" || to == "" || !write("354 end data with <CR><LF>.<CR><LF>\r\n") {
				return
			}
			var lines []string
			for {
				dataLine, readErr := reader.ReadString('\n')
				if readErr != nil {
					return
				}
				dataLine = strings.TrimSuffix(strings.TrimSuffix(dataLine, "\n"), "\r")
				if dataLine == "." {
					break
				}
				lines = append(lines, strings.TrimPrefix(dataLine, "."))
			}
			messages <- capturedSMTPMessage{from: from, to: to, raw: strings.Join(lines, "\r\n")}
			write("250 2.0.0 message accepted\r\n")
		case "RSET":
			from, to = "", ""
			write("250 2.0.0 reset\r\n")
		case "QUIT":
			write("221 2.0.0 bye\r\n")
			return
		default:
			write("502 5.5.2 command not implemented\r\n")
		}
	}
}

func cloneSMTPRequest(source map[string]any) map[string]any {
	clone := make(map[string]any, len(source)+3)
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func smtpCommandMailbox(command string) string {
	start, end := strings.Index(command, "<"), strings.LastIndex(command, ">")
	if start < 0 || end <= start {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(command[start+1 : end]))
}

func nextSMTPMessage(t *testing.T, messages <-chan capturedSMTPMessage) capturedSMTPMessage {
	t.Helper()
	select {
	case message := <-messages:
		return message
	case <-time.After(3 * time.Second):
		t.Fatal("SMTP message was not delivered")
		return capturedSMTPMessage{}
	}
}

func assertSMTPMessage(t *testing.T, captured capturedSMTPMessage, from, to, subject, body string) {
	t.Helper()
	message, err := mail.ReadMessage(strings.NewReader(captured.raw))
	if err != nil {
		t.Fatal(err)
	}
	decodedSubject, err := new(mime.WordDecoder).DecodeHeader(message.Header.Get("Subject"))
	if err != nil {
		t.Fatal(err)
	}
	decodedBody, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, message.Body))
	if err != nil {
		t.Fatal(err)
	}
	if captured.from != from || captured.to != to || decodedSubject != subject || string(decodedBody) != body {
		t.Fatalf("SMTP message envelope=(%q,%q) subject=%q body=%q", captured.from, captured.to, decodedSubject, decodedBody)
	}
}
