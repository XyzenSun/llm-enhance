package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func LoadFile(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LoadBytesWithEnv(b)
}

func LoadBytes(b []byte) (*Config, error) {
	return loadBytes(b, false)
}

func LoadBytesWithEnv(b []byte) (*Config, error) {
	return loadBytes(b, true)
}

func loadBytes(b []byte, expandEnv bool) (*Config, error) {
	if expandEnv {
		b = []byte(expandConfigEnv(string(b)))
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return nil, err
	}
	FillDefaults(&cfg)
	if err := Validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func expandConfigEnv(input string) string {
	var builder strings.Builder
	for i := 0; i < len(input); {
		if input[i] != '$' || i+1 >= len(input) {
			builder.WriteByte(input[i])
			i++
			continue
		}

		if input[i+1] == '{' {
			end := strings.IndexByte(input[i+2:], '}')
			if end < 0 {
				builder.WriteByte(input[i])
				i++
				continue
			}
			name := input[i+2 : i+2+end]
			if isRuntimeVariableName(name) {
				builder.WriteString("${" + name + "}")
			} else {
				builder.WriteString(os.Getenv(name))
			}
			i += end + 3
			continue
		}

		nameStart := i + 1
		nameEnd := nameStart
		for nameEnd < len(input) && isEnvNameChar(input[nameEnd]) {
			nameEnd++
		}
		if nameEnd == nameStart {
			builder.WriteByte(input[i])
			i++
			continue
		}
		builder.WriteString(os.Getenv(input[nameStart:nameEnd]))
		i = nameEnd
	}
	return builder.String()
}

func isRuntimeVariableName(name string) bool {
	switch name {
	case "ORIGIN", "UUID", "TIMESTAMP", "uuid":
		return true
	default:
		return false
	}
}

func isEnvNameChar(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}

func LoadEnvFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return LoadEnv(data)
}

func LoadEnv(data []byte) error {
	for _, rawLine := range bytes.Split(data, []byte{'\n'}) {
		line := strings.TrimSpace(string(rawLine))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" || os.Getenv(key) != "" {
			continue
		}
		if err := os.Setenv(key, parseEnvValue(strings.TrimSpace(value))); err != nil {
			return err
		}
	}
	return nil
}

func parseEnvValue(value string) string {
	if len(value) >= 2 {
		quote := value[0]
		if (quote == '\'' || quote == '"') && value[len(value)-1] == quote {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func LoadReload(pathType, path string) (*Config, error) {
	switch pathType {
	case "local":
		return LoadFile(path)
	case "network":
		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Get(path)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return nil, fmt.Errorf("network config returned status %d", resp.StatusCode)
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		if err != nil {
			return nil, err
		}
		return LoadBytes(b)
	default:
		return nil, fmt.Errorf("unsupported path_type")
	}
}

func FillDefaults(cfg *Config) {
	if cfg.Server.ListenAddress == "" {
		cfg.Server.ListenAddress = "127.0.0.1"
	}
	if cfg.Server.ListenPort == 0 {
		cfg.Server.ListenPort = 8080
	}
	if cfg.Server.ReadTimeout == 0 {
		cfg.Server.ReadTimeout = 15 * time.Second
	}
	if cfg.Server.WriteTimeout == 0 {
		cfg.Server.WriteTimeout = 300 * time.Second
	}
	if cfg.Server.IdleTimeout == 0 {
		cfg.Server.IdleTimeout = 60 * time.Second
	}
	if cfg.Server.MaxIdleClientNum == 0 {
		cfg.Server.MaxIdleClientNum = 100
	}
	if cfg.Runtime.DefaultTimeout == 0 {
		cfg.Runtime.DefaultTimeout = 120 * time.Second
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
	if cfg.Log.Dir == "" {
		cfg.Log.Dir = "logs"
	}
}

func StableHash(cfg *Config) (string, error) {
	b, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	return sha256Hex(b), nil
}
