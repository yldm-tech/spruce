package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

const (
	fileMode = 0o600
	dirMode  = 0o700
)

// Store keeps the settings file. It is safe for concurrent use.
type Store struct {
	path     string
	home     string
	defaults func() AppSettings
	log      *slog.Logger

	mu     sync.Mutex
	loaded bool
	cur    AppSettings
	subs   map[chan AppSettings]struct{}
}

// Open returns a Store for the file at path; home expands "~" in paths. defaults supplies the settings the file does not (usually func() AppSettings { return Defaults(home) }); it is called once, on first use.
func Open(path, home string, defaults func() AppSettings) (*Store, error) {
	if path == "" {
		return nil, errors.New("settings: empty path")
	}
	if defaults == nil {
		return nil, errors.New("settings: nil defaults")
	}
	return &Store{
		path:     filepath.Clean(path),
		home:     home,
		defaults: defaults,
		log:      slog.Default().With("component", "settings"),
		subs:     make(map[chan AppSettings]struct{}),
	}, nil
}

// Get returns the current settings.
func (s *Store) Get() AppSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	return clone(s.cur)
}

// Set normalizes next, writes it and notifies subscribers when it differs from the current settings. It returns what is stored; on a write error the settings stay as they were.
func (s *Store) Set(next AppSettings) (AppSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	return s.setLocked(next)
}

// Update applies fn to a copy of the current settings and stores the result as Set does.
func (s *Store) Update(fn func(*AppSettings)) (AppSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	next := clone(s.cur)
	fn(&next)
	return s.setLocked(next)
}

// Reset stores the defaults.
func (s *Store) Reset() (AppSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	return s.setLocked(s.defaults())
}

func (s *Store) setLocked(next AppSettings) (AppSettings, error) {
	next = Normalize(next, s.home)
	if equal(next, s.cur) {
		if _, err := os.Stat(s.path); err == nil {
			return clone(s.cur), nil
		}
	}
	if err := s.writeLocked(next); err != nil {
		return clone(s.cur), err
	}
	s.cur = next
	for ch := range s.subs {
		offer(ch, clone(next))
	}
	return clone(next), nil
}

// loadLocked reads the file once; a missing or unreadable file leaves the defaults.
func (s *Store) loadLocked() {
	if s.loaded {
		return
	}
	s.loaded = true
	base := s.defaults()
	s.cur = Normalize(base, s.home)
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		s.log.Warn("cannot read settings, using defaults", "path", s.path, "err", err)
		return
	}
	// Decoding over the defaults keeps them for fields the file lacks.
	file := clone(base)
	if err := json.Unmarshal(data, &file); err != nil {
		s.log.Warn("settings file is corrupt, using defaults", "path", s.path, "err", err)
		return
	}
	if file.Version > SchemaVersion {
		s.log.Info("settings file is from a newer version; reading what this version knows", "path", s.path, "version", file.Version)
	}
	s.cur = Normalize(file, s.home)
}

// writeLocked replaces the file through a temporary file in the same folder and a rename.
func (s *Store) writeLocked(v AppSettings) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("settings: encode: %w", err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("settings: create directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("settings: create temporary file: %w", err)
	}
	name := tmp.Name()
	fail := func(step string, err error) error {
		tmp.Close()
		os.Remove(name)
		return fmt.Errorf("settings: %s: %w", step, err)
	}
	if err := tmp.Chmod(fileMode); err != nil && !errors.Is(err, errors.ErrUnsupported) {
		return fail("set file mode", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fail("write temporary file", err)
	}
	if err := tmp.Sync(); err != nil {
		return fail("sync temporary file", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return fmt.Errorf("settings: close temporary file: %w", err)
	}
	if err := os.Rename(name, s.path); err != nil {
		os.Remove(name)
		return fmt.Errorf("settings: replace file: %w", err)
	}
	return nil
}

// Subscribe returns a channel holding the current settings and then every changed value, and a function that unsubscribes and closes it. A slow subscriber misses intermediate values, never the latest.
func (s *Store) Subscribe() (<-chan AppSettings, func()) {
	ch := make(chan AppSettings, 1)
	s.mu.Lock()
	s.loadLocked()
	s.subs[ch] = struct{}{}
	ch <- clone(s.cur)
	s.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			s.mu.Lock()
			delete(s.subs, ch)
			close(ch)
			s.mu.Unlock()
		})
	}
}

// offer replaces whatever ch holds with v. Only the Store sends, under its lock, so the send after draining cannot block.
func offer(ch chan AppSettings, v AppSettings) {
	select {
	case <-ch:
	default:
	}
	ch <- v
}

func clone(s AppSettings) AppSettings {
	s.Cleanup.Exclusions = slices.Clone(s.Cleanup.Exclusions)
	s.Cleanup.DevProjectRoots = slices.Clone(s.Cleanup.DevProjectRoots)
	s.Cleanup.InstallerRoots = slices.Clone(s.Cleanup.InstallerRoots)
	return s
}

func equal(a, b AppSettings) bool {
	return a.Version == b.Version && a.Health == b.Health && a.History == b.History && a.General == b.General &&
		a.Background == b.Background && a.Automation == b.Automation &&
		a.Cleanup.DevArtifactMinAgeDays == b.Cleanup.DevArtifactMinAgeDays && a.Cleanup.LargeFileMinMB == b.Cleanup.LargeFileMinMB &&
		slices.Equal(a.Cleanup.Exclusions, b.Cleanup.Exclusions) && slices.Equal(a.Cleanup.DevProjectRoots, b.Cleanup.DevProjectRoots) &&
		slices.Equal(a.Cleanup.InstallerRoots, b.Cleanup.InstallerRoots)
}
