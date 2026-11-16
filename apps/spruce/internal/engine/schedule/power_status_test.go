package schedule

import (
	"testing"

	"spruce/apps/spruce/internal/engine/sysinfo"
)

func TestSystemPowerStatus(t *testing.T) {
	for name, tc := range map[string]struct {
		s    systemPowerStatus
		want Power
	}{
		"laptop on AC":            {s: systemPowerStatus{ACLineStatus: 1, BatteryFlag: 8}, want: Power{OnAC: true}},
		"laptop on battery":       {s: systemPowerStatus{ACLineStatus: 0, BatteryFlag: 1}, want: Power{}},
		"unknown line status":     {s: systemPowerStatus{ACLineStatus: 255, BatteryFlag: 2}, want: Power{}},
		"unknown everything":      {s: systemPowerStatus{ACLineStatus: 255, BatteryFlag: 255}, want: Power{}},
		"desktop without battery": {s: systemPowerStatus{ACLineStatus: 255, BatteryFlag: 128}, want: Power{OnAC: true}},
		"battery saver":           {s: systemPowerStatus{ACLineStatus: 0, BatteryFlag: 2, SystemStatusFlag: 1}, want: Power{Saver: true}},
		"saver on AC":             {s: systemPowerStatus{ACLineStatus: 1, SystemStatusFlag: 1}, want: Power{OnAC: true, Saver: true}},
	} {
		if got := tc.s.power(); got != tc.want {
			t.Errorf("%s: %+v, want %+v", name, got, tc.want)
		}
	}
}

func TestBatteryOnAC(t *testing.T) {
	for b, want := range map[sysinfo.BatteryStats]bool{
		{}: true,
		{Present: true, State: sysinfo.BatteryCharging}:    true,
		{Present: true, State: sysinfo.BatteryFull}:        true,
		{Present: true, State: sysinfo.BatteryNotCharging}: true,
		{Present: true, State: sysinfo.BatteryDischarging}: false,
		{Present: true, State: sysinfo.BatteryUnknown}:     false,
		{Present: true}: false,
	} {
		if got := batteryOnAC(b); got != want {
			t.Errorf("%+v: %v", b, got)
		}
	}
}

func TestBusyPercent(t *testing.T) {
	got, err := busyPercent(cpuTimes{idle: 100, total: 200}, cpuTimes{idle: 160, total: 300})
	if err != nil || got != 40 {
		t.Errorf("busy %v, %v", got, err)
	}
	if got, err := busyPercent(cpuTimes{idle: 100, total: 200}, cpuTimes{idle: 200, total: 300}); err != nil || got != 0 {
		t.Errorf("idle %v, %v", got, err)
	}
	for _, after := range []cpuTimes{{idle: 100, total: 200}, {idle: 90, total: 300}, {idle: 100, total: 150}} {
		if _, err := busyPercent(cpuTimes{idle: 100, total: 200}, after); err == nil {
			t.Errorf("%+v: no error", after)
		}
	}
}
