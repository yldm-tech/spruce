package growth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"spruce/apps/spruce/internal/engine/fsx"
)

// ErrNoHistory is what Changes returns while fewer than two snapshots are kept (that load): nothing can be compared yet.
var ErrNoHistory = errors.New("growth: fewer than two snapshots")

// fileLayout is a snapshot file's name without the extension: its time in UTC.
const fileLayout = "20060102T150405Z"

// Store keeps the snapshots in a folder of the data folder, one JSON file each named by its time. Safe for concurrent use.
type Store struct {
	dir string
	mu  sync.Mutex
}

// Open returns the store in dir (<data>/growth/), which is made on the first Save; it does no I/O.
func Open(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("growth: no folder")
	}
	return &Store{dir: filepath.Clean(dir)}, nil
}

func (s *Store) path(at time.Time) string {
	return filepath.Join(s.dir, at.UTC().Format(fileLayout)+".json")
}

// Save writes snap as growth/<UTC yyyymmddThhmmssZ>.json: a temporary file, fsync, then rename, 0600 in a 0700 folder. A snapshot of the same second replaces the one there.
func (s *Store) Save(snap Snapshot) error {
	if snap.At.IsZero() {
		return errors.New("growth: a snapshot without a time")
	}
	snap.Version = Version
	data, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("growth: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("growth: %w", err)
	}
	if err := fsx.WriteFileAtomic(s.path(snap.At), data); err != nil {
		return fmt.Errorf("growth: %w", err)
	}
	return nil
}

// List returns the times of the snapshots kept, oldest first; a missing folder is none. Files the store did not write are ignored.
func (s *Store) List() ([]time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listLocked()
}

func (s *Store) listLocked() ([]time.Time, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("growth: %w", err)
	}
	var out []time.Time
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || !e.Type().IsRegular() {
			continue
		}
		at, err := time.Parse(fileLayout, name)
		if err != nil {
			continue
		}
		out = append(out, at)
	}
	slices.SortFunc(out, func(a, b time.Time) int { return a.Compare(b) })
	return out, nil
}

// Load reads the snapshot taken at at; a corrupt file or another Version is an error.
func (s *Store) Load(at time.Time) (Snapshot, error) {
	data, err := os.ReadFile(s.path(at))
	if err != nil {
		return Snapshot{}, fmt.Errorf("growth: %w", err)
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return Snapshot{}, fmt.Errorf("growth: snapshot %s: %w", at.UTC().Format(fileLayout), err)
	}
	if snap.Version != Version {
		return Snapshot{}, fmt.Errorf("growth: snapshot %s has version %d", at.UTC().Format(fileLayout), snap.Version)
	}
	return snap, nil
}

// Prune removes the snapshots Keep does not keep and then, while the rest take more than r.MaxBytes, the oldest of them (never the newest); it returns how many it removed.
func (s *Store) Prune(now time.Time, r Retention) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	times, err := s.listLocked()
	if err != nil {
		return 0, err
	}
	keep := Keep(times, now, r)
	kept := map[time.Time]bool{}
	for _, t := range keep {
		kept[t] = true
	}
	removed := 0
	var errs []error
	remove := func(t time.Time) {
		if err := os.Remove(s.path(t)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
			return
		}
		removed++
	}
	for _, t := range times {
		if !kept[t] {
			remove(t)
		}
	}
	if r.MaxBytes > 0 && len(keep) > 1 {
		sizes := make([]int64, len(keep))
		var total int64
		for i, t := range keep {
			if info, err := os.Stat(s.path(t)); err == nil {
				sizes[i] = info.Size()
				total += sizes[i]
			}
		}
		// keep is oldest first; the newest stays whatever its size.
		for i := 0; i < len(keep)-1 && total > r.MaxBytes; i++ {
			remove(keep[i])
			total -= sizes[i]
		}
	}
	if len(errs) > 0 {
		return removed, fmt.Errorf("growth: %w", errors.Join(errs...))
	}
	return removed, nil
}

// Clear removes every snapshot (操作历史's 清空历史); the folder stays.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("growth: %w", err)
	}
	var errs []error
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".tmp") {
			continue
		}
		if err := os.Remove(filepath.Join(s.dir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("growth: %w", errors.Join(errs...))
	}
	return nil
}

// Changes is what grew over window up to the newest snapshot: Diff of the newest against Baseline(window) among the older ones, at most n rows (n ≤ 0 for all). A snapshot that cannot be loaded (damaged, of another version) is passed over, so one bad file does not hide the list: the newest that loads is compared with the baseline among the older ones that load. ErrNoHistory while fewer than two snapshots load; other errors are the folder's. app.Overview.Growth answers with it.
func (s *Store) Changes(now time.Time, window time.Duration, n int) ([]Growth, error) {
	times, err := s.List()
	if err != nil {
		return nil, err
	}
	var cur Snapshot
	found := false
	for len(times) > 0 && !found {
		newest := times[len(times)-1]
		times = times[:len(times)-1]
		cur, err = s.Load(newest)
		found = err == nil
	}
	for found && len(times) > 0 {
		base, _ := Baseline(times, now, window)
		if old, lerr := s.Load(base); lerr == nil {
			return Diff(old, cur, n), nil
		}
		times = slices.DeleteFunc(times, func(t time.Time) bool { return t.Equal(base) })
	}
	return nil, ErrNoHistory
}
