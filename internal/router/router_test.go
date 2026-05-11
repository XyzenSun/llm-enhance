package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"ai-api-stronger/internal/config"
	"ai-api-stronger/internal/planner"
	"ai-api-stronger/internal/response"
	"ai-api-stronger/internal/upstream"
)

func testHandler(t *testing.T, upstreamURL string) (*Handler, string) {
	t.Helper()
	y := []byte(`
security: {access_key: test-access-value}
channels:
  openai:
    enabled: true
    upstream: {base_url: ` + upstreamURL + `, format: openai}
    channel_policy:
      request_headers:
        delete: [X-Remove]
        set: {X-Upstream: ok}
      response_headers:
        delete: [Server]
        set: {X-Served-By: stronger}
      response_status: {201: 202}
  disabled:
    enabled: false
    upstream: {base_url: https://disabled.example, format: openai}
`)
	cfg, err := config.LoadBytes(y)
	if err != nil {
		t.Fatal(err)
	}
	s, err := config.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return &Handler{Store: config.NewSnapshotStore(s), Clients: upstream.NewManager()}, s.ConfigHash
}

func TestProxyErrors(t *testing.T) {
	h, _ := testHandler(t, "https://example.com")
	cases := []struct {
		path string
		want int
	}{{"/bad/proxy/openai/v1", response.CodeAuthFailed}, {"/test-access-value/proxy/missing/v1", response.CodeChannelNotFound}, {"/test-access-value/proxy/disabled/v1", response.CodeChannelDisabled}}
	for _, tc := range cases {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest("POST", tc.path, strings.NewReader(`{}`)))
		var env response.Envelope
		_ = json.Unmarshal(rr.Body.Bytes(), &env)
		if env.Code != tc.want {
			t.Fatalf("%s code=%d want %d body=%s", tc.path, env.Code, tc.want, rr.Body.String())
		}
	}
}

func TestProxyBuildsFixedUpstreamURLWithoutClientOverride(t *testing.T) {
	h, _ := testHandler(t, "https://configured-upstream.example/base")
	req := httptest.NewRequest("POST", "/test-access-value/proxy/openai/v1/chat?url=https://evil.test&x=1", strings.NewReader(`{"model":"none"}`))
	plan, err := planner.BuildExecutionPlan(h.Store.Load(), planner.RouteInfo{
		Channel:  "openai",
		TailPath: "/v1/chat",
		RawQuery: req.URL.RawQuery,
	}, req, []byte(`{"model":"none"}`))
	if err != nil {
		t.Fatal(err)
	}
	if plan.UpstreamURL != "https://configured-upstream.example/base/v1/chat?url=https://evil.test&x=1" {
		t.Fatalf("bad upstream URL: %s", plan.UpstreamURL)
	}
}

func TestProxyRequestBodyIsNotCappedByRemovedRuntimeLimit(t *testing.T) {
	var forwardedBodySize int
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read upstream request body: %v", err)
		}
		forwardedBodySize = len(body)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstreamServer.Close()

	h, _ := testHandler(t, upstreamServer.URL)
	largePrompt := strings.Repeat("x", 2048)
	requestBody := `{"model":"none","messages":[{"role":"user","content":"` + largePrompt + `"}]}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/test-access-value/proxy/openai/v1/chat/completions", strings.NewReader(requestBody))
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected oversized request body to pass through, got status %d body %s", rr.Code, rr.Body.String())
	}
	if forwardedBodySize <= 1000 {
		t.Fatalf("expected proxy to forward full request body, got %d bytes", forwardedBodySize)
	}
}

func TestHealthAuthAndReloadFailurePreservesSnapshot(t *testing.T) {
	h, hash := testHandler(t, "https://example.com")
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/health", nil)
	h.ServeHTTP(rr, req)
	var env response.Envelope
	_ = json.Unmarshal(rr.Body.Bytes(), &env)
	if env.Code != response.CodeAuthFailed {
		t.Fatalf("expected auth failure")
	}
	rr = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/health", nil)
	req.Header.Set("X-Access-Key", "test-access-value")
	h.ServeHTTP(rr, req)
	_ = json.Unmarshal(rr.Body.Bytes(), &env)
	if env.Code != 0 {
		t.Fatalf("health failed: %s", rr.Body.String())
	}
	bad, _ := os.CreateTemp(t.TempDir(), "bad-*.yaml")
	_, _ = bad.WriteString("bad: [")
	_ = bad.Close()
	assertFailedReloadPreservesSnapshot(t, h, bad.Name(), hash)
}

func TestReloadInvalidValidatedConfigPreservesSnapshot(t *testing.T) {
	h, hash := testHandler(t, "https://example.com")
	invalid, err := os.CreateTemp(t.TempDir(), "invalid-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = invalid.WriteString(`
security: {access_key: test-access-value}
channels:
  invalid:
    enabled: true
    upstream: {base_url: not-a-url, format: openai}
`)
	_ = invalid.Close()
	assertFailedReloadPreservesSnapshot(t, h, invalid.Name(), hash)
}

func TestProxyUpstreamTimeoutUsesLocalSlowServer(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer slow.Close()
	h, _ := timeoutTestHandler(t, slow.URL, 25*time.Millisecond)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/test-access-value/proxy/openai/v1/chat/completions", strings.NewReader(`{"model":"timeout-model"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)

	var env response.Envelope
	_ = json.Unmarshal(rr.Body.Bytes(), &env)
	if env.Code != response.CodeUpstreamTimeout {
		t.Fatalf("expected upstream timeout code, got %d body %s", env.Code, rr.Body.String())
	}
}

func TestSuccessfulReloadMarksExistingClientsDrainingAndUsesNewSnapshot(t *testing.T) {
	oldUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"old":true}`))
	}))
	defer oldUpstream.Close()
	newUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"new":true}`))
	}))
	defer newUpstream.Close()

	h, oldHash := testHandler(t, oldUpstream.URL)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/test-access-value/proxy/openai/v1/chat/completions", strings.NewReader(`{"model":"before"}`))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("initial proxy request failed: status %d body %s", rr.Code, rr.Body.String())
	}
	if stats := h.Clients.Stats(); stats.ClientCount == 0 {
		t.Fatalf("expected an upstream client before reload")
	}

	validPath := writeConfigFile(t, newUpstream.URL, "test-access-value")
	rr = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/config/reload", strings.NewReader(`{"path_type":"local","path":"`+validPath+`"}`))
	req.Header.Set("X-Access-Key", "test-access-value")
	h.ServeHTTP(rr, req)
	var env response.Envelope
	_ = json.Unmarshal(rr.Body.Bytes(), &env)
	if env.Code != response.CodeSuccess {
		t.Fatalf("expected successful reload, got %d body %s", env.Code, rr.Body.String())
	}
	if h.Store.Load().ConfigHash == oldHash {
		t.Fatalf("snapshot did not change after successful reload")
	}
	if stats := h.Clients.Stats(); stats.DrainingCount == 0 {
		t.Fatalf("expected previous upstream clients to be marked draining after reload")
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/test-access-value/proxy/openai/v1/chat/completions", strings.NewReader(`{"model":"after"}`))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"new":true`) {
		t.Fatalf("proxy did not use reloaded upstream: status %d body %s", rr.Code, rr.Body.String())
	}
}

func assertFailedReloadPreservesSnapshot(t *testing.T, h *Handler, path, wantHash string) {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/config/reload", strings.NewReader(`{"path_type":"local","path":"`+path+`"}`))
	req.Header.Set("X-Access-Key", "test-access-value")
	h.ServeHTTP(rr, req)
	var env response.Envelope
	_ = json.Unmarshal(rr.Body.Bytes(), &env)
	if env.Code != response.CodeConfigError {
		t.Fatalf("expected config error got %d body %s", env.Code, rr.Body.String())
	}
	if h.Store.Load().ConfigHash != wantHash {
		t.Fatalf("snapshot changed on failed reload")
	}
}

func timeoutTestHandler(t *testing.T, upstreamURL string, timeout time.Duration) (*Handler, string) {
	t.Helper()
	y := []byte(`
security: {access_key: test-access-value}
runtime: {default_timeout: 2s}
channels:
  openai:
    enabled: true
    upstream: {base_url: ` + upstreamURL + `, format: openai}
    channel_policy:
      timeout: ` + timeout.String() + `
`)
	cfg, err := config.LoadBytes(y)
	if err != nil {
		t.Fatal(err)
	}
	s, err := config.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return &Handler{Store: config.NewSnapshotStore(s), Clients: upstream.NewManager()}, s.ConfigHash
}

func writeConfigFile(t *testing.T, upstreamURL, accessKey string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`
security: {access_key: ` + accessKey + `}
channels:
  openai:
    enabled: true
    upstream: {base_url: ` + upstreamURL + `, format: openai}
`)
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return f.Name()
}
