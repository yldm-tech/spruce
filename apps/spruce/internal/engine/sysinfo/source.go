package sysinfo

import (
	"context"
	"errors"
	"fmt"
	stdnet "net"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
	"spruce/apps/spruce/internal/engine/execx"
	"spruce/apps/spruce/internal/engine/health"
)

// NewSource returns the gopsutil-backed Source of this platform. On macOS runner runs ioreg for the battery; Windows reads the battery driver and does not use it.
func NewSource(runner execx.Runner) Source {
	return &systemSource{runner: runner, names: make(map[procID]string)}
}

type systemSource struct {
	runner execx.Runner

	mu sync.Mutex
	// names caches display names by process, so the executable path (and on Windows its version resource) is read once per process rather than once per sample.
	names map[procID]string
}

// Read implements Source. The CPU times are read first, closest to Readings.At.
func (s *systemSource) Read(ctx context.Context) Readings {
	r := Readings{At: time.Now(), Errors: make(map[string]error)}
	fail := func(metric string, err error) { r.Errors[metric] = err }

	if times, err := cpu.TimesWithContext(ctx, true); err != nil {
		fail(MetricCPU, err)
	} else if len(times) == 0 {
		fail(MetricCPU, errors.New("sysinfo: no CPU times"))
	} else {
		r.CPU = make([]CPUTime, len(times))
		for i, t := range times {
			// macOS reports user, system, nice and idle. Windows reports user, system (kernel minus idle), idle and interrupt, and its interrupt time is already part of the kernel time, so Irq is not added.
			total := t.User + t.System + t.Nice + t.Idle
			r.CPU[i] = CPUTime{Busy: total - t.Idle, Total: total}
		}
	}
	if load, err := loadAverage(ctx); err != nil {
		fail(MetricLoad, err)
	} else {
		r.Load = load
	}

	if vm, err := mem.VirtualMemoryWithContext(ctx); err != nil {
		fail(MetricMemory, err)
	} else {
		m := &MemoryStats{Total: vm.Total, Used: vm.Used, Percent: vm.UsedPercent}
		if sw, err := mem.SwapMemoryWithContext(ctx); err != nil {
			fail(MetricSwap, err)
		} else {
			m.Swap = &SwapStats{Total: sw.Total, Used: sw.Used}
		}
		if p, err := memoryPressure(); err != nil {
			fail(MetricPressure, err)
		} else {
			m.Pressure = p
		}
		r.Memory = m
	}

	if u, err := disk.UsageWithContext(ctx, systemVolume()); err != nil {
		fail(MetricDisk, err)
	} else {
		r.Disk = &DiskUsage{Total: u.Total, Free: u.Free, Used: u.Total - min(u.Free, u.Total)}
	}

	if io, err := disk.IOCountersWithContext(ctx); err != nil {
		fail(MetricDiskIO, err)
	} else {
		r.DiskIO = make(map[string]DiskCount, len(io))
		for name, c := range io {
			r.DiskIO[name] = DiskCount{Read: c.ReadBytes, Written: c.WriteBytes}
		}
	}

	if nics, err := net.IOCountersWithContext(ctx, true); err != nil {
		fail(MetricNetwork, err)
	} else {
		loop := loopbackNames()
		r.Network = make(map[string]NetCount, len(nics))
		for _, c := range nics {
			if loop[c.Name] || isLoopbackName(c.Name) {
				continue
			}
			r.Network[c.Name] = NetCount{Received: c.BytesRecv, Sent: c.BytesSent}
		}
	}

	if procs, err := s.processes(ctx); err != nil {
		fail(MetricProcesses, err)
	} else {
		r.Processes = procs
	}
	return r
}

// loopbackNames names the interfaces flagged as loopback. A failure to list them leaves isLoopbackName to decide.
func loopbackNames() map[string]bool {
	ifs, err := stdnet.Interfaces()
	if err != nil {
		return nil
	}
	out := make(map[string]bool)
	for _, ifi := range ifs {
		if ifi.Flags&stdnet.FlagLoopback != 0 {
			out[ifi.Name] = true
		}
	}
	return out
}

// isLoopbackName recognises the loopback interface by its usual name: lo0 on macOS, "Loopback Pseudo-Interface 1" on Windows.
func isLoopbackName(name string) bool {
	return name == "lo" || name == "lo0" || strings.HasPrefix(name, "Loopback Pseudo-Interface")
}

// processes lists every process this user may inspect with its CPU time and memory. A process whose CPU time and memory both cannot be read, or that exits meanwhile, is left out. PID 0 (kernel_task on macOS, System Idle Process on Windows) is not a process the user can do anything about and is left out too.
func (s *systemSource) processes(ctx context.Context) ([]ProcessSample, error) {
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("sysinfo: list processes: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make(map[procID]string, len(procs))
	out := make([]ProcessSample, 0, len(procs))
	for _, p := range procs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if p.Pid == 0 {
			continue
		}
		created, err := p.CreateTimeWithContext(ctx)
		if err != nil || created <= 0 {
			continue
		}
		ps := ProcessSample{PID: p.Pid, Created: created}
		if t, err := p.TimesWithContext(ctx); err == nil {
			ps.CPU, ps.HasCPU = time.Duration((t.User+t.System)*float64(time.Second)), true
		}
		if m, err := p.MemoryInfoWithContext(ctx); err == nil && m != nil {
			ps.Memory, ps.HasMemory = m.RSS, true
		}
		if !ps.HasCPU && !ps.HasMemory {
			continue
		}
		key := procID{p.Pid, created}
		name, ok := s.names[key]
		if !ok {
			if name = health.DisplayName(ctx, p); name == "" {
				continue
			}
		}
		names[key] = name
		ps.Name = name
		out = append(out, ps)
	}
	// Keeping only this reading's processes bounds the cache by the number of running processes.
	s.names = names
	return out, nil
}

// Battery implements Source.
func (s *systemSource) Battery(ctx context.Context) (BatteryStats, error) {
	return readBattery(ctx, s.runner)
}

// Info implements Source.
func (s *systemSource) Info(ctx context.Context) SystemInfo {
	var info SystemInfo
	if cpus, err := cpu.InfoWithContext(ctx); err == nil && len(cpus) > 0 {
		info.CPUModel = strings.Join(strings.Fields(cpus[0].ModelName), " ")
	}
	if n, err := cpu.CountsWithContext(ctx, false); err == nil {
		info.PhysicalCores = n
	}
	if n, err := cpu.CountsWithContext(ctx, true); err == nil {
		info.LogicalCores = n
	}
	if boot, err := host.BootTimeWithContext(ctx); err == nil && boot > 0 {
		info.BootTime = time.Unix(int64(boot), 0)
	}
	platformInfo(ctx, &info)
	return info
}
