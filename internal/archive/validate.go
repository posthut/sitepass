package archive

import (
	"fmt"
	"path"
	"strings"
)

// Limits bound unpacking. Values come from the plan row at the call site.
type Limits struct {
	MaxUnpackedBytes int64
	MaxFiles         int
	MaxRatio         int64
	MaxPathDepth     int
	MaxSegmentBytes  int
	MaxPathBytes     int
}

// DefaultLimits returns the anonymous-plan defaults from the specification.
func DefaultLimits() Limits {
	return Limits{
		MaxUnpackedBytes: 314572800,
		MaxFiles:         5000,
		MaxRatio:         200,
		MaxPathDepth:     32,
		MaxSegmentBytes:  255,
		MaxPathBytes:     4096,
	}
}

// Domain errors distinguished by callers via errors.Is.
var (
	ErrUnsafeEntry       = fmt.Errorf("archive contains an unsafe entry")
	ErrUnpackedTooLarge  = fmt.Errorf("unpacked archive exceeds size limit")
	ErrTooManyFiles      = fmt.Errorf("archive exceeds file count limit")
	ErrMalformed         = fmt.Errorf("archive is malformed")
	ErrEntrypointMissing = fmt.Errorf("entrypoint index.html not found")
	ErrCompressionBomb   = fmt.Errorf("archive compression ratio exceeds limit")
)

// cleanEntryPath validates and normalises an archive entry path relative to
// the staging root. It rejects absolute paths, traversal, nulls and backslashes.
func cleanEntryPath(name string, limits Limits) (string, error) {
	if name == "" {
		return "", fmt.Errorf("%w: empty path", ErrUnsafeEntry)
	}
	if strings.ContainsRune(name, 0) || strings.Contains(name, `\`) {
		return "", fmt.Errorf("%w: illegal character in %q", ErrUnsafeEntry, name)
	}
	normalized := strings.ReplaceAll(name, `\`, "/")
	if path.IsAbs(normalized) || strings.HasPrefix(normalized, "/") {
		return "", fmt.Errorf("%w: absolute path: %q", ErrUnsafeEntry, name)
	}
	for _, seg := range strings.Split(normalized, "/") {
		if seg == ".." {
			return "", fmt.Errorf("%w: path escapes root: %q", ErrUnsafeEntry, name)
		}
	}
	cleaned := path.Clean(normalized)
	cleaned = strings.TrimPrefix(cleaned, "./")
	if cleaned == "." || cleaned == "" {
		return "", fmt.Errorf("%w: empty cleaned path", ErrUnsafeEntry)
	}
	if strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", fmt.Errorf("%w: path escapes root: %q", ErrUnsafeEntry, name)
	}
	segments := strings.Split(cleaned, "/")
	if len(segments) > limits.MaxPathDepth {
		return "", fmt.Errorf("%w: path too deep", ErrUnsafeEntry)
	}
	for _, seg := range segments {
		if seg == "" || seg == ".." {
			return "", fmt.Errorf("%w: path escapes root: %q", ErrUnsafeEntry, name)
		}
		if len(seg) > limits.MaxSegmentBytes {
			return "", fmt.Errorf("%w: path segment too long", ErrUnsafeEntry)
		}
	}
	if len(cleaned) > limits.MaxPathBytes {
		return "", fmt.Errorf("%w: path too long", ErrUnsafeEntry)
	}
	return cleaned, nil
}
