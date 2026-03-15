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

func TestStatusP99Under100ms(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","version":"0.1.0","uptime_seconds":1}`))
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
		if err := client.GetJSON(context.Background(), "/api/v1/status", nil, &payload); err != nil {
			t.Fatalf("request failed: %v", err)
		}
		samples = append(samples, time.Since(started))
	}

	p99 := percentile(samples, 0.99)
	if p99 > 100*time.Millisecond {
		t.Fatalf("expected p99 < 100ms, got %s", p99)
	}
}

func percentile(samples []time.Duration, ratio float64) time.Duration {
	if len(samples) == 0 {
		return 0
	}

	sorted := make([]time.Duration, len(samples))
	copy(sorted, samples)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j] < sorted[i] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	index := int(float64(len(sorted)-1) * ratio)
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}
