package sysinfo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"howett.net/plist"
	"spruce/apps/spruce/internal/engine/execx"
)

// ioregTimeout bounds one ioreg run, which normally takes a few tens of milliseconds.
const ioregTimeout = 10 * time.Second

// ioregBattery lists the AppleSmartBattery service and its properties as a plist array; it prints nothing on a Mac without a battery.
var ioregBattery = execx.Command{Name: "/usr/sbin/ioreg", Args: []string{"-rn", "AppleSmartBattery", "-a"}, Timeout: ioregTimeout}

func readBattery(ctx context.Context, runner execx.Runner) (BatteryStats, error) {
	out, err := runner.Run(ctx, ioregBattery)
	if err != nil {
		return BatteryStats{}, fmt.Errorf("sysinfo: ioreg AppleSmartBattery: %w", err)
	}
	return parseSmartBattery(out.Stdout)
}

// unknownMinutes is what the battery reports for a time it is still calculating.
const unknownMinutes = 65535

// parseSmartBattery reads the output of `ioreg -rn AppleSmartBattery -a`. Two shapes exist: on Intel Macs CurrentCapacity and MaxCapacity are in mAh; on Apple Silicon they are percentages (MaxCapacity is 100) and the mAh figures are AppleRawMaxCapacity and DesignCapacity, which recent macOS versions keep only under BatteryData. Values are read leniently (integers or booleans) because their plist types differ between those places.
func parseSmartBattery(out []byte) (BatteryStats, error) {
	if len(bytes.TrimSpace(out)) == 0 {
		return BatteryStats{}, nil
	}
	var services []map[string]any
	if _, err := plist.Unmarshal(out, &services); err != nil {
		return BatteryStats{}, fmt.Errorf("sysinfo: parse ioreg battery plist: %w", err)
	}
	if len(services) == 0 {
		return BatteryStats{}, nil
	}
	props := services[0]
	if installed, ok := props["BatteryInstalled"]; ok && !truthy(installed) {
		return BatteryStats{}, nil
	}
	data, _ := props["BatteryData"].(map[string]any)
	// lookup prefers the top level and falls back to BatteryData.
	lookup := func(key string) (int64, bool) {
		if v, ok := integer(props[key]); ok {
			return v, true
		}
		return integer(data[key])
	}

	current, okCurrent := integer(props["CurrentCapacity"])
	maxCap, okMax := integer(props["MaxCapacity"])
	if !okCurrent || !okMax || maxCap <= 0 {
		return BatteryStats{}, errors.New("sysinfo: ioreg battery has no CurrentCapacity or MaxCapacity")
	}
	b := BatteryStats{Present: true, Percent: clampPercent(float64(current) / float64(maxCap) * 100)}

	external, charging, full := truthy(props["ExternalConnected"]), truthy(props["IsCharging"]), truthy(props["FullyCharged"])
	var minutes int64
	switch {
	case !external:
		b.State = BatteryDischarging
		minutes = firstKnownMinutes(lookup, "AvgTimeToEmpty", "TimeRemaining")
	case charging:
		b.State = BatteryCharging
		minutes = firstKnownMinutes(lookup, "AvgTimeToFull", "TimeRemaining")
	case full:
		b.State = BatteryFull
	default:
		b.State = BatteryNotCharging
	}
	if minutes > 0 {
		m := int(minutes)
		b.MinutesRemaining = &m
	}

	if cycles, ok := integer(props["CycleCount"]); ok && cycles >= 0 {
		c := int(cycles)
		b.CycleCount = &c
	}

	design, okDesign := lookup("DesignCapacity")
	fullCharge, okFull := lookup("AppleRawMaxCapacity")
	if !okFull && maxCap > 100 {
		// Intel: MaxCapacity itself is the full charge capacity in mAh.
		fullCharge, okFull = maxCap, true
	}
	if okDesign && okFull && design > 0 && fullCharge > 0 {
		h := float64(fullCharge) / float64(design) * 100
		// A battery can hold slightly more than its design capacity when new; anything far off is a misread.
		if h <= 150 {
			b.HealthPercent = &h
		}
	}
	return b, nil
}

// firstKnownMinutes returns the first of keys that holds a known time, or 0.
func firstKnownMinutes(lookup func(string) (int64, bool), keys ...string) int64 {
	for _, k := range keys {
		if v, ok := lookup(k); ok && v > 0 && v < unknownMinutes {
			return v
		}
	}
	return 0
}

// integer reads a plist number of any integer type.
func integer(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case uint64:
		// ioreg writes some signed registers as unsigned; the conversion restores their sign.
		return int64(n), true
	case int:
		return int64(n), true
	case float64:
		return int64(n), true
	}
	return 0, false
}

// truthy reads a plist boolean, which some keys hold as 0 or 1.
func truthy(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	default:
		n, ok := integer(v)
		return ok && n != 0
	}
}
