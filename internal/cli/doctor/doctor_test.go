package doctor

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ichinya/quiverkeep-core/internal/cli/httpclient"
	qerrors "github.com/ichinya/quiverkeep-core/internal/errors"
	"github.com/ichinya/quiverkeep-core/internal/logging"
)

func TestRunMarksUnauthorizedCoreAsReachable(t *testing.T) {
	t.Parallel()

	logger, err := logging.New(logging.Config{Level: "debug"})
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"UNAUTHORIZED","message":"UNAUTHORIZED: invalid bearer token"}`))
	}))
	defer server.Close()

	client := httpclient.New(server.URL, "", logger)
	report, err := Run(context.Background(), client, logger)
	if err == nil {
		t.Fatal("expected unauthorized error")
	}
	if qerrors.CodeOf(err) != qerrors.CodeUnauthorized {
		t.Fatalf("expected unauthorized code, got %v", qerrors.CodeOf(err))
	}
	if !report.CoreRunning {
		t.Fatal("expected doctor to mark responding core as running")
	}
	if !strings.Contains(report.Message, "invalid bearer token") {
		t.Fatalf("unexpected doctor message: %q", report.Message)
	}
}

func TestRunMarksConnectionFailureAsNotRunning(t *testing.T) {
	t.Parallel()

	logger, err := logging.New(logging.Config{Level: "debug"})
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener failed: %v", err)
	}

	client := httpclient.New("http://"+address, "", logger)
	report, err := Run(context.Background(), client, logger)
	if err == nil {
		t.Fatal("expected connection error")
	}
	if qerrors.CodeOf(err) != qerrors.CodeConnectionRefused {
		t.Fatalf("expected connection refused code, got %v", qerrors.CodeOf(err))
	}
	if report.CoreRunning {
		t.Fatal("expected doctor to mark unreachable core as not running")
	}
}
