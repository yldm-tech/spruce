package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/sysinfo"
)

// countingSource is a sysinfo.Source whose readings carry a growing CPU counter and a fake clock.
type countingSource struct {
	mu    sync.Mutex
	reads int
	at    time.Time
}

func (c *countingSource) Read(context.Context) sysinfo.Readings {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads++
	c.at = c.at.Add(2 * time.Second)
	return sysinfo.Readings{At: c.at, CPU: []sysinfo.CPUTime{{Busy: float64(c.reads), Total: float64(2 * c.reads)}}}
}

func (c *countingSource) Battery(context.Context) (sysinfo.BatteryStats, error) {
	return sysinfo.BatteryStats{}, nil
}

func (c *countingSource) Info(context.Context) sysinfo.SystemInfo {
	return sysinfo.SystemInfo{OSName: "macOS", Model: "Mac16,8"}
}

func (c *countingSource) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reads
}

func newTestSampler(t *testing.T, src sysinfo.Source) *sysinfo.Sampler {
	t.Helper()
	s, err := sysinfo.New(sysinfo.Config{Interval: 5 * time.Millisecond, History: 10, BatteryEvery: time.Minute, TopN: 8}, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSysinfoWatchStreamsUntilThePageCloses(t *testing.T) {
	src := &countingSource{at: time.Now()}
	sampler := newTestSampler(t, src)
	svc := NewSysinfo(sampler)
	if info := svc.Info(context.Background()); info.Model != "Mac16,8" {
		t.Fatalf("Info %+v", info)
	}

	// The page takes three samples and then closes the channel: Send fails and Watch returns.
	closed := errors.New("closed")
	var got []sysinfo.Vitals
	done := make(chan struct{})
	go func() {
		defer close(done)
		watch(context.Background(), sampler, func(v sysinfo.Vitals) error {
			if len(got) == 3 {
				return closed
			}
			got = append(got, v)
			return nil
		})
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("watch did not return after the page closed the channel")
	}
	for i, v := range got {
		if v.CPU == nil || v.CPU.Percent != 50 {
			t.Fatalf("sample %d: %+v", i, v)
		}
	}

	// Nobody watches: sampling stops.
	settled := waitForSettled(t, src)

	// A page coming back gets the kept samples first, then new ones, until it goes away (ctx).
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var again []sysinfo.Vitals
	done = make(chan struct{})
	go func() {
		defer close(done)
		watch(ctx, sampler, func(v sysinfo.Vitals) error {
			mu.Lock()
			defer mu.Unlock()
			again = append(again, v)
			return nil
		})
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		mu.Lock()
		n := len(again)
		mu.Unlock()
		if n >= len(got)+2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("got %d samples on return", n)
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	mu.Lock()
	if !again[0].At.Equal(got[0].At) {
		t.Errorf("history did not come first: %v, want %v", again[0].At, got[0].At)
	}
	mu.Unlock()
	if src.count() <= settled {
		t.Error("sampling did not start again")
	}
	waitForSettled(t, src)
}

// waitForSettled waits until the source stops being read, and returns the number of reads.
func waitForSettled(t *testing.T, src *countingSource) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	last := src.count()
	for {
		time.Sleep(30 * time.Millisecond)
		n := src.count()
		if n == last {
			return n
		}
		if time.Now().After(deadline) {
			t.Fatal("sampling did not stop")
		}
		last = n
	}
}
