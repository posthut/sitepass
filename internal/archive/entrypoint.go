package archive

import (
	"fmt"
	"os"
	"path/filepath"
)

// ResolveEntrypoint finds the site root containing index.html.
// It may descend exactly one directory level when the archive root holds
// a single directory and no files.
func ResolveEntrypoint(stagingDir string) (string, error) {
	index := filepath.Join(stagingDir, "index.html")
	if st, err := os.Stat(index); err == nil && !st.IsDir() {
		return stagingDir, nil
	}

	entries, err := os.ReadDir(stagingDir)
	if err != nil {
		return "", fmt.Errorf("read staging dir: %w", err)
	}
	var onlyDir string
	fileCount := 0
	dirCount := 0
	for _, e := range entries {
		name := e.Name()
		if name == "." || name == ".." {
			continue
		}
		if e.IsDir() {
			dirCount++
			onlyDir = name
			continue
		}
		fileCount++
	}
	if fileCount == 0 && dirCount == 1 {
		nested := filepath.Join(stagingDir, onlyDir)
		nestedIndex := filepath.Join(nested, "index.html")
		if st, err := os.Stat(nestedIndex); err == nil && !st.IsDir() {
			return nested, nil
		}
	}
	return "", ErrEntrypointMissing
}
