package health

import (
	"math"
	"slices"
	"testing"
	"time"

	"spruce/apps/spruce/internal/engine/sysinfo"
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func ptr[T any](v T) *T { return &v }

func info(edit func(*sysinfo.SystemInfo)) sysinfo.SystemInfo {
	i := sysinfo.SystemInfo{OSName: "macOS", OSVersion: "26.1", OSBuild: "25B78", Model: "Mac16,8", CPUModel: "Apple M4 Max",
		PhysicalCores: 16, LogicalCores: 16}
	if edit != nil {
		edit(&i)
	}
	return i
}

func cpuAt(s string, percent float64) sysinfo.Vitals {
	return sysinfo.Vitals{At: at(s), CPU: &sysinfo.CPUStats{Percent: percent}}
}

func pickCPU(v sysinfo.Vitals) (float64, bool) {
	if v.CPU == nil {
		return 0, false
	}
	return v.CPU.Percent, true
}

func check(t *testing.T, name, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %q, want %q", name, got, want)
	}
}

func TestNumberFormatting(t *testing.T) {
	check(t, "FormatPercent(23.4)", FormatPercent(23.4), "23%")
	check(t, "FormatPercent(-1)", FormatPercent(-1), "0%")
	check(t, "FormatProcessCPU(0.44)", FormatProcessCPU(0.44), "0.4%")
	check(t, "FormatProcessCPU(9.96)", FormatProcessCPU(9.96), "10.0%")
	check(t, "FormatProcessCPU(12.6)", FormatProcessCPU(12.6), "13%")
	// One logical CPU is 100 %, so a multi-threaded process passes it.
	check(t, "FormatProcessCPU(250)", FormatProcessCPU(250), "250%")
	check(t, "FormatRate(0)", FormatRate(0), "0 B/s")
	check(t, "FormatRate(1234567)", FormatRate(1_234_567), "1.2 MB/s")
	check(t, "FormatLoad", FormatLoad(sysinfo.LoadAverage{One: 2.314, Five: 1.98, Fifteen: 1.5}), "2.31 / 1.98 / 1.50")
}

func TestTimeFormatting(t *testing.T) {
	check(t, "FormatMinutes(45)", FormatMinutes(45), "45 分钟")
	check(t, "FormatMinutes(125)", FormatMinutes(125), "2 小时 5 分钟")
	check(t, "FormatMinutes(180)", FormatMinutes(180), "3 小时")

	for _, c := range []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "不到 1 分钟"},
		{12 * time.Minute, "12 分钟"},
		{5*time.Hour + 3*time.Minute, "5 小时 3 分钟"},
		{2 * time.Hour, "2 小时"},
		{(3*24+4)*time.Hour + 59*time.Minute, "3 天 4 小时"},
		{2 * 24 * time.Hour, "2 天"},
	} {
		check(t, "FormatUptime("+c.d.String()+")", FormatUptime(c.d), c.want)
	}

	check(t, "FormatBootTime(local)", FormatBootTime(time.Date(2026, 11, 11, 9, 5, 0, 0, time.Local)), "11月11日 09:05")
	check(t, "FormatBootTime(zero)", FormatBootTime(time.Time{}), "")
	check(t, "FormatBootTime(year 1)", FormatBootTime(at("0001-01-01T00:00:00Z")), "")
}

func TestBatteryAndMemoryWording(t *testing.T) {
	check(t, "charging", BatteryStateText(sysinfo.BatteryCharging), "正在充电")
	check(t, "discharging", BatteryStateText(sysinfo.BatteryDischarging), "正在使用电池")
	check(t, "full", BatteryStateText(sysinfo.BatteryFull), "已充满")
	check(t, "notCharging", BatteryStateText(sysinfo.BatteryNotCharging), "已接通电源，未在充电")
	check(t, "unknown", BatteryStateText(sysinfo.BatteryUnknown), "")

	check(t, "discharging 185", BatteryTimeText(sysinfo.BatteryDischarging, ptr(185)), "剩余约 3 小时 5 分钟")
	check(t, "charging 38", BatteryTimeText(sysinfo.BatteryCharging, ptr(38)), "充满还需 38 分钟")
	check(t, "charging nil", BatteryTimeText(sysinfo.BatteryCharging, nil), "")
	check(t, "full 30", BatteryTimeText(sysinfo.BatteryFull, ptr(30)), "")

	check(t, "caption", BatteryCaption(sysinfo.BatteryStats{State: sysinfo.BatteryCharging, MinutesRemaining: ptr(38)}), "正在充电 · 充满还需 38 分钟")
	check(t, "caption full", BatteryCaption(sysinfo.BatteryStats{State: sysinfo.BatteryFull}), "已充满")
	check(t, "caption unknown", BatteryCaption(sysinfo.BatteryStats{State: sysinfo.BatteryUnknown}), "")

	check(t, "normal", PressureText(sysinfo.PressureNormal), "正常")
	check(t, "warning", PressureText(sysinfo.PressureWarning), "偏高")
	check(t, "critical", PressureText(sysinfo.PressureCritical), "紧张")
}

func TestSystemWording(t *testing.T) {
	check(t, "cores P+E", CoresText(info(func(i *sysinfo.SystemInfo) {
		i.PhysicalCores, i.LogicalCores, i.PerformanceCores, i.EfficiencyCores = 12, 12, 8, 4
	})), "12 核（8 性能核 + 4 能效核）")
	check(t, "cores threads", CoresText(info(func(i *sysinfo.SystemInfo) { i.PhysicalCores, i.LogicalCores = 8, 16 })), "8 核 16 线程")
	check(t, "threads only", CoresText(info(func(i *sysinfo.SystemInfo) { i.PhysicalCores, i.LogicalCores = 0, 4 })), "4 线程")
	check(t, "no cores", CoresText(info(func(i *sysinfo.SystemInfo) { i.PhysicalCores, i.LogicalCores = 0, 0 })), "")

	check(t, "os", OSText(info(nil)), "macOS 26.1")
	check(t, "windows", OSText(info(func(i *sysinfo.SystemInfo) { i.OSName, i.OSVersion, i.OSBuild = "Windows 11 Pro", "24H2", "26100.2314" })),
		"Windows 11 Pro 24H2")
	check(t, "no build", OSText(info(func(i *sysinfo.SystemInfo) { i.OSBuild = "" })), "macOS 26.1")
	check(t, "nothing", OSText(info(func(i *sysinfo.SystemInfo) { i.OSName, i.OSVersion, i.OSBuild = "", "", "" })), "")

	check(t, "foot", CPUFoot(info(nil)), "Apple M4 Max · 16 核")
	check(t, "foot without model", CPUFoot(info(func(i *sysinfo.SystemInfo) { i.CPUModel = "" })), "16 核")
}

func TestUptimeText(t *testing.T) {
	boot := info(func(i *sysinfo.SystemInfo) { i.BootTime = at("2026-11-11T09:00:00Z") })
	latest := cpuAt("2026-11-14T09:00:00Z", 1)
	check(t, "measured to the sample", UptimeText(boot, &latest), "3 天")
	check(t, "no sample yet", UptimeText(boot, nil), "正在读取…")
	check(t, "no boot time", UptimeText(info(nil), &latest), "无法读取")
}

func TestLevelsAndPlatformWording(t *testing.T) {
	for p, want := range map[float64]Level{50: LevelNormal, 85: LevelWarning, 94.9: LevelWarning, 95: LevelCritical} {
		if got := UsageLevel(p); got != want {
			t.Errorf("UsageLevel(%v) = %v, want %v", p, got, want)
		}
	}
	if BatteryLevel(sysinfo.BatteryStats{State: sysinfo.BatteryDischarging, Percent: 10}) != LevelCritical ||
		BatteryLevel(sysinfo.BatteryStats{State: sysinfo.BatteryCharging, Percent: 5}) != LevelNormal {
		t.Error("the battery is critical only while it runs the machine at 10% or less")
	}
	check(t, "cores label", CoresLabel([]float64{10, 40, 5, 38.6}), "各核心占用：10%、40%、5%、39%")
	check(t, "swap darwin", SwapLabel("darwin"), "交换内存")
	check(t, "swap windows", SwapLabel("windows"), "页面文件")
	check(t, "footnote darwin", ProcessFootnote("darwin"),
		"CPU 按单个处理器核心计算，100% 即占满一个核心，与活动监视器一致。其他用户和系统的进程（如 WindowServer）无法读取，不在列表中。Spruce 只显示进程，不会结束进程。")
	check(t, "footnote windows", ProcessFootnote("windows"),
		"CPU 按单个处理器核心计算，100% 即占满一个核心；任务管理器按全部核心显示，数值会更低。受保护的系统进程读不到 CPU 占用。Spruce 只显示进程，不会结束进程。")
	check(t, "process cpu unknown", ProcessCPU(sysinfo.ProcessUsage{}), "—")
	check(t, "process memory unknown", ProcessMemory(sysinfo.ProcessUsage{}), "—")
}

// sameFloats compares columns with NaN equal to NaN.
func sameFloats(a, b []float64) bool {
	return slices.EqualFunc(a, b, func(x, y float64) bool { return x == y || math.IsNaN(x) && math.IsNaN(y) })
}

func TestChartDataMissingValues(t *testing.T) {
	samples := []sysinfo.Vitals{cpuAt("2026-11-14T09:00:00Z", 10), {At: at("2026-11-14T09:00:02Z")}, cpuAt("2026-11-14T09:00:04Z", 30)}
	x0 := float64(at("2026-11-14T09:00:00Z").Unix())
	c := ChartData(samples, pickCPU)
	if !sameFloats(c.X, []float64{x0, x0 + 2, x0 + 4}) || !sameFloats(c.Y[0], []float64{10, math.NaN(), 30}) {
		t.Fatalf("chart %v %v", c.X, c.Y)
	}
}

func TestChartDataBreaksAcrossAGap(t *testing.T) {
	samples := []sysinfo.Vitals{cpuAt("2026-11-14T09:00:00Z", 10), cpuAt("2026-11-14T09:01:00Z", 20), cpuAt("2026-11-14T09:01:02Z", 30)}
	x0 := float64(at("2026-11-14T09:00:00Z").Unix())
	c := ChartData(samples, pickCPU)
	if !sameFloats(c.X, []float64{x0, x0 + 30, x0 + 60, x0 + 62}) || !sameFloats(c.Y[0], []float64{10, math.NaN(), 20, 30}) {
		t.Fatalf("chart %v %v", c.X, c.Y)
	}
	segs := c.Segments(0)
	if len(segs) != 2 || len(segs[0]) != 1 || len(segs[1]) != 2 || segs[1][0] != (Point{x0 + 60, 20}) {
		t.Errorf("segments %v", segs)
	}
}

func TestChartDataOneColumnPerPicker(t *testing.T) {
	v := sysinfo.Vitals{At: at("2026-11-14T09:00:00Z"), DiskIO: &sysinfo.DiskIO{ReadPerSec: 5, WritePerSec: 7}}
	c := ChartData([]sysinfo.Vitals{v},
		func(v sysinfo.Vitals) (float64, bool) { return v.DiskIO.ReadPerSec, true },
		func(v sysinfo.Vitals) (float64, bool) { return v.DiskIO.WritePerSec, true })
	if len(c.X) != 1 || len(c.Y) != 2 || c.Y[0][0] != 5 || c.Y[1][0] != 7 {
		t.Errorf("chart %v %v", c.X, c.Y)
	}
}

func TestChartTop(t *testing.T) {
	c := Chart{X: []float64{0, 1, 2}, Y: [][]float64{{100, math.NaN(), 2000}, {500, 10, 0}}}
	if got := c.Top(100, 1); got != 100 {
		t.Errorf("fixed top %v", got)
	}
	if got := c.Top(0, 1000); math.Abs(got-2300) > 1e-9 {
		t.Errorf("fitted top %v", got)
	}
	// An idle disk draws a flat line at the bottom instead of magnifying noise.
	idle := Chart{X: []float64{0}, Y: [][]float64{{3}}}
	if got := idle.Top(0, 1000); math.Abs(got-1150) > 1e-9 {
		t.Errorf("idle top %v", got)
	}
}

func TestAppendSampleDropsResentHistory(t *testing.T) {
	a := sysinfo.Vitals{At: at("2026-11-14T09:00:00Z")}
	b := sysinfo.Vitals{At: at("2026-11-14T09:00:02Z")}
	list := AppendSample(AppendSample(nil, a), b)
	if got := AppendSample(list, a); &got[0] != &list[0] || len(got) != 2 {
		t.Error("an older sample was added")
	}
	if got := AppendSample(list, sysinfo.Vitals{At: at("2026-11-14T09:00:02Z")}); &got[0] != &list[0] || len(got) != 2 {
		t.Error("a sample at the same time was added")
	}
	got := AppendSample(list, sysinfo.Vitals{At: at("2026-11-14T09:00:04Z")})
	var times []string
	for _, v := range got {
		times = append(times, v.At.Format(time.RFC3339))
	}
	if !slices.Equal(times, []string{"2026-11-14T09:00:00Z", "2026-11-14T09:00:02Z", "2026-11-14T09:00:04Z"}) {
		t.Errorf("times %v", times)
	}
}

func TestAppendSampleKeepsTheNewestTwoMinutes(t *testing.T) {
	var list []sysinfo.Vitals
	start := at("2026-11-14T09:00:00Z")
	for i := range HistoryLength + 5 {
		prev := list
		list = AppendSample(list, sysinfo.Vitals{At: start.Add(time.Duration(i) * 2 * time.Second)})
		if len(prev) > 0 && !prev[0].At.Equal(start.Add(time.Duration(max(0, i-HistoryLength))*2*time.Second)) {
			t.Fatal("AppendSample changed the slice it was given")
		}
	}
	if len(list) != HistoryLength || !list[0].At.Equal(start.Add(5*2*time.Second)) {
		t.Errorf("kept %d from %v", len(list), list[0].At)
	}
}
