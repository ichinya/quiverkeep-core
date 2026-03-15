package perf

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ichinya/quiverkeep-core/internal/cli/httpclient"
	"github.com/ichinya/quiverkeep-core/internal/logging"
)

func TestProxyStatusP99Under100ms(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/proxy/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"items":[{"provider":"anthropic","enabled":true,"configured":true}]}`))
	}))
	defer server.Close()

	logger, err := logging.New(logging.Config{Level: "error"})
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}
	client := httpclient.New(server.URL, "", logger)

	samples := make([]time.Duration, 0, 100)
	for i := 0; i < 100; i++ {
		started := time.Now()
		var payload map[string]any
		if err := client.GetJSON(context.Background(), "/api/v1/proxy/status", nil, &payload); err != nil {
			t.Fatalf("request failed: %v", err)
		}
		samples = append(samples, time.Since(started))
	}

	p99 := percentile(samples, 0.99)
	if p99 > 100*time.Millisecond {
		t.Fatalf("expected p99 < 100ms, got %s", p99)
	}
}
