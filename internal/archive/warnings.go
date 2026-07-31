package archive

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func verifyStagingInsideParent(stagingDir string) error {
	resolved, err := filepath.EvalSymlinks(stagingDir)
	if err != nil {
		resolved, err = filepath.Abs(stagingDir)
		if err != nil {
			return fmt.Errorf("%w: resolve staging: %v", ErrUnsafeEntry, err)
		}
	}
	parent := filepath.Dir(resolved)
	rel, err := filepath.Rel(parent, resolved)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("%w: staging escaped parent", ErrUnsafeEntry)
	}
	return nil
}

func collectWarnings(siteRoot string) []Warning {
	var warnings []Warning
	_ = filepath.WalkDir(siteRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(siteRoot, path)
		if hasSourceTreePath(rel) {
			warnings = appendUniqueWarning(warnings, Warning{
				Code:    "source_tree_present",
				Message: "Published tree contains source or dependency directories.",
			})
		}
		info, statErr := d.Info()
		if statErr == nil && info.Size() > 2*1024*1024 {
			warnings = append(warnings, Warning{
				Code:    "large_bundle",
				Message: "A single file exceeds 2 MB.",
				Paths:   []string{"/" + filepath.ToSlash(rel)},
			})
		}
		return nil
	})
	return warnings
}

func hasSourceTreePath(rel string) bool {
	slash := filepath.ToSlash(rel)
	return slash == "src" || slash == ".git" || slash == "node_modules" ||
		strings.HasPrefix(slash, "src/") ||
		strings.HasPrefix(slash, ".git/") ||
		strings.HasPrefix(slash, "node_modules/") ||
		strings.Contains(slash, "/node_modules/")
}

func appendUniqueWarning(list []Warning, w Warning) []Warning {
	for _, existing := range list {
		if existing.Code == w.Code {
			return list
		}
	}
	return append(list, w)
}
