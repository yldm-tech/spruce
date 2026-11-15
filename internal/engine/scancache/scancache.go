package scancache

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/settings"
)

const (
	// Version is the version of the file this build writes.
	Version = 1
	// MaxFileBytes is the largest file Save writes.
	MaxFileBytes = 8 << 20
	// MaxItemsPerCat is how many items of each category an oversized result keeps.
	MaxItemsPerCat = 2000
	// MaxAge is the age past which a file is not shown.
	MaxAge = 7 * 24 * time.Hour
	// StaleAfter is the age past which a result shows as possibly outdated.
	StaleAfter = 24 * time.Hour
)

// File is the content of overview-cache.json.
type File struct {
	Version    int       `json:"version"`
	AppVersion string    `json:"appVersion"`
	SavedAt    time.Time `json:"savedAt"`
	// Mode is "background" or "foreground".
	Mode string `json:"mode"`
	// SettingsKey is Key of the settings the scan ran under.
	SettingsKey string `json:"settingsKey"`
	// Skipped are the folders the read gate kept the check out of.
	Skipped []string           `json:"skipped,omitempty"`
	Scan    cleanup.ScanResult `json:"scan"`
	// Truncated counts the items left out to stay under MaxFileBytes; the category sizes still count them.
	Truncated int `json:"truncated,omitempty"`
}

// keyed are the settings a scan's result depends on.
type keyed struct {
	Exclusions            []string `json:"exclusions"`
	DevProjectRoots       []string `json:"devProjectRoots"`
	DevArtifactMinAgeDays int      `json:"devArtifactMinAgeDays"`
	InstallerRoots        []string `json:"installerRoots"`
}

// Key hashes the settings a scan's result depends on (Cleanup.Exclusions, DevProjectRoots, DevArtifactMinAgeDays and InstallerRoots): 16 hex digits of the SHA-256 of their JSON. Nil and empty lists hash alike. Pure.
func Key(s settings.AppSettings) string {
	k := keyed{
		Exclusions:            nonNil(s.Cleanup.Exclusions),
		DevProjectRoots:       nonNil(s.Cleanup.DevProjectRoots),
		DevArtifactMinAgeDays: s.Cleanup.DevArtifactMinAgeDays,
		InstallerRoots:        nonNil(s.Cleanup.InstallerRoots),
	}
	// A struct of strings and an int always encodes.
	b, _ := json.Marshal(k)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// Stale reports whether f should show as possibly outdated: another SettingsKey or AppVersion than key and appVersion, or saved more than StaleAfter before now. A file saved in the future (the clock was set back) is stale too. Pure.
func Stale(f File, key, appVersion string, now time.Time) bool {
	age := now.Round(0).Sub(f.SavedAt.Round(0))
	return f.SettingsKey != key || f.AppVersion != appVersion || age > StaleAfter || age < 0
}

// Store keeps one File. Safe for concurrent use.
type Store struct {
	path string
	now  func() time.Time
	mu   sync.Mutex
}

// Open returns the store of the file at path (<data>/overview-cache.json). It does no I/O.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("scancache: no path")
	}
	return &Store{path: path, now: time.Now}, nil
}

// Load reads the file. ok is false, with no error, when it is missing, corrupt, of another Version or saved more than MaxAge ago; only a file that cannot be read is an error.
func (s *Store) Load() (File, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return File{}, false, nil
	}
	if err != nil {
		return File{}, false, fmt.Errorf("scancache: read: %w", err)
	}
	var f File
	if json.Unmarshal(b, &f) != nil || f.Version != Version || f.SavedAt.IsZero() {
		return File{}, false, nil
	}
	if s.now().Round(0).Sub(f.SavedAt.Round(0)) > MaxAge {
		return File{}, false, nil
	}
	if f.Scan.Categories == nil {
		f.Scan.Categories = []cleanup.Category{}
	}
	return f, true, nil
}

// Save writes f atomically (a temporary file in the same folder, fsync, rename) with mode 0600, setting Version. A result whose file would exceed MaxFileBytes keeps the MaxItemsPerCat largest items of each category, then half as many until it fits, and records how many it left out in Truncated; category sizes and the result's totals stay complete. f itself is not changed.
func (s *Store) Save(f File) error {
	f.Version = Version
	b, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("scancache: encode: %w", err)
	}
	for limit := MaxItemsPerCat; len(b) > MaxFileBytes; limit /= 2 {
		if limit == 0 {
			return fmt.Errorf("scancache: a result of %d bytes does not fit in %d", len(b), MaxFileBytes)
		}
		cut := f
		cut.Scan, cut.Truncated = truncate(f.Scan, limit)
		cut.Truncated += f.Truncated
		if b, err = json.Marshal(cut); err != nil {
			return fmt.Errorf("scancache: encode: %w", err)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeAtomic(s.path, b); err != nil {
		return fmt.Errorf("scancache: save: %w", err)
	}
	return nil
}

// Clear removes the file; a missing file is already clear.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("scancache: clear: %w", err)
	}
	return nil
}

// truncate returns a copy of res keeping the limit largest items of each category, largest first, and how many it left out. Sizes and totals are kept as they were.
func truncate(res cleanup.ScanResult, limit int) (cleanup.ScanResult, int) {
	dropped := 0
	cats := make([]cleanup.Category, len(res.Categories))
	for i, c := range res.Categories {
		if len(c.Items) > limit {
			items := slices.Clone(c.Items)
			slices.SortStableFunc(items, func(a, b cleanup.Item) int { return cmp.Compare(b.Size, a.Size) })
			dropped += len(items) - limit
			c.Items = items[:limit]
		}
		cats[i] = c
	}
	res.Categories = cats
	return res, dropped
}

// writeAtomic replaces path with b through a temporary file in the same folder, synced before the rename, so a crash leaves the old file or the new one. The file is 0600 and the folder is created (0700) when missing.
func writeAtomic(path string, b []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(name)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	ok = true
	return nil
}
