package pipeline

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"ai-api-stronger/internal/config"
	"ai-api-stronger/internal/planner"
	"ai-api-stronger/internal/privacy"
	"ai-api-stronger/internal/response"
	"ai-api-stronger/internal/upstream"
)

type recordingPrivacy struct{ events *[]string }

func (p recordingPrivacy) Do(ctx context.Context, req privacy.Request) (*http.Response, error) {
	*p.events = append(*p.events, "privacy_process:"+string(req.Body))
	body := strings.ReplaceAll(string(req.Body), "secret", "MASKED")
	if strings.Contains(body, `"stream":true`) {
		body = strings.ReplaceAll(body, `"stream":true`, `"stream":false`)
	}
	*p.events = append(*p.events, "upstream:"+body)
	responseBody := `{"id":"chatcmpl-test","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"secret"},"finish_reason":"stop"}]}`
	if strings.Contains(req.UpstreamURL, "/v1/messages") {
		responseBody = `{"id":"msg-test","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"secret"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"X-Up": []string{"ok"}}, Body: io.NopCloser(strings.NewReader(responseBody))}, nil
}

func TestWriteUpstreamResponseMapsStatusOnly(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Up", "created")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"status":201}`))
	}))
	defer up.Close()
	plan := testPlan(up.URL, config.FormatOpenAI)
	plan.ResponseStatusMap = map[int]int{201: 202}
	plan.ResponseHeaderPlan = planner.HeaderPlan{Set: map[string]string{"X-Up": "${ORIGIN}-done"}}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/k/proxy/openai/v1/chat/completions", strings.NewReader(`{"model":"m"}`))
	Pipeline{Clients: upstream.NewManager()}.Execute(rr, req, plan, []byte(`{"model":"m"}`))
	if rr.Code != http.StatusAccepted || rr.Body.String() != `{"status":201}` {
		t.Fatalf("status/body = %d %s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("X-Up"); got != "created-done" {
		t.Fatalf("response header origin interpolation failed: %q", got)
	}
}

func TestSystemPromptTransformThroughPipeline(t *testing.T) {
	var got map[string]any
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`ok`))
	}))
	defer up.Close()
	plan := testPlan(up.URL, config.FormatOpenAI)
	plan.RequestBodyPlan.SystemPrompt = "system"
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/k/proxy/openai/v1/chat/completions", strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
	Pipeline{Clients: upstream.NewManager()}.Execute(rr, req, plan, []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
	msgs := got["messages"].([]any)
	first := msgs[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "system" {
		t.Fatalf("system prompt not transformed through pipeline: %#v", got)
	}
}

func TestNonstreamToStreamOpenAIRequestShapingAndSSEResponse(t *testing.T) {
	var got map[string]any
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		body := []byte(`{"id":"chatcmpl-test","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`)
		w.Header().Set("X-Up", "ok")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer up.Close()
	plan := testPlan(up.URL, config.FormatOpenAI)
	plan.Streaming = &config.StreamingConfig{Mode: config.StreamingModeNonstreamToStream, MaxBufferBytes: 1024}
	plan.ResponseStatusMap = map[int]int{http.StatusOK: http.StatusCreated}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/k/proxy/openai/v1/chat/completions", strings.NewReader(`{"model":"m","stream":true}`))
	Pipeline{Clients: upstream.NewManager()}.Execute(rr, req, plan, []byte(`{"model":"m","stream":true}`))
	if got["stream"] != false {
		t.Fatalf("upstream stream was not forced false: %#v", got)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("expected SSE content type, got %q", ct)
	}
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected mapped fake-stream status 201, got %d", rr.Code)
	}
	if rr.Header().Get("X-Up") != "ok" {
		t.Fatalf("expected response header plan to preserve upstream header, got %#v", rr.Header())
	}
	for _, key := range []string{"Content-Length"} {
		if got := rr.Header().Get(key); got != "" {
			t.Fatalf("expected fake-stream response to drop %s, got %q in %#v", key, got, rr.Header())
		}
	}
	if !strings.Contains(rr.Body.String(), "hello") {
		t.Fatalf("expected fake stream boundary body, got %s", rr.Body.String())
	}
}

func TestFakeStreamResponseAppliesConfiguredHeaderPlanAfterDroppingInvalidUpstreamHeaders(t *testing.T) {
	upstreamHeader := http.Header{}
	upstreamHeader.Set("Content-Length", "505")
	upstreamHeader.Set("Content-Encoding", "gzip")
	upstreamHeader.Set("Transfer-Encoding", "chunked")
	upstreamHeader.Set("Connection", "keep-alive")
	upstreamHeader.Set("X-Up", "ok")
	rr := httptest.NewRecorder()
	writer := newFakeStreamResponseWriter(rr, upstreamHeader, planner.HeaderPlan{
		Set: map[string]string{
			"Content-Length":    "1234",
			"Content-Encoding":  "identity",
			"Transfer-Encoding": "chunked",
			"Connection":        "keep-alive",
			"X-Request-ID":      "${uuid}",
		},
	}, map[int]int{http.StatusOK: http.StatusAccepted}, "rid")
	_, _ = writer.Write([]byte("data"))

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected mapped status 202, got %d", rr.Code)
	}
	if rr.Header().Get("X-Up") != "ok" {
		t.Fatalf("expected safe upstream header to be preserved, got %#v", rr.Header())
	}
	for key, want := range map[string]string{
		"Content-Length":    "1234",
		"Content-Encoding":  "identity",
		"Transfer-Encoding": "chunked",
		"Connection":        "keep-alive",
		"X-Request-ID":      "rid",
	} {
		if got := rr.Header().Get(key); got != want {
			t.Fatalf("expected configured %s=%q after header plan, got %q in %#v", key, want, got, rr.Header())
		}
	}
}

func TestNonstreamToStreamOpenAINonStreamingClientStillShapesUpstreamWithoutSSEResponse(t *testing.T) {
	var got map[string]any
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-test","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`))
	}))
	defer up.Close()
	plan := testPlan(up.URL, config.FormatOpenAI)
	plan.Streaming = &config.StreamingConfig{Mode: config.StreamingModeNonstreamToStream, MaxBufferBytes: 1024}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/k/proxy/openai/v1/chat/completions", strings.NewReader(`{"model":"m"}`))
	Pipeline{Clients: upstream.NewManager()}.Execute(rr, req, plan, []byte(`{"model":"m"}`))
	if got["stream"] != false {
		t.Fatalf("upstream stream was not forced false for non-streaming client: %#v", got)
	}
	if ct := rr.Header().Get("Content-Type"); strings.Contains(ct, "text/event-stream") {
		t.Fatalf("expected ordinary non-stream content type, got %q", ct)
	}
	if !strings.Contains(rr.Body.String(), `"content":"hello"`) {
		t.Fatalf("expected ordinary upstream body, got %s", rr.Body.String())
	}
}

func TestNonstreamToStreamAnthropicRequestShapingAndSSEResponse(t *testing.T) {
	var got map[string]any
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"id":"msg-test","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer up.Close()
	plan := testPlan(up.URL, config.FormatAnthropic)
	plan.Streaming = &config.StreamingConfig{Mode: config.StreamingModeNonstreamToStream, MaxBufferBytes: 1024}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/k/proxy/anthropic/v1/messages", strings.NewReader(`{"model":"m","stream":true}`))
	Pipeline{Clients: upstream.NewManager()}.Execute(rr, req, plan, []byte(`{"model":"m","stream":true}`))
	if got["stream"] != false {
		t.Fatalf("upstream stream was not forced false: %#v", got)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("expected SSE content type, got %q", ct)
	}
}

func TestNonstreamToStreamGeminiReturns3002(t *testing.T) {
	plan := testPlan("https://example.com", config.FormatGemini)
	plan.Streaming = &config.StreamingConfig{Mode: config.StreamingModeNonstreamToStream, MaxBufferBytes: 1024}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/k/proxy/gemini/v1beta/models/gemini:streamGenerateContent", strings.NewReader(`{}`))
	Pipeline{Clients: upstream.NewManager()}.Execute(rr, req, plan, []byte(`{}`))
	var env response.Envelope
	_ = json.Unmarshal(rr.Body.Bytes(), &env)
	if rr.Code != http.StatusBadRequest || env.Code != response.CodeStreamingConflict {
		t.Fatalf("got status/code/body %d/%d/%s", rr.Code, env.Code, rr.Body.String())
	}
}

func TestPrivacyProcessReceivesTransformedRequestAndReturnsProtectedResponse(t *testing.T) {
	events := []string{}
	plan := testPlan("https://example.com/v1/chat/completions", config.FormatOpenAI)
	plan.LLMPrivateProtect = true
	plan.RequestBodyPlan.SystemPrompt = "secret system"
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/k/proxy/openai/v1/chat/completions", strings.NewReader(`{"model":"m"}`))
	Pipeline{Clients: upstream.NewManager(), Privacy: recordingPrivacy{events: &events}}.Execute(rr, req, plan, []byte(`{"model":"m"}`))
	if !reflect.DeepEqual(events, []string{
		`privacy_process:{"messages":[{"content":"secret system","role":"system"}],"model":"m"}`,
		`upstream:{"messages":[{"content":"MASKED system","role":"system"}],"model":"m"}`,
	}) {
		t.Fatalf("unexpected privacy process events: %#v", events)
	}
	if rr.Body.String() != `{"id":"chatcmpl-test","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"secret"},"finish_reason":"stop"}]}` {
		t.Fatalf("protected response was not written: %s", rr.Body.String())
	}
}

func TestPrivacyAndChannelLevelNonstreamToStreamShapesBeforePrivacy(t *testing.T) {
	events := []string{}
	plan := testPlan("https://example.com/v1/chat/completions", config.FormatOpenAI)
	plan.LLMPrivateProtect = true
	plan.RequestBodyPlan.SystemPrompt = "secret"
	plan.Streaming = &config.StreamingConfig{Mode: config.StreamingModeNonstreamToStream, MaxBufferBytes: 1024}
	plan.Match = planner.MatchMeta{PolicySource: planner.PolicySourceChannel}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/k/proxy/openai/v1/chat/completions", strings.NewReader(`{"model":"m","stream":true}`))
	Pipeline{Clients: upstream.NewManager(), Privacy: recordingPrivacy{events: &events}}.Execute(rr, req, plan, []byte(`{"model":"m","stream":true}`))
	if !reflect.DeepEqual(events, []string{
		`privacy_process:{"messages":[{"content":"secret","role":"system"}],"model":"m","stream":false}`,
		`upstream:{"messages":[{"content":"MASKED","role":"system"}],"model":"m","stream":false}`,
	}) {
		t.Fatalf("unexpected combined ordering/events: %#v", events)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("expected SSE content type, got %q", ct)
	}
	if !strings.Contains(rr.Body.String(), "secret") {
		t.Fatalf("expected restored body at fake-stream boundary, got %s", rr.Body.String())
	}
}

func TestPrivacyAndModelLevelNonstreamToStreamShapesBeforePrivacy(t *testing.T) {
	events := []string{}
	plan := testPlan("https://example.com/v1/messages", config.FormatAnthropic)
	plan.LLMPrivateProtect = true
	plan.Streaming = &config.StreamingConfig{Mode: config.StreamingModeNonstreamToStream, MaxBufferBytes: 1024}
	plan.Match = planner.MatchMeta{PolicySource: planner.PolicySourceModelRule, MatchedModelRule: true, MatchedModelRuleModel: "m"}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/k/proxy/anthropic/v1/messages", strings.NewReader(`{"model":"m","stream":true}`))
	Pipeline{Clients: upstream.NewManager(), Privacy: recordingPrivacy{events: &events}}.Execute(rr, req, plan, []byte(`{"model":"m","stream":true}`))
	if !reflect.DeepEqual(events, []string{
		`privacy_process:{"model":"m","stream":false}`,
		`upstream:{"model":"m","stream":false}`,
	}) {
		t.Fatalf("unexpected model-level combined ordering/events: %#v", events)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("expected SSE content type, got %q", ct)
	}
}

func TestNonstreamToStreamWithoutGlobalBufferLimitAllowsLargeBufferedResponse(t *testing.T) {
	largeContent := strings.Repeat("hello", 600)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"chatcmpl-test","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"` + largeContent + `"},"finish_reason":"stop"}]}`))
	}))
	defer up.Close()

	plan := testPlan(up.URL, config.FormatOpenAI)
	plan.Streaming = &config.StreamingConfig{Mode: config.StreamingModeNonstreamToStream}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/k/proxy/openai/v1/chat/completions", strings.NewReader(`{"model":"m","stream":true}`))
	Pipeline{Clients: upstream.NewManager()}.Execute(rr, req, plan, []byte(`{"model":"m","stream":true}`))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected unlimited buffered response to succeed, got status %d body %s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("expected SSE content type, got %q", ct)
	}
	if !strings.Contains(rr.Body.String(), largeContent[:64]) {
		t.Fatalf("expected fake-stream body to include buffered content, got %s", rr.Body.String())
	}
}

func testPlan(upstreamURL, format string) *planner.ExecutionPlan {
	return &planner.ExecutionPlan{
		RequestID:         "rid",
		ChannelName:       format,
		ModelName:         "m",
		UpstreamURL:       upstreamURL,
		UpstreamFormat:    format,
		Timeout:           time.Second,
		ResponseStatusMap: map[int]int{},
	}
}
