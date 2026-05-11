package planner

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"ai-api-stronger/internal/config"
)

const (
	PolicySourceChannel   = "channel"
	PolicySourceModelRule = "model_rule"
)

type RouteInfo struct {
	AccessKey string
	Channel   string
	TailPath  string
	RawQuery  string
}

// ExecutionPlan is the only request strategy object consumed by pipeline. It
// contains typed, immutable request-time data and never retains raw YAML nodes.
type ExecutionPlan struct {
	RequestID          string
	ChannelName        string
	ModelName          string
	UpstreamURL        string
	UpstreamFormat     string
	Timeout            time.Duration
	Proxy              *config.ProxyConfig
	Streaming          *config.StreamingConfig
	LLMPrivateProtect  bool
	RequestHeaderPlan  HeaderPlan
	RequestBodyPlan    RequestBodyPlan
	ResponseHeaderPlan HeaderPlan
	ResponseStatusMap  map[int]int
	Match              MatchMeta
}

type HeaderPlan struct {
	Set    map[string]string
	Delete []string
}

type RequestBodyPlan struct {
	ModelRewrite map[string]string
	SystemPrompt string
	MaxTokens    *int
	Temperature  *float64
	CustomSet    []config.CustomBodySet
}

type MatchMeta struct {
	PolicySource          string
	MatchedModelRule      bool
	MatchedModelRuleModel string
}

func BuildExecutionPlan(snapshot *config.RuntimeSnapshot, route RouteInfo, r *http.Request, body []byte) (*ExecutionPlan, error) {
	channel := snapshot.Channels[route.Channel]
	if channel == nil {
		return nil, ErrChannelNotFound
	}
	if !channel.Enabled {
		return nil, ErrChannelDisabled
	}

	originalModel, err := ExtractModel(channel.Upstream.Format, route.TailPath, body)
	if err != nil {
		return nil, err
	}
	matchedRule := enabledModelRule(channel, originalModel)
	selectedPolicy := SelectPolicy(snapshot.Runtime, channel, matchedRule)
	upstreamURL, err := BuildUpstreamURL(channel.Upstream.BaseURL, route.TailPath, route.RawQuery)
	if err != nil {
		return nil, err
	}

	executionPlan := &ExecutionPlan{
		RequestID:          EnsureRequestID(r),
		ChannelName:        channel.Name,
		ModelName:          originalModel,
		UpstreamURL:        upstreamURL,
		UpstreamFormat:     channel.Upstream.Format,
		Timeout:            selectedPolicy.Timeout,
		Proxy:              selectedPolicy.Proxy,
		Streaming:          selectedPolicy.Streaming,
		LLMPrivateProtect:  selectedPolicy.LLMPrivateProtect,
		RequestHeaderPlan:  selectedPolicy.RequestHeaderPlan,
		RequestBodyPlan:    selectedPolicy.RequestBodyPlan,
		ResponseHeaderPlan: selectedPolicy.ResponseHeaderPlan,
		ResponseStatusMap:  selectedPolicy.ResponseStatusMap,
		Match:              selectedPolicy.Match,
	}
	return executionPlan, ValidatePlan(executionPlan, snapshot)
}

func enabledModelRule(channel *config.ChannelRuntime, originalModel string) *config.ModelRuleRuntime {
	modelRule := channel.ModelRules[originalModel]
	if modelRule == nil || !modelRule.Enabled {
		return nil
	}
	return modelRule
}

func BuildUpstreamURL(baseURL, tailPath, rawQuery string) (string, error) {
	upstreamURL, err := url.Parse(baseURL)
	if err != nil || upstreamURL.Scheme == "" || upstreamURL.Host == "" {
		return "", fmt.Errorf("invalid upstream base URL")
	}
	// Client input may only contribute the proxy tail path and ordinary query;
	// scheme, host, and base path remain fixed by the configured channel.
	upstreamURL.RawQuery = ""
	upstreamURL.Fragment = ""
	upstreamURL.Path = strings.TrimRight(upstreamURL.Path, "/") + "/" + strings.TrimLeft(tailPath, "/")
	upstreamURL.RawQuery = rawQuery
	return upstreamURL.String(), nil
}

func EnsureRequestID(r *http.Request) string {
	if requestID := r.Header.Get("X-Request-Id"); requestID != "" {
		return requestID
	}
	return fmt.Sprintf("req-%d", time.Now().UnixNano())
}

func ValidatePlan(plan *ExecutionPlan, snapshot *config.RuntimeSnapshot) error {
	if plan.ChannelName == "" {
		return fmt.Errorf("channel is empty")
	}
	if _, err := url.ParseRequestURI(plan.UpstreamURL); err != nil {
		return err
	}
	if plan.UpstreamFormat != config.FormatOpenAI && plan.UpstreamFormat != config.FormatAnthropic && plan.UpstreamFormat != config.FormatGemini {
		return fmt.Errorf("unsupported format")
	}
	if plan.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	if plan.Streaming != nil && plan.Streaming.Mode != config.StreamingModeNonstreamToStream {
		return fmt.Errorf("unsupported streaming mode")
	}
	if plan.LLMPrivateProtect && snapshot.Privacy == nil {
		return fmt.Errorf("privacy config missing")
	}
	return nil
}
