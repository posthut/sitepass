package archive

import (
	"bytes"
	"fmt"
)

// Format is a detected artifact format.
type Format string

const (
	FormatTarGz  Format = "tar.gz"
	FormatZip    Format = "zip"
	FormatTarZst Format = "tar.zst"
	FormatHTML   Format = "html"
)

// ErrUnsupportedFormat is returned when magic bytes do not match a known format.
var ErrUnsupportedFormat = fmt.Errorf("unsupported archive format")

// DetectFormat inspects the leading bytes of an artifact.
func DetectFormat(head []byte) (Format, error) {
	if len(head) == 0 {
		return "", ErrUnsupportedFormat
	}
	if len(head) >= 2 && head[0] == 0x1f && head[1] == 0x8b {
		return FormatTarGz, nil
	}
	if len(head) >= 4 && head[0] == 'P' && head[1] == 'K' && (head[2] == 3 || head[2] == 5 || head[2] == 7) {
		return FormatZip, nil
	}
	if len(head) >= 4 && head[0] == 0x28 && head[1] == 0xb5 && head[2] == 0x2f && head[3] == 0xfd {
		return FormatTarZst, nil
	}
	trimmed := bytes.TrimLeft(head, " \t\r\n")
	if len(trimmed) > 0 && trimmed[0] == '<' {
		return FormatHTML, nil
	}
	return "", ErrUnsupportedFormat
}
