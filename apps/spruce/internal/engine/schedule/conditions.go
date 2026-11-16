package schedule

import (
	"context"
	"errors"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"spruce/apps/spruce/internal/engine/execx"
)

// Power is what the machine says about its power.
type Power struct {
	// OnAC is set on external power; a machine without a battery is on AC, and an unknown state is not.
	OnAC bool
	// Saver is set in macOS Low Power Mode and Windows battery saver.
	Saver bool
	// Thermal is macOS NSProcessInfo thermalState: 0 nominal, 1 fair, 2 serious, 3 critical; 0 on Windows.
	Thermal int
}

// Conditions reads what decides whether background work may run now. The system implementation never starts a prompt.
type Conditions interface {
	// Power reads the power state.
	Power(ctx context.Context) (Power, error)
	// CPU is how busy the whole machine was over window, 0 to 100.
	CPU(ctx context.Context, window time.Duration) (float64, error)
}

// MaxThermal is the highest thermal state background work starts in: from serious (2) on the machine counts as hot.
const MaxThermal = 1

// Verdict says whether background work may start now.
type Verdict struct {
	OK bool
	// Reason is a sentence in Simplified Chinese for the page when OK is false.
	Reason string
}

// Reasons a scheduled check waits, in Simplified Chinese.
const (
	ReasonBattery  = "正在使用电池，接通电源后再检查"
	ReasonSaver    = "低电量模式已开启，稍后再检查"
	ReasonHot      = "电脑有点热，稍后再检查"
	ReasonBusy     = "电脑正忙，稍后再检查"
	ReasonCleaning = "正在清理，完成后再检查"
	ReasonLock     = "无法确认是否有另一个 Spruce 正在清理，稍后再检查"
	ReasonPower    = "无法确认电源状态，稍后再检查"
)

// Check decides whether a scheduled check may start: no clean in this process (cleaning), on AC power, not in a power saving mode, not hot, and the machine's load over p.CPUWindow at most p.MaxCPUPercent. A probe error blocks, as does a load that cannot be read: the check waits rather than guesses. The load is read last, since measuring it takes p.CPUWindow. Pure apart from c.
func Check(ctx context.Context, p Policy, c Conditions, cleaning bool) Verdict {
	if cleaning {
		return Verdict{Reason: ReasonCleaning}
	}
	if v := powerVerdict(ctx, c); !v.OK {
		return v
	}
	load, err := c.CPU(ctx, p.CPUWindow)
	if err != nil || load > p.MaxCPUPercent {
		return Verdict{Reason: ReasonBusy}
	}
	return Verdict{OK: true}
}

// powerVerdict is the power part of Check, which WatchPower repeats while a run goes on.
func powerVerdict(ctx context.Context, c Conditions) Verdict {
	pw, err := c.Power(ctx)
	switch {
	case err != nil:
		return Verdict{Reason: ReasonPower}
	case !pw.OnAC:
		return Verdict{Reason: ReasonBattery}
	case pw.Saver:
		return Verdict{Reason: ReasonSaver}
	case pw.Thermal > MaxThermal:
		return Verdict{Reason: ReasonHot}
	}
	return Verdict{OK: true}
}

// BusyVerdict is the Verdict for an error of Activity.BeginBackground: ErrBusy and ErrBusyElsewhere wait for the clean to end, ErrLock for the lock to be readable; any other error waits as a busy machine would. nil is OK.
func BusyVerdict(err error) Verdict {
	switch {
	case err == nil:
		return Verdict{OK: true}
	case errors.Is(err, ErrBusy), errors.Is(err, ErrBusyElsewhere):
		return Verdict{Reason: ReasonCleaning}
	case errors.Is(err, ErrLock):
		return Verdict{Reason: ReasonLock}
	}
	return Verdict{Reason: ReasonBusy}
}

// WatchPower reads c's power every p.Watch until ctx ends and calls stop once, with the Verdict, the first time the machine leaves AC power, enters a power saving mode, gets hot or the power cannot be read; it then returns. A run calls it on its own goroutine and cancels itself in stop. The load is not measured again: the run itself makes the machine busy.
func WatchPower(ctx context.Context, p Policy, c Conditions, stop func(Verdict)) {
	if p.Watch <= 0 {
		return
	}
	t := time.NewTicker(p.Watch)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if v := powerVerdict(ctx, c); !v.OK {
			if ctx.Err() != nil {
				return
			}
			stop(v)
			return
		}
	}
}

// systemConditions reads the real machine; Power is per platform (conditions_darwin.go, conditions_windows.go).
type systemConditions struct {
	runner execx.Runner
}

// SystemConditions returns the conditions of this machine. On macOS runner runs /usr/sbin/ioreg for the battery (sysinfo); Windows asks kernel32 and does not use it. Nothing it reads prompts for a permission.
func SystemConditions(runner execx.Runner) Conditions {
	return systemConditions{runner: runner}
}

// CPU reads the machine's CPU times twice, window apart, and returns the share that was not idle, as sysinfo does for the 健康监测 page.
func (systemConditions) CPU(ctx context.Context, window time.Duration) (float64, error) {
	before, err := cpuTotal(ctx)
	if err != nil {
		return 0, err
	}
	t := time.NewTimer(window)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-t.C:
	}
	after, err := cpuTotal(ctx)
	if err != nil {
		return 0, err
	}
	return busyPercent(before, after)
}

// cpuTotal reads the times of all CPUs together.
func cpuTotal(ctx context.Context) (cpuTimes, error) {
	times, err := cpu.TimesWithContext(ctx, false)
	if err != nil {
		return cpuTimes{}, err
	}
	if len(times) == 0 {
		return cpuTimes{}, errors.New("schedule: no CPU times")
	}
	t := times[0]
	return cpuTimes{idle: t.Idle, total: t.User + t.System + t.Nice + t.Idle}, nil
}
