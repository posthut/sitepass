package archive

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectFormat(t *testing.T) {
	html, err := os.ReadFile(filepath.Join("..", "..", "testdata", "archives", "valid-single.html"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := DetectFormat(html)
	if err != nil || got != FormatHTML {
		t.Fatalf("html: got %q err=%v", got, err)
	}
	gz, err := os.ReadFile(filepath.Join("..", "..", "testdata", "archives", "valid-spa.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	got, err = DetectFormat(gz[:8])
	if err != nil || got != FormatTarGz {
		t.Fatalf("tar.gz: got %q err=%v", got, err)
	}
}

func TestUnpack_Fixtures(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "archives")
	cases := []struct {
		name    string
		file    string
		wantErr error
	}{
		{"valid spa", "valid-spa.tar.gz", nil},
		{"valid nested", "valid-nested-dist.tar.gz", nil},
		{"valid html", "valid-single.html", nil},
		{"traversal", "traversal.tar.gz", ErrUnsafeEntry},
		{"no index", "no-index.tar.gz", ErrEntrypointMissing},
		{"truncated", "truncated.tar.gz", ErrMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			staging, err := os.MkdirTemp("", "sitepass-unpack-*")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(staging)
			path := filepath.Join(root, tc.file)
			st, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			res, err := UnpackFile(path, staging, st.Size(), DefaultLimits())
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
				if _, err := os.Stat(filepath.Join(res.SiteRoot, "index.html")); err != nil {
					t.Fatalf("missing index.html: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("want %v, got %v", tc.wantErr, err)
			}
		})
	}
}
