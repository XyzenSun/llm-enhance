package upstream

import (
	"fmt"
	"net/http"
	"net/url"
)

func NewTransport(key ClientKey) http.RoundTripper {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// YAML is the only runtime configuration source, so request execution must
	// not inherit environment proxy settings behind the snapshot/planner boundary.
	transport.Proxy = nil
	if key.ProxyAddress == "" {
		return transport
	}
	proxyURL, err := url.Parse(key.ProxyAddress)
	if err != nil {
		return proxyConfigErrorTransport{err: fmt.Errorf("parse configured proxy: %w", err)}
	}
	transport.Proxy = http.ProxyURL(proxyURL)
	return transport
}

type proxyConfigErrorTransport struct {
	err error
}

func (t proxyConfigErrorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, t.err
}
