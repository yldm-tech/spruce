package undo

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/history"
)

// maxJournalLine bounds one line of a journal: a move with two long paths fits many times over.
const maxJournalLine = 1 << 20

// JournalMeta is what a run's journal knows before its clean ends, enough to write the history run if Spruce ends mid-clean.
type JournalMeta struct {
	RunID   string       `json:"runId"`
	Kind    history.Kind `json:"kind"`
	Rule    string       `json:"rule,omitempty"`
	Started time.Time    `json:"started"`
}

// Journal is the on-disk record of a clean that is still running: one line per move, written and fsynced as it happens. Safe for concurrent use.
type Journal struct {
	l     *Ledger
	runID string

	mu sync.Mutex
	f  *os.File
}

// Begin starts <dir>/<runID>.partial: one JSON line with meta, then Journal.Add appends one line per move (write and fsync; a few hundred microseconds per item, small next to a Trash move). Moves without a restorable TrashedPath are journaled too, for the history count. Record (or Close and Recover) ends it.
func (l *Ledger) Begin(meta JournalMeta) (*Journal, error) {
	if !validRunID(meta.RunID) {
		return nil, fmt.Errorf("%w: %q", ErrBadRunID, meta.RunID)
	}
	line, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("undo: encode journal: %w", err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(l.dir, dirMode); err != nil {
		return nil, fmt.Errorf("undo: create folder: %w", err)
	}
	f, err := os.OpenFile(l.journalPath(meta.RunID), os.O_WRONLY|os.O_CREATE|os.O_EXCL|os.O_APPEND, fileMode)
	if err != nil {
		return nil, fmt.Errorf("undo: start journal: %w", err)
	}
	j := &Journal{l: l, runID: meta.RunID, f: f}
	if err := j.writeLocked(line); err != nil {
		f.Close()
		os.Remove(f.Name())
		return nil, err
	}
	l.open[meta.RunID] = true
	return j, nil
}

// Add appends m to the journal and syncs it to disk.
func (j *Journal) Add(m cleanup.Moved) error {
	line, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("undo: encode move: %w", err)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.f == nil {
		return errors.New("undo: the journal is closed")
	}
	return j.writeLocked(line)
}

func (j *Journal) writeLocked(line []byte) error {
	if _, err := j.f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("undo: write journal: %w", err)
	}
	if err := syncJournal(j.f); err != nil {
		return fmt.Errorf("undo: sync journal: %w", err)
	}
	return nil
}

// Close closes the journal's file, leaving it on disk; Record removes it. Closing twice is harmless.
func (j *Journal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.f == nil {
		return nil
	}
	err := j.f.Close()
	j.f = nil
	return err
}

// Recovered is a run whose journal outlived Spruce: what it moved before Spruce ended.
type Recovered struct {
	Meta  JournalMeta
	Moves []cleanup.Moved
	// Kept is how many moves the ledger now holds as restorable.
	Kept int
	// Ended is when the journal was last written, the best known end of the run.
	Ended time.Time
}

// Recover turns every leftover journal (Spruce ended before Record) into a ledger entry and returns what each run moved, so the caller appends the history run that is missing; then it removes the journals. Journals this ledger began and has not finished are left alone. A line that does not decode (the last one cut short) is ignored; a journal without its first line is removed.
func (l *Ledger) Recover() ([]Recovered, error) {
	l.mu.Lock()
	names, err := os.ReadDir(l.dir)
	l.mu.Unlock()
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("undo: read ledger: %w", err)
	}
	var out []Recovered
	var errs []error
	for _, n := range names {
		name := n.Name()
		if !n.Type().IsRegular() || !strings.HasSuffix(name, journalExt) {
			continue
		}
		runID := strings.TrimSuffix(name, journalExt)
		l.mu.Lock()
		running := l.open[runID]
		l.mu.Unlock()
		if running || !validRunID(runID) {
			continue
		}
		path := filepath.Join(l.dir, name)
		rec, ok, err := readJournal(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !ok || rec.Meta.RunID != runID {
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, err)
			}
			continue
		}
		kept, err := l.Record(runID, rec.Ended, rec.Moves)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		rec.Kept = kept
		out = append(out, rec)
	}
	return out, errors.Join(errs...)
}

// readJournal decodes the journal at path; ok is false when its first line is not a JournalMeta.
func readJournal(path string) (Recovered, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return Recovered{}, false, fmt.Errorf("undo: open journal: %w", err)
	}
	defer f.Close()
	var rec Recovered
	if st, err := f.Stat(); err == nil {
		rec.Ended = st.ModTime()
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), maxJournalLine)
	first := true
	for sc.Scan() {
		if first {
			first = false
			if err := json.Unmarshal(sc.Bytes(), &rec.Meta); err != nil || rec.Meta.RunID == "" {
				return Recovered{}, false, nil
			}
			continue
		}
		var m cleanup.Moved
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil || m.Path == "" {
			continue
		}
		rec.Moves = append(rec.Moves, m)
	}
	if first {
		return Recovered{}, false, nil
	}
	// A line longer than the buffer ends the scan; what came before still counts.
	if rec.Ended.IsZero() {
		rec.Ended = rec.Meta.Started
	}
	return rec, true, nil
}
