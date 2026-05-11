package streaming

import (
	"encoding/json"
	"strings"

	"ai-api-stronger/internal/config"
)

func ClientWantsStream(format, tailPath string, body []byte) bool {
	switch format {
	case config.FormatOpenAI:
		var doc map[string]any
		if json.Unmarshal(body, &doc) == nil {
			if v, ok := doc["stream"].(bool); ok {
				return v
			}
		}
	case config.FormatAnthropic:
		var doc map[string]any
		if json.Unmarshal(body, &doc) == nil {
			if v, ok := doc["stream"].(bool); ok {
				return v
			}
		}
	case config.FormatGemini:
		return strings.Contains(tailPath, ":streamGenerateContent")
	}
	return false
}
