package publish

import (
	"fmt"
	"os"
	"path/filepath"
)

// SwitchAtomically makes siteRoot visible as <buildsRoot>/<label>/current
// via an atomic rename of a temporary symlink. Published files become
// mode 0444 and directories 0555.
func SwitchAtomically(buildsRoot, label, siteRoot string, revision int) (string, error) {
	if label == "" || revision <= 0 {
		return "", fmt.Errorf("label and revision are required")
	}
	tokenDir := filepath.Join(buildsRoot, label)
	revDir := filepath.Join(tokenDir, fmt.Sprintf("rev-%d", revision))
	if err := os.MkdirAll(tokenDir, 0o755); err != nil {
		return "", fmt.Errorf("create token dir: %w", err)
	}
	if err := os.RemoveAll(revDir); err != nil {
		return "", fmt.Errorf("clear revision dir: %w", err)
	}
	if err := os.Rename(siteRoot, revDir); err != nil {
		return "", fmt.Errorf("move staging into revision dir: %w", err)
	}
	if err := hardenTree(revDir); err != nil {
		return "", err
	}

	tmpLink := filepath.Join(tokenDir, "current.tmp")
	finalLink := filepath.Join(tokenDir, "current")
	_ = os.Remove(tmpLink)
	if err := os.Symlink(filepath.Base(revDir), tmpLink); err != nil {
		return "", fmt.Errorf("create temp symlink: %w", err)
	}
	if err := os.Rename(tmpLink, finalLink); err != nil {
		_ = os.Remove(tmpLink)
		return "", fmt.Errorf("atomic switch: %w", err)
	}
	return revDir, nil
}

func hardenTree(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.Chmod(path, 0o555)
		}
		return os.Chmod(path, 0o444)
	})
}

// RemoveRevision deletes a revision directory after the grace period.
func RemoveRevision(buildsRoot, label string, revision int) error {
	path := filepath.Join(buildsRoot, label, fmt.Sprintf("rev-%d", revision))
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove revision: %w", err)
	}
	return nil
}
