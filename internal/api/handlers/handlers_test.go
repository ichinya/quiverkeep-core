package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ichinya/quiverkeep-core/internal/config"
	"github.com/ichinya/quiverkeep-core/internal/domain"
	"github.com/ichinya/quiverkeep-core/internal/logging"
	"github.com/ichinya/quiverkeep-core/internal/storage"
)

func TestUsageAndLimitsEndpoints(t *testing.T) {
	t.Parallel()

	api, cleanup := newTestAPI(t)
	defer cleanup()

	mux := http.NewServeMux()
	api.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage?service=openai", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status for /usage: %d", rec.Code)
	}

	var usageResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &usageResp); err != nil {
		t.Fatalf("invalid usage response json: %v", err)
	}
	if _, ok := usageResp["items"]; !ok {
		t.Fatalf("usage response missing items")
	}
	if _, ok := usageResp["total"]; !ok {
		t.Fatalf("usage response missing total")
	}

	limitsReq := httptest.NewRequest(http.MethodGet, "/api/v1/limits", nil)
	limitsRec := httptest.NewRecorder()
	mux.ServeHTTP(limitsRec, limitsReq)
	if limitsRec.Code != http.StatusOK {
		t.Fatalf("unexpected status for /limits: %d", limitsRec.Code)
	}
}

func TestProvidersEndpoint(t *testing.T) {
	t.Parallel()

	api, cleanup := newTestAPI(t)
	defer cleanup()

	mux := http.NewServeMux()
	api.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/providers", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status for /providers: %d", rec.Code)
	}
}

func newTestAPI(t *testing.T) (*API, func()) {
	t.Helper()

	tempDir := t.TempDir()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(tempDir, "core.db")
	cfg.Providers.OpenAI.Key = "test-openai"
	meta := config.Metadata{
		ConfigDir: tempDir,
		Path:      filepath.Join(tempDir, "config.json"),
	}

	logger, err := logging.New(logging.Config{Level: "debug"})
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}

	store, err := storage.New(cfg, meta, logger)
	if err != nil {
		t.Fatalf("store init failed: %v", err)
	}

	ctx := context.Background()
	if err := store.InsertUsage(ctx, domain.UsageRecord{
		Service:   "openai",
		Model:     "gpt-5",
		TokensIn:  10,
		TokensOut: 5,
		Cost:      0.12,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("insert usage failed: %v", err)
	}

	limit := int64(100)
	if err := store.UpsertSubscription(ctx, domain.Subscription{
		Service:    "openai",
		Plan:       "pro",
		LimitValue: &limit,
		Used:       10,
	}); err != nil {
		t.Fatalf("upsert subscription failed: %v", err)
	}

	api := New(store, cfg, logger)
	return api, func() {
		_ = store.Close()
	}
}
