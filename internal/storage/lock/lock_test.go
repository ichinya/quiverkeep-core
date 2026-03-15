package lock

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	qerrors "github.com/ichinya/quiverkeep-core/internal/errors"
	"github.com/ichinya/quiverkeep-core/internal/logging"
)

func TestAcquireKeepsLiveLockEvenWhenFileIsOld(t *testing.T) {
	t.Parallel()

	logger, err := logging.New(logging.Config{Level: "debug"})
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}

	lockPath := filepath.Join(t.TempDir(), "quiverkeep.lock")
	writeLockFile(t, lockPath, os.Getpid())
	setOldModTime(t, lockPath)

	held, err := Acquire(lockPath, logger)
	if err == nil {
		if held != nil {
			_ = held.Release()
		}
		t.Fatal("expected live lock to block acquisition")
	}
	if qerrors.CodeOf(err) != qerrors.CodeStorageLock {
		t.Fatalf("expected storage lock error, got %v", err)
	}

	if _, statErr := os.Stat(lockPath); statErr != nil {
		t.Fatalf("expected live lock file to remain, got %v", statErr)
	}
}

func TestAcquireRemovesStaleLockWhenOwnerProcessIsGone(t *testing.T) {
	t.Parallel()

	logger, err := logging.New(logging.Config{Level: "debug"})
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}

	lockPath := filepath.Join(t.TempDir(), "quiverkeep.lock")
	writeLockFile(t, lockPath, 999999999)
	setOldModTime(t, lockPath)

	held, err := Acquire(lockPath, logger)
	if err != nil {
		t.Fatalf("expected stale lock to be replaced, got %v", err)
	}
	defer func() {
		if releaseErr := held.Release(); releaseErr != nil {
			t.Fatalf("release lock failed: %v", releaseErr)
		}
	}()

	content, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lock file failed: %v", err)
	}
	if !strings.Contains(string(content), "pid="+strconv.Itoa(os.Getpid())) {
		t.Fatalf("expected lock file to be rewritten for current process, got %q", string(content))
	}
}

func writeLockFile(t *testing.T, path string, pid int) {
	t.Helper()

	if err := os.WriteFile(path, []byte("pid="+strconv.Itoa(pid)+"\ntime="+time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600); err != nil {
		t.Fatalf("write lock file failed: %v", err)
	}
}

func setOldModTime(t *testing.T, path string) {
	t.Helper()

	oldTime := time.Now().Add(-staleAfter - time.Minute)
	if err := os.Chtimes(path, oldTime, oldTime); err != nil {
		t.Fatalf("set modtime failed: %v", err)
	}
}
