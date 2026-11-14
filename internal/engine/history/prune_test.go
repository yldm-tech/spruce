package history

import (
	"os"
	"testing"
	"time"
)

func TestPruneAndClear(t *testing.T) {
	s, path := open(t)
	for i := range 5 {
		r := run(i)
		r.StartedAt = base.AddDate(0, 0, i*10)
		if i == 4 {
			r.Kind = KindSystemJunk
			r.Results = nil
			r.Summary = &Totals{Freed: 1 << 30, Items: 34, Failed: 1}
			r.Details = []string{"系统缓存：已清理 34 项"}
		}
		if err := s.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	// Keep the runs of the last 25 days before the newest (days 20, 30, 40).
	n, err := s.Prune(RetentionCutoff(base.AddDate(0, 0, 40), 25))
	if err != nil || n != 2 {
		t.Fatalf("Prune = %d, %v", n, err)
	}
	runs, err := s.List(0)
	if err != nil || ids(runs) != "run-0004,run-0003,run-0002" {
		t.Fatalf("after prune %s, %v", ids(runs), err)
	}
	if got := runs[0]; got.Summary == nil || got.Summary.Freed != 1<<30 || len(got.Details) != 1 || got.Kind != KindSystemJunk {
		t.Errorf("clean-up run round trip %+v", got)
	}
	if runs[1].Summary != nil || runs[1].Details != nil {
		t.Errorf("maintenance run gained fields: %+v", runs[1])
	}
	if n, err := s.Prune(base); err != nil || n != 0 {
		t.Errorf("second Prune = %d, %v", n, err)
	}

	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file after Clear: %v", err)
	}
	if err := s.Clear(); err != nil {
		t.Errorf("Clear twice: %v", err)
	}
	if err := s.Append(run(9)); err != nil {
		t.Fatal(err)
	}
	if runs, _ := s.List(0); ids(runs) != "run-0009" {
		t.Errorf("after clear and append: %s", ids(runs))
	}
}

func TestRetentionCutoff(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	if got := RetentionCutoff(now, 180); !got.Equal(time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("cutoff %v", got)
	}
}
