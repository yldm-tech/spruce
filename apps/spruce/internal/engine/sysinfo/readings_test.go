package sysinfo

import (
	"math"
	"slices"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestRing(t *testing.T) {
	r := newRing[int](3)
	if _, ok := r.last(); ok || len(r.values()) != 0 {
		t.Fatal("empty ring has values")
	}
	r.push(1)
	r.push(2)
	if got := r.values(); !slices.Equal(got, []int{1, 2}) {
		t.Fatalf("values %v", got)
	}
	for _, v := range []int{3, 4, 5} {
		r.push(v)
	}
	if got := r.values(); !slices.Equal(got, []int{3, 4, 5}) {
		t.Fatalf("after wrapping: %v", got)
	}
	if last, ok := r.last(); !ok || last != 5 || r.len() != 3 {
		t.Fatalf("last %d, len %d", last, r.len())
	}
	// values is a copy: changing it does not change the ring.
	got := r.values()
	got[0] = 99
	if r.values()[0] != 3 {
		t.Fatal("values shares the ring's storage")
	}
	r.push(6)
	if got := r.values(); !slices.Equal(got, []int{4, 5, 6}) {
		t.Fatalf("after another push: %v", got)
	}
	r.reset()
	if _, ok := r.last(); ok || r.len() != 0 {
		t.Fatal("reset kept values")
	}
	r.push(7)
	if got := r.values(); !slices.Equal(got, []int{7}) {
		t.Fatalf("after reset: %v", got)
	}
}

func TestCPUStats(t *testing.T) {
	prev := []CPUTime{{Busy: 10, Total: 100}, {Busy: 50, Total: 100}}
	cur := []CPUTime{{Busy: 11, Total: 102}, {Busy: 52, Total: 102}}
	got, ok := cpuStats(prev, cur)
	if !ok || !near(got.PerCore[0], 50) || !near(got.PerCore[1], 100) || !near(got.Percent, 75) {
		t.Fatalf("%+v %v", got, ok)
	}
	if _, ok := cpuStats(prev, cur[:1]); ok {
		t.Error("a changed CPU count gave a percent")
	}
	if _, ok := cpuStats(nil, cur); ok {
		t.Error("no baseline gave a percent")
	}
	if _, ok := cpuStats(prev, prev); ok {
		t.Error("no elapsed time gave a percent")
	}
	// A counter that went backwards on one CPU leaves it at 0 without skewing the total.
	back := []CPUTime{{Busy: 5, Total: 102}, {Busy: 52, Total: 102}}
	got, ok = cpuStats(prev, back)
	if !ok || got.PerCore[0] != 0 || !near(got.Percent, 100) {
		t.Fatalf("backwards: %+v %v", got, ok)
	}
}

func TestDeriveThroughput(t *testing.T) {
	prev := Readings{
		At:      t0,
		DiskIO:  map[string]DiskCount{"disk0": {Read: 1000, Written: 5000}, "disk4": {Read: 1 << 40, Written: 1 << 40}},
		Network: map[string]NetCount{"en0": {Received: 10_000, Sent: 2_000}, "utun3": {Received: 900, Sent: 900}},
	}
	cur := Readings{
		At: t0.Add(2 * time.Second),
		// disk0 grew; disk4 was ejected; disk5 appeared with a large total since boot.
		DiskIO: map[string]DiskCount{"disk0": {Read: 5000, Written: 9000}, "disk5": {Read: 1 << 30, Written: 1 << 30}},
		// en0 grew; utun3 came back with counters reset to less than before.
		Network: map[string]NetCount{"en0": {Received: 30_000, Sent: 3_000}, "utun3": {Received: 100, Sent: 50}},
	}
	v := derive(&prev, cur, 8)
	if v.DiskIO == nil || !near(v.DiskIO.ReadPerSec, 2000) || !near(v.DiskIO.WritePerSec, 2000) {
		t.Errorf("disk %+v", v.DiskIO)
	}
	if v.Network == nil || !near(v.Network.DownloadPerSec, 10_000) || !near(v.Network.UploadPerSec, 500) {
		t.Errorf("network %+v", v.Network)
	}
	if !v.At.Equal(cur.At) {
		t.Errorf("at %v", v.At)
	}
}

func TestDeriveMissingMetrics(t *testing.T) {
	mem := &MemoryStats{Total: 16, Used: 8, Percent: 50}
	disk := &DiskUsage{Total: 100, Used: 40, Free: 60}
	load := &LoadAverage{One: 1, Five: 2, Fifteen: 3}
	prev := Readings{At: t0, CPU: []CPUTime{{0, 0}}, Network: map[string]NetCount{}}
	cur := Readings{At: t0.Add(time.Second), CPU: []CPUTime{{1, 2}}, Load: load, Memory: mem, Disk: disk, DiskIO: map[string]DiskCount{}}

	v := derive(&prev, cur, 8)
	if v.CPU == nil || v.CPU.Load != load || v.Memory != mem || v.Disk != disk {
		t.Errorf("gauges %+v", v)
	}
	// Disk I/O failed in prev and the network in cur: both are missing, independently of the rest.
	if v.DiskIO != nil || v.Network != nil || v.Processes != nil || v.Battery != nil {
		t.Errorf("rates without both readings: %+v", v)
	}

	// Without a baseline, or with time not moving forward, there are no rates but the gauges still show.
	for _, p := range []*Readings{nil, {At: cur.At, CPU: prev.CPU}, {At: cur.At.Add(time.Second), CPU: prev.CPU}} {
		v := derive(p, cur, 8)
		if v.CPU != nil || v.Memory != mem || v.Disk != disk {
			t.Errorf("baseline %+v: %+v", p, v)
		}
	}
}

func TestTopProcesses(t *testing.T) {
	ms := func(d float64) time.Duration { return time.Duration(d * float64(time.Millisecond)) }
	proc := func(pid int32, created int64, name string, cpu time.Duration, mem uint64) ProcessSample {
		return ProcessSample{PID: pid, Created: created, Name: name, CPU: cpu, HasCPU: true, Memory: mem, HasMemory: true}
	}
	prev := []ProcessSample{
		proc(1, 100, "Xcode", ms(1000), 3e9),
		proc(2, 100, "Safari", ms(500), 1e9),
		proc(3, 100, "old", ms(9000), 5e8), // exits, and a new process reuses its PID
		proc(4, 100, "idle", ms(10), 1e6),
	}
	cur := []ProcessSample{
		proc(1, 100, "Xcode", ms(1000+3000), 3e9),                               // 3 s of CPU in 2 s: 150 %
		proc(2, 100, "Safari", ms(500+200), 2e9),                                // 10 %
		proc(3, 200, "new", ms(50), 4e9),                                        // new: no CPU figure yet
		proc(4, 100, "idle", ms(10), 1e6),                                       // 0 %
		{PID: 5, Created: 100, Name: "protected", Memory: 6e9, HasMemory: true}, // memory only
		{PID: 6, Created: 100, Name: "no-memory", CPU: ms(10), HasCPU: true},    // no memory figure
		proc(7, 100, "tie-b", ms(0), 1e6),
	}
	got := topProcesses(prev, cur, 2, 3)

	names := func(us []ProcessUsage) []string {
		var out []string
		for _, u := range us {
			out = append(out, u.Name)
		}
		return out
	}
	if n := names(got.TopCPU); !slices.Equal(n, []string{"Xcode", "Safari", "idle"}) {
		t.Errorf("top CPU %v", n)
	}
	if !near(*got.TopCPU[0].CPUPercent, 150) || !near(*got.TopCPU[1].CPUPercent, 10) || *got.TopCPU[2].CPUPercent != 0 {
		t.Errorf("CPU percents %v %v %v", *got.TopCPU[0].CPUPercent, *got.TopCPU[1].CPUPercent, *got.TopCPU[2].CPUPercent)
	}
	if n := names(got.TopMemory); !slices.Equal(n, []string{"protected", "new", "Xcode"}) {
		t.Errorf("top memory %v", n)
	}
	if got.TopMemory[0].CPUPercent != nil || got.TopMemory[1].CPUPercent != nil || *got.TopMemory[0].Memory != 6e9 {
		t.Errorf("memory list entries %+v", got.TopMemory[:2])
	}
	if cap(got.TopCPU) != len(got.TopCPU) || cap(got.TopMemory) != len(got.TopMemory) {
		t.Error("top lists keep the whole process list alive")
	}

	// The first reading has memory but no CPU figures.
	first := topProcesses(nil, cur, 0, 8)
	if len(first.TopCPU) != 0 || len(first.TopMemory) != 6 {
		t.Errorf("first reading: %d by CPU, %d by memory", len(first.TopCPU), len(first.TopMemory))
	}
}
