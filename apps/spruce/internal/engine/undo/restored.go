package undo

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	// restoredVersion is the format of restored.json.
	restoredVersion = 1
	// maxRestored and restoredFor bound restored.json: the newest 5000 IDs of the last 180 days.
	maxRestored = 5000
	restoredFor = 180 * 24 * time.Hour
)

// restoredSet is restored.json: item IDs (hashes of paths, never the paths) and when each was put back.
type restoredSet struct {
	Version int                  `json:"version"`
	Items   map[string]time.Time `json:"items"`
}

// Restored returns the item IDs (cleanup.ItemID of the original paths) the user put back within the last 180 days at now, with when. rules.Plan skips them, so an automatic rule never takes again what the user put back.
func (l *Ledger) Restored(now time.Time) map[string]time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	return trimRestored(l.readRestoredLocked(), now)
}

func (l *Ledger) readRestoredLocked() map[string]time.Time {
	data, err := os.ReadFile(filepath.Join(l.dir, restoredFile))
	if err != nil {
		return map[string]time.Time{}
	}
	var s restoredSet
	if err := json.Unmarshal(data, &s); err != nil || s.Version != restoredVersion || s.Items == nil {
		return map[string]time.Time{}
	}
	return s.Items
}

// trimRestored drops the IDs older than restoredFor at now and keeps the newest maxRestored.
func trimRestored(items map[string]time.Time, now time.Time) map[string]time.Time {
	out := make(map[string]time.Time, len(items))
	for id, at := range items {
		if now.Sub(at) <= restoredFor {
			out[id] = at
		}
	}
	if len(out) <= maxRestored {
		return out
	}
	ids := slices.SortedFunc(maps.Keys(out), func(a, b string) int {
		if c := out[b].Compare(out[a]); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})
	for _, id := range ids[maxRestored:] {
		delete(out, id)
	}
	return out
}

// addRestoredLocked records ids as put back at now.
func (l *Ledger) addRestoredLocked(ids []string, now time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	items := l.readRestoredLocked()
	for _, id := range ids {
		items[id] = now.UTC()
	}
	return l.writeJSON(filepath.Join(l.dir, restoredFile), restoredSet{Version: restoredVersion, Items: trimRestored(items, now)})
}

// pruneRestoredLocked rewrites restored.json without what has expired at now, when anything has.
func (l *Ledger) pruneRestoredLocked(now time.Time) error {
	items := l.readRestoredLocked()
	trimmed := trimRestored(items, now)
	if len(trimmed) == len(items) {
		return nil
	}
	return l.writeJSON(filepath.Join(l.dir, restoredFile), restoredSet{Version: restoredVersion, Items: trimmed})
}
