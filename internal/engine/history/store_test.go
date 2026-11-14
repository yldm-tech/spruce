package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/taskresult"
)

var base = time.Date(2026, 11, 14, 1, 0, 0, 0, time.UTC)

func run(i int) Run {
	start := base.Add(time.Duration(i) * time.Minute)
	return Run{
		ID:        fmt.Sprintf("run-%04d", i),
		Kind:      KindMaintenance,
		StartedAt: start,
		EndedAt:   start.Add(40 * time.Second),
		Results: []taskresult.Result{
			{TaskID: "dns", Outcome: taskresult.Applied, Message: "已刷新 DNS 缓存", Duration: 120 * time.Millisecond},
			{TaskID: "databases", Outcome: taskresult.Skipped, Message: "需要先退出 Safari 和邮件", Detail: "Safari is running"},
		},
	}
}

func open(t *testing.T) (Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nested", "dir", "history.jsonl")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return s, path
}

func ids(runs []Run) string {
	var b []string
	for _, r := range runs {
		b = append(b, r.ID)
	}
	return strings.Join(b, ",")
}

func TestOpenDoesNoIO(t *testing.T) {
	s, path := open(t)
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("Open created the directory: %v", err)
	}
	runs, err := s.List(0)
	if err != nil || runs == nil || len(runs) != 0 {
		t.Fatalf("List on a missing file = %#v, %v; want empty", runs, err)
	}
	if _, err := Open(""); err == nil {
		t.Error("Open accepted an empty path")
	}
}

func TestAppendAndListNewestFirst(t *testing.T) {
	s, path := open(t)
	for i := range 3 {
		if err := s.Append(run(i)); err != nil {
			t.Fatal(err)
		}
	}
	runs, err := s.List(0)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(runs); got != "run-0002,run-0001,run-0000" {
		t.Fatalf("List(0) = %s", got)
	}
	want := run(2)
	got := runs[0]
	if !got.StartedAt.Equal(want.StartedAt) || got.Kind != want.Kind || len(got.Results) != 2 || got.Results[0] != want.Results[0] || got.Results[1] != want.Results[1] {
		t.Errorf("round trip: got %+v, want %+v", got, want)
	}
	if runs, _ := s.List(2); ids(runs) != "run-0002,run-0001" {
		t.Errorf("List(2) = %s", ids(runs))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n"); len(lines) != 3 {
		t.Errorf("file has %d lines, want 3", len(lines))
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("file mode %v, want 0600", perm)
		}
		dir, err := os.Stat(filepath.Dir(path))
		if err != nil {
			t.Fatal(err)
		}
		if perm := dir.Mode().Perm(); perm != 0o700 {
			t.Errorf("directory mode %v, want 0700", perm)
		}
	}
}

func TestTruncatedLastLineIsSkippedAndNotGluedOnto(t *testing.T) {
	s, path := open(t)
	if err := s.Append(run(0)); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash in the middle of writing the next line.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"id":"run-cut","kind":"maint`); err != nil {
		t.Fatal(err)
	}
	f.Close()

	// A fresh Store, as after a restart.
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	runs, err := s.List(0)
	if err != nil || ids(runs) != "run-0000" {
		t.Fatalf("List after crash = %s, %v", ids(runs), err)
	}
	if err := s.Append(run(1)); err != nil {
		t.Fatal(err)
	}
	runs, err = s.List(0)
	if err != nil || ids(runs) != "run-0001,run-0000" {
		t.Fatalf("List after append = %s, %v", ids(runs), err)
	}
}

func TestCorruptMiddleLineIsSkipped(t *testing.T) {
	_, path := open(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	content := `{"id":"a","kind":"maintenance","startedAt":"2026-11-14T01:00:00Z","endedAt":"2026-11-14T01:01:00Z","results":[]}
not json at all

{"id":"b","kind":"maintenance","startedAt":"2026-11-14T02:00:00Z","endedAt":"2026-11-14T02:01:00Z","results":[]}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	runs, err := s.List(0)
	if err != nil || ids(runs) != "b,a" {
		t.Fatalf("List = %s, %v", ids(runs), err)
	}
}

func TestCappedAtMaxRuns(t *testing.T) {
	s, path := open(t)
	// Seed a full file directly (each Append fsyncs, which is slow on macOS), then append past the cap.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	var seed strings.Builder
	for i := range MaxRuns {
		line, err := json.Marshal(run(i))
		if err != nil {
			t.Fatal(err)
		}
		seed.Write(line)
		seed.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(seed.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	total := MaxRuns + 25
	for i := MaxRuns; i < total; i++ {
		if err := s.Append(run(i)); err != nil {
			t.Fatal(err)
		}
	}
	runs, err := s.List(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != MaxRuns || runs[0].ID != run(total-1).ID || runs[MaxRuns-1].ID != run(total-MaxRuns).ID {
		t.Fatalf("kept %d runs, newest %s, oldest %s", len(runs), runs[0].ID, runs[len(runs)-1].ID)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(data), "\n"); n != MaxRuns {
		t.Errorf("file has %d lines, want %d", n, MaxRuns)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("leftover files next to the history: %v", entries)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("rewritten file mode %v, want 0600", perm)
		}
	}

	// A store reopened on a full file trims on its first Append as well.
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append(run(total)); err != nil {
		t.Fatal(err)
	}
	runs, err = s.List(0)
	if err != nil || len(runs) != MaxRuns || runs[0].ID != run(total).ID {
		t.Fatalf("after reopen: %d runs, newest %s, %v", len(runs), runs[0].ID, err)
	}
}

func TestConcurrentAppendAndList(t *testing.T) {
	s, _ := open(t)
	var wg sync.WaitGroup
	for i := range 40 {
		wg.Go(func() {
			if err := s.Append(run(i)); err != nil {
				t.Error(err)
			}
		})
		wg.Go(func() {
			if _, err := s.List(5); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	runs, err := s.List(0)
	if err != nil || len(runs) != 40 {
		t.Fatalf("%d runs, %v", len(runs), err)
	}
}
