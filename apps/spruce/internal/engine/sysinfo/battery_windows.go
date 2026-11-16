package sysinfo

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync/atomic"
	"time"

	"github.com/distatus/battery"
	"github.com/yusufpapurcu/wmi"
	"spruce/apps/spruce/internal/engine/execx"
)

// readBattery sums every battery the battery driver lists (capacities in mWh) and asks WMI for the cycle count. runner is not used on Windows.
func readBattery(ctx context.Context, _ execx.Runner) (BatteryStats, error) {
	if err := ctx.Err(); err != nil {
		return BatteryStats{}, err
	}
	all, err := battery.GetAll()
	var fatal battery.ErrFatal
	if errors.As(err, &fatal) {
		return BatteryStats{}, fmt.Errorf("sysinfo: read batteries: %w", err)
	}
	// Otherwise err is nil or lists per-battery problems; a battery with a partial error still has the fields that were read.
	var sum driverBattery
	for _, b := range all {
		if b == nil {
			continue
		}
		sum.add(b)
	}
	if sum.count == 0 {
		if err != nil {
			return BatteryStats{}, fmt.Errorf("sysinfo: read batteries: %w", err)
		}
		return BatteryStats{}, nil
	}
	out := sum.stats()
	if cycles, ok := cycleCount(ctx); ok {
		out.CycleCount = &cycles
	}
	return out, nil
}

// driverBattery accumulates the batteries of a machine with several.
type driverBattery struct {
	count                     int
	current, full, design     float64
	rate                      float64
	charging, discharging, ac bool
}

func (d *driverBattery) add(b *battery.Battery) {
	d.count++
	d.current += b.Current
	d.full += b.Full
	d.design += b.Design
	d.rate += b.ChargeRate
	switch b.State.Raw {
	case battery.Charging:
		d.charging = true
	case battery.Discharging, battery.Empty:
		d.discharging = true
	case battery.Full, battery.Idle:
		// The driver reports "power on line, not charging" as Full.
		d.ac = true
	}
}

func (d *driverBattery) stats() BatteryStats {
	b := BatteryStats{Present: true, State: BatteryUnknown}
	if d.full > 0 {
		b.Percent = clampPercent(d.current / d.full * 100)
	}
	var minutes float64
	switch {
	case d.discharging:
		b.State = BatteryDischarging
		if d.rate > 0 {
			minutes = d.current / d.rate * 60
		}
	case d.charging:
		b.State = BatteryCharging
		if d.rate > 0 && d.full > d.current {
			minutes = (d.full - d.current) / d.rate * 60
		}
	case d.ac && b.Percent >= 99:
		b.State = BatteryFull
	case d.ac:
		b.State = BatteryNotCharging
	}
	// Rates swing with load; beyond a day the estimate means nothing.
	if minutes >= 1 && minutes < 24*60 {
		m := int(math.Round(minutes))
		b.MinutesRemaining = &m
	}
	if d.design > 0 && d.full > 0 {
		if h := d.full / d.design * 100; h <= 150 {
			b.HealthPercent = &h
		}
	}
	return b
}

// wmiTimeout bounds a WMI query; WMI is a service that can stall, and the cycle count is not worth waiting for.
const wmiTimeout = 5 * time.Second

// wmiBusy is set while a WMI query runs, so a stalled query is not joined by more.
var wmiBusy atomic.Bool

// batteryCycleCount is the root\WMI class the battery miniport fills when the firmware reports a cycle count.
type batteryCycleCount struct {
	CycleCount uint32
}

// cycleCount sums the cycle counts WMI reports; ok is false when there are none, WMI fails or it does not answer within wmiTimeout.
func cycleCount(ctx context.Context) (int, bool) {
	if !wmiBusy.CompareAndSwap(false, true) {
		return 0, false
	}
	type result struct {
		rows []batteryCycleCount
		err  error
	}
	done := make(chan result, 1)
	go func() {
		defer wmiBusy.Store(false)
		var rows []batteryCycleCount
		err := wmi.QueryNamespace("SELECT CycleCount FROM BatteryCycleCount", &rows, `root\WMI`)
		done <- result{rows, err}
	}()
	timer := time.NewTimer(wmiTimeout)
	defer timer.Stop()
	select {
	case r := <-done:
		if r.err != nil || len(r.rows) == 0 {
			return 0, false
		}
		total := 0
		for _, row := range r.rows {
			total += int(row.CycleCount)
		}
		// Firmware without a counter reports 0.
		return total, total > 0
	case <-ctx.Done():
		return 0, false
	case <-timer.C:
		return 0, false
	}
}
