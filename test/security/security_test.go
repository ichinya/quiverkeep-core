package security

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ichinya/quiverkeep-core/internal/api/handlers"
	"github.com/ichinya/quiverkeep-core/internal/api/middleware"
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
	t.Cleanup(func() {
		_ = logger.Close()
	})

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

func TestProxyEndpointRequiresTokenInRemoteMode(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	logger, err := logging.New(logging.Config{Level: "debug"})
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}
	t.Cleanup(func() {
		_ = logger.Close()
	})

	cfg := config.Default()
	cfg.Core.Bind = "0.0.0.0"
	cfg.Proxy.Enabled = true
	cfg.Providers.Anthropic.Key = "anthropic-secret"
	cfg.Storage.Path = filepath.Join(tempDir, "security.db")
	meta := config.Metadata{
		ConfigDir: tempDir,
		Path:      filepath.Join(tempDir, "config.json"),
	}

	store, err := storage.New(cfg, meta, logger)
	if err != nil {
		t.Fatalf("store init failed: %v", err)
	}
	defer store.Close()

	api := handlers.New(store, cfg, logger)
	mux := http.NewServeMux()
	api.Register(mux)
	handler := middleware.Auth(cfg, logger, true)(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy/anthropic/messages", strings.NewReader(`{"model":"claude"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for remote proxy request without token, got %d", rec.Code)
	}
}

func TestProxyEndpointRequiresTokenInLoopbackMode(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "security.log")
	logger, err := logging.New(logging.Config{Level: "debug", Path: logPath})
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}
	t.Cleanup(func() {
		_ = logger.Close()
	})

	cfg := config.Default()
	cfg.Proxy.Enabled = true
	cfg.Providers.Anthropic.Key = "anthropic-secret"
	cfg.Storage.Path = filepath.Join(tempDir, "security.db")
	meta := config.Metadata{
		ConfigDir: tempDir,
		Path:      filepath.Join(tempDir, "config.json"),
	}

	store, err := storage.New(cfg, meta, logger)
	if err != nil {
		t.Fatalf("store init failed: %v", err)
	}
	defer store.Close()

	api := handlers.New(store, cfg, logger)
	mux := http.NewServeMux()
	api.Register(mux)
	handler := middleware.Auth(cfg, logger, false)(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy/anthropic/messages", strings.NewReader(`{"model":"claude"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for loopback proxy request without token, got %d", rec.Code)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed reading log file: %v", err)
	}
	logText := string(logBytes)
	if !strings.Contains(logText, `"operation":"auth"`) {
		t.Fatalf("expected auth operation log for proxy auth rejection")
	}
	if !strings.Contains(logText, `"reason":"proxy_spend"`) {
		t.Fatalf("expected proxy_spend rejection reason in auth log")
	}
	if strings.Contains(logText, "anthropic-secret") {
		t.Fatalf("auth logs leaked provider secret")
	}
}

func TestProxyStatusDoesNotExposeProviderSecret(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	logger, err := logging.New(logging.Config{Level: "debug"})
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}
	t.Cleanup(func() {
		_ = logger.Close()
	})

	cfg := config.Default()
	cfg.Proxy.Enabled = true
	cfg.Providers.Anthropic.Key = "anthropic-secret-value"
	cfg.Storage.Path = filepath.Join(tempDir, "security.db")
	meta := config.Metadata{
		ConfigDir: tempDir,
		Path:      filepath.Join(tempDir, "config.json"),
	}

	store, err := storage.New(cfg, meta, logger)
	if err != nil {
		t.Fatalf("store init failed: %v", err)
	}
	defer store.Close()

	api := handlers.New(store, cfg, logger)
	mux := http.NewServeMux()
	api.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/proxy/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "anthropic-secret-value") {
		t.Fatalf("proxy status leaked provider secret")
	}
}
