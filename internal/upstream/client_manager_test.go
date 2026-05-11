package upstream

import (
	"testing"
	"time"

	"ai-api-stronger/internal/config"
	"ai-api-stronger/internal/planner"
)

func TestKeyFromPlanSeparatesDifferentSelectedProxies(t *testing.T) {
	planA := &planner.ExecutionPlan{
		ChannelName:    "openai",
		UpstreamURL:    "https://api.openai.com/v1/chat/completions",
		UpstreamFormat: "openai",
		Timeout:        30 * time.Second,
		Proxy:          &config.ProxyConfig{Type: "http", Address: "http://127.0.0.1:18080"},
	}
	planB := &planner.ExecutionPlan{
		ChannelName:    "openai",
		UpstreamURL:    "https://api.openai.com/v1/chat/completions",
		UpstreamFormat: "openai",
		Timeout:        30 * time.Second,
		Proxy:          &config.ProxyConfig{Type: "socks5", Address: "socks5://127.0.0.1:11080"},
	}

	keyA := KeyFromPlan(planA)
	keyB := KeyFromPlan(planB)
	if keyA == keyB {
		t.Fatalf("different selected proxies should not share a managed client key")
	}
	if keyA.ProxyAddress != "http://127.0.0.1:18080" || keyB.ProxyAddress != "socks5://127.0.0.1:11080" {
		t.Fatalf("proxy addresses were not preserved in client keys: %+v %+v", keyA, keyB)
	}
}
