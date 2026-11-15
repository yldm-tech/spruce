package smartscan

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
)

// Source is one cleaner whose scan 智能扫描 includes.
type Source struct {
	// ID names the source in merged category IDs and in Category.Source: the CleanerID of its page, e.g. "system-junk".
	ID string
	// New builds the source's scanner. New calls it once, so 智能扫描 has an instance nobody else uses (see the package documentation).
	New func() cleanup.Scanner
}

// idSep joins a source's ID and a category ID into a merged category ID.
const idSep = ":"

// permille is the scale of a source's share of the progress.
const permille = 1000

// scanFailed is what the page is told about a source whose scan failed for a reason that is not a cleanup.UserError.
const scanFailed = "这部分没能扫描完成，详情已记录到日志"

// source is a Source with its own scanner.
type source struct {
	id      string
	scanner cleanup.Scanner
}

// Scanner is the cleanup.Scanner of the 智能扫描 page. It is safe for concurrent use; one scan or clean runs at a time.
type Scanner struct {
	sources []source
	log     *slog.Logger
	now     func() time.Time

	busy sync.Mutex

	mu   sync.Mutex
	last *merged
	// pending is set while a scan runs and stays set when it does not finish with every source: the sources have then remembered scans the merged one does not match, so Clean refuses until a scan completes.
	pending bool
}

var _ cleanup.Scanner = (*Scanner)(nil)

// merged is what Scanner keeps of its last scan: for every merged category, where it came from and its items.
type merged struct {
	// order is the merged category IDs in scan order.
	order []string
	cats  map[string]origin
}

// origin is a merged category's source and its own ID there, with the IDs of its items in order.
type origin struct {
	source int
	id     string
	title  string
	items  []string
	// command is set for a category cleaned by its tool's command, which goes as a whole once the command ran.
	command bool
}

// New returns a Scanner over sources, in the order given, building each source's scanner now. It panics on an empty or repeated source ID or a nil constructor, so a wiring mistake shows at startup.
func New(sources []Source, log *slog.Logger) *Scanner {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	s := &Scanner{log: log, now: time.Now}
	seen := map[string]bool{}
	for _, src := range sources {
		if src.ID == "" || strings.Contains(src.ID, idSep) || seen[src.ID] || src.New == nil {
			panic(fmt.Sprintf("smartscan: bad source %q", src.ID))
		}
		seen[src.ID] = true
		sc := src.New()
		if sc == nil {
			panic(fmt.Sprintf("smartscan: source %q built no scanner", src.ID))
		}
		s.sources = append(s.sources, source{id: src.ID, scanner: sc})
	}
	return s
}

// MergedID is the ID a category of the source gets in the merged scan.
func MergedID(sourceID, categoryID string) string {
	return sourceID + idSep + categoryID
}

// progressState collects the sources' progress and reports it, never concurrently.
type progressState struct {
	mu     sync.Mutex
	fn     func(cleanup.Progress)
	states []cleanup.SourceState
}

// update applies change to source i's state and reports every source, unless the source has already finished (a late event of a scanner that returned).
func (p *progressState) update(i int, path string, change func(*cleanup.SourceState)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.states[i].Status != cleanup.SourceScanning {
		return
	}
	change(&p.states[i])
	p.emitLocked(path)
}

// finish records source i's final state and reports every source.
func (p *progressState) finish(i int, st cleanup.SourceState) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.states[i] = st
	p.emitLocked("")
}

func (p *progressState) emitLocked(path string) {
	ev := cleanup.Progress{Phase: cleanup.PhaseScanning, Path: path, Total: permille * len(p.states), Sources: slices.Clone(p.states)}
	for _, st := range p.states {
		ev.Bytes += st.Bytes
		switch {
		case st.Status != cleanup.SourceScanning:
			ev.Done += permille
		case st.Total > 0:
			// A running source never counts as complete.
			ev.Done += min(permille-1, permille*st.Done/st.Total)
		}
	}
	p.fn(ev)
}

// snapshot returns a copy of every source's state.
func (p *progressState) snapshot() []cleanup.SourceState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.states)
}

// outcome is what one source's scan returned.
type outcome struct {
	res cleanup.ScanResult
	err error
}

// Scan runs every source's scan at once and returns the merged result, which it remembers for Clean.
func (s *Scanner) Scan(ctx context.Context, progress func(cleanup.Progress)) (cleanup.ScanResult, error) {
	if !s.busy.TryLock() {
		return cleanup.ScanResult{}, cleanup.ErrBusy
	}
	defer s.busy.Unlock()
	if progress == nil {
		progress = func(cleanup.Progress) {}
	}
	s.mu.Lock()
	s.pending = true
	s.mu.Unlock()
	started := s.now()
	ps := &progressState{fn: progress, states: make([]cleanup.SourceState, len(s.sources))}
	for i, src := range s.sources {
		ps.states[i] = cleanup.SourceState{ID: src.id, Status: cleanup.SourceScanning}
	}
	ps.mu.Lock()
	ps.emitLocked("")
	ps.mu.Unlock()

	outs := make([]outcome, len(s.sources))
	var wg sync.WaitGroup
	for i, src := range s.sources {
		wg.Go(func() {
			res, err := src.scanner.Scan(ctx, func(p cleanup.Progress) {
				ps.update(i, p.Path, func(st *cleanup.SourceState) {
					st.Done, st.Total, st.Bytes = p.Done, p.Total, p.Bytes
				})
			})
			outs[i] = outcome{res: res, err: err}
			st := cleanup.SourceState{ID: src.id, Status: cleanup.SourceDone, Bytes: res.Size, Items: res.Items, Denied: res.Denied}
			if err != nil {
				st = cleanup.SourceState{ID: src.id, Status: cleanup.SourceFailed, Error: s.sentence(ctx, src.id, err)}
			}
			ps.finish(i, st)
		})
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return cleanup.ScanResult{}, err
	}

	var firstErr error
	failed := 0
	var cats []cleanup.Category
	var denied int64
	last := &merged{cats: map[string]origin{}}
	seen := map[string]bool{}
	for i, out := range outs {
		if out.err != nil {
			failed++
			if firstErr == nil {
				firstErr = out.err
			}
			continue
		}
		denied += out.res.Denied
		for _, c := range out.res.Categories {
			had := len(c.Items)
			c.Items = slices.DeleteFunc(slices.Clone(c.Items), func(it cleanup.Item) bool { return seen[it.ID] })
			if had > 0 && len(c.Items) == 0 {
				continue
			}
			o := origin{source: i, id: c.ID, title: c.Title, command: c.Command != ""}
			for _, it := range c.Items {
				seen[it.ID] = true
				o.items = append(o.items, it.ID)
			}
			c.ID = MergedID(s.sources[i].id, c.ID)
			c.Source = s.sources[i].id
			// The sources' roots stay with the sources, which delete; the merged scan never does.
			c.Roots = nil
			cats = append(cats, c)
			last.order = append(last.order, c.ID)
			last.cats[c.ID] = o
		}
	}
	if len(s.sources) > 0 && failed == len(s.sources) {
		return cleanup.ScanResult{}, firstErr
	}
	res := cleanup.NewResult(cats, started)
	res.Denied = denied
	res.Sources = ps.snapshot()
	s.mu.Lock()
	s.last = last
	s.pending = failed > 0
	s.mu.Unlock()
	return res, nil
}

// Pending reports whether the last scan did not finish with every source (stopped, failed, or still running); Clean refuses until one does.
func (s *Scanner) Pending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pending
}

// sentence is what the page is told about a source's failed scan; the error itself goes to the log.
func (s *Scanner) sentence(ctx context.Context, id string, err error) string {
	if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		return "已取消"
	}
	s.log.Warn("smart scan source failed", "source", id, "err", err)
	var ue *cleanup.UserError
	if errors.As(err, &ue) {
		return ue.Message
	}
	if errors.Is(err, cleanup.ErrBusy) {
		return "这部分正在扫描或清理，请稍后再试"
	}
	return scanFailed
}

// part is the share of a clean that goes to one source.
type part struct {
	source int
	req    cleanup.CleanRequest
	// cats are the merged IDs of the categories in req, in the same order.
	cats   []string
	titles []string
	// counts are how many items of each category req names.
	counts []int
	items  int
}

// plan resolves req against the last merged scan into one request per source, in source order, every item named.
func (s *Scanner) plan(req cleanup.CleanRequest) ([]part, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil {
		return nil, 0, cleanup.ErrNoScan
	}
	if s.pending {
		return nil, 0, fmt.Errorf("%w: the last scan did not finish", cleanup.ErrStale)
	}
	picked := map[string]bool{}
	for _, id := range req.CategoryIDs {
		if _, ok := s.last.cats[id]; !ok {
			return nil, 0, fmt.Errorf("%w: category %q", cleanup.ErrStale, id)
		}
		picked[id] = true
	}
	named := map[string]bool{}
	for _, id := range req.ItemIDs {
		named[id] = true
	}
	// Decided once: named shrinks as items are found, and must not turn into "every item" for the categories after the last named one.
	filter := len(named) > 0
	parts := make([]part, len(s.sources))
	total := 0
	for _, id := range s.last.order {
		if !picked[id] {
			continue
		}
		o := s.last.cats[id]
		var items []string
		for _, it := range o.items {
			if !filter || named[it] {
				items = append(items, it)
				delete(named, it)
			}
		}
		if len(items) == 0 {
			continue
		}
		p := &parts[o.source]
		p.source = o.source
		p.req.Mode = req.Mode
		p.req.CategoryIDs = append(p.req.CategoryIDs, o.id)
		p.req.ItemIDs = append(p.req.ItemIDs, items...)
		p.cats = append(p.cats, id)
		p.titles = append(p.titles, o.title)
		p.counts = append(p.counts, len(items))
		p.items += len(items)
		total += len(items)
	}
	for id := range named {
		return nil, 0, fmt.Errorf("%w: item %q", cleanup.ErrStale, id)
	}
	if total == 0 {
		return nil, 0, cleanup.ErrNothingSelected
	}
	return slices.DeleteFunc(parts, func(p part) bool { return p.items == 0 }), total, nil
}

// Clean cleans what req names from the last merged scan, source by source; see the package documentation. It returns an error, and deletes nothing, only when the request cannot be planned (cleanup.ErrBadMode, ErrBusy, ErrNoScan, ErrStale, ErrNothingSelected) or the first source refuses it.
func (s *Scanner) Clean(ctx context.Context, req cleanup.CleanRequest, progress func(cleanup.Progress)) (cleanup.CleanResult, error) {
	if err := cleanup.CheckMode(req.Mode); err != nil {
		return cleanup.CleanResult{}, err
	}
	if !s.busy.TryLock() {
		return cleanup.CleanResult{}, cleanup.ErrBusy
	}
	defer s.busy.Unlock()
	if progress == nil {
		progress = func(cleanup.Progress) {}
	}
	parts, total, err := s.plan(req)
	if err != nil {
		return cleanup.CleanResult{}, err
	}
	res := cleanup.CleanResult{StartedAt: s.now(), Failed: []cleanup.Failure{}, Categories: []cleanup.CategoryOutcome{}}
	done := 0
	ran := false
	onMoved := cleanup.OnMoved(ctx)
	var handled []string
	for _, p := range parts {
		src := s.sources[p.source]
		if ctx.Err() != nil {
			res.Interrupted = true
			res.Categories = append(res.Categories, untouched(src.id, p)...)
			continue
		}
		base, baseBytes := done, res.Freed+res.Trashed
		srcCtx := cleanup.WithOnMoved(ctx, func(m cleanup.Moved) {
			m.CategoryID = MergedID(src.id, m.CategoryID)
			onMoved(m)
		})
		r, err := src.scanner.Clean(srcCtx, p.req, func(ev cleanup.Progress) {
			out := cleanup.Progress{Phase: cleanup.PhaseCleaning, Path: ev.Path, Done: base + min(ev.Done, p.items), Total: total, Bytes: baseBytes + ev.Bytes}
			if ev.CategoryID != "" {
				out.CategoryID = MergedID(src.id, ev.CategoryID)
			}
			progress(out)
		})
		if err != nil {
			if !ran {
				return cleanup.CleanResult{}, err
			}
			// Earlier sources have deleted their part, so the clean goes on; this part is reported as left in place, one failure per category.
			s.log.Warn("smart clean source refused its part", "source", src.id, "err", err)
			outs := untouched(src.id, p)
			for i := range outs {
				outs[i].Failed = p.counts[i]
				res.Failed = append(res.Failed, cleanup.Failure{CategoryID: p.cats[i], Reason: fmt.Sprintf("「%s」没有清理：扫描结果已过期，请重新扫描", p.titles[i]), Detail: err.Error()})
			}
			res.Categories = append(res.Categories, outs...)
			done += p.items
			continue
		}
		ran = true
		done += p.items
		handled = append(handled, handledItems(p, r)...)
		res.Freed += r.Freed
		res.Trashed += r.Trashed
		res.Removed += r.Removed
		res.Skipped += r.Skipped
		res.Interrupted = res.Interrupted || r.Interrupted
		for _, f := range r.Failed {
			f.CategoryID = MergedID(src.id, f.CategoryID)
			res.Failed = append(res.Failed, f)
		}
		for _, c := range r.Categories {
			c.CategoryID = MergedID(src.id, c.CategoryID)
			c.Source = src.id
			res.Categories = append(res.Categories, c)
		}
		for _, m := range r.Moved {
			m.CategoryID = MergedID(src.id, m.CategoryID)
			res.Moved = append(res.Moved, m)
		}
	}
	progress(cleanup.Progress{Phase: cleanup.PhaseCleaning, Done: min(done, total), Total: total, Bytes: res.Freed + res.Trashed})
	res.Partial = res.Interrupted || len(res.Failed) > 0
	res.Duration = s.now().Sub(res.StartedAt)
	res.Handled = handled
	s.forget(handled)
	return res, nil
}

// handledItems are the items of part p its source dropped from its own remembered scan in r, so the merged scan never names an item the source has forgotten: what the source lists in CleanResult.Handled and CleanResult.Moved and, when the clean was not interrupted, every planned item that did not fail. After an interrupted clean the items not reached stay in the merged scan, as they stay in the source's.
func handledItems(p part, r cleanup.CleanResult) []string {
	seen := map[string]bool{}
	var ids []string
	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if !r.Interrupted {
		failed := make(map[string]bool, len(r.Failed))
		for _, f := range r.Failed {
			failed[f.ItemID] = true
		}
		for _, id := range p.req.ItemIDs {
			if !failed[id] {
				add(id)
			}
		}
	}
	for _, id := range r.Handled {
		add(id)
	}
	for _, m := range r.Moved {
		add(m.ItemID)
	}
	return ids
}

// forget drops the handled items from the merged scan, and a command category whose command ran as a whole, so the rest can be cleaned without another scan.
func (s *Scanner) forget(ids []string) {
	if len(ids) == 0 {
		return
	}
	gone := make(map[string]bool, len(ids))
	for _, id := range ids {
		gone[id] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil {
		return
	}
	next := &merged{cats: make(map[string]origin, len(s.last.cats))}
	for _, id := range s.last.order {
		o := s.last.cats[id]
		had := len(o.items)
		o.items = slices.DeleteFunc(slices.Clone(o.items), func(it string) bool { return gone[it] })
		if o.command && had > 0 && len(o.items) == 0 {
			continue
		}
		next.order = append(next.order, id)
		next.cats[id] = o
	}
	s.last = next
}

// untouched is the outcome of a part that was not cleaned: one empty line per category.
func untouched(sourceID string, p part) []cleanup.CategoryOutcome {
	out := make([]cleanup.CategoryOutcome, len(p.cats))
	for i, id := range p.cats {
		out[i] = cleanup.CategoryOutcome{CategoryID: id, Title: p.titles[i], Source: sourceID}
	}
	return out
}
