package cli

import (
	"os"
	"path/filepath"
)

// pickDefaultConfig tries to make local/dev and docker usage easy:
// - if ./config/config.yaml exists (repo root), use it
// - else fall back to /config/config.yaml (Docker)
func pickDefaultConfig() string {
	candidate := filepath.Join(".", "config", "config.yaml")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return "/config/config.yaml"
}
