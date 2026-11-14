// Package cleanflowfake is the cleaner pages' dependencies for tests: a Cleaner whose scans and cleans the test scripts, and Files that records what it was asked to reveal. Only tests import it.
package cleanflowfake

import (
	"context"
	"sync"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
)

// Cleaner answers Scan with OnScan and Clean with OnClean (an empty result when nil) and records every call.
type Cleaner struct {
	OnScan  func(ctx context.Context, id app.CleanerID, progress func(cleanup.Progress)) (cleanup.ScanResult, error)
	OnClean func(ctx context.Context, id app.CleanerID, req cleanup.CleanRequest, progress func(cleanup.Progress)) (cleanup.CleanResult, error)

	mu     sync.Mutex
	scans  []app.CleanerID
	cleans []Clean
}

// Clean is one recorded Clean call.
type Clean struct {
	ID  app.CleanerID
	Req cleanup.CleanRequest
}

// Scan implements cleanflow.Cleaner.
func (c *Cleaner) Scan(ctx context.Context, id app.CleanerID, progress func(cleanup.Progress)) (cleanup.ScanResult, error) {
	c.mu.Lock()
	c.scans = append(c.scans, id)
	c.mu.Unlock()
	if c.OnScan == nil {
		return cleanup.ScanResult{Categories: []cleanup.Category{}}, nil
	}
	return c.OnScan(ctx, id, progress)
}

// Clean implements cleanflow.Cleaner.
func (c *Cleaner) Clean(ctx context.Context, id app.CleanerID, req cleanup.CleanRequest, progress func(cleanup.Progress)) (cleanup.CleanResult, error) {
	c.mu.Lock()
	c.cleans = append(c.cleans, Clean{ID: id, Req: req})
	c.mu.Unlock()
	if c.OnClean == nil {
		return cleanup.CleanResult{}, nil
	}
	return c.OnClean(ctx, id, req, progress)
}

// Scans returns the cleaners scanned so far, in order.
func (c *Cleaner) Scans() []app.CleanerID {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]app.CleanerID(nil), c.scans...)
}

// Cleans returns the cleans asked for so far, in order.
func (c *Cleaner) Cleans() []Clean {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Clean(nil), c.cleans...)
}

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
