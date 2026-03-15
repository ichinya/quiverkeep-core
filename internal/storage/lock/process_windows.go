//go:build windows

package lock

import (
	"errors"

	"golang.org/x/sys/windows"
)

const stillActiveExitCode uint32 = 259

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}

	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(handle)

	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}

	return code == stillActiveExitCode
}
