package streaming

import (
	"encoding/json"
	"errors"

	"ai-api-stronger/internal/config"
	"ai-api-stronger/internal/planner"
)

var (
	ErrUnsupportedFakeStream = errors.New("nonstream_to_stream unsupported for format")
	ErrResponseTooLarge      = errors.New("nonstream_to_stream response exceeds buffer limit")
)

func ValidateNonstreamToStream(plan *planner.ExecutionPlan) error {
	if plan.Streaming == nil || plan.Streaming.Mode != config.StreamingModeNonstreamToStream {
		return nil
	}
	if plan.UpstreamFormat == config.FormatGemini {
		return ErrUnsupportedFakeStream
	}
	return nil
}

func ShapeNonstreamRequest(format string, body []byte) ([]byte, error) {
	if format != config.FormatOpenAI && format != config.FormatAnthropic {
		return nil, ErrUnsupportedFakeStream
	}
	var doc map[string]any
	if len(body) > 0 {
		if err := json.Unmarshal(body, &doc); err != nil {
			return nil, err
		}
	} else {
		doc = map[string]any{}
	}
	doc["stream"] = false
	return json.Marshal(doc)
}
