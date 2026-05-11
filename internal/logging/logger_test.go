package logging

import (
	"bytes"
	"strings"
	"testing"
)

func TestLoggerExcludesSensitiveValues(t *testing.T) {
	var b bytes.Buffer
	l := NewWriter(&b, "debug")
	l.Info("proxy_request_failed", Fields{"authorization": "test-auth-value", "api_key": "test-api-key-value", "request_body": "test body value", "system_prompt": "test prompt value", "channel": "openai"})
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, bad := range []string{"test-auth-value", "test-api-key-value", "test body value", "\":\"test prompt value\""} {
		if strings.Contains(out, bad) {
			t.Fatalf("log leaked %q in %s", bad, out)
		}
	}
	if !strings.Contains(out, "openai") || !strings.Contains(out, "authorization_present") {
		t.Fatalf("expected safe fields in %s", out)
	}
}
