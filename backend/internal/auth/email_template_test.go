package auth

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDefaultEmailTemplatesUseLowercaseJSONFields(t *testing.T) {
	payload, err := json.Marshal(DefaultEmailTemplates())
	if err != nil {
		t.Fatal(err)
	}
	value := string(payload)
	if !strings.Contains(value, `"event":"email_verification"`) || strings.Contains(value, `"Event"`) {
		t.Fatalf("DefaultEmailTemplates JSON = %s", value)
	}
}

func TestRenderEmailTemplateEscapesHTMLAndRejectsUnknownFields(t *testing.T) {
	subject, htmlBody, err := RenderEmailTemplate("Hello {{.UserName}}", `<p>{{.UserName}}</p>`, EmailTemplateData{UserName: `<script>alert(1)</script>`})
	if err != nil {
		t.Fatalf("RenderEmailTemplate() error = %v", err)
	}
	if subject != "Hello <script>alert(1)</script>" {
		t.Fatalf("subject = %q", subject)
	}
	if strings.Contains(htmlBody, "<script>") || !strings.Contains(htmlBody, "&lt;script") {
		t.Fatalf("HTML was not escaped: %q", htmlBody)
	}
	if _, _, err := RenderEmailTemplate("{{.Unknown}}", "body", EmailTemplateData{}); err == nil {
		t.Fatal("unknown field error = nil")
	}
}
