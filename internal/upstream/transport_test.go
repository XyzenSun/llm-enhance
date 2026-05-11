package upstream

import (
	"net/http"
	"testing"
)

func TestNewTransportDisablesEnvironmentProxyWhenNoConfiguredProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9999")
	transport, ok := NewTransport(ClientKey{}).(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", NewTransport(ClientKey{}))
	}
	if transport.Proxy != nil {
		t.Fatalf("expected environment proxy inheritance to be disabled")
	}
}

func TestNewTransportUsesConfiguredHTTPAndSOCKS5Proxy(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		proxyURL string
	}{
		{name: "http", proxyURL: "http://user:pass@127.0.0.1:18080"},
		{name: "socks5", proxyURL: "socks5://user:pass@127.0.0.1:11080"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			transport, ok := NewTransport(ClientKey{ProxyAddress: testCase.proxyURL}).(*http.Transport)
			if !ok {
				t.Fatalf("transport type = %T, want *http.Transport", NewTransport(ClientKey{ProxyAddress: testCase.proxyURL}))
			}
			if transport.Proxy == nil {
				t.Fatalf("expected configured proxy function")
			}
			request, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
			if err != nil {
				t.Fatal(err)
			}
			proxyURL, err := transport.Proxy(request)
			if err != nil {
				t.Fatalf("transport.Proxy returned error: %v", err)
			}
			if proxyURL.String() != testCase.proxyURL {
				t.Fatalf("proxy URL = %q, want %q", proxyURL.String(), testCase.proxyURL)
			}
		})
	}
}
