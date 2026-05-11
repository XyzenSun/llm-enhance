package planner

import (
	"net/http/httptest"
	"testing"
	"time"

	"ai-api-stronger/internal/config"
)

func TestBuildExecutionPlanExactRuleNoMerge(t *testing.T) {
	yaml := []byte(`
security: {access_key: test-access-value}
runtime: {default_timeout: 120s}
llm_private_protect_config:
  model: trusted
  base_url: https://trusted.example
  api_key: test-private-credential
channels:
  openai:
    enabled: true
    llm_private_protect_enable: true
    upstream: {base_url: https://api.openai.com/base, format: openai}
    channel_policy:
      timeout: 60s
      request_headers:
        set: {X-Channel: yes}
      request_body:
        max_tokens: 1
    model_rules:
      - model: gpt-5
        enabled: true
        llm_private_protect_enable: false
        policy:
          request_headers:
            set: {X-Rule: yes}
          streaming_setting:
            mode: nonstream_to_stream
`)
	cfg, err := config.LoadBytes(yaml)
	if err != nil {
		t.Fatal(err)
	}
	s, err := config.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/test-access-value/proxy/openai/v1/chat?x=1", nil)
	plan, err := BuildExecutionPlan(s, RouteInfo{Channel: "openai", TailPath: "/v1/chat", RawQuery: "x=1"}, r, []byte(`{"model":"gpt-5"}`))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Match.PolicySource != PolicySourceModelRule || !plan.Match.MatchedModelRule {
		t.Fatalf("expected model_rule source")
	}
	if plan.RequestHeaderPlan.Set["X-Rule"] != "yes" || plan.RequestHeaderPlan.Set["X-Channel"] != "" {
		t.Fatalf("policy was merged or not selected")
	}
	if plan.RequestBodyPlan.MaxTokens != nil {
		t.Fatalf("channel request body leaked into model rule plan")
	}
	if plan.LLMPrivateProtect {
		t.Fatalf("privacy should follow model rule source")
	}
	if plan.Timeout != 120*time.Second {
		t.Fatalf("default timeout not applied, got %v", plan.Timeout)
	}
	if plan.Streaming.MaxBufferBytes != 0 || plan.Streaming.Timeout != 120*time.Second {
		t.Fatalf("streaming defaults not applied")
	}
	if plan.UpstreamURL != "https://api.openai.com/base/v1/chat?x=1" {
		t.Fatalf("bad upstream URL: %s", plan.UpstreamURL)
	}
}

func TestDisabledRuleFallsBackToChannel(t *testing.T) {
	yaml := []byte(`
security: {access_key: test-access-value}
channels:
  openai:
    enabled: true
    upstream: {base_url: https://api.openai.com, format: openai}
    channel_policy:
      request_headers:
        set: {X-Channel: yes}
    model_rules:
      - model: gpt-5
        enabled: false
        policy:
          request_headers:
            set: {X-Rule: yes}
`)
	cfg, _ := config.LoadBytes(yaml)
	s, _ := config.BuildSnapshot(cfg)
	r := httptest.NewRequest("POST", "/", nil)
	plan, err := BuildExecutionPlan(s, RouteInfo{Channel: "openai", TailPath: "v1/chat"}, r, []byte(`{"model":"gpt-5"}`))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Match.PolicySource != PolicySourceChannel || plan.RequestHeaderPlan.Set["X-Channel"] != "yes" {
		t.Fatalf("disabled rule did not fall back")
	}
}

func TestExtractGeminiModel(t *testing.T) {
	got, err := ExtractModel(config.FormatGemini, "/v1beta/models/gemini-2.5-pro:generateContent", nil)
	if err != nil || got != "gemini-2.5-pro" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestBuildExecutionPlanSelectsConfiguredProxyCandidate(t *testing.T) {
	originalChooser := chooseProxyIndex
	chooseProxyIndex = func(proxyCount int) int {
		if proxyCount != 2 {
			t.Fatalf("proxyCount = %d, want 2", proxyCount)
		}
		return 1
	}
	defer func() { chooseProxyIndex = originalChooser }()

	yaml := []byte(`
security: {access_key: test-access-value}
runtime: {default_timeout: 120s}
channels:
  openai:
    enabled: true
    upstream: {base_url: https://api.openai.com, format: openai}
    channel_policy:
      proxies:
        - http://127.0.0.1:18080
        - socks5://127.0.0.1:11080
`)
	cfg, err := config.LoadBytes(yaml)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := config.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "/test-access-value/proxy/openai/v1/chat/completions", nil)
	plan, err := BuildExecutionPlan(snapshot, RouteInfo{Channel: "openai", TailPath: "v1/chat/completions"}, request, []byte(`{"model":"gpt-5"}`))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Proxy == nil {
		t.Fatalf("expected a concrete proxy to be selected")
	}
	if plan.Proxy.Type != "socks5" || plan.Proxy.Address != "socks5://127.0.0.1:11080" {
		t.Fatalf("selected proxy = %#v", plan.Proxy)
	}
}
