package config

import "sync/atomic"

// RuntimeSnapshot is request-time configuration. BuildSnapshot clones and
// indexes YAML data so handlers can treat a loaded snapshot as immutable.
type RuntimeSnapshot struct {
	ConfigHash string
	Server     ServerConfig
	Security   SecurityConfig
	Runtime    RuntimeConfig
	Log        LogConfig
	Privacy    *LLMPrivateProtectConfig
	Channels   map[string]*ChannelRuntime
}

type ChannelRuntime struct {
	Name                    string
	Enabled                 bool
	LLMPrivateProtectEnable bool
	Upstream                UpstreamConfig
	ChannelPolicy           PolicyRuntime
	ModelRules              map[string]*ModelRuleRuntime
}

type ModelRuleRuntime struct {
	Model                   string
	Enabled                 bool
	LLMPrivateProtectEnable bool
	Policy                  PolicyRuntime
}

type PolicyRuntime struct {
	Timeout          int64
	Proxies          []ProxyConfig
	RequestHeaders   HeaderMutation
	RequestBody      RequestBodyRuntime
	ResponseHeaders  HeaderMutation
	ResponseStatus   map[int]int
	StreamingSetting *StreamingConfig
}

type RequestBodyRuntime struct {
	ModelRewrite map[string]string
	SystemPrompt string
	MaxTokens    *int
	Temperature  *float64
	CustomSet    []CustomBodySet
}

type CustomBodySet struct {
	Path  []string
	Value any
}

type SnapshotStore struct {
	value atomic.Pointer[RuntimeSnapshot]
}

func NewSnapshotStore(snapshot *RuntimeSnapshot) *SnapshotStore {
	store := &SnapshotStore{}
	if snapshot != nil {
		store.Store(snapshot)
	}
	return store
}

func (s *SnapshotStore) Load() *RuntimeSnapshot {
	return s.value.Load()
}

func (s *SnapshotStore) Store(snapshot *RuntimeSnapshot) {
	s.value.Store(snapshot)
}

func BuildSnapshot(cfg *Config) (*RuntimeSnapshot, error) {
	configHash, err := StableHash(cfg)
	if err != nil {
		return nil, err
	}
	snapshot := &RuntimeSnapshot{
		ConfigHash: configHash,
		Server:     cfg.Server,
		Security:   cfg.Security,
		Runtime:    cfg.Runtime,
		Log:        cfg.Log,
		Privacy:    clonePrivacy(cfg.Privacy),
		Channels:   make(map[string]*ChannelRuntime, len(cfg.Channels)),
	}
	for channelName, channelConfig := range cfg.Channels {
		channelPolicy, err := compilePolicy(channelConfig.ChannelPolicy)
		if err != nil {
			return nil, err
		}
		channelRuntime := &ChannelRuntime{
			Name:                    channelName,
			Enabled:                 channelConfig.Enabled,
			LLMPrivateProtectEnable: channelConfig.LLMPrivateProtectEnable,
			Upstream:                channelConfig.Upstream,
			ChannelPolicy:           channelPolicy,
			ModelRules:              make(map[string]*ModelRuleRuntime, len(channelConfig.ModelRules)),
		}
		// Exact model-rule lookup is compiled once at snapshot build time; disabled
		// rules stay indexed so planner can intentionally treat them as no match.
		for _, modelRuleConfig := range channelConfig.ModelRules {
			modelRulePolicy, err := compilePolicy(modelRuleConfig.Policy)
			if err != nil {
				return nil, err
			}
			channelRuntime.ModelRules[modelRuleConfig.Model] = &ModelRuleRuntime{
				Model:                   modelRuleConfig.Model,
				Enabled:                 modelRuleConfig.Enabled,
				LLMPrivateProtectEnable: modelRuleConfig.LLMPrivateProtectEnable,
				Policy:                  modelRulePolicy,
			}
		}
		snapshot.Channels[channelName] = channelRuntime
	}
	return snapshot, nil
}

func compilePolicy(policyConfig PolicyConfig) (PolicyRuntime, error) {
	compiledProxies, err := compileProxies(policyConfig.Proxies)
	if err != nil {
		return PolicyRuntime{}, err
	}
	return PolicyRuntime{
		Timeout:          int64(policyConfig.Timeout),
		Proxies:          compiledProxies,
		RequestHeaders:   cloneHeaderMutation(policyConfig.RequestHeaders),
		RequestBody:      compileRequestBody(policyConfig.RequestBody),
		ResponseHeaders:  cloneHeaderMutation(policyConfig.ResponseHeaders),
		ResponseStatus:   cloneIntMap(policyConfig.ResponseStatus),
		StreamingSetting: cloneStreaming(policyConfig.StreamingSetting),
	}, nil
}

func compileProxies(proxyURLs []string) ([]ProxyConfig, error) {
	if len(proxyURLs) == 0 {
		return nil, nil
	}
	compiled := make([]ProxyConfig, 0, len(proxyURLs))
	for _, rawProxyURL := range proxyURLs {
		proxyURL, err := parseProxyURL(rawProxyURL)
		if err != nil {
			return nil, err
		}
		compiled = append(compiled, ProxyConfig{Type: proxyURL.Scheme, Address: proxyURL.String()})
	}
	return compiled, nil
}

func compileRequestBody(requestBodyConfig RequestBodyConfig) RequestBodyRuntime {
	requestBodyRuntime := RequestBodyRuntime{
		ModelRewrite: cloneStringMap(requestBodyConfig.ModelRewrite),
		SystemPrompt: requestBodyConfig.SystemPrompt,
		MaxTokens:    cloneIntPtr(requestBodyConfig.MaxTokens),
		Temperature:  cloneFloatPtr(requestBodyConfig.Temperature),
	}
	for _, customPath := range sortedCustomPaths(requestBodyConfig.Custom) {
		requestBodyRuntime.CustomSet = append(requestBodyRuntime.CustomSet, CustomBodySet{Path: splitPath(customPath), Value: requestBodyConfig.Custom[customPath]})
	}
	return requestBodyRuntime
}

func splitPath(path string) []string {
	var parts []string
	segmentStart := 0
	for index := 0; index <= len(path); index++ {
		if index == len(path) || path[index] == '/' {
			parts = append(parts, path[segmentStart:index])
			segmentStart = index + 1
		}
	}
	return parts
}

func clonePrivacy(in *LLMPrivateProtectConfig) *LLMPrivateProtectConfig {
	if in == nil {
		return nil
	}
	value := *in
	return &value
}

func cloneProxy(in *ProxyConfig) *ProxyConfig {
	if in == nil {
		return nil
	}
	value := *in
	return &value
}

func cloneProxyList(in []ProxyConfig) []ProxyConfig {
	if len(in) == 0 {
		return nil
	}
	out := make([]ProxyConfig, len(in))
	copy(out, in)
	return out
}

func cloneStreaming(in *StreamingConfig) *StreamingConfig {
	if in == nil {
		return nil
	}
	value := *in
	return &value
}

func cloneIntPtr(in *int) *int {
	if in == nil {
		return nil
	}
	value := *in
	return &value
}

func cloneFloatPtr(in *float64) *float64 {
	if in == nil {
		return nil
	}
	value := *in
	return &value
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneIntMap(in map[int]int) map[int]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[int]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneHeaderMutation(in HeaderMutation) HeaderMutation {
	return HeaderMutation{Set: cloneStringMap(in.Set), Delete: dedupeStrings(in.Delete)}
}

func dedupeStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, value := range in {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
