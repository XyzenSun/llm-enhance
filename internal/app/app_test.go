package app

import (
	"path/filepath"
	"testing"
)

func TestConfigtestYAMLBuildsRunnableApp(t *testing.T) {
	t.Setenv("REAL_UPSTREAM_BASE_URL", "https://example.com")
	t.Setenv("REAL_UPSTREAM_API_KEY", "test-key")
	t.Setenv("REAL_UPSTREAM_MODEL", "test-model")
	configPath := filepath.Clean(filepath.Join("..", "..", "configtest.yaml"))
	a, err := New(configPath)
	if err != nil {
		t.Fatalf("New(%q): %v", configPath, err)
	}
	defer func() { _ = a.Logger.Close() }()

	if a.Server == nil {
		t.Fatalf("server was not initialized")
	}
	if a.Server.Addr != "127.0.0.1:28080" {
		t.Fatalf("server addr = %q, want 127.0.0.1:28080", a.Server.Addr)
	}

	snapshot := a.Snapshot.Load()
	if snapshot == nil {
		t.Fatalf("runtime snapshot was not initialized")
	}
	if snapshot.Security.AccessKey != "real-upstream-access" {
		t.Fatalf("unexpected access key in snapshot")
	}
	ch := snapshot.Channels["realopenai"]
	if ch == nil {
		t.Fatalf("realopenai channel was not loaded")
	}
	if !ch.Enabled {
		t.Fatalf("realopenai channel is disabled")
	}
	if ch.Upstream.Format != "openai" {
		t.Fatalf("realopenai format = %q, want openai", ch.Upstream.Format)
	}
	if ch.Upstream.BaseURL != "https://example.com" {
		t.Fatalf("realopenai base_url = %q, want env-expanded URL", ch.Upstream.BaseURL)
	}
	if a.Privacy == nil {
		t.Fatalf("privacy processor should be configured when configtest enables privacy")
	}
}
