package main

import (
	"embed"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"spruce/apps/spruce/internal/engine/sysinfo"
	"spruce/apps/spruce/internal/ui/pages/health"
	"spruce/apps/spruce/internal/ui/pages/health/healthfake"
	"spruce/apps/spruce/internal/ui/shell"
	"spruce/apps/spruce/internal/ui/store/storefake"
)

// interFonts are the app's bundled fonts, which nsgo.Fonts registers in main and a test must register itself.
//
//go:embed resources/fonts/*.ttf
var interFonts embed.FS

// healthScreensApp is screensApp with the app's fonts registered first, so the target's screenshots draw Inter at every weight as the app does: without them a text of Latin letters only falls back to the system font at the regular weight.
func healthScreensApp(t *testing.T, fakes *storefake.Services, d shell.Deps) *nsgotest.App {
	t.Helper()
	d.Stores = fakes.Deps()
	root := core.AppRoot(shell.App(d))
	a := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		files, err := interFonts.ReadDir("resources/fonts")
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			data, err := interFonts.ReadFile("resources/fonts/" + f.Name())
			if err != nil {
				t.Fatal(err)
			}
			if err := cx.RegisterFont(data); err != nil {
				t.Fatal(err)
			}
		}
		return root(cx)
	}, nsgotest.Size(1280, 820))
	a.WaitFor("id:sidebar", 10*time.Second)
	a.Press("id:nav-health")
	return a
}

// screenSamples are two minutes of a busy machine, one sample every two seconds, with sampling stopped for 20 seconds in the middle so the charts show a break. The last sample is at end.
func screenSamples(end time.Time) []sysinfo.Vitals {
	var out []sysinfo.Vitals
	for i := range 60 {
		if i >= 25 && i < 35 {
			continue
		}
		f := float64(i)
		v := healthfake.Sample(end.Add(-time.Duration(59-i) * 2 * time.Second))
		cpu := 38 + 18*math.Sin(f/5) + 6*math.Sin(f*1.7)
		perCore := make([]float64, 12)
		for c := range perCore {
			perCore[c] = min(100, max(0, cpu+22*math.Sin(f/3+float64(c))))
		}
		v.CPU.Percent, v.CPU.PerCore = cpu, perCore
		v.Memory.Percent = 70 + 3*math.Sin(f/9)
		v.Memory.Used = uint64(v.Memory.Percent / 100 * float64(v.Memory.Total))
		v.DiskIO.ReadPerSec = 2.5e6 + 2e6*math.Sin(f/4)
		v.DiskIO.WritePerSec = 1.2e6 + 1e6*math.Cos(f/3)
		v.Network.DownloadPerSec = 900e3 + 700e3*math.Sin(f/6)
		v.Network.UploadPerSec = 150e3 + 120e3*math.Cos(f/5)
		out = append(out, v)
	}
	return out
}

// waitSamples waits until the page drew the samples, moving the clock so the batched samples come through.
func waitSamples(a *nsgotest.App, selector string) {
	a.WaitUntil(10*time.Second, func() bool {
		if a.Exists(selector) {
			return true
		}
		a.Advance(50 * time.Millisecond)
		return false
	})
}

// TestScreensHealth shows the 健康监测 page reading, with two minutes of samples, scrolled to its processes, and on a machine without a battery, for comparison with the design.
func TestScreensHealth(t *testing.T) {
	info := healthfake.Info()
	// The reference screenshot's machine.
	info.CPUModel = "Apple M4 Pro"
	svc := &healthfake.Service{SystemInfo: info}
	fakes := storefake.New(t.TempDir())
	fakes.Settings.Health.CPUThreshold, fakes.Settings.Health.SustainMinutes, fakes.Settings.Health.Enabled = 40, 10, true
	a := healthScreensApp(t, fakes, shell.Deps{Health: health.Deps{Sysinfo: svc}})
	a.WaitFor("id:card-cpu", 10*time.Second)
	a.WaitUntil(10*time.Second, func() bool { _, open := svc.Watches(); return open })
	a.Screenshot("health-reading")

	end := time.Date(2026, 11, 14, 9, 2, 0, 0, time.UTC)
	samples := screenSamples(end)
	svc.Send(samples...)
	last := samples[len(samples)-1]
	waitSamples(a, `id:card-cpu >> text:"`+health.FormatPercent(last.CPU.Percent)+`"`)
	a.Screenshot("health-idle")

	a.Scroll("id:health-page", 1000)
	a.Screenshot("health-processes")
	a.Scroll("id:health-page", -1000)

	noBattery := healthfake.Sample(end.Add(2 * time.Second))
	noBattery.CPU.Percent = 12
	noBattery.Battery = &sysinfo.BatteryStats{Present: false, State: sysinfo.BatteryUnknown}
	svc.Send(noBattery)
	waitSamples(a, `id:card-cpu >> text:"12%"`)
	a.WaitForGone("id:card-battery", 10*time.Second)
	a.Screenshot("health-no-battery")
}
