package doctor

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/ichinya/quiverkeep-core/internal/cli/httpclient"
	qerrors "github.com/ichinya/quiverkeep-core/internal/errors"
	"github.com/ichinya/quiverkeep-core/internal/logging"
)

type Report struct {
	RepositoryPath       string `json:"repository_path"`
	WorkspacePath        string `json:"workspace_path"`
	Branch               string `json:"branch"`
	CoreRunning          bool   `json:"core_running"`
	ProxyStatusReachable bool   `json:"proxy_status_reachable"`
	ProxyEnabled         bool   `json:"proxy_enabled"`
	Message              string `json:"message"`
}

func Run(ctx context.Context, client *httpclient.Client, logger *logging.Logger) (Report, error) {
	started := time.Now()
	cwd, _ := os.Getwd()

	report := Report{
		RepositoryPath: cwd,
		WorkspacePath:  filepath.Dir(cwd),
		Branch:         os.Getenv("QUIVERKEEP_BRANCH"),
		CoreRunning:    false,
		Message:        "Core is not reachable",
	}

	var status map[string]any
	logger.Info("doctor check start",
		"component", "cli",
		"operation", "doctor",
		"check", "core_status",
	)
	if err := client.GetJSON(ctx, "/api/v1/status", nil, &status); err == nil {
		report.CoreRunning = true
		logger.Debug("doctor core status parsed",
			"component", "cli",
			"operation", "doctor",
			"check", "core_status",
			"payload_keys", len(status),
		)

		var proxyStatus map[string]any
		proxyStarted := time.Now()
		logger.Info("doctor proxy check start",
			"component", "cli",
			"operation", "doctor",
			"check", "proxy_status",
		)
		proxyErr := client.GetJSON(ctx, "/api/v1/proxy/status", nil, &proxyStatus)
		if proxyErr == nil {
			report.ProxyStatusReachable = true
			report.ProxyEnabled = extractProxyEnabled(proxyStatus)
			report.Message = "All checks passed"
			logger.Debug("doctor proxy status parsed",
				"component", "cli",
				"operation", "doctor",
				"check", "proxy_status",
				"proxy_enabled", report.ProxyEnabled,
				"payload_keys", len(proxyStatus),
			)
			logger.Info("doctor proxy check finish",
				"component", "cli",
				"operation", "doctor",
				"check", "proxy_status",
				"duration_ms", time.Since(proxyStarted).Milliseconds(),
				"proxy_status_reachable", report.ProxyStatusReachable,
				"proxy_enabled", report.ProxyEnabled,
			)
			logger.Info("doctor report generated",
				"component", "cli",
				"operation", "doctor",
				"core_running", report.CoreRunning,
				"proxy_status_reachable", report.ProxyStatusReachable,
				"proxy_enabled", report.ProxyEnabled,
				"duration_ms", time.Since(started).Milliseconds(),
			)
			return report, nil
		}

		report.Message = proxyErr.Error()
		logger.Error("doctor proxy check failed",
			"component", "cli",
			"operation", "doctor",
			"check", "proxy_status",
			"duration_ms", time.Since(proxyStarted).Milliseconds(),
			"error_code", qerrors.CodeOf(proxyErr),
		)
		logger.Info("doctor report generated",
			"component", "cli",
			"operation", "doctor",
			"core_running", report.CoreRunning,
			"proxy_status_reachable", report.ProxyStatusReachable,
			"duration_ms", time.Since(started).Milliseconds(),
		)
		return report, proxyErr
	} else {
		report.Message = err.Error()
		code := qerrors.CodeOf(err)
		if code != qerrors.CodeConnectionRefused && code != qerrors.CodeCoreNotRunning {
			report.CoreRunning = true
		}
		logger.Error("doctor core check failed",
			"component", "cli",
			"operation", "doctor",
			"check", "core_status",
			"error_code", code,
		)

		logger.Info("doctor report generated",
			"component", "cli",
			"operation", "doctor",
			"core_running", report.CoreRunning,
			"duration_ms", time.Since(started).Milliseconds(),
		)

		return report, err
	}
}

func extractProxyEnabled(payload map[string]any) bool {
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
	if !ok {
		return false
	}
	return enabled
}
