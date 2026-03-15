package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ichinya/quiverkeep-core/internal/config"
	"github.com/ichinya/quiverkeep-core/internal/domain"
	qerrors "github.com/ichinya/quiverkeep-core/internal/errors"
	"github.com/ichinya/quiverkeep-core/internal/logging"
)

func TestAnthropicProxyForwardSuccessTracksUsage(t *testing.T) {
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
		if r.Header.Get("X-API-Key") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Anthropic-Request-Id", "anthropic_req_1")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_1","model":"claude-3-5-sonnet","usage":{"input_tokens":12,"output_tokens":7}}`))
	}))
	defer upstream.Close()

	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "proxy.log")
	logger, err := logging.New(logging.Config{Level: "debug", Path: logPath})
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}
	t.Cleanup(func() {
		_ = logger.Close()
	})

	cfg := config.Default()
	cfg.Proxy.Enabled = true
	cfg.Proxy.Anthropic.BaseURL = upstream.URL
	cfg.Proxy.Anthropic.TimeoutSeconds = 2
	cfg.Providers.Anthropic.Key = "anthropic-test-key"

	store := &captureStore{}
	proxy := NewAnthropicProxy(cfg, logger, store)

	response, err := proxy.Forward(context.Background(), ForwardRequest{
		Payload:   []byte(`{"model":"claude-3-5-sonnet","messages":[]}`),
		RequestID: "req-success",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status code: %d", response.StatusCode)
	}
	if response.Headers.Get("Anthropic-Request-Id") != "anthropic_req_1" {
		t.Fatalf("expected anthropic request header passthrough")
	}

	records := store.recordsSnapshot()
	if len(records) != 1 {
		t.Fatalf("expected one usage record, got %d", len(records))
	}
	if records[0].TokensIn != 12 || records[0].TokensOut != 7 {
		t.Fatalf("unexpected usage record: %+v", records[0])
	}

	status := proxy.Status()
	if status.LastSuccessAt == nil {
		t.Fatalf("expected success timestamp in diagnostics")
	}
	if status.LastErrorCode != "" {
		t.Fatalf("expected empty error code after success, got %s", status.LastErrorCode)
	}

	logPayload, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed reading proxy log: %v", err)
	}
	logText := string(logPayload)
	if !strings.Contains(logText, `"operation":"proxy_usage"`) {
		t.Fatalf("expected proxy usage operation logs")
	}
	if !strings.Contains(logText, `"tokens_in":12`) {
		t.Fatalf("expected extracted usage tokens in logs")
	}
}

func TestAnthropicProxyForwardDisabled(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Proxy.Enabled = false
	cfg.Providers.Anthropic.Key = "anthropic-test-key"

	logger, err := logging.New(logging.Config{Level: "debug"})
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}
	t.Cleanup(func() {
		_ = logger.Close()
	})

	proxy := NewAnthropicProxy(cfg, logger, &captureStore{})
	_, err = proxy.Forward(context.Background(), ForwardRequest{
		Payload:   []byte(`{"model":"claude-3-5-sonnet","messages":[]}`),
		RequestID: "req-disabled",
	})
	if qerrors.CodeOf(err) != qerrors.CodeProxyDisabled {
		t.Fatalf("expected PROXY_DISABLED, got %v", qerrors.CodeOf(err))
	}

	status := proxy.Status()
	if status.LastErrorCode != string(qerrors.CodeProxyDisabled) {
		t.Fatalf("expected diagnostics error code PROXY_DISABLED, got %s", status.LastErrorCode)
	}
}

func TestAnthropicProxyForwardNotConfigured(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Proxy.Enabled = true
	cfg.Providers.Anthropic.Key = ""

	logger, err := logging.New(logging.Config{Level: "debug"})
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}
	t.Cleanup(func() {
		_ = logger.Close()
	})

	proxy := NewAnthropicProxy(cfg, logger, &captureStore{})
	_, err = proxy.Forward(context.Background(), ForwardRequest{
		Payload:   []byte(`{"model":"claude-3-5-sonnet","messages":[]}`),
		RequestID: "req-not-configured",
	})
	if qerrors.CodeOf(err) != qerrors.CodeProxyNotConfigured {
		t.Fatalf("expected PROXY_NOT_CONFIGURED, got %v", qerrors.CodeOf(err))
	}
}

func TestAnthropicProxyMapsUpstreamNonSuccess(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"upstream_error"}}`))
	}))
	defer upstream.Close()

	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "proxy.log")
	logger, err := logging.New(logging.Config{Level: "debug", Path: logPath})
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}
	t.Cleanup(func() {
		_ = logger.Close()
	})

	cfg := config.Default()
	cfg.Proxy.Enabled = true
	cfg.Proxy.Anthropic.BaseURL = upstream.URL
	cfg.Providers.Anthropic.Key = "anthropic-test-key"

	proxy := NewAnthropicProxy(cfg, logger, &captureStore{})
	_, err = proxy.Forward(context.Background(), ForwardRequest{
		Payload:   []byte(`{"model":"claude-3-5-sonnet","messages":[]}`),
		RequestID: "req-upstream-failure",
	})
	if qerrors.CodeOf(err) != qerrors.CodeProxyUpstreamError {
		t.Fatalf("expected PROXY_UPSTREAM_ERROR, got %v", qerrors.CodeOf(err))
	}

	status := proxy.Status()
	if status.LastUpstreamCode == nil || *status.LastUpstreamCode != http.StatusBadGateway {
		t.Fatalf("expected last upstream status 502 in diagnostics")
	}
	if status.LastErrorCode != string(qerrors.CodeProxyUpstreamError) {
		t.Fatalf("expected diagnostics error code PROXY_UPSTREAM_ERROR, got %s", status.LastErrorCode)
	}

	logPayload, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed reading proxy log: %v", err)
	}
	logText := string(logPayload)
	if !strings.Contains(logText, `"error_code":"PROXY_UPSTREAM_ERROR"`) {
		t.Fatalf("expected stable upstream error code in logs")
	}
	if !strings.Contains(logText, `"retry_decision":"no_retry"`) {
		t.Fatalf("expected retry decision field in logs")
	}
}

func TestAnthropicProxyMapsTimeout(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_1"}`))
	}))
	defer upstream.Close()

	logger, err := logging.New(logging.Config{Level: "debug"})
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}
	t.Cleanup(func() {
		_ = logger.Close()
	})

	cfg := config.Default()
	cfg.Proxy.Enabled = true
	cfg.Proxy.Anthropic.BaseURL = upstream.URL
	cfg.Proxy.Anthropic.TimeoutSeconds = 1
	cfg.Providers.Anthropic.Key = "anthropic-test-key"

	proxy := NewAnthropicProxy(cfg, logger, &captureStore{})
	_, err = proxy.Forward(context.Background(), ForwardRequest{
		Payload:   []byte(`{"model":"claude-3-5-sonnet","messages":[]}`),
		RequestID: "req-timeout",
	})
	if qerrors.CodeOf(err) != qerrors.CodeProxyTimeout {
		t.Fatalf("expected PROXY_TIMEOUT, got %v", qerrors.CodeOf(err))
	}
}

func TestSanitizeEndpointForLogsRedactsCredentialsAndQuery(t *testing.T) {
	t.Parallel()

	sanitized, redacted := sanitizeEndpointForLogs("https://user:secret@example.test/v1/messages?token=secret#frag")
	if !redacted {
		t.Fatalf("expected endpoint to be redacted")
	}
	if strings.Contains(sanitized, "secret") {
		t.Fatalf("sanitized endpoint leaked secret")
	}
	if strings.Contains(sanitized, "?") {
		t.Fatalf("sanitized endpoint leaked query")
	}
	if strings.Contains(sanitized, "#") {
		t.Fatalf("sanitized endpoint leaked fragment")
	}
}

type captureStore struct {
	mu      sync.Mutex
	records []domain.UsageRecord
	fail    bool
}

func (s *captureStore) InsertUsage(_ context.Context, usage domain.UsageRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fail {
		return errors.New("insert usage failed")
	}

	s.records = append(s.records, usage)
	return nil
}

func (s *captureStore) recordsSnapshot() []domain.UsageRecord {
	s.mu.Lock()
	defer s.mu.Unlock()

	records := make([]domain.UsageRecord, len(s.records))
	copy(records, s.records)
	return records
}
