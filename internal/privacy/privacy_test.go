package privacy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"ai-api-stronger/internal/config"
)

type recordingMasker struct {
	sessionIDs []string
	req        *http.Request
	body       string
}

func (m *recordingMasker) Process(req *http.Request, sessionID ...string) (*http.Response, error) {
	m.req = req
	m.sessionIDs = append(m.sessionIDs, sessionID...)
	b, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	m.body = string(b)
	return &http.Response{StatusCode: http.StatusAccepted, Header: http.Header{"X-Privacy": []string{"ok"}}, Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}, nil
}

func TestLibraryProcessorDelegatesToMaskerProcess(t *testing.T) {
	masker := &recordingMasker{}
	processor := NewLibraryProcessorWithMasker(masker)
	resp, err := processor.Do(context.Background(), Request{
		Method:      http.MethodPost,
		UpstreamURL: "https://upstream.example/v1/chat/completions",
		Headers:     http.Header{"X-Test": []string{"value"}},
		Body:        []byte(`{"model":"m"}`),
		SessionID:   "session-1",
	})
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if masker.req == nil {
		t.Fatalf("masker Process was not called")
	}
	if masker.req.Method != http.MethodPost || masker.req.URL.String() != "https://upstream.example/v1/chat/completions" {
		t.Fatalf("request = %s %s", masker.req.Method, masker.req.URL.String())
	}
	if got := masker.req.Header.Get("X-Test"); got != "value" {
		t.Fatalf("header X-Test = %q", got)
	}
	if masker.body != `{"model":"m"}` {
		t.Fatalf("body was not passed through")
	}
	if len(masker.sessionIDs) != 1 || masker.sessionIDs[0] != "session-1" {
		t.Fatalf("session IDs = %#v", masker.sessionIDs)
	}
}

func TestNewLibraryProcessorBuildsWithMemoryStore(t *testing.T) {
	processor, err := NewLibraryProcessor(&config.LLMPrivateProtectConfig{Model: "trusted-model", BaseURL: "https://trusted.example", APIKey: "test-key"}, 0)
	if err != nil {
		t.Fatalf("NewLibraryProcessor returned error: %v", err)
	}
	if processor == nil {
		t.Fatalf("processor is nil")
	}
}

func TestNormalizeTrustedLLMBaseURL(t *testing.T) {
	testCases := []struct {
		name        string
		inputURL    string
		expectedURL string
	}{
		{name: "root URL gets v1 suffix", inputURL: "https://api.openai.com", expectedURL: "https://api.openai.com/v1"},
		{name: "existing v1 path stays unchanged", inputURL: "https://api.openai.com/v1", expectedURL: "https://api.openai.com/v1"},
		{name: "custom base path also gets v1 suffix", inputURL: "https://proxy.example/openai", expectedURL: "https://proxy.example/openai/v1"},
		{name: "trailing slash is trimmed before normalization", inputURL: "https://api.openai.com/", expectedURL: "https://api.openai.com/v1"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := normalizeTrustedLLMBaseURL(testCase.inputURL); got != testCase.expectedURL {
				t.Fatalf("normalizeTrustedLLMBaseURL(%q) = %q, want %q", testCase.inputURL, got, testCase.expectedURL)
			}
		})
	}
}

func TestSessionIDTrimsWhitespace(t *testing.T) {
	headers := http.Header{"X-Privacy-Session-Id": []string{"  session-123  "}}
	if got := SessionID(headers); got != "session-123" {
		t.Fatalf("SessionID returned %q", got)
	}
}

func TestNewLibraryProcessorAcceptsTimeoutWithNormalizedBaseURL(t *testing.T) {
	processor, err := NewLibraryProcessor(&config.LLMPrivateProtectConfig{Model: "trusted-model", BaseURL: "https://trusted.example", APIKey: "test-key"}, 30*time.Second)
	if err != nil {
		t.Fatalf("NewLibraryProcessor returned error: %v", err)
	}
	if processor == nil {
		t.Fatalf("processor is nil")
	}
}

func TestShouldRetryWithCompatibilityFallback(t *testing.T) {
	testCases := []struct {
		name     string
		err      error
		expected bool
	}{
		{name: "nil error does not retry", err: nil, expected: false},
		{name: "choices parse failure retries", err: errors.New("可信 LLM 响应中 choices 字段缺失或无效"), expected: true},
		{name: "choice shape failure retries", err: errors.New("可信 LLM 响应中第一个 choice 格式无效"), expected: true},
		{name: "message shape failure retries", err: errors.New("可信 LLM 响应中 message 字段无效"), expected: true},
		{name: "content part failure retries", err: errors.New("可信 LLM 响应中 content 的第 0 个部分格式无效"), expected: true},
		{name: "non compatibility error does not retry", err: errors.New("network timeout"), expected: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := shouldRetryWithCompatibilityFallback(testCase.err); got != testCase.expected {
				t.Fatalf("shouldRetryWithCompatibilityFallback(%v) = %t, want %t", testCase.err, got, testCase.expected)
			}
		})
	}
}
