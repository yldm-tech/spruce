package health_test

import (
	"errors"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	enginehealth "spruce/apps/spruce/internal/engine/health"
	"spruce/apps/spruce/internal/engine/sysinfo"
	"spruce/apps/spruce/internal/ui/nav"
	"spruce/apps/spruce/internal/ui/pages/health"
	"spruce/apps/spruce/internal/ui/pages/health/healthfake"
	"spruce/apps/spruce/internal/ui/shared"
	"spruce/apps/spruce/internal/ui/store/storefake"
)

const wait = 5 * time.Second

var errBoom = errors.New("boom")

var t0 = time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)

type page struct {
	*nsgotest.App
	t     *testing.T
	svc   *healthfake.Service
	fakes *storefake.Services
	nav   *nav.Navigator
	shown *core.Signal[bool]
}

func newService() *healthfake.Service { return &healthfake.Service{SystemInfo: healthfake.Info()} }

func newFakes(t *testing.T) *storefake.Services {
	f := storefake.New(t.TempDir())
	f.Settings.Health.CPUThreshold, f.Settings.Health.SustainMinutes, f.Settings.Health.Enabled = 40, 10, true
	return f
}

// start mounts the page as the shell does, under a switch the test turns off to take the page away.
func start(t *testing.T, svc *healthfake.Service, fakes *storefake.Services) *page {
	t.Helper()
	p := &page{t: t, svc: svc, fakes: fakes}
	p.App = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		storefake.Provide(cx, fakes.Deps())
		p.nav = nav.NewNavigator(cx, nav.Health)
		nav.Key.Provide(cx, p.nav)
		health.DepsKey.Provide(cx, health.Deps{Sysinfo: svc})
		p.shown = core.NewSignal(cx, true)
		return core.Show{When: p.shown.Get, Then: health.Page}
	}, nsgotest.Size(1052, 820))
	p.WaitFor("id:card-cpu", wait)
	return p
}

// waitTicking settles until cond holds, moving the manual clock so batched samples (async.Batch) come through.
func (p *page) waitTicking(cond func() bool) {
	p.WaitUntil(wait, func() bool {
		if cond() {
			return true
		}
		p.Advance(50 * time.Millisecond)
		return false
	})
}

// send delivers samples through the page's watch and waits until the newest shows (its CPU figure is unique to it).
func (p *page) send(samples ...sysinfo.Vitals) {
	p.WaitUntil(wait, func() bool { _, open := p.svc.Watches(); return open })
	if !p.svc.Send(samples...) {
		p.t.Fatal("the page does not watch")
	}
	last := samples[len(samples)-1]
	if last.CPU == nil {
		p.waitTicking(func() bool { return p.Exists(`id:card-cpu >> text:"无法读取"`) })
		return
	}
	p.waitTicking(func() bool { return p.Exists(`id:card-cpu >> text:"` + health.FormatPercent(last.CPU.Percent) + `"`) })
}

// has fails the test unless scope shows text as a whole text.
func (p *page) has(scope, text string) {
	p.t.Helper()
	if !p.Exists("id:" + scope + ` >> text:"` + text + `"`) {
		p.t.Errorf("%s does not show %q", scope, text)
	}
}

// strokes returns the points of each line chart's stroked line, in drawing order.
func (p *page) strokes(id string) []int {
	var out []int
	n := 0
	for _, op := range p.Find("id:" + id).Ops {
		switch op.Kind {
		case core.DrawBeginPath:
			n = 0
		case core.DrawMoveTo, core.DrawLineTo:
			n++
		case core.DrawStroke:
			out = append(out, n)
		}
	}
	return out
}

func sample(sec int, edit func(*sysinfo.Vitals)) sysinfo.Vitals {
	v := healthfake.Sample(t0.Add(time.Duration(sec) * time.Second))
	if edit != nil {
		edit(&v)
	}
	return v
}

func TestReadingUntilTheFirstSample(t *testing.T) {
	svc := newService()
	p := start(t, svc, newFakes(t))
	p.Find(`role:header "健康监测"`)
	p.Find("text:实时查看 CPU、内存、磁盘、网络和电池，找出最占资源的进程")
	p.has("card-cpu", "正在读取…")
	p.has("card-network", "正在读取…")
	p.has("top-cpu", "正在读取…")
	if p.Exists("id:card-battery") {
		t.Error("a battery card shows before the first sample")
	}
	p.WaitUntil(wait, func() bool { n, open := svc.Watches(); return n == 1 && open })
}

func TestShowsEveryCardFromASample(t *testing.T) {
	p := start(t, newService(), newFakes(t))
	p.send(sample(0, nil))

	p.has("card-cpu", "23%")
	p.has("card-cpu", "负载 2.31 / 1.98 / 1.75")
	if got := p.Find("id:core-bars").Props.Label; got != "各核心占用：10%、40%、5%、39%" {
		t.Errorf("core bars %q", got)
	}
	p.has("card-cpu", "Apple M4 Max · 12 核（8 性能核 + 4 能效核）")

	p.has("card-memory", "68%")
	// Sizes read as the system's file manager counts them (1000s on macOS, 1024s on Windows), as format.Bytes renders them.
	p.has("card-memory", "已用 "+health.FormatBytes(24.5e9)+"，共 "+health.FormatBytes(36e9))
	p.has("card-memory", "已用 "+health.FormatBytes(1.2e9))
	p.has("card-memory", "偏高")
	if got := p.Find("id:meter-memory").Props.Value.Now; got != 68 {
		t.Errorf("memory meter at %v", got)
	}

	p.has("card-disk", health.FormatBytes(245e9)+" 可用")
	p.has("card-disk", "读取 "+health.FormatRate(1.2e6))
	p.has("card-disk", "写入 "+health.FormatRate(300e3))

	p.has("card-network", "↓ "+health.FormatRate(2.5e6))
	p.has("card-network", "↑ "+health.FormatRate(120e3))

	p.has("card-battery", "87%")
	p.has("card-battery", "正在充电 · 充满还需 38 分钟")
	p.has("card-battery", "213 次")
	p.has("card-battery", "89%（相对设计容量）")

	p.has("card-system", "macOS 26.1")
	p.has("card-system", "25B78")
	p.has("card-system", "Mac16,8")
	// Uptime is measured to the sample's time: 3 days from boot.
	p.has("card-system", "3 天")
	p.has("card-system", "11月11日 "+t0.Add(-72*time.Hour).Local().Format("15:04"))
}

func TestDrawsTheHistoryThenEveryNewSample(t *testing.T) {
	p := start(t, newService(), newFakes(t))
	low := func(v *sysinfo.Vitals) { v.CPU.Percent = 11 }
	p.send(sample(0, low), sample(2, low), sample(4, nil))
	for _, id := range []string{"chart-cpu", "chart-memory"} {
		if got := p.strokes(id); !slices.Equal(got, []int{3}) {
			t.Errorf("%s strokes %v", id, got)
		}
	}
	p.send(sample(6, func(v *sysinfo.Vitals) { v.CPU.Percent = 40 }))
	// Disk and network draw two series each.
	for id, want := range map[string][]int{"chart-cpu": {4}, "chart-memory": {4}, "chart-disk": {4, 4}, "chart-network": {4, 4}} {
		if got := p.strokes(id); !slices.Equal(got, want) {
			t.Errorf("%s strokes %v, want %v", id, got, want)
		}
	}
	// The history the sampler sends again when the page watches again is not drawn twice.
	p.svc.Send(sample(2, nil), sample(4, nil))
	p.Advance(time.Second)
	if got := p.strokes("chart-cpu"); !slices.Equal(got, []int{4}) {
		t.Errorf("resent history drawn: %v", got)
	}
}

func TestBreaksTheLineAcrossAGap(t *testing.T) {
	p := start(t, newService(), newFakes(t))
	p.send(sample(0, nil), sample(2, nil), sample(60, nil), sample(62, func(v *sysinfo.Vitals) { v.CPU.Percent = 50 }))
	if got := p.strokes("chart-cpu"); !slices.Equal(got, []int{2, 2}) {
		t.Errorf("strokes %v", got)
	}
}

func TestShowsUnreadableMetricsAndKeepsTheOthers(t *testing.T) {
	p := start(t, newService(), newFakes(t))
	p.send(sample(0, func(v *sysinfo.Vitals) { v.Network, v.Disk, v.Battery, v.Processes = nil, nil, nil, nil }))
	p.has("card-network", "无法读取")
	p.has("card-disk", "无法读取")
	// The battery could not be read: its card says so rather than disappearing.
	p.has("card-battery", "无法读取")
	p.has("card-cpu", "23%")
	p.has("top-cpu", "无法读取")
}

func TestHasNoBatteryCardWithoutABattery(t *testing.T) {
	p := start(t, newService(), newFakes(t))
	p.send(sample(0, func(v *sysinfo.Vitals) {
		v.Battery = &sysinfo.BatteryStats{Present: false, State: sysinfo.BatteryUnknown}
	}))
	if p.Exists("id:card-battery") {
		t.Error("a machine without a battery shows a battery card")
	}
}

func TestStopsWatchingWhenThePageGoes(t *testing.T) {
	svc := newService()
	p := start(t, svc, newFakes(t))
	p.WaitUntil(wait, func() bool { _, open := svc.Watches(); return open })
	p.Post(func() { p.shown.Set(false) })
	p.WaitForGone("id:card-cpu", wait)
	p.WaitUntil(wait, func() bool { _, open := svc.Watches(); return !open })
}

func TestOffersARetryWhenWatchingFails(t *testing.T) {
	svc := newService()
	svc.Fail = 1
	p := start(t, svc, newFakes(t))
	p.WaitFor("id:watch-error", wait)
	p.Find("id:watch-error >> text:无法读取系统状态")
	p.Press("id:watch-retry")
	p.WaitForGone("id:watch-error", wait)
	p.WaitUntil(wait, func() bool { n, open := svc.Watches(); return n == 2 && open })
	p.send(sample(0, nil))
	p.has("card-cpu", "23%")
}

func TestListsTheTopProcesses(t *testing.T) {
	p := start(t, newService(), newFakes(t))
	p.send(sample(0, nil))
	for _, text := range []string{"Xcode", "PID 812", "148%", health.FormatBytes(3.1e9)} {
		p.has("top-cpu-812", text)
	}
	for _, text := range []string{"Safari", "PID 90", "4.3%", health.FormatBytes(1e9)} {
		p.has("top-cpu-90", text)
	}
	if a, b := p.Find("id:top-cpu-812").Frame.Origin.Y, p.Find("id:top-cpu-90").Frame.Origin.Y; a >= b {
		t.Error("the list is not in the sampler's order")
	}
	for _, text := range []string{"launchd-helper", "PID 300", "—", health.FormatBytes(2e9)} {
		p.has("top-memory-300", text)
	}
	// The process cell is named for assistive technology.
	p.Find("id:top-memory-300 >> label:launchd-helper，PID 300")
	p.Find(`text:"` + health.ProcessFootnote(runtime.GOOS) + `"`)
	p.Find("text:Spruce 只显示进程，不会结束进程")
}

func TestOpensActivityMonitorAndNeverOffersToEndAProcess(t *testing.T) {
	fakes := newFakes(t)
	p := start(t, newService(), fakes)
	p.send(sample(0, nil))
	p.Press(`role:button "在` + shared.Monitor().Name + `中打开"`)
	p.WaitUntil(wait, func() bool { m, _ := fakes.Counts(); return m == 1 })
	if p.Exists("role:button /结束|退出|强制/") {
		t.Error("the page offers to end a process")
	}
	if p.Exists("id:open-monitor-error") {
		t.Error("an error shows after the monitor opened")
	}
}

func TestSaysWhenTheMonitorCannotOpen(t *testing.T) {
	fakes := newFakes(t)
	fakes.MonitorErr = errBoom
	p := start(t, newService(), fakes)
	// A memory sample without a pressure level, as on Windows, has no pressure row.
	p.send(sample(0, func(v *sysinfo.Vitals) { v.Memory.Pressure = "" }))
	if p.Exists(`id:card-memory >> text:"内存压力"`) {
		t.Error("a pressure row shows without a pressure level")
	}
	p.Press("id:open-monitor")
	if got := p.WaitFor("id:open-monitor-error", wait).Text; got != "无法打开"+shared.Monitor().Name {
		t.Errorf("error %q", got)
	}
}

func TestShowsTheSustainedCPUAlert(t *testing.T) {
	fakes := newFakes(t)
	fakes.Alerts = []enginehealth.Alert{{PID: 812, Name: "Xcode", CPUPercent: 148, Since: t0, Duration: 12 * time.Minute, Threshold: 40}}
	p := start(t, newService(), fakes)
	p.WaitFor("id:alert-banner", wait)
	p.Find("id:alert-banner >> text:Xcode 已经持续占用 148% CPU 有 12 分钟")
	fakes.Alert(nil)
	p.WaitForGone("id:alert-banner", wait)
}

func TestStatesTheAlertSettingsAndLinksToSettings(t *testing.T) {
	p := start(t, newService(), newFakes(t))
	p.WaitFor("id:alert-settings >> text:高 CPU 提醒：单个进程持续 10 分钟占用超过 40% CPU 时提醒", wait)
	p.Press(`role:link "调整阈值"`)
	p.WaitUntil(wait, func() bool { return p.nav.Current() == nav.Settings })
	if got := p.nav.TakeSettingsTab(); got != nav.SettingsHealth {
		t.Errorf("设置 opens on %q", got)
	}
}

func TestSaysWhenTheAlertIsOff(t *testing.T) {
	fakes := newFakes(t)
	fakes.Settings.Health.Enabled = false
	p := start(t, newService(), fakes)
	p.WaitFor("id:alert-settings >> text:高 CPU 提醒已关闭", wait)
	p.Press(`role:link "去设置开启"`)
	p.WaitUntil(wait, func() bool { return p.nav.Current() == nav.Settings })
	if got := p.nav.TakeSettingsTab(); got != nav.SettingsHealth {
		t.Errorf("设置 opens on %q", got)
	}
}
