package sysinfo

import (
	"cmp"
	"context"
	"slices"
	"time"
)

// Source reads the machine. Implementations must be safe for concurrent use: a sampler that stops and starts again may briefly overlap two reads.
type Source interface {
	// Read takes the counters and gauges of one instant. A metric that fails is left nil and its error recorded in Readings.Errors; Read itself never fails.
	Read(ctx context.Context) Readings
	// Battery reads the battery; a machine without one returns Present false and no error.
	Battery(ctx context.Context) (BatteryStats, error)
	// Info reads the static facts; fields that fail stay empty.
	Info(ctx context.Context) SystemInfo
}

// Metric names, the keys of Readings.Errors.
const (
	MetricCPU       = "cpu"
	MetricLoad      = "load"
	MetricMemory    = "memory"
	MetricSwap      = "swap"
	MetricPressure  = "pressure"
	MetricDisk      = "disk"
	MetricDiskIO    = "diskIo"
	MetricNetwork   = "network"
	MetricProcesses = "processes"
	MetricBattery   = "battery"
)

// Readings are the raw values of one instant; Vitals are derived from two consecutive ones. A nil field could not be read.
type Readings struct {
	At time.Time
	// CPU holds each logical CPU's cumulative times.
	CPU    []CPUTime
	Load   *LoadAverage
	Memory *MemoryStats
	Disk   *DiskUsage
	// DiskIO holds cumulative byte counts by physical disk.
	DiskIO map[string]DiskCount
	// Network holds cumulative byte counts by interface, loopback excluded.
	Network   map[string]NetCount
	Processes []ProcessSample
	// Errors says why a metric is missing, by Metric name.
	Errors map[string]error
}

// CPUTime is one logical CPU's cumulative time in seconds: Busy is everything but idle (and I/O wait), Total includes idle.
type CPUTime struct {
	Busy  float64
	Total float64
}

// DiskCount is one disk's cumulative bytes read and written.
type DiskCount struct {
	Read    uint64
	Written uint64
}

// NetCount is one interface's cumulative bytes received and sent.
type NetCount struct {
	Received uint64
	Sent     uint64
}

// ProcessSample is one process at one instant.
type ProcessSample struct {
	PID int32
	// Created tells a reused PID from the process that had it before (Unix milliseconds).
	Created int64
	Name    string
	// CPU is the cumulative user plus system time; valid when HasCPU.
	CPU    time.Duration
	HasCPU bool
	// Memory is the resident memory in bytes; valid when HasMemory.
	Memory    uint64
	HasMemory bool
}

// derive turns two consecutive readings into a sample: rates come from the change since prev (absent without prev, or when a metric is missing from either), gauges from cur. topN bounds each process list.
func derive(prev *Readings, cur Readings, topN int) Vitals {
	v := Vitals{At: cur.At, Memory: cur.Memory, Disk: cur.Disk}
	var secs float64
	if prev != nil {
		secs = cur.At.Sub(prev.At).Seconds()
	}
	if secs <= 0 {
		prev = nil
	}

	if prev != nil {
		if c, ok := cpuStats(prev.CPU, cur.CPU); ok {
			c.Load = cur.Load
			v.CPU = &c
		}
		if prev.DiskIO != nil && cur.DiskIO != nil {
			v.DiskIO = &DiskIO{
				ReadPerSec:  float64(sumDelta(prev.DiskIO, cur.DiskIO, func(c DiskCount) uint64 { return c.Read })) / secs,
				WritePerSec: float64(sumDelta(prev.DiskIO, cur.DiskIO, func(c DiskCount) uint64 { return c.Written })) / secs,
			}
		}
		if prev.Network != nil && cur.Network != nil {
			v.Network = &NetworkIO{
				UploadPerSec:   float64(sumDelta(prev.Network, cur.Network, func(c NetCount) uint64 { return c.Sent })) / secs,
				DownloadPerSec: float64(sumDelta(prev.Network, cur.Network, func(c NetCount) uint64 { return c.Received })) / secs,
			}
		}
	}
	if cur.Processes != nil {
		var before []ProcessSample
		if prev != nil {
			before = prev.Processes
		}
		v.Processes = topProcesses(before, cur.Processes, secs, topN)
	}
	return v
}

// cpuStats computes each CPU's use between two readings of the same CPUs. It fails when either reading is missing or the number of CPUs changed.
func cpuStats(prev, cur []CPUTime) (CPUStats, bool) {
	if len(cur) == 0 || len(prev) != len(cur) {
		return CPUStats{}, false
	}
	per := make([]float64, len(cur))
	var busy, total float64
	for i := range cur {
		db, dt := cur[i].Busy-prev[i].Busy, cur[i].Total-prev[i].Total
		if dt > 0 && db >= 0 {
			per[i] = clampPercent(db / dt * 100)
			busy += db
			total += dt
		}
	}
	if total <= 0 {
		return CPUStats{}, false
	}
	return CPUStats{Percent: clampPercent(busy / total * 100), PerCore: per}, true
}

func clampPercent(p float64) float64 {
	return min(max(p, 0), 100)
}

// sumDelta adds up how much field grew for every key present in both maps. A counter that went backwards (reset or replaced) adds nothing, and keys that appear or disappear are ignored, so a disk plugged in or an interface coming up never reads as a burst.
func sumDelta[T any](prev, cur map[string]T, field func(T) uint64) uint64 {
	var sum uint64
	for k, c := range cur {
		p, ok := prev[k]
		if !ok {
			continue
		}
		if a, b := field(p), field(c); b >= a {
			sum += b - a
		}
	}
	return sum
}

type procID struct {
	pid     int32
	created int64
}

// topProcesses returns the topN processes by CPU (percent of one logical CPU over secs, from the change since prev) and by memory.
func topProcesses(prev, cur []ProcessSample, secs float64, topN int) *ProcessLists {
	before := make(map[procID]ProcessSample, len(prev))
	for _, p := range prev {
		before[procID{p.PID, p.Created}] = p
	}
	all := make([]ProcessUsage, 0, len(cur))
	for _, p := range cur {
		u := ProcessUsage{PID: p.PID, Name: p.Name}
		if p.HasMemory {
			m := p.Memory
			u.Memory = &m
		}
		if b, ok := before[procID{p.PID, p.Created}]; ok && secs > 0 && p.HasCPU && b.HasCPU && p.CPU >= b.CPU {
			pct := (p.CPU - b.CPU).Seconds() / secs * 100
			u.CPUPercent = &pct
		}
		all = append(all, u)
	}

	byCPU := slices.DeleteFunc(slices.Clone(all), func(u ProcessUsage) bool { return u.CPUPercent == nil })
	slices.SortFunc(byCPU, func(a, b ProcessUsage) int {
		if c := cmp.Compare(*b.CPUPercent, *a.CPUPercent); c != 0 {
			return c
		}
		return cmp.Compare(a.PID, b.PID)
	})
	byMemory := slices.DeleteFunc(all, func(u ProcessUsage) bool { return u.Memory == nil })
	slices.SortFunc(byMemory, func(a, b ProcessUsage) int {
		if c := cmp.Compare(*b.Memory, *a.Memory); c != 0 {
			return c
		}
		return cmp.Compare(a.PID, b.PID)
	})
	// Clone, so the samples the sampler keeps do not hold on to every process.
	return &ProcessLists{TopCPU: slices.Clone(byCPU[:min(topN, len(byCPU))]), TopMemory: slices.Clone(byMemory[:min(topN, len(byMemory))])}
}
