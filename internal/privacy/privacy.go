package privacy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"ai-api-stronger/internal/config"

	masker "github.com/xyzensun/llm-privacy-masker"
	maskerprotocol "github.com/xyzensun/llm-privacy-masker/protocol"
	maskerstore "github.com/xyzensun/llm-privacy-masker/store"
)

const (
	defaultRequestTTL = 5 * time.Minute
	// defaultCompatibilitySystemPrompt 复用隐私库默认提示词，供兼容回退路径继续做真实敏感信息识别。
	defaultCompatibilitySystemPrompt = `## Role 你是一个隐私信息识别专家。从文本中识别敏感信息，例如：手机号(PHONE)、邮箱(EMAIL)、身份证号(ID_CARD)、密码(PASSWORD)、IP地址(IP)、银行卡号(BANK_CARD)。 只提取敏感信息本身，不要包含前面的描述文字。例如"我的手机号是13812345678"中，敏感信息应为"13812345678"而非"手机号13812345678"。 ## Input 你获取到的输入为一段可能含有敏感信息的文本 ## Output 1.只允许返回严格 JSON 对象，不要输出任何额外文本、markdown、代码块或解释。 2.顶层必须是对象，且必须包含 entries 数组字段 3.每个 entry 必须包含 original、placeholder、type 三个非空字符串字段 4.placeholder 必须使用具体类型加数字的格式，例如 ${PHONE_1}、${EMAIL_2}、${ID_CARD_3}、${PASSWORD_1}、${IP_1}、${BANK_CARD_1} 不要返回 ${TYPE_N} 这种示例占位符 5.如果没有识别到新的敏感信息，返回 {"entries":[]} ## Example ### Example 1 有敏感信息 INPUT： 帮我做一个简单的HTML网页来介绍我，我的手机号是13812345678，今年29岁，平时喜欢打篮球 OUTPUT：{"entries":[{"original":"13812345678","placeholder":"${PHONE_1}","type":"PHONE"}]} ### Example 2 无敏感信息 INPUT： 帮我查询一下最近发生的新闻 OUTPUT：{"entries":[]} ### Example 3 错误示例（错误原因：你应返回严格 JSON 对象，不要输出任何额外文本、markdown、代码块或解释。） INPUT：帮我做一个简单的HTML网页来介绍我，我的手机号是13812345678，今年29岁，平时喜欢打篮球 OUTPUT：这里是结果：{"entries":[{"original":"13812345678","placeholder":"${PHONE_1}","type":"PHONE"}]}`
)

// Processor 定义隐私保护库在请求管线中的执行边界。
type Processor interface {
	Do(ctx context.Context, req Request) (*http.Response, error)
}

// Request 是交给隐私保护库处理的完整上游请求。
type Request struct {
	// Method 是发往上游的 HTTP 方法。
	Method string
	// UpstreamURL 是已由路由层固定构造的上游地址。
	UpstreamURL string
	// Headers 是已完成请求头策略处理的上游请求头。
	Headers http.Header
	// Body 是已完成语义化处理和流式整形的上游请求体。
	Body []byte
	// SessionID 是可选的隐私保护会话标识。
	SessionID string
}

// ProcessFunc 便于测试用函数实现 Processor。
type ProcessFunc func(ctx context.Context, req Request) (*http.Response, error)

// Do 调用底层函数执行隐私保护处理。
func (f ProcessFunc) Do(ctx context.Context, req Request) (*http.Response, error) {
	return f(ctx, req)
}

// Masker 是 llm-privacy-masker 暴露的请求处理能力。
type Masker interface {
	Process(req *http.Request, sessionID ...string) (*http.Response, error)
}

// LibraryProcessor 使用真实 llm-privacy-masker 执行脱敏、转发和反脱敏。
type LibraryProcessor struct {
	// masker 是真实隐私保护库构建出的处理实例。
	masker Masker
	// compatibilityProcessor 仅在结构化输出与当前真实上游不兼容时回退使用。
	compatibilityProcessor *compatibilityProcessor
}

// compatibilityProcessor 在真实隐私库结构化解析失败时，回退到兼容链路继续完成脱敏与反脱敏。
type compatibilityProcessor struct {
	store            maskerstore.Store
	protocolHandlers map[maskerprotocol.ProtocolType]maskerprotocol.Protocol
	trustedLLMClient *compatibilityTrustedLLMClient
	httpClient       *http.Client
}

// compatibilityTrustedLLMClient 负责用 OpenAI 兼容接口向可信 LLM 请求敏感信息映射。
type compatibilityTrustedLLMClient struct {
	baseURL      string
	apiKey       string
	model        string
	systemPrompt string
	temperature  float64
	httpClient   *http.Client
}

// NewLibraryProcessor 根据配置创建真实隐私保护处理器。
func NewLibraryProcessor(cfg *config.LLMPrivateProtectConfig, timeout time.Duration) (*LibraryProcessor, error) {
	normalizedTrustedBaseURL := normalizeTrustedLLMBaseURL(cfg.BaseURL)
	builder := masker.Builder().
		WithTrustedLLMBaseURL(normalizedTrustedBaseURL).
		WithTrustedLLMAPIKey(cfg.APIKey).
		WithTrustedLLMModelName(cfg.Model).
		WithTrustedLLMSystemPrompt(cfg.SystemPrompt).
		WithTrustedLLMTemperature(cfg.Temperature)
	if timeout > 0 {
		builder = builder.WithTimeout(timeout)
	}
	if cfg.Redis != "" {
		builder = builder.WithSessionStoreType("redis").WithRedisConnectionURL(cfg.Redis)
	} else {
		builder = builder.WithSessionStoreType("memory")
	}
	maskerInstance, err := builder.Build()
	if err != nil {
		return nil, err
	}
	compatibilityProcessor, err := newCompatibilityProcessor(cfg, timeout)
	if err != nil {
		return nil, err
	}
	return &LibraryProcessor{masker: maskerInstance, compatibilityProcessor: compatibilityProcessor}, nil
}

// newCompatibilityProcessor 初始化兼容回退链路所需的协议处理器、映射存储和 HTTP 客户端。
func newCompatibilityProcessor(cfg *config.LLMPrivateProtectConfig, timeout time.Duration) (*compatibilityProcessor, error) {
	trustedTimeout := timeout
	if trustedTimeout <= 0 {
		trustedTimeout = masker.DefaultTrustedLLMConfig().Timeout
	}
	mappingStore, err := newCompatibilityStore(cfg.Redis)
	if err != nil {
		return nil, err
	}
	return &compatibilityProcessor{
		store: mappingStore,
		protocolHandlers: map[maskerprotocol.ProtocolType]maskerprotocol.Protocol{
			maskerprotocol.ProtocolTypeOpenAI:    maskerprotocol.NewOpenAI(),
			maskerprotocol.ProtocolTypeAnthropic: maskerprotocol.NewAnthropic(),
			maskerprotocol.ProtocolTypeGemini:    maskerprotocol.NewGemini(),
		},
		trustedLLMClient: &compatibilityTrustedLLMClient{
			baseURL:      normalizeTrustedLLMBaseURL(cfg.BaseURL),
			apiKey:       cfg.APIKey,
			model:        cfg.Model,
			systemPrompt: cfg.SystemPrompt,
			temperature:  cfg.Temperature,
			httpClient:   &http.Client{Timeout: trustedTimeout},
		},
		httpClient: &http.Client{Timeout: trustedTimeout},
	}, nil
}

// newCompatibilityStore 按配置选择内存或 Redis 映射存储。
func newCompatibilityStore(redisURL string) (maskerstore.Store, error) {
	if redisURL == "" {
		return maskerstore.NewMemoryStore(), nil
	}
	return maskerstore.NewRedisStore(redisURL, 0, defaultRequestTTL)
}

// NewLibraryProcessorWithMasker 用指定 masker 创建处理器，供边界测试使用。
func NewLibraryProcessorWithMasker(m Masker) *LibraryProcessor {
	return &LibraryProcessor{masker: m}
}

// normalizeTrustedLLMBaseURL 兼容最终配置里的根地址写法，补出可信 LLM 所需的 /v1 API 前缀。
func normalizeTrustedLLMBaseURL(rawBaseURL string) string {
	parsedBaseURL, err := url.Parse(strings.TrimSpace(rawBaseURL))
	if err != nil {
		return strings.TrimRight(rawBaseURL, "/")
	}
	parsedBaseURL.RawPath = ""
	path := strings.TrimRight(parsedBaseURL.Path, "/")
	if path == "" {
		parsedBaseURL.Path = "/v1"
		return strings.TrimRight(parsedBaseURL.String(), "/")
	}
	if strings.HasSuffix(path, "/v1") {
		parsedBaseURL.Path = path
		return strings.TrimRight(parsedBaseURL.String(), "/")
	}
	parsedBaseURL.Path = path + "/v1"
	return strings.TrimRight(parsedBaseURL.String(), "/")
}

// Do 将管线请求转成 http.Request 并交给隐私保护库处理。
func (p *LibraryProcessor) Do(ctx context.Context, req Request) (*http.Response, error) {
	httpReq, err := buildHTTPRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	protectedResponse, err := p.processWithMasker(httpReq, req.SessionID)
	if err == nil || p.compatibilityProcessor == nil || !shouldRetryWithCompatibilityFallback(err) {
		return protectedResponse, err
	}
	return p.compatibilityProcessor.Do(ctx, req)
}

// buildHTTPRequest 构造交给隐私库处理的上游 HTTP 请求对象。
func buildHTTPRequest(ctx context.Context, req Request) (*http.Request, error) {
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.UpstreamURL, bytes.NewReader(req.Body))
	if err != nil {
		return nil, err
	}
	httpReq.Header = req.Headers.Clone()
	return httpReq, nil
}

// processWithMasker 将会话信息按隐私库约定传入真实处理器。
func (p *LibraryProcessor) processWithMasker(httpReq *http.Request, sessionID string) (*http.Response, error) {
	if sessionID != "" {
		return p.masker.Process(httpReq, sessionID)
	}
	return p.masker.Process(httpReq)
}

var compatibilityFallbackErrorSubstrings = []string{
	"可信 LLM 响应中 choices 字段缺失或无效",
	"可信 LLM 响应中第一个 choice 格式无效",
	"可信 LLM 响应中 message 字段无效",
	"可信 LLM 响应中 content 字段缺失或无效",
	"可信 LLM 响应中 content 的第 ",
	"可信 LLM 响应内容不是严格 JSON",
	"可信 LLM 响应缺少 entries 字段",
	"可信 LLM 响应中的 entries 字段无效",
}

// shouldRetryWithCompatibilityFallback 仅在可信 LLM 结构化响应不兼容时触发兼容回退。
func shouldRetryWithCompatibilityFallback(err error) bool {
	if err == nil {
		return false
	}
	errText := err.Error()
	for _, fallbackErrorSubstring := range compatibilityFallbackErrorSubstrings {
		if strings.Contains(errText, fallbackErrorSubstring) {
			return true
		}
	}
	return false
}

// Do 走兼容回退链路，完成请求脱敏、真实上游转发和响应反脱敏。
func (p *compatibilityProcessor) Do(ctx context.Context, req Request) (*http.Response, error) {
	requestProtocolType, err := maskerprotocol.JudgeProtocol(req.UpstreamURL, req.Body)
	if err != nil {
		return nil, fmt.Errorf("判断请求协议失败: %w", err)
	}
	requestProtocol, ok := p.protocolHandlers[requestProtocolType]
	if !ok {
		return nil, fmt.Errorf("不支持的协议类型: %s", requestProtocolType)
	}
	maskedRequestBody, placeholderToOriginal, requestID, err := p.processRequest(ctx, requestProtocol, req.Body, req.SessionID)
	if err != nil {
		return nil, fmt.Errorf("处理请求失败: %w", err)
	}
	targetURL, maskedRequestBody, err := requestProtocol.ForceNonStream(req.UpstreamURL, maskedRequestBody)
	if err != nil {
		return nil, fmt.Errorf("强制关闭流式传输失败: %w", err)
	}
	upstreamRequest, err := buildCompatibilityUpstreamRequest(ctx, req.Method, targetURL, req.Headers, maskedRequestBody)
	if err != nil {
		return nil, fmt.Errorf("构建上游请求失败: %w", err)
	}
	upstreamResponse, err := p.httpClient.Do(upstreamRequest)
	if err != nil {
		return nil, fmt.Errorf("执行上游请求失败: %w", err)
	}
	defer upstreamResponse.Body.Close()
	upstreamResponseBody, err := io.ReadAll(upstreamResponse.Body)
	if err != nil {
		return nil, fmt.Errorf("读取上游响应失败: %w", err)
	}
	if upstreamResponse.StatusCode < 200 || upstreamResponse.StatusCode >= 300 {
		if requestID != "" {
			if deleteErr := p.store.DeleteRequestMappings(requestID); deleteErr != nil {
				return &http.Response{StatusCode: upstreamResponse.StatusCode, Header: upstreamResponse.Header.Clone(), Body: io.NopCloser(bytes.NewReader(upstreamResponseBody))}, fmt.Errorf("删除请求映射失败: %w", deleteErr)
			}
		}
		return &http.Response{StatusCode: upstreamResponse.StatusCode, Header: upstreamResponse.Header.Clone(), Body: io.NopCloser(bytes.NewReader(upstreamResponseBody))}, nil
	}
	rewrittenResponseBody, err := requestProtocol.RewriteResponse(upstreamResponseBody, placeholderToOriginal)
	if err != nil {
		return nil, fmt.Errorf("处理响应失败: %w", err)
	}
	if requestID != "" {
		if err := p.store.DeleteRequestMappings(requestID); err != nil {
			return nil, fmt.Errorf("删除请求映射失败: %w", err)
		}
	}
	return &http.Response{StatusCode: upstreamResponse.StatusCode, Header: upstreamResponse.Header.Clone(), Body: io.NopCloser(bytes.NewReader(rewrittenResponseBody))}, nil
}

// processRequest 提取文本、请求可信 LLM 识别映射，并返回脱敏后的请求体。
func (p *compatibilityProcessor) processRequest(ctx context.Context, requestProtocol maskerprotocol.Protocol, requestBody []byte, sessionID string) ([]byte, map[string]string, string, error) {
	originalToPlaceholder := make(map[string]string)
	placeholderToOriginal := make(map[string]string)
	requestID := ""
	cacheHit := false
	if sessionID != "" {
		loadedOriginalToPlaceholder, loadedPlaceholderToOriginal, err := p.store.LoadSessionMappings(sessionID)
		if err != nil {
			return nil, nil, "", fmt.Errorf("加载会话映射失败: %w", err)
		}
		originalToPlaceholder = loadedOriginalToPlaceholder
		placeholderToOriginal = loadedPlaceholderToOriginal
		cacheHit = len(originalToPlaceholder) > 0 && len(placeholderToOriginal) > 0
	} else {
		requestID = generateRequestID()
	}
	if sessionID != "" && cacheHit {
		latestUserText, hasUserText, err := requestProtocol.LatestUserText(requestBody)
		if err != nil {
			return nil, nil, "", err
		}
		if hasUserText && latestUserText != "" {
			preMaskedText := masker.ApplyOriginalToPlaceholder(latestUserText, originalToPlaceholder)
			detectedMappings, err := p.trustedLLMClient.DetectMappings(ctx, originalToPlaceholder, preMaskedText)
			if err != nil {
				return nil, nil, "", fmt.Errorf("可信 LLM 检测失败: %w", err)
			}
			if len(detectedMappings.Entries) > 0 {
				if err := masker.MergeMappings(originalToPlaceholder, placeholderToOriginal, detectedMappings.Entries); err != nil {
					return nil, nil, "", fmt.Errorf("合并映射失败: %w", err)
				}
			}
			if err := p.store.SaveSessionMappings(sessionID, originalToPlaceholder, placeholderToOriginal); err != nil {
				return nil, nil, "", fmt.Errorf("保存会话映射失败: %w", err)
			}
		}
	} else {
		textNodes, err := requestProtocol.ExtractRequestTextNodes(requestBody)
		if err != nil {
			return nil, nil, "", err
		}
		allTexts := make([]string, 0, len(textNodes))
		for _, textNode := range textNodes {
			if textNode.Text != "" {
				allTexts = append(allTexts, textNode.Text)
			}
		}
		if len(allTexts) > 0 {
			combinedText := strings.Join(allTexts, "\n---\n")
			detectedMappings, err := p.trustedLLMClient.DetectMappings(ctx, originalToPlaceholder, combinedText)
			if err != nil {
				return nil, nil, "", fmt.Errorf("可信 LLM 检测失败（全量模式）: %w", err)
			}
			if len(detectedMappings.Entries) > 0 {
				if err := masker.MergeMappings(originalToPlaceholder, placeholderToOriginal, detectedMappings.Entries); err != nil {
					return nil, nil, "", fmt.Errorf("合并映射失败（全量模式）: %w", err)
				}
			}
		}
		if sessionID != "" {
			if err := p.store.SaveSessionMappings(sessionID, originalToPlaceholder, placeholderToOriginal); err != nil {
				return nil, nil, "", fmt.Errorf("保存会话映射失败: %w", err)
			}
		} else {
			if err := p.store.SaveRequestMappings(requestID, originalToPlaceholder, placeholderToOriginal); err != nil {
				return nil, nil, "", fmt.Errorf("保存请求映射失败: %w", err)
			}
		}
	}
	rewrittenBody, err := requestProtocol.RewriteRequest(requestBody, originalToPlaceholder)
	if err != nil {
		return nil, nil, "", err
	}
	return rewrittenBody, placeholderToOriginal, requestID, nil
}

// buildCompatibilityUpstreamRequest 保留既有请求头并构造兼容回退链路的上游请求。
func buildCompatibilityUpstreamRequest(ctx context.Context, method string, targetURL string, headers http.Header, requestBody []byte) (*http.Request, error) {
	upstreamRequest, err := http.NewRequestWithContext(ctx, method, targetURL, bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	for key, values := range headers {
		for _, value := range values {
			upstreamRequest.Header.Add(key, value)
		}
	}
	if upstreamRequest.Header.Get("Content-Type") == "" {
		upstreamRequest.Header.Set("Content-Type", "application/json")
	}
	return upstreamRequest, nil
}

// DetectMappings 通过可信 LLM 返回新的敏感信息映射集合。
func (c *compatibilityTrustedLLMClient) DetectMappings(ctx context.Context, knownMappings map[string]string, newMessage string) (*masker.TrustedLLMMappingResponse, error) {
	requestBody := map[string]any{
		"model": c.model,
		"messages": []map[string]any{
			{"role": "system", "content": compatibilitySystemPrompt(c.systemPrompt)},
			{"role": "user", "content": compatibilityUserPrompt(knownMappings, newMessage)},
		},
		"temperature": c.temperature,
	}
	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("序列化可信 LLM 请求失败: %w", err)
	}
	targetURL := strings.TrimRight(c.baseURL, "/") + "/chat/completions"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("创建可信 LLM 请求失败: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("执行可信 LLM 请求失败: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("读取可信 LLM 响应失败: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("可信 LLM 返回状态码 %d", response.StatusCode)
	}
	responseContent, err := extractCompatibilityTrustedLLMContent(responseBody)
	if err != nil {
		return nil, err
	}
	return parseCompatibilityTrustedLLMMappingResponse([]byte(responseContent))
}

// compatibilitySystemPrompt 优先使用配置提示词，否则回退到兼容链路默认提示词。
func compatibilitySystemPrompt(configuredPrompt string) string {
	if configuredPrompt != "" {
		return configuredPrompt
	}
	return defaultCompatibilitySystemPrompt
}

// compatibilityUserPrompt 组织可信 LLM 识别新增敏感信息所需的用户提示。
func compatibilityUserPrompt(knownMappings map[string]string, newMessage string) string {
	knownMappingsJSON, _ := json.Marshal(knownMappings)
	return fmt.Sprintf("已知映射：%s\n待检测文本：%s\n请识别新的敏感信息，并仅返回严格 JSON 对象。", string(knownMappingsJSON), newMessage)
}

// extractCompatibilityTrustedLLMContent 从 OpenAI 兼容响应里提取可信 LLM 输出文本。
func extractCompatibilityTrustedLLMContent(body []byte) (string, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("解析可信 LLM OpenAI 响应失败: %w", err)
	}
	choicesValue, ok := payload["choices"]
	if !ok {
		return "", fmt.Errorf("可信 LLM 响应中 choices 字段缺失或无效")
	}
	choices, ok := choicesValue.([]any)
	if !ok || len(choices) == 0 {
		return "", fmt.Errorf("可信 LLM 响应中 choices 字段缺失或无效")
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		return "", fmt.Errorf("可信 LLM 响应中第一个 choice 格式无效")
	}
	message, ok := choice["message"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("可信 LLM 响应中 message 字段无效")
	}
	if content, ok := message["content"].(string); ok && content != "" {
		return content, nil
	}
	parts, ok := message["content"].([]any)
	if !ok {
		return "", fmt.Errorf("可信 LLM 响应中 content 字段缺失或无效")
	}
	texts := make([]string, 0, len(parts))
	for partIndex, rawPart := range parts {
		part, ok := rawPart.(map[string]any)
		if !ok {
			return "", fmt.Errorf("可信 LLM 响应中 content 的第 %d 个部分格式无效", partIndex)
		}
		partText, _ := part["text"].(string)
		if partText != "" {
			texts = append(texts, partText)
		}
	}
	if len(texts) == 0 {
		return "", fmt.Errorf("可信 LLM 响应中 content 字段缺失或无效")
	}
	return strings.Join(texts, "\n"), nil
}

// parseCompatibilityTrustedLLMMappingResponse 校验并解析可信 LLM 返回的映射 JSON。
func parseCompatibilityTrustedLLMMappingResponse(body []byte) (*masker.TrustedLLMMappingResponse, error) {
	var rawResponse map[string]any
	if err := json.Unmarshal(body, &rawResponse); err != nil {
		return nil, fmt.Errorf("可信 LLM 响应内容不是严格 JSON: %w", err)
	}
	entriesValue, ok := rawResponse["entries"]
	if !ok {
		return nil, fmt.Errorf("可信 LLM 响应缺少 entries 字段")
	}
	entries, ok := entriesValue.([]any)
	if !ok {
		return nil, fmt.Errorf("可信 LLM 响应中的 entries 字段无效")
	}
	var response masker.TrustedLLMMappingResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("解析可信 LLM 映射响应失败: %w", err)
	}
	for entryIndex, entry := range response.Entries {
		if entry.Original == "" {
			return nil, fmt.Errorf("可信 LLM 映射的第 %d 条 original 为空", entryIndex)
		}
		if entry.Placeholder == "" {
			return nil, fmt.Errorf("可信 LLM 映射的第 %d 条 placeholder 为空", entryIndex)
		}
		if entry.Type == "" {
			return nil, fmt.Errorf("可信 LLM 映射的第 %d 条 type 为空", entryIndex)
		}
		if err := masker.ValidatePlaceholder(entry.Placeholder); err != nil {
			return nil, err
		}
	}
	if len(response.Entries) != len(entries) {
		return nil, fmt.Errorf("可信 LLM 响应中的 entries 字段存在无效条目")
	}
	return &response, nil
}

// generateRequestID 为无会话请求生成一次性映射存储键。
func generateRequestID() string {
	return fmt.Sprintf("req-%d", time.Now().UnixNano())
}

// SessionID 从请求头提取并裁剪隐私保护会话标识。
func SessionID(headers interface{ Get(string) string }) string {
	return strings.TrimSpace(headers.Get("X-Privacy-Session-Id"))
}
