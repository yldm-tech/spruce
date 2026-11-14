package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func receive(t *testing.T, ch <-chan Run) Run {
	t.Helper()
	select {
	case r, ok := <-ch:
		if !ok {
			t.Fatal("channel closed")
		}
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("no run delivered")
		return Run{}
	}
}

func TestSubscribeDeliversAppendedRuns(t *testing.T) {
	s, _ := open(t)
	w, ok := s.(Watcher)
	if !ok {
		t.Fatal("the file store is not a Watcher")
	}
	ch, stop := w.Subscribe()
	other, stopOther := w.Subscribe()
	for i := range 2 {
		if err := s.Append(run(i)); err != nil {
			t.Fatal(err)
		}
	}
	if got := receive(t, ch); got.ID != "run-0000" || len(got.Results) != 2 {
		t.Errorf("first run %+v", got)
	}
	if got := receive(t, ch); got.ID != "run-0001" {
		t.Errorf("second run %+v", got)
	}
	if got := receive(t, other); got.ID != "run-0000" {
		t.Errorf("other subscriber %+v", got)
	}

	// Prune and Clear record nothing.
	if _, err := s.Prune(base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-ch:
		t.Errorf("unexpected run %+v", r)
	default:
	}

	stop()
	stop()
	if _, ok := <-ch; ok {
		t.Error("channel open after unsubscribe")
	}
	if err := s.Append(run(2)); err != nil {
		t.Fatal(err)
	}
	stopOther()
}

func TestSlowSubscriberNeverBlocksAppend(t *testing.T) {
	s, _ := open(t)
	ch, stop := s.(Watcher).Subscribe()
	defer stop()
	done := make(chan error, 1)
	go func() {
		for i := range WatchBuffer + 5 {
			if err := s.Append(run(i)); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Append blocked on a subscriber that does not read")
	}
	if n := len(ch); n != WatchBuffer {
		t.Errorf("buffered %d runs, want %d", n, WatchBuffer)
	}
	if got := receive(t, ch); got.ID != "run-0000" {
		t.Errorf("oldest kept run %s", got.ID)
	}
}

func TestFailedAppendIsNotAnnounced(t *testing.T) {
	dir := t.TempDir()
	// The history's directory is a file, so the first Append cannot create it.
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(filepath.Join(blocker, "history.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	ch, stop := s.(Watcher).Subscribe()
	defer stop()
	if err := s.Append(run(0)); err == nil {
		t.Fatal("Append succeeded under a file")
	}
	select {
	case r := <-ch:
		t.Errorf("failed append announced %+v", r)
	default:
	}
}
