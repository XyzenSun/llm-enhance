package pipeline

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"ai-api-stronger/internal/config"
	"ai-api-stronger/internal/planner"
)

type BodyTransformResult struct {
	Body []byte
	Meta BodyTransformMeta
}

type BodyTransformMeta struct {
	OriginalModel        string
	RewrittenModel       string
	ModelRewriteApplied  bool
	SystemPromptInserted bool
	MaxTokensApplied     bool
	TemperatureApplied   bool
	CustomSetCount       int
}

type BodyTransformError struct{ Message string }

func (e BodyTransformError) Error() string { return e.Message }

func TransformRequestBody(format string, body []byte, originalModel string, plan planner.RequestBodyPlan) (*BodyTransformResult, error) {
	return TransformRequestBodyAt(format, body, originalModel, plan, time.Now())
}

func TransformRequestBodyAt(format string, body []byte, originalModel string, plan planner.RequestBodyPlan, now time.Time) (*BodyTransformResult, error) {
	if isZeroBodyPlan(plan) {
		return &BodyTransformResult{Body: body, Meta: BodyTransformMeta{OriginalModel: originalModel}}, nil
	}

	requestDocument, err := decodeRequestDocument(body)
	if err != nil {
		return nil, err
	}
	originalDocument := cloneJSONDocument(requestDocument)
	transformMeta := BodyTransformMeta{OriginalModel: originalModel}
	applyModelRewrite(format, requestDocument, originalModel, plan.ModelRewrite, &transformMeta)
	if plan.SystemPrompt != "" {
		if err := applySystemPrompt(format, requestDocument, plan.SystemPrompt); err != nil {
			return nil, err
		}
		transformMeta.SystemPromptInserted = true
	}
	if plan.MaxTokens != nil {
		applyMaxTokens(format, requestDocument, *plan.MaxTokens)
		transformMeta.MaxTokensApplied = true
	}
	if plan.Temperature != nil {
		applyTemperature(format, requestDocument, *plan.Temperature)
		transformMeta.TemperatureApplied = true
	}
	for _, customSet := range plan.CustomSet {
		applyCustom(requestDocument, customSet.Path, interpolateCustomValue(originalDocument, customSet, now))
		transformMeta.CustomSetCount++
	}

	transformedBody, err := json.Marshal(requestDocument)
	if err != nil {
		return nil, err
	}
	return &BodyTransformResult{Body: transformedBody, Meta: transformMeta}, nil
}

func decodeRequestDocument(body []byte) (map[string]any, error) {
	requestDocument := map[string]any{}
	if strings.TrimSpace(string(body)) == "" {
		return requestDocument, nil
	}
	if err := json.Unmarshal(body, &requestDocument); err != nil {
		return nil, BodyTransformError{Message: "request body must be a JSON object"}
	}
	return requestDocument, nil
}

func isZeroBodyPlan(plan planner.RequestBodyPlan) bool {
	return len(plan.ModelRewrite) == 0 && plan.SystemPrompt == "" && plan.MaxTokens == nil && plan.Temperature == nil && len(plan.CustomSet) == 0
}

func applyModelRewrite(format string, requestDocument map[string]any, originalModel string, rewrites map[string]string, meta *BodyTransformMeta) {
	if len(rewrites) == 0 {
		return
	}
	rewrittenModel, ok := rewrites[originalModel]
	if !ok {
		return
	}
	meta.RewrittenModel = rewrittenModel
	meta.ModelRewriteApplied = true
	if format == config.FormatGemini {
		// Gemini model identity lives in the URL path; only rewrite a body model
		// field when one is already present, preserving the external route shape.
		if _, exists := requestDocument["model"]; exists {
			requestDocument["model"] = rewrittenModel
		}
		return
	}
	requestDocument["model"] = rewrittenModel
}

func applySystemPrompt(format string, requestDocument map[string]any, prompt string) error {
	switch format {
	case config.FormatOpenAI:
		return applyOpenAISystemPrompt(requestDocument, prompt)
	case config.FormatAnthropic:
		return applyAnthropicSystemPrompt(requestDocument, prompt)
	case config.FormatGemini:
		return applyGeminiSystemPrompt(requestDocument, prompt)
	default:
		return fmt.Errorf("unsupported format")
	}
}

func applyOpenAISystemPrompt(requestDocument map[string]any, prompt string) error {
	systemMessage := map[string]any{"role": "system", "content": prompt}
	messagesValue, exists := requestDocument["messages"]
	if !exists {
		requestDocument["messages"] = []any{systemMessage}
		return nil
	}
	messages, ok := messagesValue.([]any)
	if !ok {
		return BodyTransformError{Message: "openai messages must be an array"}
	}
	requestDocument["messages"] = append([]any{systemMessage}, messages...)
	return nil
}

func applyAnthropicSystemPrompt(requestDocument map[string]any, prompt string) error {
	systemBlock := map[string]any{"type": "text", "text": prompt}
	systemValue, exists := requestDocument["system"]
	if !exists {
		requestDocument["system"] = prompt
		return nil
	}
	switch existingSystem := systemValue.(type) {
	case string:
		requestDocument["system"] = []any{systemBlock, map[string]any{"type": "text", "text": existingSystem}}
	case []any:
		requestDocument["system"] = append([]any{systemBlock}, existingSystem...)
	default:
		return BodyTransformError{Message: "anthropic system must be string or array"}
	}
	return nil
}

func applyGeminiSystemPrompt(requestDocument map[string]any, prompt string) error {
	systemInstruction := ensureObject(requestDocument, "systemInstruction")
	newPart := map[string]any{"text": prompt}
	partsValue, exists := systemInstruction["parts"]
	if !exists {
		systemInstruction["parts"] = []any{newPart}
		return nil
	}
	parts, ok := partsValue.([]any)
	if !ok {
		return BodyTransformError{Message: "gemini systemInstruction.parts must be an array"}
	}
	systemInstruction["parts"] = append([]any{newPart}, parts...)
	return nil
}

func applyMaxTokens(format string, requestDocument map[string]any, maxTokens int) {
	if format == config.FormatGemini {
		generationConfig := ensureObject(requestDocument, "generationConfig")
		generationConfig["maxOutputTokens"] = maxTokens
		return
	}
	requestDocument["max_tokens"] = maxTokens
}

func applyTemperature(format string, requestDocument map[string]any, temperature float64) {
	if format == config.FormatGemini {
		generationConfig := ensureObject(requestDocument, "generationConfig")
		generationConfig["temperature"] = temperature
		return
	}
	requestDocument["temperature"] = temperature
}

func applyCustom(requestDocument map[string]any, path []string, value any) {
	if len(path) == 0 {
		return
	}
	currentObject := requestDocument
	for _, pathSegment := range path[:len(path)-1] {
		nextObject, _ := currentObject[pathSegment].(map[string]any)
		if nextObject == nil {
			nextObject = map[string]any{}
			currentObject[pathSegment] = nextObject
		}
		currentObject = nextObject
	}
	currentObject[path[len(path)-1]] = value
}

func interpolateCustomValue(originalDocument map[string]any, customSet config.CustomBodySet, now time.Time) any {
	value, ok := customSet.Value.(string)
	if !ok {
		return customSet.Value
	}
	return interpolateRuntimeVariables(value, bodyPathString(originalDocument, customSet.Path), now)
}

func bodyPathString(requestDocument map[string]any, path []string) string {
	value, ok := bodyPathValue(requestDocument, path)
	if !ok || value == nil {
		return ""
	}
	switch typedValue := value.(type) {
	case string:
		return typedValue
	case bool:
		return strconv.FormatBool(typedValue)
	case float64:
		return strconv.FormatFloat(typedValue, 'f', -1, 64)
	default:
		encoded, err := json.Marshal(typedValue)
		if err != nil {
			return ""
		}
		return string(encoded)
	}
}

func bodyPathValue(requestDocument map[string]any, path []string) (any, bool) {
	if len(path) == 0 {
		return nil, false
	}
	var current any = requestDocument
	for _, pathSegment := range path {
		currentObject, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = currentObject[pathSegment]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func cloneJSONDocument(requestDocument map[string]any) map[string]any {
	encoded, err := json.Marshal(requestDocument)
	if err != nil {
		return map[string]any{}
	}
	var cloned map[string]any
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		return map[string]any{}
	}
	return cloned
}

func ensureObject(requestDocument map[string]any, key string) map[string]any {
	objectValue, _ := requestDocument[key].(map[string]any)
	if objectValue == nil {
		objectValue = map[string]any{}
		requestDocument[key] = objectValue
	}
	return objectValue
}
