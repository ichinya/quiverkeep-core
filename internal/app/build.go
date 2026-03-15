package app

import "github.com/ichinya/quiverkeep-core/internal/version"

type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

func DefaultBuildInfo() BuildInfo {
	return BuildInfo{
		Version: version.BuildVersion,
		Commit:  version.BuildCommit,
		Date:    version.BuildDate,
	}
}
