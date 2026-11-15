package app

import (
	"slices"
	"sync"

	"github.com/yldm-tech/spruce/internal/host/folderaccess"
)

// GateRecorder is the read gate of 概览's checks, between the sources' MayRead and folderaccess.Store. A background check reads through Background: it asks the store and records the prompting folders the store kept it out of, for OverviewResult.Skipped. A foreground check, which the user started, reads through Foreground: it may read everything (as without a gate) and records the prompting folders it entered, which the controller hands to Store.Observe once the check has completed, so later background checks may enter them too. Safe for concurrent use.
type GateRecorder struct {
	access *folderaccess.Store

	mu sync.Mutex
	// skipped and entered are the prompting folders of the current background and foreground check, by the folder's path.
	skipped map[string]bool
	entered map[string]bool
}

// NewGateRecorder returns the gate over access; nil lets every read happen and records nothing (Windows has no prompting folders either).
func NewGateRecorder(access *folderaccess.Store) *GateRecorder {
	return &GateRecorder{access: access, skipped: map[string]bool{}, entered: map[string]bool{}}
}

// Background is the MayRead of the background check's sources: whether the store allows a background read of path. A refused read records the prompting folder it would have entered.
func (g *GateRecorder) Background(path string) bool {
	if g == nil || g.access == nil || g.access.Allow(path) {
		return true
	}
	if folder, ok, err := g.access.PromptingFor(path); err == nil && ok {
		g.mu.Lock()
		g.skipped[folder] = true
		g.mu.Unlock()
	}
	return false
}

// Foreground is the MayRead of the foreground check's sources: every read may happen; one that enters a prompting folder records it.
func (g *GateRecorder) Foreground(path string) bool {
	if g == nil || g.access == nil {
		return true
	}
	if folder, ok, err := g.access.PromptingFor(path); err == nil && ok {
		g.mu.Lock()
		g.entered[folder] = true
		g.mu.Unlock()
	}
	return true
}

// begin forgets what the previous check of mode recorded; the controller calls it as a check starts.
func (g *GateRecorder) begin(mode CheckMode) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if mode == CheckBackground {
		g.skipped = map[string]bool{}
	} else {
		g.entered = map[string]bool{}
	}
}

// recorded returns, sorted, the folders the current check of mode was kept out of (background) or entered (foreground).
func (g *GateRecorder) recorded(mode CheckMode) []string {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	set := g.entered
	if mode == CheckBackground {
		set = g.skipped
	}
	var out []string
	for f := range set {
		out = append(out, f)
	}
	slices.Sort(out)
	return out
}

// observe tells the store that the completed foreground check entered folders.
func (g *GateRecorder) observe(folders []string) error {
	if g == nil || g.access == nil {
		return nil
	}
	for _, f := range folders {
		if err := g.access.Observe(f); err != nil {
			return err
		}
	}
	return nil
}
