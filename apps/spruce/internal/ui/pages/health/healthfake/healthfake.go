// Package healthfake is the 健康监测 page's Service for tests: static facts from a field and a watch the test feeds sample by sample. Only tests import it.
package healthfake

import (
	"context"
	"sync"
	"time"

	"spruce/apps/spruce/internal/engine/sysinfo"
)

// Service answers Info from SystemInfo and keeps every Watch open until its context ends, sending what Send gives it.
type Service struct {
	mu sync.Mutex
	// SystemInfo is what Info returns.
	SystemInfo sysinfo.SystemInfo
	// Fail is how many of the next watches end at once, as a watch that failed.
	Fail int

	watches int
	send    func(sysinfo.Vitals) error
	active  bool
}

// Info implements the page's Service.
func (s *Service) Info(context.Context) sysinfo.SystemInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.SystemInfo
}

// Watch implements the page's Service: it records send and waits for ctx, unless Fail asks it to end at once.
func (s *Service) Watch(ctx context.Context, send func(sysinfo.Vitals) error) {
	s.mu.Lock()
	s.watches++
	if s.Fail > 0 {
		s.Fail--
		s.mu.Unlock()
		return
	}
	s.send, s.active = send, true
	s.mu.Unlock()
	<-ctx.Done()
	s.mu.Lock()
	s.send, s.active = nil, false
	s.mu.Unlock()
}

// Send delivers samples through the open watch, from the test's goroutine as the sampler's would; it reports whether a watch was open.
func (s *Service) Send(samples ...sysinfo.Vitals) bool {
	s.mu.Lock()
	send := s.send
	s.mu.Unlock()
	if send == nil {
		return false
	}
	for _, v := range samples {
		if send(v) != nil {
			return false
		}
	}
	return true
}

// Watches returns how many watches started and whether one is open.
func (s *Service) Watches() (started int, open bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.watches, s.active
}

// Info is the design's Apple Silicon Mac, booted at 09:00 UTC on 11 November 2026.
func Info() sysinfo.SystemInfo {
	return sysinfo.SystemInfo{OSName: "macOS", OSVersion: "26.1", OSBuild: "25B78", Model: "Mac16,8", CPUModel: "Apple M4 Max",
		PhysicalCores: 12, LogicalCores: 12, PerformanceCores: 8, EfficiencyCores: 4, BootTime: time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)}
}

func ptr[T any](v T) *T { return &v }

// Sample is a full sample at t: CPU 23%, memory 68% under warning pressure, a 494 GB disk, some disk and network traffic, a charging battery and two processes in each list.
func Sample(t time.Time) sysinfo.Vitals {
	return sysinfo.Vitals{
		At:      t,
		CPU:     &sysinfo.CPUStats{Percent: 23.4, PerCore: []float64{10, 40, 5, 38.6}, Load: &sysinfo.LoadAverage{One: 2.31, Five: 1.98, Fifteen: 1.75}},
		Memory:  &sysinfo.MemoryStats{Total: 36e9, Used: 24.5e9, Percent: 68, Swap: &sysinfo.SwapStats{Total: 2e9, Used: 1.2e9}, Pressure: sysinfo.PressureWarning},
		Disk:    &sysinfo.DiskUsage{Total: 494e9, Used: 249e9, Free: 245e9},
		DiskIO:  &sysinfo.DiskIO{ReadPerSec: 1.2e6, WritePerSec: 300e3},
		Network: &sysinfo.NetworkIO{DownloadPerSec: 2.5e6, UploadPerSec: 120e3},
		Battery: &sysinfo.BatteryStats{Present: true, Percent: 87, State: sysinfo.BatteryCharging, MinutesRemaining: ptr(38), CycleCount: ptr(213),
			HealthPercent: ptr(89.2)},
		Processes: &sysinfo.ProcessLists{
			TopCPU: []sysinfo.ProcessUsage{
				{PID: 812, Name: "Xcode", CPUPercent: ptr(148.2), Memory: ptr(uint64(3.1e9))},
				{PID: 90, Name: "Safari", CPUPercent: ptr(4.26), Memory: ptr(uint64(1e9))},
			},
			TopMemory: []sysinfo.ProcessUsage{
				{PID: 812, Name: "Xcode", CPUPercent: ptr(148.2), Memory: ptr(uint64(3.1e9))},
				{PID: 300, Name: "launchd-helper", Memory: ptr(uint64(2e9))},
			},
		},
	}
}
