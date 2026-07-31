package health

import (
	"fmt"
	"syscall"
)

// DiskUsagePercent returns used space percentage for the filesystem
// containing path.
func DiskUsagePercent(path string) (int, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", path, err)
	}
	if st.Blocks == 0 {
		return 0, fmt.Errorf("statfs %s: zero blocks", path)
	}
	used := st.Blocks - st.Bavail
	return int((used * 100) / st.Blocks), nil
}
