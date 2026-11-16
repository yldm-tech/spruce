package sysinfo

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"

	"howett.net/plist"
	"spruce/apps/spruce/internal/engine/execx/execxtest"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func intPtr(n int) *int { return &n }

func checkBattery(t *testing.T, got BatteryStats, want BatteryStats) {
	t.Helper()
	if got.Present != want.Present || got.State != want.State || math.Abs(got.Percent-want.Percent) > 0.01 {
		t.Errorf("present %v state %q percent %.2f, want %v %q %.2f", got.Present, got.State, got.Percent, want.Present, want.State, want.Percent)
	}
	eqInt := func(name string, a, b *int) {
		if (a == nil) != (b == nil) || (a != nil && *a != *b) {
			t.Errorf("%s = %v, want %v", name, deref(a), deref(b))
		}
	}
	eqInt("MinutesRemaining", got.MinutesRemaining, want.MinutesRemaining)
	eqInt("CycleCount", got.CycleCount, want.CycleCount)
	if (got.HealthPercent == nil) != (want.HealthPercent == nil) || (got.HealthPercent != nil && math.Abs(*got.HealthPercent-*want.HealthPercent) > 0.01) {
		t.Errorf("HealthPercent = %v, want %v", deref(got.HealthPercent), deref(want.HealthPercent))
	}
}

func deref[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

func pct(p float64) *float64 { return &p }

func TestParseSmartBatteryIntel(t *testing.T) {
	// Capacities in mAh, no AppleRaw keys: MaxCapacity is the full charge capacity. Discharging, so the time to empty counts; the huge unsigned Amperage must not break parsing.
	got, err := parseSmartBattery(fixture(t, "ioreg-intel.plist"))
	if err != nil {
		t.Fatal(err)
	}
	checkBattery(t, got, BatteryStats{
		Present:          true,
		Percent:          4301.0 / 5735 * 100,
		State:            BatteryDischarging,
		MinutesRemaining: intPtr(263),
		CycleCount:       intPtr(412),
		HealthPercent:    pct(5735.0 / 6669 * 100),
	})
}

func TestParseSmartBatteryAppleSilicon(t *testing.T) {
	// CurrentCapacity and MaxCapacity are percentages; health uses AppleRawMaxCapacity over DesignCapacity, never MaxCapacity (100).
	got, err := parseSmartBattery(fixture(t, "ioreg-apple-silicon.plist"))
	if err != nil {
		t.Fatal(err)
	}
	checkBattery(t, got, BatteryStats{
		Present:          true,
		Percent:          87,
		State:            BatteryCharging,
		MinutesRemaining: intPtr(38),
		CycleCount:       intPtr(213),
		HealthPercent:    pct(3907.0 / 4382 * 100),
	})
}

func TestParseSmartBatteryAppleSiliconBatteryData(t *testing.T) {
	// Recent macOS keeps DesignCapacity and AppleRawMaxCapacity only under BatteryData. Full on external power: AvgTimeToEmpty is stale and must not show.
	got, err := parseSmartBattery(fixture(t, "ioreg-apple-silicon-batterydata.plist"))
	if err != nil {
		t.Fatal(err)
	}
	checkBattery(t, got, BatteryStats{
		Present:       true,
		Percent:       100,
		State:         BatteryFull,
		CycleCount:    intPtr(154),
		HealthPercent: pct(5344.0 / 6249 * 100),
	})
}

func marshalServices(t *testing.T, services ...map[string]any) []byte {
	t.Helper()
	b, err := plist.Marshal(services, plist.XMLFormat)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseSmartBatteryStates(t *testing.T) {
	base := func(extra map[string]any) map[string]any {
		m := map[string]any{"CurrentCapacity": 80, "MaxCapacity": 100, "CycleCount": 10}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	cases := []struct {
		name    string
		props   map[string]any
		state   BatteryState
		minutes *int
	}{
		{"held at 80 % on power", base(map[string]any{"ExternalConnected": true, "IsCharging": false, "FullyCharged": false, "AvgTimeToEmpty": 500}), BatteryNotCharging, nil},
		{"discharging, still calculating", base(map[string]any{"ExternalConnected": false, "AvgTimeToEmpty": 65535, "TimeRemaining": 65535}), BatteryDischarging, nil},
		{"discharging, average unknown, time remaining known", base(map[string]any{"ExternalConnected": false, "AvgTimeToEmpty": 65535, "TimeRemaining": 120}), BatteryDischarging, intPtr(120)},
		{"charging, flags as integers", base(map[string]any{"ExternalConnected": 1, "IsCharging": 1, "AvgTimeToFull": 45}), BatteryCharging, intPtr(45)},
	}
	for _, c := range cases {
		got, err := parseSmartBattery(marshalServices(t, c.props))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got.State != c.state || deref(got.MinutesRemaining) != deref(c.minutes) || got.Percent != 80 {
			t.Errorf("%s: state %q minutes %v percent %v", c.name, got.State, deref(got.MinutesRemaining), got.Percent)
		}
		if got.HealthPercent != nil {
			t.Errorf("%s: health %v without a design capacity", c.name, *got.HealthPercent)
		}
	}
}

func TestParseSmartBatteryAbsentOrBroken(t *testing.T) {
	for name, out := range map[string][]byte{
		"no output (desktop Mac)": nil,
		"blank":                   []byte("\n"),
		"empty array":             marshalServices(t),
		"not installed":           marshalServices(t, map[string]any{"BatteryInstalled": false, "CurrentCapacity": 0, "MaxCapacity": 100}),
	} {
		got, err := parseSmartBattery(out)
		if err != nil || got.Present {
			t.Errorf("%s: %+v, %v; want no battery", name, got, err)
		}
	}
	for name, out := range map[string][]byte{
		"garbage":          []byte("ioreg: error"),
		"no capacity":      marshalServices(t, map[string]any{"CycleCount": 3}),
		"zero max":         marshalServices(t, map[string]any{"CurrentCapacity": 0, "MaxCapacity": 0}),
		"capacity strings": marshalServices(t, map[string]any{"CurrentCapacity": "80", "MaxCapacity": "100"}),
	} {
		if got, err := parseSmartBattery(out); err == nil {
			t.Errorf("%s: %+v, want an error", name, got)
		}
	}
}

func TestParseSmartBatteryImplausibleHealth(t *testing.T) {
	// A design capacity read in the wrong unit would give an absurd health; it is dropped, not shown.
	got, err := parseSmartBattery(marshalServices(t, map[string]any{"CurrentCapacity": 50, "MaxCapacity": 100, "AppleRawMaxCapacity": 5000, "DesignCapacity": 50}))
	if err != nil || got.HealthPercent != nil {
		t.Fatalf("%+v, %v", got, err)
	}
}

func TestReadBatteryRunsIoreg(t *testing.T) {
	f := execxtest.New()
	f.On("/usr/sbin/ioreg", "-rn", "AppleSmartBattery", "-a").Return(string(fixture(t, "ioreg-apple-silicon.plist")), 0)
	got, err := readBattery(context.Background(), f)
	if err != nil || !got.Present || got.Percent != 87 {
		t.Fatalf("%+v, %v", got, err)
	}

	f = execxtest.New()
	f.On("/usr/sbin/ioreg", "-rn", "AppleSmartBattery", "-a").Return("", 1)
	if _, err := readBattery(context.Background(), f); err == nil {
		t.Fatal("a failed ioreg read as no battery")
	}
}

func TestPressureLevel(t *testing.T) {
	for level, want := range map[uint32]MemoryPressure{0: PressureNormal, 1: PressureNormal, 2: PressureWarning, 4: PressureCritical} {
		if got := pressureLevel(level); got != want {
			t.Errorf("pressureLevel(%d) = %q, want %q", level, got, want)
		}
	}
}
