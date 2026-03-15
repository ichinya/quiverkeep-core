package storage

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ichinya/quiverkeep-core/internal/config"
	"github.com/ichinya/quiverkeep-core/internal/domain"
	"github.com/ichinya/quiverkeep-core/internal/logging"
)

func TestStoreUsageAndLimitsLifecycle(t *testing.T) {
	t.Parallel()

	store, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()

	if err := store.InsertUsage(ctx, domain.UsageRecord{
		Service:   "openai",
		Model:     "gpt-5",
		TokensIn:  100,
		TokensOut: 50,
		Cost:      1.25,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("insert usage failed: %v", err)
	}

	items, err := store.ListUsage(ctx, domain.UsageFilter{Service: "openai"})
	if err != nil {
		t.Fatalf("list usage failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one usage item, got %d", len(items))
	}

	total, err := store.UsageSummary(ctx, domain.UsageFilter{Service: "openai"})
	if err != nil {
		t.Fatalf("usage summary failed: %v", err)
	}
	if total.TokensIn != 100 || total.TokensOut != 50 {
		t.Fatalf("unexpected totals: %+v", total)
	}

	limitValue := int64(1000)
	reset := time.Now().UTC().Add(24 * time.Hour)
	if err := store.UpsertSubscription(ctx, domain.Subscription{
		Service:    "openai",
		Plan:       "pro",
		LimitValue: &limitValue,
		Used:       100,
		ResetDate:  &reset,
	}); err != nil {
		t.Fatalf("upsert subscription failed: %v", err)
	}

	limits, err := store.Limits(ctx)
	if err != nil {
		t.Fatalf("limits failed: %v", err)
	}
	if len(limits) != 1 {
		t.Fatalf("expected one limits item, got %d", len(limits))
	}
	if limits[0].Percentage == nil {
		t.Fatalf("expected percentage to be calculated")
	}
}

func TestStoreLockPreventsSecondInstance(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(tempDir, "quiverkeep.db")
	meta := config.Metadata{
		ConfigDir: tempDir,
		Path:      filepath.Join(tempDir, "config.json"),
	}

	logger, err := logging.New(logging.Config{Level: "debug"})
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}

	first, err := New(cfg, meta, logger)
	if err != nil {
		t.Fatalf("first store init failed: %v", err)
	}
	defer first.Close()

	second, err := New(cfg, meta, logger)
	if err == nil {
		if second != nil {
			_ = second.Close()
		}
		t.Fatalf("expected second instance to fail due to lock")
	}
}

func TestStoreFilePermissions(t *testing.T) {
	t.Parallel()

	store, cleanup := newTestStore(t)
	defer cleanup()

	if runtime.GOOS == "windows" {
		return
	}

	info, err := os.Stat(store.DbPath())
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected db permissions 0600, got %o", info.Mode().Perm())
	}
}

func newTestStore(t *testing.T) (*Store, func()) {
	t.Helper()

	tempDir := t.TempDir()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(tempDir, "quiverkeep.db")
	meta := config.Metadata{
		ConfigDir: tempDir,
		Path:      filepath.Join(tempDir, "config.json"),
	}

	logger, err := logging.New(logging.Config{Level: "debug"})
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}

	store, err := New(cfg, meta, logger)
	if err != nil {
		t.Fatalf("store init failed: %v", err)
	}

	return store, func() {
		_ = store.Close()
	}
}
