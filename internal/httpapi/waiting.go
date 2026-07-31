package httpapi

import (
	"fmt"
	"os"
	"path/filepath"
)

const waitingHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <meta name="robots" content="noindex, nofollow" />
  <title>Waiting for upload — Sitepass</title>
  <style>
    body{margin:0;min-height:100vh;display:grid;place-items:center;font-family:system-ui,sans-serif;background:#f9faf9;color:#16191a}
    main{max-width:36rem;padding:2rem}
    h1{font-size:1.5rem;margin:0 0 .75rem}
    p{margin:0;color:#666d6b;line-height:1.5}
  </style>
</head>
<body>
  <main>
    <h1>Waiting for upload</h1>
    <p>This preview URL is reserved. Refresh after your agent uploads a build.</p>
  </main>
</body>
</html>
`

func seedWaitingPage(buildsDir, label string) error {
	rev := filepath.Join(buildsDir, label, "rev-0")
	if err := os.MkdirAll(rev, 0o755); err != nil {
		return fmt.Errorf("mkdir waiting revision: %w", err)
	}
	index := filepath.Join(rev, "index.html")
	if err := os.WriteFile(index, []byte(waitingHTML), 0o644); err != nil {
		return fmt.Errorf("write waiting page: %w", err)
	}
	current := filepath.Join(buildsDir, label, "current")
	tmp := current + ".tmp"
	_ = os.Remove(tmp)
	if err := os.Symlink("rev-0", tmp); err != nil {
		return fmt.Errorf("symlink waiting: %w", err)
	}
	if err := os.Rename(tmp, current); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("switch waiting: %w", err)
	}
	return nil
}
