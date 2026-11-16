package schedule_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"spruce/apps/spruce/internal/engine/schedule"
)

// fakeConditions answers from fields; it never reads the machine.
type fakeConditions struct {
	mu       sync.Mutex
	power    schedule.Power
	powerErr error
	cpu      float64
	cpuErr   error
	// cpuCalls counts CPU reads; window is the last window asked for.
	cpuCalls int
	window   time.Duration
}

func (f *fakeConditions) Power(context.Context) (schedule.Power, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.power, f.powerErr
}

func (f *fakeConditions) CPU(_ context.Context, window time.Duration) (float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cpuCalls++
	f.window = window
	return f.cpu, f.cpuErr
}

func (f *fakeConditions) set(p schedule.Power, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.power, f.powerErr = p, err
}

func TestCheck(t *testing.T) {
	ac := schedule.Power{OnAC: true}
	for name, tc := range map[string]struct {
		cleaning bool
		c        *fakeConditions
		want     schedule.Verdict
		cpuRead  bool
	}{
		"idle on AC":        {c: &fakeConditions{power: ac, cpu: 12}, want: schedule.Verdict{OK: true}, cpuRead: true},
		"at the CPU limit":  {c: &fakeConditions{power: ac, cpu: 40}, want: schedule.Verdict{OK: true}, cpuRead: true},
		"busy":              {c: &fakeConditions{power: ac, cpu: 40.5}, want: schedule.Verdict{Reason: "电脑正忙，稍后再检查"}, cpuRead: true},
		"load unreadable":   {c: &fakeConditions{power: ac, cpuErr: errors.New("no times")}, want: schedule.Verdict{Reason: "电脑正忙，稍后再检查"}, cpuRead: true},
		"cleaning":          {cleaning: true, c: &fakeConditions{power: ac}, want: schedule.Verdict{Reason: "正在清理，完成后再检查"}},
		"battery":           {c: &fakeConditions{power: schedule.Power{}}, want: schedule.Verdict{Reason: "正在使用电池，接通电源后再检查"}},
		"low power mode":    {c: &fakeConditions{power: schedule.Power{OnAC: true, Saver: true}}, want: schedule.Verdict{Reason: "低电量模式已开启，稍后再检查"}},
		"fair is fine":      {c: &fakeConditions{power: schedule.Power{OnAC: true, Thermal: 1}}, want: schedule.Verdict{OK: true}, cpuRead: true},
		"serious is hot":    {c: &fakeConditions{power: schedule.Power{OnAC: true, Thermal: 2}}, want: schedule.Verdict{Reason: "电脑有点热，稍后再检查"}},
		"power unreadable":  {c: &fakeConditions{powerErr: errors.New("ioreg")}, want: schedule.Verdict{Reason: "无法确认电源状态，稍后再检查"}},
		"battery and saver": {c: &fakeConditions{power: schedule.Power{Saver: true}}, want: schedule.Verdict{Reason: "正在使用电池，接通电源后再检查"}},
	} {
		t.Run(name, func(t *testing.T) {
			p := schedule.DefaultPolicy()
			if got := schedule.Check(t.Context(), p, tc.c, tc.cleaning); got != tc.want {
				t.Errorf("verdict %+v, want %+v", got, tc.want)
			}
			if read := tc.c.cpuCalls > 0; read != tc.cpuRead {
				t.Errorf("CPU read %v, want %v", read, tc.cpuRead)
			}
			if tc.cpuRead && tc.c.window != p.CPUWindow {
				t.Errorf("window %v", tc.c.window)
			}
		})
	}
}

func TestBusyVerdict(t *testing.T) {
	for err, want := range map[error]schedule.Verdict{
		nil:                       {OK: true},
		schedule.ErrBusy:          {Reason: "正在清理，完成后再检查"},
		schedule.ErrBusyElsewhere: {Reason: "正在清理，完成后再检查"},
		fmt.Errorf("%w: %w", schedule.ErrLock, errors.New("EACCES")): {Reason: "无法确认是否有另一个 Spruce 正在清理，稍后再检查"},
		errors.New("other"): {Reason: "电脑正忙，稍后再检查"},
	} {
		if got := schedule.BusyVerdict(err); got != want {
			t.Errorf("%v: %+v, want %+v", err, got, want)
		}
	}
}

func TestWatchPowerStopsWhenPowerGoes(t *testing.T) {
	c := &fakeConditions{power: schedule.Power{OnAC: true}}
	p := schedule.Policy{Watch: time.Millisecond}
	stopped := make(chan schedule.Verdict, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		schedule.WatchPower(t.Context(), p, c, func(v schedule.Verdict) { stopped <- v })
	}()
	time.Sleep(20 * time.Millisecond)
	select {
	case v := <-stopped:
		t.Fatalf("stopped on AC: %+v", v)
	default:
	}
	c.set(schedule.Power{OnAC: true, Saver: true}, nil)
	select {
	case v := <-stopped:
		if v.Reason != "低电量模式已开启，稍后再检查" {
			t.Errorf("verdict %+v", v)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("not stopped")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("WatchPower did not return")
	}
	if c.cpuCalls != 0 {
		t.Errorf("load read %d times while watching", c.cpuCalls)
	}
}

func TestWatchPowerReturnsWithContext(t *testing.T) {
	c := &fakeConditions{power: schedule.Power{OnAC: true}}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		schedule.WatchPower(ctx, schedule.Policy{Watch: time.Millisecond}, c, func(v schedule.Verdict) { t.Errorf("stopped: %+v", v) })
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("WatchPower did not return")
	}
}
