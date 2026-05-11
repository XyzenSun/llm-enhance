package planner

import (
	"encoding/json"
	"errors"
	"strings"

	"ai-api-stronger/internal/config"
)

var (
	ErrChannelNotFound    = errors.New("channel not found")
	ErrChannelDisabled    = errors.New("channel disabled")
	ErrInvalidRequestBody = errors.New("invalid request body")
)

func ExtractModel(format, tailPath string, body []byte) (string, error) {
	switch format {
	case config.FormatOpenAI, config.FormatAnthropic:
		if len(strings.TrimSpace(string(body))) == 0 {
			return "", nil
		}
		var doc map[string]any
		if err := json.Unmarshal(body, &doc); err != nil {
			return "", ErrInvalidRequestBody
		}
		if v, ok := doc["model"].(string); ok {
			return v, nil
		}
		return "", nil
	case config.FormatGemini:
		return extractGeminiModel(tailPath), nil
	default:
		return "", errors.New("unsupported format")
	}
}

func extractGeminiModel(tailPath string) string {
	parts := strings.Split(strings.Trim(tailPath, "/"), "/")
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "models" {
			model := parts[i+1]
			if idx := strings.Index(model, ":"); idx >= 0 {
				model = model[:idx]
			}
			return model
		}
	}
	return ""
}
