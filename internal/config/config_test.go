package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ichinya/quiverkeep-core/internal/logging"
)

func TestLoadCreatesDefaultConfig(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	logger := newTestLogger(t)

	cfg, meta, err := Load(LoadOptions{
		ConfigPath: configPath,
	}, logger)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.Core.URL == "" {
		t.Fatalf("expected default URL to be populated")
	}
	if !meta.CreatedDefault {
		t.Fatalf("expected CreatedDefault=true")
	}
	if meta.Path != configPath {
		t.Fatalf("unexpected config path %s", meta.Path)
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("expected config file to exist: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("expected config file permissions 0600, got %o", info.Mode().Perm())
	}
}

func TestOverridesPrecedenceFlagsOverEnv(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	logger := newTestLogger(t)

	if err := os.Setenv("QUIVERKEEP_URL", "http://127.0.0.1:9000"); err != nil {
		t.Fatalf("setenv failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("QUIVERKEEP_URL")
	})

	cfg, _, err := Load(LoadOptions{
		ConfigPath: configPath,
		URL:        "http://127.0.0.1:9100",
	}, logger)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.Core.URL != "http://127.0.0.1:9100" {
		t.Fatalf("expected flag override to win, got %s", cfg.Core.URL)
	}
}

func TestSanitizedMasksSecrets(t *testing.T) {
	t.Parallel()

	token := "my-token"
	cfg := Config{
		Core: CoreConfig{
			Token: &token,
		},
		Providers: ProvidersConfig{
			OpenAI: ProviderEntry{Key: "openai-secret"},
			Copilot: ProviderEntry{
				Token: "copilot-secret",
			},
		},
	}

	safe := cfg.Sanitized()
	if safe.Providers.OpenAI.Key != "***" {
		t.Fatalf("expected openai key to be masked")
	}
	if safe.Providers.Copilot.Token != "***" {
		t.Fatalf("expected copilot token to be masked")
	}
	if safe.Core.Token == nil || *safe.Core.Token != "***" {
		t.Fatalf("expected core token to be masked")
	}
}

func newTestLogger(t *testing.T) *logging.Logger {
	t.Helper()
	logger, err := logging.New(logging.Config{Level: "debug"})
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}
	return logger
}
