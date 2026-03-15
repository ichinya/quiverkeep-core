package doctor

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/ichinya/quiverkeep-core/internal/cli/httpclient"
	"github.com/ichinya/quiverkeep-core/internal/logging"
)

type Report struct {
	RepositoryPath string `json:"repository_path"`
	WorkspacePath  string `json:"workspace_path"`
	Branch         string `json:"branch"`
	CoreRunning    bool   `json:"core_running"`
	Message        string `json:"message"`
}

func Run(ctx context.Context, client *httpclient.Client, logger *logging.Logger) Report {
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
	if err := client.GetJSON(ctx, "/api/v1/status", nil, &status); err == nil {
		report.CoreRunning = true
		report.Message = "All checks passed"
	}

	logger.Info("doctor report generated",
		"component", "cli",
		"operation", "doctor",
		"core_running", report.CoreRunning,
		"duration_ms", time.Since(started).Milliseconds(),
	)

	return report
}
