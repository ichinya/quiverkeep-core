package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ichinya/quiverkeep-core/internal/api/server"
	"github.com/ichinya/quiverkeep-core/internal/cli/httpclient"
	"github.com/ichinya/quiverkeep-core/internal/config"
	"github.com/ichinya/quiverkeep-core/internal/domain"
	"github.com/ichinya/quiverkeep-core/internal/logging"
	"github.com/ichinya/quiverkeep-core/internal/storage"
)

func TestCoreProxyAnthropicFlow(t *testing.T) {
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
			_, _ = w.Write([]byte(`{"error":"missing api key"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_1","model":"claude-3-5-sonnet","usage":{"input_tokens":10,"output_tokens":5}}`))
	}))
	defer upstream.Close()

	port := pickFreePort(t)
	tempDir := t.TempDir()
	cfg := config.Default()
	cfg.Core.Port = port
	cfg.Core.Bind = "127.0.0.1"
	cfg.Core.URL = fmt.Sprintf("http://127.0.0.1:%d", port)
	token := "core-token"
	cfg.Core.Token = &token
	cfg.Proxy.Enabled = true
	cfg.Proxy.Anthropic.BaseURL = upstream.URL
	cfg.Proxy.Anthropic.TimeoutSeconds = 2
	cfg.Providers.Anthropic.Key = "anthropic-provider-key"
	cfg.Storage.Path = filepath.Join(tempDir, "core.db")
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
	defer store.Close()

	srv := server.New(cfg, logger, store)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- srv.Run(ctx)
	}()

	time.Sleep(200 * time.Millisecond)

	if err := callProxyEndpoint(cfg.Core.URL, token); err != nil {
		cancel()
		<-done
		t.Fatalf("proxy endpoint call failed: %v", err)
	}

	client := httpclient.New(cfg.Core.URL, token, logger)
	var proxyStatus map[string]any
	if err := client.GetJSON(context.Background(), "/api/v1/proxy/status", nil, &proxyStatus); err != nil {
		cancel()
		<-done
		t.Fatalf("proxy status call failed: %v", err)
	}

	if !extractEnabledFlag(t, proxyStatus) {
		cancel()
		<-done
		t.Fatalf("expected proxy enabled=true in status payload")
	}

	items, err := store.ListUsage(context.Background(), domain.UsageFilter{Service: "anthropic"})
	if err != nil {
		cancel()
		<-done
		t.Fatalf("usage query failed: %v", err)
	}
	if len(items) == 0 {
		cancel()
		<-done
		t.Fatalf("expected persisted anthropic usage after proxy call")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("server run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("server did not shutdown in time")
	}
}

func callProxyEndpoint(baseURL string, token string) error {
	body := strings.NewReader(`{"model":"claude-3-5-sonnet","messages":[]}`)
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/proxy/anthropic/messages", body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	return nil
}

func extractEnabledFlag(t *testing.T, payload map[string]any) bool {
	t.Helper()

	rawItems, ok := payload["items"]
	if !ok {
		return false
	}
	items, ok := rawItems.([]any)
	if !ok || len(items) == 0 {
		return false
	}
	first, ok := items[0].(map[string]any)
	if !ok {
		return false
	}
	enabled, ok := first["enabled"].(bool)
	return ok && enabled
}
