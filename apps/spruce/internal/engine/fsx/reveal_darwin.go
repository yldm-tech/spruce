package fsx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"spruce/apps/spruce/internal/engine/execx"
)

// Reveal shows path selected in a Finder window, through /usr/bin/open -R.
func Reveal(ctx context.Context, runner execx.Runner, path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%w: %q", ErrNotAbsolute, path)
	}
	if _, err := os.Lstat(path); err != nil {
		return fmt.Errorf("fsx: %w", err)
	}
	if _, err := runner.Run(ctx, execx.Command{Name: "/usr/bin/open", Args: []string{"-R", filepath.Clean(path)}, Timeout: 15 * time.Second}); err != nil {
		return fmt.Errorf("fsx: reveal %q: %w", path, err)
	}
	return nil
}
