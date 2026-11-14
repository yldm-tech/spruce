package sysinfo

import (
	"context"
	"fmt"
	"os"

	"github.com/shirou/gopsutil/v4/load"
	"golang.org/x/sys/unix"
)

// dataVolume holds the user's data since macOS 10.15; the sealed system volume mounted at / is read-only and shares its APFS container.
const dataVolume = "/System/Volumes/Data"

// systemVolume is the Data volume, or / on a system without one.
func systemVolume() string {
	if _, err := os.Stat(dataVolume); err == nil {
		return dataVolume
	}
	return "/"
}

func loadAverage(ctx context.Context) (*LoadAverage, error) {
	a, err := load.AvgWithContext(ctx)
	if err != nil {
		return nil, err
	}
	return &LoadAverage{One: a.Load1, Five: a.Load5, Fifteen: a.Load15}, nil
}

// memoryPressure reads kern.memorystatus_vm_pressure_level: 1 normal, 2 warning, 4 critical (the kernel's kVMPressure levels).
func memoryPressure() (MemoryPressure, error) {
	level, err := unix.SysctlUint32("kern.memorystatus_vm_pressure_level")
	if err != nil {
		return "", fmt.Errorf("sysinfo: memory pressure: %w", err)
	}
	return pressureLevel(level), nil
}

func pressureLevel(level uint32) MemoryPressure {
	switch {
	case level >= 4:
		return PressureCritical
	case level >= 2:
		return PressureWarning
	default:
		return PressureNormal
	}
}

// platformInfo fills the macOS version and build, the model identifier and, on Apple Silicon, the split into performance and efficiency cores, all from sysctl.
func platformInfo(_ context.Context, info *SystemInfo) {
	info.OSName = "macOS"
	if v, err := unix.Sysctl("kern.osproductversion"); err == nil {
		info.OSVersion = v
	}
	if v, err := unix.Sysctl("kern.osversion"); err == nil {
		info.OSBuild = v
	}
	if v, err := unix.Sysctl("hw.model"); err == nil {
		info.Model = v
	}
	// Apple Silicon has two performance levels, perflevel0 the performance cores and perflevel1 the efficiency cores; Intel Macs have one or no hw.nperflevels.
	if n, err := unix.SysctlUint32("hw.nperflevels"); err == nil && n >= 2 {
		p, perr := unix.SysctlUint32("hw.perflevel0.physicalcpu")
		e, eerr := unix.SysctlUint32("hw.perflevel1.physicalcpu")
		if perr == nil && eerr == nil {
			info.PerformanceCores, info.EfficiencyCores = int(p), int(e)
		}
	}
}
