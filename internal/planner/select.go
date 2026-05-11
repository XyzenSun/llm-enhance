package planner

import (
	"math/rand"
	"time"

	"ai-api-stronger/internal/config"
)

type selectedPolicy struct {
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

var chooseProxyIndex = func(proxyCount int) int {
	return rand.Intn(proxyCount)
}

func SelectPolicy(runtime config.RuntimeConfig, channel *config.ChannelRuntime, modelRule *config.ModelRuleRuntime) selectedPolicy {
	policy, privacyEnabled, matchMeta := policySource(channel, modelRule)
	return selectedPolicy{
		Timeout:            selectedTimeout(policy, runtime),
		Proxy:              selectedProxy(policy.Proxies),
		Streaming:          selectedStreaming(policy, runtime),
		LLMPrivateProtect:  privacyEnabled,
		RequestHeaderPlan:  headerPlanFromRuntime(policy.RequestHeaders),
		RequestBodyPlan:    requestBodyPlanFromRuntime(policy.RequestBody),
		ResponseHeaderPlan: headerPlanFromRuntime(policy.ResponseHeaders),
		ResponseStatusMap:  cloneIntMap(policy.ResponseStatus),
		Match:              matchMeta,
	}
}

func policySource(channel *config.ChannelRuntime, modelRule *config.ModelRuleRuntime) (config.PolicyRuntime, bool, MatchMeta) {
	if modelRule != nil && modelRule.Enabled {
		// Policy selection is exclusive: an enabled model rule replaces the
		// channel policy instead of inheriting or merging any channel fields.
		return modelRule.Policy, modelRule.LLMPrivateProtectEnable, MatchMeta{
			PolicySource:          PolicySourceModelRule,
			MatchedModelRule:      true,
			MatchedModelRuleModel: modelRule.Model,
		}
	}
	return channel.ChannelPolicy, channel.LLMPrivateProtectEnable, MatchMeta{PolicySource: PolicySourceChannel}
}

func selectedTimeout(policy config.PolicyRuntime, runtime config.RuntimeConfig) time.Duration {
	timeout := time.Duration(policy.Timeout)
	if timeout == 0 {
		return runtime.DefaultTimeout
	}
	return timeout
}

func selectedStreaming(policy config.PolicyRuntime, runtime config.RuntimeConfig) *config.StreamingConfig {
	streamingConfig := cloneStreaming(policy.StreamingSetting)
	if streamingConfig == nil {
		return nil
	}
	if streamingConfig.Timeout == 0 {
		streamingConfig.Timeout = runtime.DefaultTimeout
	}
	return streamingConfig
}

func headerPlanFromRuntime(headerMutation config.HeaderMutation) HeaderPlan {
	return HeaderPlan{
		Set:    cloneStringMap(headerMutation.Set),
		Delete: append([]string(nil), headerMutation.Delete...),
	}
}

func requestBodyPlanFromRuntime(requestBody config.RequestBodyRuntime) RequestBodyPlan {
	return RequestBodyPlan{
		ModelRewrite: cloneStringMap(requestBody.ModelRewrite),
		SystemPrompt: requestBody.SystemPrompt,
		MaxTokens:    cloneIntPtr(requestBody.MaxTokens),
		Temperature:  cloneFloatPtr(requestBody.Temperature),
		CustomSet:    append([]config.CustomBodySet(nil), requestBody.CustomSet...),
	}
}

func selectedProxy(proxies []config.ProxyConfig) *config.ProxyConfig {
	if len(proxies) == 0 {
		return nil
	}
	selected := proxies[chooseProxyIndex(len(proxies))]
	return cloneProxy(&selected)
}

func cloneProxy(in *config.ProxyConfig) *config.ProxyConfig {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func cloneStreaming(in *config.StreamingConfig) *config.StreamingConfig {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func cloneIntPtr(in *int) *int {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func cloneFloatPtr(in *float64) *float64 {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneIntMap(in map[int]int) map[int]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[int]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
