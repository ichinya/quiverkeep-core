package security

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ichinya/quiverkeep-core/internal/config"
	"github.com/ichinya/quiverkeep-core/internal/domain"
	"github.com/ichinya/quiverkeep-core/internal/logging"
	"github.com/ichinya/quiverkeep-core/internal/storage"
)

func TestConfigMasking(t *testing.T) {
	t.Parallel()

	token := "secret-token"
	cfg := config.Config{
		Core: config.CoreConfig{
			Token: &token,
		},
		Providers: config.ProvidersConfig{
			OpenAI: config.ProviderEntry{Key: "openai-key"},
		},
	}

	safe := cfg.Sanitized()
	if safe.Core.Token == nil || *safe.Core.Token != "***" {
		t.Fatalf("expected token to be masked")
	}
	if safe.Providers.OpenAI.Key != "***" {
		t.Fatalf("expected provider key to be masked")
	}
}

func TestFilePermissionsWhenSupported(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		return
	}

	tempDir := t.TempDir()
	logger, err := logging.New(logging.Config{Level: "debug"})
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}

	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(tempDir, "secure.db")
	meta := config.Metadata{
		ConfigDir: tempDir,
		Path:      filepath.Join(tempDir, "config.json"),
	}

	loaded, _, err := config.Load(config.LoadOptions{ConfigPath: meta.Path}, logger)
	if err != nil {
		t.Fatalf("config load failed: %v", err)
	}
	cfg = loaded
	cfg.Storage.Path = filepath.Join(tempDir, "secure.db")

	store, err := storage.New(cfg, meta, logger)
	if err != nil {
		t.Fatalf("store init failed: %v", err)
	}
	defer store.Close()

	if err := store.InsertUsage(t.Context(), domain.UsageRecord{
		Service:   "openai",
		Model:     "gpt-5",
		TokensIn:  1,
		TokensOut: 1,
		Cost:      0.01,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("insert usage failed: %v", err)
	}

	configInfo, err := os.Stat(meta.Path)
	if err != nil {
		t.Fatalf("config stat failed: %v", err)
	}
	if configInfo.Mode().Perm() != 0o600 {
		t.Fatalf("expected config permissions 0600, got %o", configInfo.Mode().Perm())
	}

	dbInfo, err := os.Stat(cfg.Storage.Path)
	if err != nil {
		t.Fatalf("db stat failed: %v", err)
	}
	if dbInfo.Mode().Perm() != 0o600 {
		t.Fatalf("expected db permissions 0600, got %o", dbInfo.Mode().Perm())
	}
}
