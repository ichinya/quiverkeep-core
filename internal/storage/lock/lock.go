package lock

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	qerrors "github.com/ichinya/quiverkeep-core/internal/errors"
	"github.com/ichinya/quiverkeep-core/internal/logging"
)

const staleAfter = 5 * time.Minute

type FileLock struct {
	path   string
	logger *logging.Logger
}

func Acquire(path string, logger *logging.Logger) (*FileLock, error) {
	if err := os.MkdirAll(filepathDir(path), 0o700); err != nil {
		return nil, qerrors.Wrap(qerrors.CodeStorageLock, "failed to create lock dir", err)
	}

	if err := handlePotentialStaleLock(path, logger); err != nil {
		return nil, err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, qerrors.Wrap(qerrors.CodeStorageLock, "core already running or lock unavailable", err)
	}
	defer file.Close()

	if _, err := fmt.Fprintf(file, "pid=%d\ntime=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339)); err != nil {
		return nil, qerrors.Wrap(qerrors.CodeStorageLock, "failed writing lock metadata", err)
	}

	if logger != nil {
		logger.Info("storage lock acquired", "lock_path", path, "pid", os.Getpid())
	}

	return &FileLock{
		path:   path,
		logger: logger,
	}, nil
}

func (l *FileLock) Release() error {
	if l == nil {
		return nil
	}
	if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
		return qerrors.Wrap(qerrors.CodeStorageLock, "failed releasing lock", err)
	}
	if l.logger != nil {
		l.logger.Info("storage lock released", "lock_path", l.path)
	}
	return nil
}

func handlePotentialStaleLock(path string, logger *logging.Logger) error {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if info.IsDir() {
		return qerrors.New(qerrors.CodeStorageLock, "lock path is a directory")
	}

	content, readErr := os.ReadFile(path)
	if readErr != nil {
		return qerrors.Wrap(qerrors.CodeStorageLock, "failed reading existing lock", readErr)
	}

	age := time.Since(info.ModTime())
	if age < staleAfter {
		return qerrors.New(qerrors.CodeStorageLock, "active lock exists")
	}

	pid := extractPID(string(content))
	if pid > 0 && processExists(pid) {
		if logger != nil {
			logger.Warn("[FIX] lock file exceeded stale threshold but owning process is still active",
				"lock_path", path,
				"age_seconds", int(age.Seconds()),
				"pid", pid,
			)
		}
		return qerrors.New(qerrors.CodeStorageLock, "active lock exists")
	}

	if logger != nil {
		logger.Warn("[FIX] stale lock detected, removing",
			"lock_path", path,
			"age_seconds", int(age.Seconds()),
			"pid", pid,
		)
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return qerrors.Wrap(qerrors.CodeStorageLock, "failed removing stale lock", err)
	}

	return nil
}

func extractPID(raw string) int {
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		if !strings.HasPrefix(line, "pid=") {
			continue
		}
		parsed, err := strconv.Atoi(strings.TrimPrefix(line, "pid="))
		if err == nil {
			return parsed
		}
	}
	return 0
}

func filepathDir(path string) string {
	lastSlash := strings.LastIndexAny(path, `/\`)
	if lastSlash == -1 {
		return "."
	}
	return path[:lastSlash]
}
