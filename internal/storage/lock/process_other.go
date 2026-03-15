//go:build !unix && !windows

package lock

func processExists(pid int) bool {
	return false
}
