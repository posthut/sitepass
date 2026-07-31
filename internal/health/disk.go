package health

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
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

// MemAvailableMB returns Linux MemAvailable from /proc/meminfo.
func MemAvailableMB() (int, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "MemAvailable:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			break
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0, err
		}
		return int(kb / 1024), nil
	}
	if err := sc.Err(); err != nil {
		return 0, err
	}
	return 0, fmt.Errorf("MemAvailable not found")
}
