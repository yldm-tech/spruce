package schedule_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/schedule"
)

const gb = int64(1) << 30

func TestStateStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "background.json")
	s := schedule.OpenState(path)
	st, err := s.Load()
	if err != nil || st.Version != schedule.StateVersion || st.RuleRuns == nil || len(st.RuleRuns) != 0 || !st.LastSummary.IsZero() || st.LowDisk != (schedule.LowDisk{}) {
		t.Fatalf("missing file: %+v, %v", st, err)
	}
	st.RuleRuns["app-caches"] = t0
	st.LastSummary = t0.Add(-time.Hour)
	st.LowDisk = schedule.LowDisk{LastAt: t0, LastFree: 3 * gb, Armed: false}
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	got, err := schedule.OpenState(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if !got.RuleRuns["app-caches"].Equal(t0) || !got.LastSummary.Equal(t0.Add(-time.Hour)) || got.LowDisk.LastFree != 3*gb || !got.LowDisk.LastAt.Equal(t0) {
		t.Errorf("loaded %+v", got)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("file mode %v, %v", info.Mode(), err)
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("left temporary files: %v", entries)
	}
}

func TestStateStoreIgnoresBadFiles(t *testing.T) {
	for name, body := range map[string]string{
		"corrupt":       "{not json",
		"other version": `{"version":9,"ruleRuns":{"app-caches":"2026-11-01T00:00:00Z"}}`,
		"empty":         "",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "background.json")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			st, err := schedule.OpenState(path).Load()
			if err != nil || len(st.RuleRuns) != 0 || st.RuleRuns == nil || !st.LastSummary.IsZero() {
				t.Errorf("state %+v, %v", st, err)
			}
		})
	}
}

func TestLowDiskAlert(t *testing.T) {
	threshold := 20 * gb
	type step struct {
		free   int64
		at     time.Duration
		notify bool
	}
	for name, steps := range map[string][]step{
		"first reading below alerts once": {
			{free: 15 * gb, notify: true},
			{free: 14 * gb, at: time.Hour},
			{free: 14 * gb, at: 30 * time.Hour},
		},
		"above the threshold never alerts": {
			{free: 25 * gb},
			{free: 20 * gb, at: time.Hour},
		},
		"a further 5 GB drop alerts again after a day": {
			{free: 15 * gb, notify: true},
			{free: 9 * gb, at: time.Hour},
			{free: 9 * gb, at: 25 * time.Hour, notify: true},
			{free: 6 * gb, at: 50 * time.Hour},
			{free: 4 * gb, at: 75 * time.Hour, notify: true},
		},
		"rising just above the threshold does not re-arm": {
			{free: 15 * gb, notify: true},
			{free: 21 * gb, at: 30 * time.Hour},
			{free: 15 * gb, at: 31 * time.Hour},
		},
		"rising 2 GB above re-arms": {
			{free: 15 * gb, notify: true},
			{free: 22 * gb, at: 30 * time.Hour},
			{free: 19 * gb, at: 31 * time.Hour, notify: true},
		},
		"a re-armed crossing still waits a day": {
			{free: 15 * gb, notify: true},
			{free: 23 * gb, at: time.Hour},
			{free: 19 * gb, at: 2 * time.Hour},
			{free: 19 * gb, at: 25 * time.Hour, notify: true},
		},
		"an alert in the future does not block": {
			{free: 15 * gb, at: 48 * time.Hour, notify: true},
			{free: 22 * gb, at: 49 * time.Hour},
			{free: 15 * gb, notify: true},
		},
	} {
		t.Run(name, func(t *testing.T) {
			var st schedule.LowDisk
			for i, s := range steps {
				var notify bool
				notify, st = schedule.LowDiskAlert(st, s.free, threshold, t0.Add(s.at))
				if notify != s.notify {
					t.Fatalf("step %d (%d GB at %v): notify %v", i, s.free/gb, s.at, notify)
				}
			}
		})
	}
	if notify, _ := schedule.LowDiskAlert(schedule.LowDisk{}, 0, 0, t0); notify {
		t.Error("alert without a threshold")
	}
}
