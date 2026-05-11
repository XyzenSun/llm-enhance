package pipeline

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"ai-api-stronger/internal/config"
	"ai-api-stronger/internal/planner"
)

var uuidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestTransformOpenAIOrderAndCustomOverride(t *testing.T) {
	max := 10
	temp := 0.7
	plan := planner.RequestBodyPlan{ModelRewrite: map[string]string{"gpt-5": "gpt-5.5"}, SystemPrompt: "system", MaxTokens: &max, Temperature: &temp, CustomSet: []config.CustomBodySet{{Path: []string{"max_tokens"}, Value: float64(99)}}}
	res, err := TransformRequestBody(config.FormatOpenAI, []byte(`{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`), "gpt-5", plan)
	if err != nil {
		t.Fatal(err)
	}
	doc := decode(t, res.Body)
	if doc["model"] != "gpt-5.5" || doc["max_tokens"] != float64(99) {
		t.Fatalf("transform failed: %v", doc)
	}
	msgs := doc["messages"].([]any)
	first := msgs[0].(map[string]any)
	if first["role"] != "system" {
		t.Fatalf("system prompt not first")
	}
}

func TestTransformAnthropicSystemString(t *testing.T) {
	plan := planner.RequestBodyPlan{SystemPrompt: "new"}
	res, err := TransformRequestBody(config.FormatAnthropic, []byte(`{"system":"old","messages":[]}`), "", plan)
	if err != nil {
		t.Fatal(err)
	}
	sys := decode(t, res.Body)["system"].([]any)
	if sys[0].(map[string]any)["text"] != "new" || sys[1].(map[string]any)["text"] != "old" {
		t.Fatalf("bad anthropic system: %v", sys)
	}
}

func TestTransformGeminiFieldsAndCustomCreatesObjects(t *testing.T) {
	max := 12
	temp := 0.4
	plan := planner.RequestBodyPlan{SystemPrompt: "s", MaxTokens: &max, Temperature: &temp, CustomSet: []config.CustomBodySet{{Path: []string{"generationConfig", "thinkingConfig", "includeThoughts"}, Value: true}}}
	res, err := TransformRequestBody(config.FormatGemini, []byte(`{}`), "gemini", plan)
	if err != nil {
		t.Fatal(err)
	}
	doc := decode(t, res.Body)
	parts := doc["systemInstruction"].(map[string]any)["parts"].([]any)
	if parts[0].(map[string]any)["text"] != "s" {
		t.Fatalf("system instruction missing")
	}
	gc := doc["generationConfig"].(map[string]any)
	if gc["maxOutputTokens"] != float64(12) || gc["temperature"] != 0.4 || gc["thinkingConfig"].(map[string]any)["includeThoughts"] != true {
		t.Fatalf("bad gemini fields: %v", gc)
	}
}

func TestTransformCustomStringInterpolatesRuntimeVariables(t *testing.T) {
	now := time.Unix(1700000000, 0)
	max := 10
	plan := planner.RequestBodyPlan{MaxTokens: &max, CustomSet: []config.CustomBodySet{
		{Path: []string{"max_tokens"}, Value: "${ORIGIN}|${TIMESTAMP}"},
		{Path: []string{"metadata", "trace"}, Value: "${ORIGIN}suffix"},
		{Path: []string{"session", "id"}, Value: "${UUID}/${UUID}"},
	}}
	res, err := TransformRequestBodyAt(config.FormatOpenAI, []byte(`{"model":"m","max_tokens":7}`), "m", plan, now)
	if err != nil {
		t.Fatal(err)
	}
	doc := decode(t, res.Body)
	if got := doc["max_tokens"]; got != "7|1700000000" {
		t.Fatalf("custom max_tokens = %#v, want original numeric value with timestamp", got)
	}
	metadata := doc["metadata"].(map[string]any)
	if got := metadata["trace"]; got != "suffix" {
		t.Fatalf("missing origin should become empty string, got %#v", got)
	}
	session := doc["session"].(map[string]any)
	uuids := strings.Split(session["id"].(string), "/")
	if len(uuids) != 2 || !uuidRE.MatchString(uuids[0]) || !uuidRE.MatchString(uuids[1]) || uuids[0] == uuids[1] {
		t.Fatalf("uuid interpolation failed: %#v", session["id"])
	}
}

func TestHeaderDeleteThenSet(t *testing.T) {
	h := http.Header{"X-Test": []string{"old"}, "Connection": []string{"close"}}
	out := ApplyRequestHeaders(h, planner.HeaderPlan{Delete: []string{"X-Test"}, Set: map[string]string{"X-Test": "new", "X-Request-Id": "${uuid}"}}, "rid")
	if out.Get("Connection") != "" || out.Get("X-Test") != "new" || out.Get("X-Request-Id") != "rid" {
		t.Fatalf("bad headers: %v", out)
	}
}

func TestHeaderPlanInterpolatesRuntimeVariables(t *testing.T) {
	now := time.Unix(1700000000, 0)
	h := http.Header{"X-Test": []string{"old"}}
	ApplyHeaderPlanAt(h, planner.HeaderPlan{Set: map[string]string{
		"X-Test":    "${ORIGIN}|${TIMESTAMP}|${UUID}|${UUID}|${uuid}",
		"X-Missing": "${ORIGIN}suffix",
	}}, "rid", now)
	parts := strings.Split(h.Get("X-Test"), "|")
	if len(parts) != 5 {
		t.Fatalf("expected 5 interpolated parts, got %q", h.Get("X-Test"))
	}
	if parts[0] != "old" || parts[1] != "1700000000" || parts[4] != "rid" {
		t.Fatalf("unexpected interpolated header parts: %#v", parts)
	}
	if !uuidRE.MatchString(parts[2]) || !uuidRE.MatchString(parts[3]) || parts[2] == parts[3] {
		t.Fatalf("expected distinct UUIDs in header interpolation, got %#v", parts)
	}
	if got := h.Get("X-Missing"); got != "suffix" {
		t.Fatalf("missing origin should become empty string, got %q", got)
	}
}
