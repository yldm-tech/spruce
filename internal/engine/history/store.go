package history

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// Open returns a Store keeping its runs in the file at path (JSON Lines, one Run per line, appended). Open performs no I/O: the directory and the file are created on the first Append, and a missing file lists as empty.
func Open(path string) (Store, error) {
	if path == "" {
		return nil, errors.New("history: empty path")
	}
	return &fileStore{path: filepath.Clean(path), count: -1, log: slog.Default().With("component", "history")}, nil
}

const (
	fileMode = 0o600
	dirMode  = 0o700
)

type fileStore struct {
	path string
	log  *slog.Logger

	mu sync.Mutex
	// count is the number of valid runs in the file, or -1 until the first Append reads it.
	count int
}

// Append writes run as one line with a single write, fsyncs it, and trims the file to the newest MaxRuns runs once it holds more.
func (s *fileStore) Append(run Run) error {
	line, err := json.Marshal(run)
	if err != nil {
		return fmt.Errorf("history: encode run %q: %w", run.ID, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.count < 0 {
		runs, err := s.readLocked()
		if err != nil {
			return err
		}
		s.count = len(runs)
	}
	if err := s.appendLocked(line); err != nil {
		return err
	}
	s.count++
	if s.count > MaxRuns {
		return s.trimLocked()
	}
	return nil
}

func (s *fileStore) appendLocked(line []byte) error {
	if err := os.MkdirAll(filepath.Dir(s.path), dirMode); err != nil {
		return fmt.Errorf("history: create directory: %w", err)
	}
	f, err := os.OpenFile(s.path, os.O_RDWR|os.O_CREATE|os.O_APPEND, fileMode)
	if err != nil {
		return fmt.Errorf("history: open: %w", err)
	}
	defer f.Close()

	// A crash during an earlier write can leave a partial last line without its newline; start on a fresh line so this run is not glued onto it.
	buf := make([]byte, 0, len(line)+2)
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("history: stat: %w", err)
	}
	if size := info.Size(); size > 0 {
		last := make([]byte, 1)
		if _, err := f.ReadAt(last, size-1); err != nil {
			return fmt.Errorf("history: read last byte: %w", err)
		}
		if last[0] != '\n' {
			buf = append(buf, '\n')
		}
	}
	buf = append(buf, line...)
	buf = append(buf, '\n')
	if _, err := f.Write(buf); err != nil {
		return fmt.Errorf("history: write: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("history: sync: %w", err)
	}
	return f.Close()
}

// trimLocked rewrites the file with the newest MaxRuns valid runs through a temporary file in the same directory and a rename, so a crash leaves either the old or the new file.
func (s *fileStore) trimLocked() error {
	runs, err := s.readLocked()
	if err != nil {
		return err
	}
	if len(runs) > MaxRuns {
		runs = runs[len(runs)-MaxRuns:]
	}

	var buf bytes.Buffer
	for _, r := range runs {
		line, err := json.Marshal(r)
		if err != nil {
			return fmt.Errorf("history: encode run %q: %w", r.ID, err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("history: create temporary file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("history: write temporary file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("history: sync temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("history: close temporary file: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("history: replace file: %w", err)
	}
	s.count = len(runs)
	return nil
}

// List returns up to limit runs, newest first; limit <= 0 means all (never more than MaxRuns).
func (s *fileStore) List(limit int) ([]Run, error) {
	s.mu.Lock()
	runs, err := s.readLocked()
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if len(runs) > MaxRuns {
		runs = runs[len(runs)-MaxRuns:]
	}
	slices.Reverse(runs)
	if limit > 0 && len(runs) > limit {
		runs = runs[:limit]
	}
	return runs, nil
}

// readLocked returns the file's valid runs, oldest first. A missing file has none; a line that does not decode (such as a last line cut short by a crash) is logged and skipped.
func (s *fileStore) readLocked() ([]Run, error) {
	f, err := os.Open(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return []Run{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("history: open: %w", err)
	}
	defer f.Close()

	runs := []Run{}
	r := bufio.NewReader(f)
	for lineNo := 1; ; lineNo++ {
		line, err := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var run Run
			if derr := json.Unmarshal(line, &run); derr != nil {
				s.log.Warn("skipping unreadable history line", "path", s.path, "line", lineNo, "err", derr)
			} else {
				runs = append(runs, run)
			}
		}
		if errors.Is(err, io.EOF) {
			return runs, nil
		}
		if err != nil {
			return nil, fmt.Errorf("history: read: %w", err)
		}
	}
}
