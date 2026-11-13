package health

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestProcessSourceSeesOwnProcess reads the real process table (read-only): the test binary must appear with a create time, a name and CPU time that grows while it works.
func TestProcessSourceSeesOwnProcess(t *testing.T) {
	src := NewProcessSource()
	ctx := context.Background()
	self := int32(os.Getpid())

	find := func() ProcessSample {
		t.Helper()
		snap, err := src.Sample(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if snap.At.IsZero() || len(snap.Processes) < 2 {
			t.Fatalf("snapshot at %v with %d processes", snap.At, len(snap.Processes))
		}
		for _, p := range snap.Processes {
			if p.PID == self {
				return p
			}
		}
		t.Fatalf("own PID %d missing from %d processes", self, len(snap.Processes))
		return ProcessSample{}
	}

	first := find()
	if first.Name == "" || first.CreateTime.IsZero() || first.CreateTime.After(time.Now()) {
		t.Fatalf("own process %+v", first)
	}
	for deadline := time.Now().Add(200 * time.Millisecond); time.Now().Before(deadline); {
		// Burn CPU so the cumulative time visibly grows.
	}
	second := find()
	if second.CPUTime <= first.CPUTime || !second.CreateTime.Equal(first.CreateTime) || second.Name != first.Name {
		t.Fatalf("first %+v, second %+v", first, second)
	}
}

func TestSourceHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewProcessSource().Sample(ctx); err == nil {
		t.Fatal("Sample succeeded with a cancelled context")
	}
}

// BenchmarkProcessSourceSample measures one real sample of the process table, which the monitor takes every Interval.
func BenchmarkProcessSourceSample(b *testing.B) {
	src := NewProcessSource()
	ctx := context.Background()
	var n int
	for b.Loop() {
		snap, err := src.Sample(ctx)
		if err != nil {
			b.Fatal(err)
		}
		n = len(snap.Processes)
	}
	b.ReportMetric(float64(n), "processes")
}
