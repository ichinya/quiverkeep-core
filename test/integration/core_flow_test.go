package integration

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/ichinya/quiverkeep-core/internal/api/server"
	"github.com/ichinya/quiverkeep-core/internal/cli/httpclient"
	"github.com/ichinya/quiverkeep-core/internal/config"
	"github.com/ichinya/quiverkeep-core/internal/logging"
	"github.com/ichinya/quiverkeep-core/internal/storage"
)

func TestCoreServeStatusFlow(t *testing.T) {
	t.Parallel()

	port := pickFreePort(t)
	tempDir := t.TempDir()
	cfg := config.Default()
	cfg.Core.Port = port
	cfg.Core.Bind = "127.0.0.1"
	cfg.Core.URL = fmt.Sprintf("http://127.0.0.1:%d", port)
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

	client := httpclient.New(cfg.Core.URL, "", logger)
	var payload map[string]any
	if err := client.GetJSON(context.Background(), "/api/v1/status", nil, &payload); err != nil {
		cancel()
		<-done
		t.Fatalf("status request failed: %v", err)
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

func pickFreePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to pick free port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}
