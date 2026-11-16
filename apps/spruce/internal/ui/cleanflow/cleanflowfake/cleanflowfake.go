// Package cleanflowfake is what the tests of the pages that list cleanup items share: scan results with their totals, and Files that records what it was asked to reveal. Only tests import it.
package cleanflowfake

import (
	"context"
	"sync"

	"spruce/apps/spruce/internal/engine/cleanup"
)

// Result returns a scan result over categories with the totals filled in, as cleanup.NewResult leaves it.
func Result(categories ...cleanup.Category) cleanup.ScanResult {
	r := cleanup.ScanResult{Categories: categories}
	if r.Categories == nil {
		r.Categories = []cleanup.Category{}
	}
	for _, c := range categories {
		r.Size += c.Size
		r.Items += len(c.Items)
	}
	return r
}

// Files records the paths it was asked to reveal and answers with Err.
type Files struct {
	Err error

	mu       sync.Mutex
	revealed []string
}

// Reveal implements the pages' Files.
func (f *Files) Reveal(_ context.Context, path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revealed = append(f.revealed, path)
	return f.Err
}

// Revealed returns the paths revealed so far, in order.
func (f *Files) Revealed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.revealed...)
}
