package streaming

import (
	"testing"

	"ai-api-stronger/internal/config"
	"ai-api-stronger/internal/planner"
)

func TestStreamingDetectionAndShape(t *testing.T) {
	if !ClientWantsStream(config.FormatOpenAI, "", []byte(`{"stream":true}`)) {
		t.Fatalf("openai stream not detected")
	}
	if !ClientWantsStream(config.FormatGemini, "/v1beta/models/gemini:streamGenerateContent", nil) {
		t.Fatalf("gemini stream not detected")
	}
	body, err := ShapeNonstreamRequest(config.FormatOpenAI, []byte(`{"stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"stream":false}` {
		t.Fatalf("bad shaped body: %s", body)
	}
}

func TestGeminiNonstreamToStreamRejected(t *testing.T) {
	plan := &planner.ExecutionPlan{UpstreamFormat: config.FormatGemini, Streaming: &config.StreamingConfig{Mode: config.StreamingModeNonstreamToStream}}
	if err := ValidateNonstreamToStream(plan); err != ErrUnsupportedFakeStream {
		t.Fatalf("expected unsupported gemini, got %v", err)
	}
}
