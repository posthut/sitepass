package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// Result is a successfully unpacked and entrypoint-resolved staging tree.
type Result struct {
	StagingDir   string
	SiteRoot     string
	Format       Format
	FileCount    int
	UnpackedSize int64
	Warnings     []Warning
}

// Warning is a non-blocking observation about the published tree.
type Warning struct {
	Code    string
	Message string
	Paths   []string
}

// UnpackFile detects the format of path and unpacks it into stagingDir.
func UnpackFile(path string, stagingDir string, compressedSize int64, limits Limits) (*Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open artifact: %w", err)
	}
	defer f.Close()

	head := make([]byte, 512)
	n, err := io.ReadFull(f, head)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, fmt.Errorf("read artifact head: %w", err)
	}
	head = head[:n]
	format, err := DetectFormat(head)
	if err != nil {
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("rewind artifact: %w", err)
	}

	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return nil, fmt.Errorf("create staging dir: %w", err)
	}

	var fileCount int
	var unpacked int64
	switch format {
	case FormatHTML:
		fileCount, unpacked, err = unpackHTML(f, stagingDir, limits)
	case FormatTarGz:
		fileCount, unpacked, err = unpackTarGz(f, stagingDir, compressedSize, limits)
	case FormatTarZst:
		fileCount, unpacked, err = unpackTarZst(f, stagingDir, compressedSize, limits)
	case FormatZip:
		fileCount, unpacked, err = unpackZip(path, stagingDir, compressedSize, limits)
	default:
		return nil, ErrUnsupportedFormat
	}
	if err != nil {
		_ = os.RemoveAll(stagingDir)
		return nil, err
	}

	if err := verifyStagingInsideParent(stagingDir); err != nil {
		_ = os.RemoveAll(stagingDir)
		return nil, err
	}

	siteRoot, err := ResolveEntrypoint(stagingDir)
	if err != nil {
		_ = os.RemoveAll(stagingDir)
		return nil, err
	}

	warnings := collectWarnings(siteRoot)
	return &Result{
		StagingDir:   stagingDir,
		SiteRoot:     siteRoot,
		Format:       format,
		FileCount:    fileCount,
		UnpackedSize: unpacked,
		Warnings:     warnings,
	}, nil
}

func unpackHTML(r io.Reader, stagingDir string, limits Limits) (int, int64, error) {
	limited := io.LimitReader(r, limits.MaxUnpackedBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: read html: %v", ErrMalformed, err)
	}
	if int64(len(data)) > limits.MaxUnpackedBytes {
		return 0, 0, ErrUnpackedTooLarge
	}
	target := filepath.Join(stagingDir, "index.html")
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return 0, 0, fmt.Errorf("write index.html: %w", err)
	}
	return 1, int64(len(data)), nil
}

func unpackTarGz(r io.Reader, stagingDir string, compressedSize int64, limits Limits) (int, int64, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: gzip: %v", ErrMalformed, err)
	}
	defer gz.Close()
	return unpackTar(gz, stagingDir, compressedSize, limits)
}

func unpackTarZst(r io.Reader, stagingDir string, compressedSize int64, limits Limits) (int, int64, error) {
	zr, err := zstd.NewReader(r)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: zstd: %v", ErrMalformed, err)
	}
	defer zr.Close()
	return unpackTar(zr, stagingDir, compressedSize, limits)
}

func unpackTar(r io.Reader, stagingDir string, compressedSize int64, limits Limits) (int, int64, error) {
	tr := tar.NewReader(r)
	var files int
	var unpacked int64
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, 0, fmt.Errorf("%w: tar: %v", ErrMalformed, err)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			cleaned, err := cleanEntryPath(hdr.Name, limits)
			if err != nil {
				// tar often includes "./" as a directory entry; skip it.
				if errors.Is(err, ErrUnsafeEntry) && (hdr.Name == "." || hdr.Name == "./" || strings.Trim(hdr.Name, "/") == "") {
					continue
				}
				return 0, 0, err
			}
			if err := os.MkdirAll(filepath.Join(stagingDir, cleaned), 0o755); err != nil {
				return 0, 0, fmt.Errorf("mkdir: %w", err)
			}
		case tar.TypeReg, tar.TypeRegA:
			n, err := writeRegularFile(stagingDir, hdr.Name, tr, limits, &files, &unpacked, compressedSize)
			if err != nil {
				return 0, 0, err
			}
			_ = n
		default:
			return 0, 0, fmt.Errorf("%w: unsupported tar entry type %v", ErrUnsafeEntry, hdr.Typeflag)
		}
	}
	return files, unpacked, nil
}

func unpackZip(path string, stagingDir string, compressedSize int64, limits Limits) (int, int64, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: zip: %v", ErrMalformed, err)
	}
	defer zr.Close()

	var files int
	var unpacked int64
	for _, zf := range zr.File {
		name := zf.Name
		if strings.HasSuffix(name, "/") {
			cleaned, err := cleanEntryPath(strings.TrimSuffix(name, "/"), limits)
			if err != nil {
				return 0, 0, err
			}
			if err := os.MkdirAll(filepath.Join(stagingDir, cleaned), 0o755); err != nil {
				return 0, 0, fmt.Errorf("mkdir: %w", err)
			}
			continue
		}
		if zf.Mode()&os.ModeSymlink != 0 || !zf.Mode().IsRegular() {
			// zip.FileHeader Mode for dirs already handled; refuse non-regular.
			if zf.Mode() != 0 && !zf.Mode().IsRegular() && !strings.HasSuffix(zf.Name, "/") {
				return 0, 0, fmt.Errorf("%w: non-regular zip entry", ErrUnsafeEntry)
			}
		}
		rc, err := zf.Open()
		if err != nil {
			return 0, 0, fmt.Errorf("%w: open zip entry: %v", ErrMalformed, err)
		}
		_, err = writeRegularFile(stagingDir, name, rc, limits, &files, &unpacked, compressedSize)
		_ = rc.Close()
		if err != nil {
			return 0, 0, err
		}
	}
	return files, unpacked, nil
}

func writeRegularFile(
	stagingDir, name string,
	r io.Reader,
	limits Limits,
	files *int,
	unpacked *int64,
	compressedSize int64,
) (int64, error) {
	cleaned, err := cleanEntryPath(name, limits)
	if err != nil {
		return 0, err
	}
	if *files+1 > limits.MaxFiles {
		return 0, ErrTooManyFiles
	}
	target := filepath.Join(stagingDir, cleaned)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return 0, fmt.Errorf("mkdir parent: %w", err)
	}
	remaining := limits.MaxUnpackedBytes - *unpacked
	if remaining <= 0 {
		return 0, ErrUnpackedTooLarge
	}
	limited := io.LimitReader(r, remaining+1)
	f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, fmt.Errorf("create file: %w", err)
	}
	n, err := io.Copy(f, limited)
	closeErr := f.Close()
	if err != nil {
		return 0, fmt.Errorf("write file: %w", err)
	}
	if closeErr != nil {
		return 0, closeErr
	}
	if n > remaining {
		return 0, ErrUnpackedTooLarge
	}
	*unpacked += n
	*files++
	if compressedSize > 0 && limits.MaxRatio > 0 && *unpacked > compressedSize*limits.MaxRatio {
		return 0, ErrCompressionBomb
	}
	return n, nil
}
