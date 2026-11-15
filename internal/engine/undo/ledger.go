package undo

import (
	"cmp"
	"context"
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

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/trash"
)

const (
	// Version is the format of an entry file.
	Version = 1
	// Window is how long after a move it can be put back.
	Window = 30 * 24 * time.Hour
	// MaxEntries, MaxBytes and Window bound what Prune keeps.
	MaxEntries = 200
	MaxBytes   = 32 << 20
	// MaxMoves is how many moves one entry keeps; a larger clean keeps the first MaxMoves and is marked Partial.
	MaxMoves = 20000
	// orphanGrace is how long Prune keeps an entry whose run keep rejects, counted from when its file was last written: a clean (or Recover) writes its entry just before the history run, which a prune in between must not outrun.
	orphanGrace = time.Hour
)

// File names inside the ledger's folder.
const (
	entryExt     = ".json"
	journalExt   = ".partial"
	restoredFile = "restored.json"
	tmpPattern   = ".*.tmp"
	dirMode      = 0o700
	fileMode     = 0o600
)

var (
	// ErrNoEntry is returned for a run the ledger holds nothing restorable of: never recorded, pruned, cleared, or older than Window. It matches fs.ErrNotExist.
	ErrNoEntry = fmt.Errorf("undo: nothing to put back for this run: %w", fs.ErrNotExist)
	// ErrRestoring is returned while the same run is being put back already.
	ErrRestoring = errors.New("undo: this run is being put back already")
	// ErrBadRunID is returned for a run ID that cannot name a ledger file.
	ErrBadRunID = errors.New("undo: invalid run ID")
	// ErrNotSaved is returned (joined) by Restore when the items went back but the entry or restored.json could not be rewritten.
	ErrNotSaved = errors.New("undo: the ledger could not record what was put back")
)

// Move is one move of an entry, and whether it was put back.
type Move struct {
	cleanup.Moved
	Restored   bool   `json:"restored,omitempty"`
	RestoredTo string `json:"restoredTo,omitempty"`
	// Tried is set when a 放回原处 tried this move and could not put it back (not when it stopped before): the run is then no longer offered for 放回原处, which never retries by itself.
	Tried bool `json:"tried,omitempty"`
}

// open reports whether m is still offered for 放回原处: neither put back nor tried.
func (m Move) open() bool { return !m.Restored && !m.Tried }

// Entry is what the ledger keeps of one run.
type Entry struct {
	Version int       `json:"version"`
	RunID   string    `json:"runId"`
	At      time.Time `json:"at"`
	// Partial is set when the run moved more than MaxMoves items; the first MaxMoves are kept.
	Partial bool   `json:"partial,omitempty"`
	Moves   []Move `json:"moves"`
}

// Outcome is what Restore did with one move.
type Outcome struct {
	Move Move
	// To is where the item went back to, "" when it did not.
	To string
	// Reason is a sentence for the page, "" on success.
	Reason string
	// Err is the cause, for the log; nil on success.
	Err error
}

// Ledger is the undo ledger in one folder. Safe for concurrent use within a process.
type Ledger struct {
	dir string

	mu sync.Mutex
	// open are the runs whose journal this ledger began and has not finished, which Recover leaves alone.
	open map[string]bool
	// restoring are the runs Restore is putting back.
	restoring map[string]bool
}

// Open returns the ledger in dir (<data>/undo). Nothing is read or written until first use; the folder is made (0700) on the first write.
func Open(dir string) (*Ledger, error) {
	if dir == "" || !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("undo: the ledger folder must be an absolute path, got %q", dir)
	}
	return &Ledger{dir: filepath.Clean(dir), open: map[string]bool{}, restoring: map[string]bool{}}, nil
}

// Keepable reports whether Spruce can put m back later: the system said where the item went, directly inside a Trash folder the restorer accepts (trash.InTrash; never on Windows), with its identity, and the original lies strictly inside the recorded root.
func Keepable(m cleanup.Moved) bool { return keepable(m, trash.InTrash) }

// keepable is Keepable with inTrash in place of trash.InTrash, so a run of moves reads the Trash folders once (trash.InTrashNow).
func keepable(m cleanup.Moved, inTrash func(string) bool) bool {
	if m.Ino == 0 || m.Root == "" || !inTrash(m.TrashedPath) {
		return false
	}
	rel, err := filepath.Rel(m.Root, m.Path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// Kept returns how many of moves, and how many bytes, Record keeps: the Keepable ones, at most MaxMoves.
func Kept(moves []cleanup.Moved) (items int, bytes int64) {
	inTrash := trash.InTrashNow()
	for _, m := range moves {
		if items == MaxMoves {
			break
		}
		if keepable(m, inTrash) {
			items++
			bytes += m.Size
		}
	}
	return items, bytes
}

// validRunID reports whether id can name a ledger file: letters, digits and hyphens, as app.newRunID makes them.
func validRunID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if (r < '0' || r > '9') && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && r != '-' {
			return false
		}
	}
	return true
}

func (l *Ledger) entryPath(runID string) string   { return filepath.Join(l.dir, runID+entryExt) }
func (l *Ledger) journalPath(runID string) string { return filepath.Join(l.dir, runID+journalExt) }

// Record keeps the moves of runID that Spruce can put back (Keepable, at most MaxMoves) as its entry, made at, and returns how many it kept; nothing to keep writes nothing. Either way it removes the run's journal.
func (l *Ledger) Record(runID string, at time.Time, moves []cleanup.Moved) (kept int, err error) {
	if !validRunID(runID) {
		return 0, fmt.Errorf("%w: %q", ErrBadRunID, runID)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	defer func() {
		delete(l.open, runID)
		if rerr := os.Remove(l.journalPath(runID)); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) && err == nil {
			err = fmt.Errorf("undo: remove journal: %w", rerr)
		}
	}()
	e := Entry{Version: Version, RunID: runID, At: at.UTC()}
	inTrash := trash.InTrashNow()
	for _, m := range moves {
		if !keepable(m, inTrash) {
			continue
		}
		if len(e.Moves) == MaxMoves {
			e.Partial = true
			break
		}
		e.Moves = append(e.Moves, Move{Moved: m})
	}
	if len(e.Moves) == 0 {
		return 0, nil
	}
	if err := l.writeJSON(l.entryPath(runID), e); err != nil {
		return 0, err
	}
	return len(e.Moves), nil
}

// Load returns the entry of runID; ErrNoEntry when there is none.
func (l *Ledger) Load(runID string) (Entry, error) {
	if !validRunID(runID) {
		return Entry{}, fmt.Errorf("%w: %q", ErrBadRunID, runID)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.loadLocked(runID)
}

func (l *Ledger) loadLocked(runID string) (Entry, error) {
	data, err := os.ReadFile(l.entryPath(runID))
	if errors.Is(err, fs.ErrNotExist) {
		return Entry{}, ErrNoEntry
	}
	if err != nil {
		return Entry{}, fmt.Errorf("undo: read entry: %w", err)
	}
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil || e.Version != Version || e.RunID != runID {
		// A damaged or foreign entry cannot be trusted to put anything back.
		return Entry{}, ErrNoEntry
	}
	return e, nil
}

// within reports whether e can still be put back at now.
func within(e Entry, now time.Time) bool { return now.Sub(e.At) <= Window }

// Restorable reports how many moves of runID, and how many bytes, 放回原处 still offers at now (neither put back nor tried by an earlier 放回原处 that ran to its end); ok is false when none.
func (l *Ledger) Restorable(runID string, now time.Time) (items int, bytes int64, ok bool) {
	e, err := l.Load(runID)
	if err != nil || !within(e, now) {
		return 0, 0, false
	}
	for _, m := range e.Moves {
		if m.open() {
			items++
			bytes += m.Size
		}
	}
	return items, bytes, items > 0
}

// Pending counts the moves of the entries within Window at now that 放回原处 still offers (as Restorable), for the 清空废纸篓 dialog.
func (l *Ledger) Pending(now time.Time) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, id := range l.entryIDsLocked() {
		e, err := l.loadLocked(id)
		if err != nil || !within(e, now) {
			continue
		}
		for _, m := range e.Moves {
			if m.open() {
				n++
			}
		}
	}
	return n
}

// entryIDsLocked lists the run IDs of the entry files.
func (l *Ledger) entryIDsLocked() []string {
	names, err := os.ReadDir(l.dir)
	if err != nil {
		return nil
	}
	var ids []string
	for _, n := range names {
		name := n.Name()
		if !n.Type().IsRegular() || name == restoredFile || !strings.HasSuffix(name, entryExt) {
			continue
		}
		if id := strings.TrimSuffix(name, entryExt); validRunID(id) {
			ids = append(ids, id)
		}
	}
	return ids
}

// keepBoth reports whether m comes back under another name when its place is taken: the user's own files (installers, large files, an app and its leftovers, 空间透视's moves); never caches, logs and build output, which the apps make again.
func keepBoth(m cleanup.Moved) bool {
	switch m.Kind {
	case cleanup.KindInstaller, cleanup.KindLargeFile, cleanup.KindLeftover:
		return true
	}
	return m.CategoryID == SpaceCategory
}

// SpaceCategory is the CategoryID 空间透视 records its moves under.
const SpaceCategory = "space"

// regenerated reports whether m is something its app makes again (caches, logs, build output), so a taken place means new content rather than another item of the user's.
func regenerated(m cleanup.Moved) bool {
	switch m.Kind {
	case cleanup.KindCache, cleanup.KindLog, cleanup.KindDevCache, cleanup.KindProjectArtifact:
		return true
	}
	return false
}

// Reason is the sentence the page shows for a failure of Restorer.Restore on m.
func Reason(m cleanup.Moved, err error) string {
	switch {
	case errors.Is(err, trash.ErrGone):
		return "已从废纸篓清除，无法放回"
	case errors.Is(err, trash.ErrOccupied):
		if regenerated(m) {
			return "原位置已有新生成的内容，未放回"
		}
		return "原位置已有同名项目"
	case errors.Is(err, trash.ErrParentGone):
		return "原来的文件夹已不存在"
	case errors.Is(err, trash.ErrReplaced):
		return "废纸篓里的项目已被替换，未放回"
	case errors.Is(err, fs.ErrPermission):
		return "没有权限放回，可以在废纸篓里手动放回"
	case errors.Is(err, trash.ErrRestoreUnsupported):
		return "这台电脑上不能放回原处"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "已取消"
	}
	return "没能放回，可以在废纸篓里手动放回"
}

// Restore puts back every move of runID that is still open (neither put back nor tried in vain by an earlier 放回原处) through r, one at a time, then rewrites the entry (restored moves, and tried ones that failed for another reason than ctx) and adds every restored move's item ID to restored.json (of its original path and, when it came back under another name, of that path too). Moves are independent: one that fails is reported with its reason and the rest go on; nothing is overwritten or retried. KeepBoth is set for the user's own files (installers, large files, an app and its leftovers, 空间透视's moves), never for caches, logs and build output. The error is non-nil only for a missing or expired entry (ErrNoEntry), a restore of the same run already going on (ErrRestoring), ctx (the outcomes then hold what was done before it ended) or a ledger that could not be rewritten afterwards (ErrNotSaved, with every outcome).
func (l *Ledger) Restore(ctx context.Context, runID string, r trash.Restorer) ([]Outcome, error) {
	if !validRunID(runID) {
		return nil, fmt.Errorf("%w: %q", ErrBadRunID, runID)
	}
	l.mu.Lock()
	if l.restoring[runID] {
		l.mu.Unlock()
		return nil, ErrRestoring
	}
	e, err := l.loadLocked(runID)
	if err == nil && !within(e, time.Now()) {
		err = ErrNoEntry
	}
	if err != nil {
		l.mu.Unlock()
		return nil, err
	}
	l.restoring[runID] = true
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		delete(l.restoring, runID)
		l.mu.Unlock()
	}()

	var outs []Outcome
	var ctxErr error
	for _, m := range e.Moves {
		if !m.open() {
			continue
		}
		if err := ctx.Err(); err != nil {
			ctxErr = err
			break
		}
		to, err := r.Restore(ctx, trash.RestoreRequest{TrashedPath: m.TrashedPath, Original: m.Path, Root: m.Root, Dir: m.Dir, Dev: m.Dev, Ino: m.Ino,
			KeepBoth: keepBoth(m.Moved)})
		o := Outcome{Move: m, To: to, Err: err}
		if err != nil {
			o.Reason = Reason(m.Moved, err)
		} else {
			o.Move.Restored, o.Move.RestoredTo = true, to
		}
		outs = append(outs, o)
	}

	restored := map[string]string{}
	tried := map[string]bool{}
	var ids []string
	for _, o := range outs {
		switch {
		case o.Err == nil:
			restored[o.Move.Path] = o.To
			ids = append(ids, cleanup.ItemID(o.Move.Path))
			// An item put back under another name (KeepBoth) is known to the scanners by that new path, so its ID is remembered too and an automatic rule does not take it again.
			if o.To != "" && filepath.Clean(o.To) != filepath.Clean(o.Move.Path) {
				ids = append(ids, cleanup.ItemID(o.To))
			}
		case !errors.Is(o.Err, context.Canceled) && !errors.Is(o.Err, context.DeadlineExceeded):
			tried[o.Move.Path] = true
		}
	}
	if len(restored) > 0 || len(tried) > 0 {
		if err := l.markRestored(runID, restored, tried, ids, time.Now()); err != nil {
			// The items are back either way; the caller reports the outcomes and logs this.
			return outs, errors.Join(ctxErr, fmt.Errorf("%w: %w", ErrNotSaved, err))
		}
	}
	return outs, ctxErr
}

// markRestored rewrites runID's entry with the moves restored names put back and those tried names tried in vain (both keyed by original path) and records ids in restored.json.
func (l *Ledger) markRestored(runID string, restored map[string]string, tried map[string]bool, ids []string, now time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	var errs []error
	// The entry is read again: Prune or Clear may have run meanwhile.
	if e, err := l.loadLocked(runID); err == nil {
		for i, m := range e.Moves {
			if to, ok := restored[m.Path]; ok {
				e.Moves[i].Restored, e.Moves[i].RestoredTo, e.Moves[i].Tried = true, to, false
			} else if tried[m.Path] {
				e.Moves[i].Tried = true
			}
		}
		errs = append(errs, l.writeJSON(l.entryPath(runID), e))
	}
	if len(ids) > 0 {
		errs = append(errs, l.addRestoredLocked(ids, now))
	}
	return errors.Join(errs...)
}

// Prune drops the entries older than Window at now, those whose run keep rejects (nil keeps every run) once their file is older than orphanGrace (the history run is appended just after the entry is written), and then the oldest beyond MaxEntries or MaxBytes in all, and returns how many it dropped. Journals of running cleans are left alone. It also drops expired IDs from restored.json.
func (l *Ledger) Prune(now time.Time, keep func(runID string) bool) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	type info struct {
		id   string
		at   time.Time
		size int64
	}
	var live []info
	dropped := 0
	var errs []error
	drop := func(id string) {
		if err := os.Remove(l.entryPath(id)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
			return
		}
		dropped++
	}
	for _, id := range l.entryIDsLocked() {
		e, err := l.loadLocked(id)
		if err != nil || !within(e, now) {
			drop(id)
			continue
		}
		st, err := os.Stat(l.entryPath(id))
		if err != nil {
			continue
		}
		if keep != nil && !keep(id) && now.Sub(st.ModTime()) >= orphanGrace {
			drop(id)
			continue
		}
		live = append(live, info{id: id, at: e.At, size: st.Size()})
	}
	// Newest first; what is past the count or the size goes.
	slices.SortFunc(live, func(a, b info) int { return cmp.Or(b.at.Compare(a.at), strings.Compare(b.id, a.id)) })
	var total int64
	for i, e := range live {
		total += e.size
		if i >= MaxEntries || total > MaxBytes {
			drop(e.id)
		}
	}
	if err := l.pruneRestoredLocked(now); err != nil {
		errs = append(errs, err)
	}
	return dropped, errors.Join(errs...)
}

// Clear deletes every entry and journal, keeping restored.json (it holds only hashes of item IDs, which keep the automatic rules away from what the user put back) and the journals of the cleans this ledger is running, which go on writing to them and record their entry when they end.
func (l *Ledger) Clear() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	names, err := os.ReadDir(l.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("undo: read ledger: %w", err)
	}
	var errs []error
	for _, n := range names {
		name := n.Name()
		if name == restoredFile || !n.Type().IsRegular() || (strings.HasSuffix(name, journalExt) && l.open[strings.TrimSuffix(name, journalExt)]) {
			continue
		}
		if strings.HasSuffix(name, entryExt) || strings.HasSuffix(name, journalExt) || strings.HasSuffix(name, ".tmp") {
			if err := os.Remove(filepath.Join(l.dir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// writeJSON replaces path with v through a temporary file in the same folder, fsynced and renamed; files are 0600 in a 0700 folder.
func (l *Ledger) writeJSON(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("undo: encode: %w", err)
	}
	if err := os.MkdirAll(l.dir, dirMode); err != nil {
		return fmt.Errorf("undo: create folder: %w", err)
	}
	tmp, err := os.CreateTemp(l.dir, filepath.Base(path)+tmpPattern)
	if err != nil {
		return fmt.Errorf("undo: create temporary file: %w", err)
	}
	name := tmp.Name()
	fail := func(step string, err error) error {
		tmp.Close()
		os.Remove(name)
		return fmt.Errorf("undo: %s: %w", step, err)
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
		return fmt.Errorf("undo: close temporary file: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return fmt.Errorf("undo: replace file: %w", err)
	}
	return nil
}
