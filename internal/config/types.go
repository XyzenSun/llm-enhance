package config

import "time"

const (
	FormatOpenAI    = "openai"
	FormatAnthropic = "anthropic"
	FormatGemini    = "gemini"

	StreamingModeNonstreamToStream = "nonstream_to_stream"
)

type Config struct {
	Server   ServerConfig             `yaml:"server" json:"server"`
	Security SecurityConfig           `yaml:"security" json:"security"`
	Runtime  RuntimeConfig            `yaml:"runtime" json:"runtime"`
	Log      LogConfig                `yaml:"log" json:"log"`
	Privacy  *LLMPrivateProtectConfig `yaml:"llm_private_protect_config" json:"llm_private_protect_config,omitempty"`
	Channels map[string]ChannelConfig `yaml:"channels" json:"channels"`
}

type ServerConfig struct {
	ListenAddress    string        `yaml:"listen_address" json:"listen_address"`
	ListenPort       int           `yaml:"listen_port" json:"listen_port"`
	ReadTimeout      time.Duration `yaml:"read_timeout" json:"read_timeout"`
	WriteTimeout     time.Duration `yaml:"write_timeout" json:"write_timeout"`
	IdleTimeout      time.Duration `yaml:"idle_timeout" json:"idle_timeout"`
	MaxIdleClientNum int           `yaml:"max_idle_client_num" json:"max_idle_client_num"`
}

type SecurityConfig struct {
	AccessKey string `yaml:"access_key" json:"access_key"`
}

type RuntimeConfig struct {
	DefaultTimeout time.Duration `yaml:"default_timeout" json:"default_timeout"`
}

type LogConfig struct {
	Enable        bool   `yaml:"enable" json:"enable"`
	Level         string `yaml:"level" json:"level"`
	RetentionDays int    `yaml:"retention_days" json:"retention_days"`
	Output        string `yaml:"output" json:"output"`
	Dir           string `yaml:"dir" json:"dir"`
}

type LLMPrivateProtectConfig struct {
	Model        string  `yaml:"model" json:"model"`
	BaseURL      string  `yaml:"base_url" json:"base_url"`
	APIKey       string  `yaml:"api_key" json:"api_key"`
	SystemPrompt string  `yaml:"system_prompt" json:"system_prompt"`
	Temperature  float64 `yaml:"temperature" json:"temperature"`
	Redis        string  `yaml:"redis" json:"redis"`
}

type ChannelConfig struct {
	Enabled                 bool              `yaml:"enabled" json:"enabled"`
	LLMPrivateProtectEnable bool              `yaml:"llm_private_protect_enable" json:"llm_private_protect_enable"`
	Upstream                UpstreamConfig    `yaml:"upstream" json:"upstream"`
	ChannelPolicy           PolicyConfig      `yaml:"channel_policy" json:"channel_policy"`
	ModelRules              []ModelRuleConfig `yaml:"model_rules" json:"model_rules"`
}

type UpstreamConfig struct {
	BaseURL string `yaml:"base_url" json:"base_url"`
	Format  string `yaml:"format" json:"format"`
}

type ModelRuleConfig struct {
	Model                   string       `yaml:"model" json:"model"`
	Enabled                 bool         `yaml:"enabled" json:"enabled"`
	LLMPrivateProtectEnable bool         `yaml:"llm_private_protect_enable" json:"llm_private_protect_enable"`
	Policy                  PolicyConfig `yaml:"policy" json:"policy"`
}

type PolicyConfig struct {
	Timeout          time.Duration     `yaml:"timeout" json:"timeout"`
	Proxies          []string          `yaml:"proxies" json:"proxies,omitempty"`
	RequestHeaders   HeaderMutation    `yaml:"request_headers" json:"request_headers"`
	RequestBody      RequestBodyConfig `yaml:"request_body" json:"request_body"`
	ResponseHeaders  HeaderMutation    `yaml:"response_headers" json:"response_headers"`
	ResponseStatus   map[int]int       `yaml:"response_status" json:"response_status"`
	StreamingSetting *StreamingConfig  `yaml:"streaming_setting" json:"streaming_setting,omitempty"`
}

type ProxyConfig struct {
	Type    string `json:"type"`
	Address string `json:"address"`
}

type HeaderMutation struct {
	Set    map[string]string `yaml:"set" json:"set"`
	Delete []string          `yaml:"delete" json:"delete"`
}

type RequestBodyConfig struct {
	ModelRewrite map[string]string `yaml:"model_rewrite" json:"model_rewrite"`
	SystemPrompt string            `yaml:"system_prompt" json:"system_prompt"`
	MaxTokens    *int              `yaml:"max_tokens" json:"max_tokens,omitempty"`
	Temperature  *float64          `yaml:"temperature" json:"temperature,omitempty"`
	Custom       map[string]any    `yaml:"custom" json:"custom"`
}

type StreamingConfig struct {
	Mode           string        `yaml:"mode" json:"mode"`
	MaxBufferBytes int64         `yaml:"max_buffer_bytes" json:"max_buffer_bytes"`
	Timeout        time.Duration `yaml:"timeout" json:"timeout"`
}
