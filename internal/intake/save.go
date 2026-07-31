package intake

import (
	"fmt"
	"io"
	"os"
)

// SavedBody is a temporary file holding the raw request body.
type SavedBody struct {
	Path string
	Size int64
	file *os.File
}

// Close removes the temporary file.
func (s *SavedBody) Close() error {
	if s == nil || s.file == nil {
		return nil
	}
	name := s.file.Name()
	_ = s.file.Close()
	s.file = nil
	return os.Remove(name)
}

// ErrTooLarge means the body exceeded maxBytes while streaming.
var ErrTooLarge = fmt.Errorf("body exceeds size limit")

// SaveStream writes r to a temporary file, stopping if more than maxBytes
// are read. dir must be owned by the process.
func SaveStream(dir string, r io.Reader, maxBytes int64) (*SavedBody, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("maxBytes must be positive")
	}
	f, err := os.CreateTemp(dir, "sitepass-upload-*")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	limited := &countingLimitedReader{R: r, N: maxBytes}
	n, err := io.Copy(f, limited)
	if err != nil {
		name := f.Name()
		_ = f.Close()
		_ = os.Remove(name)
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		name := f.Name()
		_ = f.Close()
		_ = os.Remove(name)
		return nil, fmt.Errorf("rewind temp file: %w", err)
	}
	return &SavedBody{Path: f.Name(), Size: n, file: f}, nil
}

type countingLimitedReader struct {
	R io.Reader
	N int64
}

func (l *countingLimitedReader) Read(p []byte) (int, error) {
	if l.N <= 0 {
		// Drain one more byte to distinguish exact limit vs overflow.
		var one [1]byte
		n, err := l.R.Read(one[:])
		if n > 0 {
			return 0, ErrTooLarge
		}
		return 0, err
	}
	if int64(len(p)) > l.N {
		p = p[:l.N]
	}
	n, err := l.R.Read(p)
	l.N -= int64(n)
	return n, err
}
