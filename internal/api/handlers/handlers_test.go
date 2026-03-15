package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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

func TestProxyStatusEndpoint(t *testing.T) {
	t.Parallel()

	api, cleanup := newTestAPI(t)
	defer cleanup()

	mux := http.NewServeMux()
	api.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/proxy/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status for /proxy/status: %d", rec.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid proxy status response: %v", err)
	}
	if _, ok := payload["items"]; !ok {
		t.Fatalf("proxy status response missing items")
	}
}

func TestAnthropicProxyForwardAndUsagePersistence(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_1","model":"claude-3-5-sonnet","usage":{"input_tokens":12,"output_tokens":7}}`))
	}))
	defer upstream.Close()

	cfg := config.Default()
	cfg.Proxy.Enabled = true
	cfg.Proxy.Anthropic.BaseURL = upstream.URL
	cfg.Providers.Anthropic.Key = "test-anthropic"

	api, cleanup := newTestAPIWithConfig(t, cfg)
	defer cleanup()

	mux := http.NewServeMux()
	api.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy/anthropic/messages", strings.NewReader(`{"model":"claude-3-5-sonnet"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status for proxy forward: %d body=%s", rec.Code, rec.Body.String())
	}

	items, err := api.store.ListUsage(context.Background(), domain.UsageFilter{Service: "anthropic"})
	if err != nil {
		t.Fatalf("list usage failed: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("expected anthropic usage record to be persisted")
	}
	if items[0].TokensIn != 12 || items[0].TokensOut != 7 {
		t.Fatalf("unexpected persisted usage totals: %+v", items[0])
	}
}

func TestAnthropicProxyReturnsServiceUnavailableWhenDisabled(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Proxy.Enabled = false
	cfg.Providers.Anthropic.Key = "test-anthropic"

	api, cleanup := newTestAPIWithConfig(t, cfg)
	defer cleanup()

	mux := http.NewServeMux()
	api.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy/anthropic/messages", strings.NewReader(`{"model":"claude-3-5-sonnet"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAnthropicProxyMapsUpstreamFailure(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"upstream_error"}}`))
	}))
	defer upstream.Close()

	cfg := config.Default()
	cfg.Proxy.Enabled = true
	cfg.Proxy.Anthropic.BaseURL = upstream.URL
	cfg.Providers.Anthropic.Key = "test-anthropic"

	api, cleanup := newTestAPIWithConfig(t, cfg)
	defer cleanup()

	mux := http.NewServeMux()
	api.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy/anthropic/messages", strings.NewReader(`{"model":"claude-3-5-sonnet"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status 502, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAnthropicProxyMapsUpstreamClientFailure(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error"}}`))
	}))
	defer upstream.Close()

	cfg := config.Default()
	cfg.Proxy.Enabled = true
	cfg.Proxy.Anthropic.BaseURL = upstream.URL
	cfg.Providers.Anthropic.Key = "test-anthropic"

	api, cleanup := newTestAPIWithConfig(t, cfg)
	defer cleanup()

	mux := http.NewServeMux()
	api.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy/anthropic/messages", strings.NewReader(`{"model":"claude-3-5-sonnet"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAnthropicProxyMapsUpstreamRateLimitFailure(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error"}}`))
	}))
	defer upstream.Close()

	cfg := config.Default()
	cfg.Proxy.Enabled = true
	cfg.Proxy.Anthropic.BaseURL = upstream.URL
	cfg.Providers.Anthropic.Key = "test-anthropic"

	api, cleanup := newTestAPIWithConfig(t, cfg)
	defer cleanup()

	mux := http.NewServeMux()
	api.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy/anthropic/messages", strings.NewReader(`{"model":"claude-3-5-sonnet"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status 502, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAnthropicProxyMapsTimeoutFailure(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_1"}`))
	}))
	defer upstream.Close()

	cfg := config.Default()
	cfg.Proxy.Enabled = true
	cfg.Proxy.Anthropic.BaseURL = upstream.URL
	cfg.Proxy.Anthropic.TimeoutSeconds = 1
	cfg.Providers.Anthropic.Key = "test-anthropic"

	api, cleanup := newTestAPIWithConfig(t, cfg)
	defer cleanup()

	mux := http.NewServeMux()
	api.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy/anthropic/messages", strings.NewReader(`{"model":"claude-3-5-sonnet"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected status 504, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func newTestAPI(t *testing.T) (*API, func()) {
	t.Helper()

	return newTestAPIWithConfig(t, config.Default())
}

func newTestAPIWithConfig(t *testing.T, cfg config.Config) (*API, func()) {
	t.Helper()

	tempDir := t.TempDir()
	cfg.Storage.Path = filepath.Join(tempDir, "core.db")
	if strings.TrimSpace(cfg.Providers.OpenAI.Key) == "" {
		cfg.Providers.OpenAI.Key = "test-openai"
	}
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
