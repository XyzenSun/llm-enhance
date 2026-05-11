package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
)

var channelNameRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func Validate(cfg *Config) error {
	if len(cfg.Security.AccessKey) <= 8 {
		return fmt.Errorf("security.access_key is required and must be longer than 8")
	}
	if cfg.Server.ListenPort < 1 || cfg.Server.ListenPort > 65535 {
		return fmt.Errorf("server.listen_port must be in range 1..65535")
	}
	if cfg.Server.ReadTimeout <= 0 || cfg.Server.WriteTimeout <= 0 || cfg.Server.IdleTimeout <= 0 || cfg.Server.MaxIdleClientNum <= 0 {
		return fmt.Errorf("server timeouts and max_idle_client_num must be positive")
	}
	if cfg.Runtime.DefaultTimeout <= 0 {
		return fmt.Errorf("runtime.default_timeout must be positive")
	}
	if !oneOf(cfg.Log.Level, "debug", "info", "warn", "error") {
		return fmt.Errorf("log.level must be debug, info, warn, or error")
	}
	if cfg.Log.RetentionDays < 0 {
		return fmt.Errorf("log.retention_days must not be negative")
	}
	if len(cfg.Channels) == 0 {
		return fmt.Errorf("channels is required")
	}
	privacyNeeded := false
	for name, ch := range cfg.Channels {
		if err := validateChannel(name, ch); err != nil {
			return err
		}
		if ch.LLMPrivateProtectEnable {
			privacyNeeded = true
		}
		seen := map[string]struct{}{}
		for _, rule := range ch.ModelRules {
			if rule.Model == "" {
				return fmt.Errorf("channels.%s.model_rules.model is required", name)
			}
			if _, ok := seen[rule.Model]; ok {
				return fmt.Errorf("channels.%s has duplicate model rule %q", name, rule.Model)
			}
			seen[rule.Model] = struct{}{}
			if rule.LLMPrivateProtectEnable {
				privacyNeeded = true
			}
			if err := validatePolicy(name, rule.Policy); err != nil {
				return err
			}
		}
		if err := validatePolicy(name, ch.ChannelPolicy); err != nil {
			return err
		}
	}
	if privacyNeeded && (cfg.Privacy == nil || cfg.Privacy.Model == "" || cfg.Privacy.BaseURL == "" || cfg.Privacy.APIKey == "") {
		return fmt.Errorf("llm_private_protect_config model, base_url, and api_key are required when privacy can be enabled")
	}
	return nil
}

func validateChannel(name string, ch ChannelConfig) error {
	if name == "" || !channelNameRE.MatchString(name) {
		return fmt.Errorf("invalid channel name %q", name)
	}
	u, err := url.Parse(ch.Upstream.BaseURL)
	if err != nil || u.Scheme == "" || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("channels.%s.upstream.base_url must be valid http or https URL", name)
	}
	if !oneOf(ch.Upstream.Format, FormatOpenAI, FormatAnthropic, FormatGemini) {
		return fmt.Errorf("channels.%s.upstream.format is unsupported", name)
	}
	return nil
}

func validatePolicy(channel string, p PolicyConfig) error {
	if p.Timeout < 0 {
		return fmt.Errorf("channels.%s.policy.timeout must not be negative", channel)
	}
	for index, rawProxyURL := range p.Proxies {
		if _, err := parseProxyURL(rawProxyURL); err != nil {
			return fmt.Errorf("channels.%s.policy.proxies[%d] is invalid: %w", channel, index, err)
		}
	}
	if err := validateHeaders(channel, p.RequestHeaders); err != nil {
		return err
	}
	if err := validateHeaders(channel, p.ResponseHeaders); err != nil {
		return err
	}
	if p.RequestBody.MaxTokens != nil && *p.RequestBody.MaxTokens <= 0 {
		return fmt.Errorf("channels.%s.request_body.max_tokens must be positive", channel)
	}
	if p.RequestBody.Temperature != nil && *p.RequestBody.Temperature < 0 {
		return fmt.Errorf("channels.%s.request_body.temperature must be non-negative", channel)
	}
	for path := range p.RequestBody.Custom {
		if path == "" || strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") || strings.Contains(path, "//") {
			return fmt.Errorf("channels.%s.request_body.custom path %q is invalid", channel, path)
		}
	}
	for from, to := range p.ResponseStatus {
		if from < 100 || from > 599 || to < 100 || to > 599 {
			return fmt.Errorf("channels.%s.response_status contains invalid HTTP status", channel)
		}
	}
	if p.StreamingSetting != nil {
		if p.StreamingSetting.Mode != StreamingModeNonstreamToStream {
			return fmt.Errorf("channels.%s.streaming_setting.mode is unsupported", channel)
		}
		if p.StreamingSetting.MaxBufferBytes < 0 || p.StreamingSetting.Timeout < 0 {
			return fmt.Errorf("channels.%s.streaming_setting limits must not be negative", channel)
		}
	}
	return nil
}

func validateHeaders(channel string, h HeaderMutation) error {
	for k := range h.Set {
		if strings.TrimSpace(k) == "" {
			return fmt.Errorf("channels.%s header set key is empty", channel)
		}
	}
	for _, k := range h.Delete {
		if strings.TrimSpace(k) == "" {
			return fmt.Errorf("channels.%s header delete key is empty", channel)
		}
	}
	return nil
}

func parseProxyURL(rawProxyURL string) (*url.URL, error) {
	proxyURL, err := url.Parse(strings.TrimSpace(rawProxyURL))
	if err != nil {
		return nil, fmt.Errorf("must be a valid URL")
	}
	if !oneOf(proxyURL.Scheme, "http", "socks5") {
		return nil, fmt.Errorf("scheme must be http or socks5")
	}
	if proxyURL.Host == "" {
		return nil, fmt.Errorf("host is required")
	}
	if proxyURL.RawQuery != "" || proxyURL.Fragment != "" {
		return nil, fmt.Errorf("query and fragment are not supported")
	}
	return proxyURL, nil
}

func oneOf(v string, allowed ...string) bool {
	return slices.Contains(allowed, v)
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func sortedCustomPaths(custom map[string]any) []string {
	paths := make([]string, 0, len(custom))
	for path := range custom {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}
