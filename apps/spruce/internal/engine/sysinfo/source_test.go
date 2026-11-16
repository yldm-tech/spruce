package sysinfo

import (
	"context"
	"os"
	"testing"
	"time"

	"spruce/apps/spruce/internal/engine/execx/execxtest"
)

// TestSystemSourceReads reads the real machine (read-only, no battery): every metric gopsutil supports here must come back, and two readings must derive a sample with every rate.
func TestSystemSourceReads(t *testing.T) {
	src := NewSource(execxtest.New())
	ctx := context.Background()
	first := src.Read(ctx)
	for metric, err := range first.Errors {
		t.Errorf("%s: %v", metric, err)
	}
	if len(first.CPU) == 0 || first.Memory == nil || first.Memory.Total == 0 || first.Memory.Swap == nil || first.Disk == nil || first.Disk.Total == 0 || first.DiskIO == nil || first.Network == nil {
		t.Fatalf("missing metrics: %+v", first)
	}
	for name := range first.Network {
		if isLoopbackName(name) {
			t.Errorf("loopback %q counted", name)
		}
	}
	self := int32(os.Getpid())
	found := false
	for _, p := range first.Processes {
		if p.PID == 0 {
			t.Error("PID 0 listed")
		}
		if p.PID == self {
			found = p.HasCPU && p.HasMemory && p.Memory > 0 && p.Name != ""
		}
	}
	if !found {
		t.Errorf("own process missing or incomplete among %d", len(first.Processes))
	}

	time.Sleep(200 * time.Millisecond)
	second := src.Read(ctx)
	v := derive(&first, second, 8)
	if v.CPU == nil || len(v.CPU.PerCore) != len(first.CPU) || v.DiskIO == nil || v.Network == nil || v.Processes == nil || len(v.Processes.TopMemory) == 0 {
		t.Fatalf("derived %+v", v)
	}

	info := src.Info(ctx)
	if info.OSName == "" || info.LogicalCores == 0 || info.CPUModel == "" || info.BootTime.IsZero() || info.BootTime.After(time.Now()) {
		t.Errorf("info %+v", info)
	}
}
