// Package datadir locates Spruce's data folder: filepath.Join(os.UserConfigDir(), "Spruce"), which holds history.jsonl, settings.json, window.json, app-icons/ and the single-instance lock. It is the folder earlier builds of Spruce used (their framework's user data path, the same join), so existing data stays where it is.
package datadir

import (
	"fmt"
	"os"
	"path/filepath"
)

// name is the folder's name under the user's config folder.
const name = "Spruce"

// Dir returns the data folder, creating it when missing.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("datadir: %w", err)
	}
	return in(base)
}

// in returns the data folder under base, creating it when missing.
func in(base string) (string, error) {
	dir := filepath.Join(base, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("datadir: %w", err)
	}
	return dir, nil
}
