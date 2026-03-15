package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	qerrors "github.com/ichinya/quiverkeep-core/internal/errors"
	"github.com/ichinya/quiverkeep-core/internal/logging"
)

const (
	defaultConfigFile = "config.json"
	legacyConfigDir   = ".quiverkeep"
)

type CoreConfig struct {
	URL       string  `json:"url"`
	AutoStart bool    `json:"auto_start"`
	Token     *string `json:"token"`
	Bind      string  `json:"bind"`
	Port      int     `json:"port"`
}

type ProviderEntry struct {
	Key   string `json:"key,omitempty"`
	Token string `json:"token,omitempty"`
}

type ProvidersConfig struct {
	OpenAI    ProviderEntry `json:"openai"`
	Anthropic ProviderEntry `json:"anthropic"`
	Copilot   ProviderEntry `json:"copilot"`
}

type ProxyAnthropicConfig struct {
	BaseURL        string `json:"base_url"`
	Version        string `json:"version"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

type ProxyConfig struct {
	Enabled   bool                 `json:"enabled"`
	Anthropic ProxyAnthropicConfig `json:"anthropic"`
}

type StorageConfig struct {
	Path string `json:"path"`
}

type LoggingConfig struct {
	Level string `json:"level"`
	Path  string `json:"path"`
}

type Config struct {
	Core      CoreConfig      `json:"core"`
	Providers ProvidersConfig `json:"providers"`
	Proxy     ProxyConfig     `json:"proxy"`
	Storage   StorageConfig   `json:"storage"`
	Logging   LoggingConfig   `json:"logging"`
}

type Metadata struct {
	Path           string
	ConfigDir      string
	UsedLegacyPath bool
	CreatedDefault bool
}

type LoadOptions struct {
	ConfigPath string
	URL        string
	Bind       string
	Port       int
	Token      string
	LogLevel   string
}

func Default() Config {
	return Config{
		Core: CoreConfig{
			URL:       "http://127.0.0.1:8765",
			AutoStart: true,
			Token:     nil,
			Bind:      "127.0.0.1",
			Port:      8765,
		},
		Providers: ProvidersConfig{},
		Proxy: ProxyConfig{
			Enabled: false,
			Anthropic: ProxyAnthropicConfig{
				BaseURL:        "https://api.anthropic.com",
				Version:        "2023-06-01",
				TimeoutSeconds: 30,
			},
		},
		Storage: StorageConfig{
			Path: "",
		},
		Logging: LoggingConfig{
			Level: "info",
			Path:  "",
		},
	}
}

func (c Config) Sanitized() Config {
	safe := c
	safe.Core.Token = sanitizeNullableToken(c.Core.Token)
	safe.Providers.OpenAI.Key = sanitize(c.Providers.OpenAI.Key)
	safe.Providers.Anthropic.Key = sanitize(c.Providers.Anthropic.Key)
	safe.Providers.Copilot.Token = sanitize(c.Providers.Copilot.Token)
	return safe
}

func (c Config) HasToken() bool {
	return c.Core.Token != nil && strings.TrimSpace(*c.Core.Token) != ""
}

func (c Config) EffectiveURL() string {
	if strings.TrimSpace(c.Core.URL) != "" {
		return strings.TrimSpace(c.Core.URL)
	}
	return fmt.Sprintf("http://%s:%d", c.Core.Bind, c.Core.Port)
}

func Load(opts LoadOptions, logger *logging.Logger) (Config, Metadata, error) {
	cfg := Default()

	primaryPath, legacyPath, err := resolveConfigPaths(opts.ConfigPath)
	if err != nil {
		return Config{}, Metadata{}, qerrors.Wrap(qerrors.CodeConfigPermission, "failed to resolve config paths", err)
	}

	targetPath := primaryPath
	usedLegacy := false

	switch {
	case fileExists(primaryPath):
	case fileExists(legacyPath):
		targetPath = legacyPath
		usedLegacy = true
	}

	meta := Metadata{
		Path:           targetPath,
		ConfigDir:      filepath.Dir(targetPath),
		UsedLegacyPath: usedLegacy,
		CreatedDefault: false,
	}

	if logger != nil {
		logger.Debug("config path resolved",
			"primary_path", primaryPath,
			"legacy_path", legacyPath,
			"selected_path", targetPath,
			"used_legacy_path", usedLegacy,
		)
	}

	if !fileExists(targetPath) {
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
			return Config{}, meta, qerrors.Wrap(qerrors.CodeConfigPermission, "failed to create config dir", err)
		}

		if err := writeConfigFile(targetPath, cfg); err != nil {
			return Config{}, meta, qerrors.Wrap(qerrors.CodeConfigPermission, "failed to create default config", err)
		}

		meta.CreatedDefault = true
		if logger != nil {
			logger.Info("default config created", "path", targetPath)
		}
	} else {
		loaded, err := readConfigFile(targetPath)
		if err != nil {
			return Config{}, meta, err
		}
		cfg = mergeConfig(cfg, loaded)
	}

	cfg = applyEnv(cfg, logger)
	cfg = applyFlags(cfg, opts, logger)

	if logger != nil {
		logger.Debug("config loaded", "config", cfg.Sanitized())
	}

	return cfg, meta, nil
}

func resolveConfigPaths(explicitPath string) (string, string, error) {
	if strings.TrimSpace(explicitPath) != "" {
		clean := filepath.Clean(explicitPath)
		return clean, clean, nil
	}

	base, err := os.UserConfigDir()
	if err != nil {
		return "", "", err
	}

	primary := filepath.Join(base, "quiverkeep", defaultConfigFile)
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	legacy := filepath.Join(home, legacyConfigDir, defaultConfigFile)

	return primary, legacy, nil
}

func readConfigFile(path string) (Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return Config{}, qerrors.Wrap(qerrors.CodeConfigPermission, "permission denied when reading config", err)
		}
		return Config{}, qerrors.Wrap(qerrors.CodeConfigParse, "failed to read config", err)
	}

	var cfg Config
	if err := json.Unmarshal(content, &cfg); err != nil {
		return Config{}, qerrors.Wrap(qerrors.CodeConfigParse, "invalid config json", err)
	}

	return cfg, nil
}

func writeConfigFile(path string, cfg Config) error {
	payload, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return err
	}

	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}

	return nil
}

func mergeConfig(base Config, loaded Config) Config {
	if strings.TrimSpace(loaded.Core.URL) != "" {
		base.Core.URL = loaded.Core.URL
	}
	if loaded.Core.Token != nil {
		base.Core.Token = loaded.Core.Token
	}
	if strings.TrimSpace(loaded.Core.Bind) != "" {
		base.Core.Bind = loaded.Core.Bind
	}
	if loaded.Core.Port > 0 {
		base.Core.Port = loaded.Core.Port
	}
	base.Core.AutoStart = loaded.Core.AutoStart || base.Core.AutoStart

	base.Providers = loaded.Providers
	base.Proxy.Enabled = loaded.Proxy.Enabled

	if strings.TrimSpace(loaded.Proxy.Anthropic.BaseURL) != "" {
		base.Proxy.Anthropic.BaseURL = loaded.Proxy.Anthropic.BaseURL
	}
	if strings.TrimSpace(loaded.Proxy.Anthropic.Version) != "" {
		base.Proxy.Anthropic.Version = loaded.Proxy.Anthropic.Version
	}
	if loaded.Proxy.Anthropic.TimeoutSeconds > 0 {
		base.Proxy.Anthropic.TimeoutSeconds = loaded.Proxy.Anthropic.TimeoutSeconds
	}

	if strings.TrimSpace(loaded.Storage.Path) != "" {
		base.Storage.Path = loaded.Storage.Path
	}
	if strings.TrimSpace(loaded.Logging.Level) != "" {
		base.Logging.Level = loaded.Logging.Level
	}
	if strings.TrimSpace(loaded.Logging.Path) != "" {
		base.Logging.Path = loaded.Logging.Path
	}

	return base
}

func applyEnv(cfg Config, logger *logging.Logger) Config {
	overrideString(&cfg.Core.URL, "QUIVERKEEP_URL", logger)
	overrideString(&cfg.Core.Bind, "QUIVERKEEP_BIND", logger)
	overrideInt(&cfg.Core.Port, "QUIVERKEEP_PORT", logger)
	overrideNullableToken(&cfg.Core.Token, "QUIVERKEEP_TOKEN", logger)

	overrideString(&cfg.Logging.Level, "QUIVERKEEP_LOG_LEVEL", logger)
	overrideString(&cfg.Logging.Path, "QUIVERKEEP_LOG_PATH", logger)
	overrideString(&cfg.Storage.Path, "QUIVERKEEP_STORAGE_PATH", logger)

	overrideString(&cfg.Providers.OpenAI.Key, "OPENAI_API_KEY", logger)
	overrideString(&cfg.Providers.Anthropic.Key, "ANTHROPIC_API_KEY", logger)
	overrideString(&cfg.Providers.Copilot.Token, "GITHUB_TOKEN", logger)
	overrideBool(&cfg.Proxy.Enabled, "QUIVERKEEP_PROXY_ENABLED", logger)
	overrideString(&cfg.Proxy.Anthropic.BaseURL, "QUIVERKEEP_PROXY_ANTHROPIC_BASE_URL", logger)
	overrideString(&cfg.Proxy.Anthropic.Version, "QUIVERKEEP_PROXY_ANTHROPIC_VERSION", logger)
	overrideInt(&cfg.Proxy.Anthropic.TimeoutSeconds, "QUIVERKEEP_PROXY_TIMEOUT_SECONDS", logger)

	return cfg
}

func applyFlags(cfg Config, opts LoadOptions, logger *logging.Logger) Config {
	if strings.TrimSpace(opts.URL) != "" {
		cfg.Core.URL = strings.TrimSpace(opts.URL)
		if logger != nil {
			logger.Debug("config override from flags", "field", "core.url")
		}
	}
	if strings.TrimSpace(opts.Bind) != "" {
		cfg.Core.Bind = strings.TrimSpace(opts.Bind)
		if logger != nil {
			logger.Debug("config override from flags", "field", "core.bind")
		}
	}
	if opts.Port > 0 {
		cfg.Core.Port = opts.Port
		if logger != nil {
			logger.Debug("config override from flags", "field", "core.port")
		}
	}
	if strings.TrimSpace(opts.Token) != "" {
		token := strings.TrimSpace(opts.Token)
		cfg.Core.Token = &token
		if logger != nil {
			logger.Debug("config override from flags", "field", "core.token")
		}
	}
	if strings.TrimSpace(opts.LogLevel) != "" {
		cfg.Logging.Level = strings.TrimSpace(opts.LogLevel)
		if logger != nil {
			logger.Debug("config override from flags", "field", "logging.level")
		}
	}
	return cfg
}

func ResolveDataDir(meta Metadata) string {
	if strings.TrimSpace(meta.ConfigDir) != "" {
		return meta.ConfigDir
	}

	base, err := os.UserConfigDir()
	if err != nil {
		return "."
	}
	return filepath.Join(base, "quiverkeep")
}

func sanitize(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	return "***"
}

func sanitizeNullableToken(raw *string) *string {
	if raw == nil {
		return nil
	}
	masked := sanitize(*raw)
	return &masked
}

func overrideString(target *string, key string, logger *logging.Logger) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return
	}
	*target = raw
	if logger != nil {
		logger.Debug("config override from env", "field", strings.ToLower(key))
	}
}

func overrideInt(target *int, key string, logger *logging.Logger) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		if logger != nil {
			logger.Warn("invalid int env value ignored", "field", strings.ToLower(key), "value", raw)
		}
		return
	}
	*target = parsed
	if logger != nil {
		logger.Debug("config override from env", "field", strings.ToLower(key), "value", parsed)
	}
}

func overrideBool(target *bool, key string, logger *logging.Logger) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return
	}

	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		if logger != nil {
			logger.Warn("invalid bool env value ignored", "field", strings.ToLower(key), "value", raw)
		}
		return
	}

	*target = parsed
	if logger != nil {
		logger.Debug("config override from env", "field", strings.ToLower(key), "value", parsed)
	}
}

func overrideNullableToken(target **string, key string, logger *logging.Logger) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return
	}
	token := raw
	*target = &token
	if logger != nil {
		logger.Debug("config override from env", "field", strings.ToLower(key))
	}
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
