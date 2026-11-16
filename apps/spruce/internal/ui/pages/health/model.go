package health

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"spruce/apps/spruce/internal/engine/sysinfo"
	"spruce/apps/spruce/internal/ui/format"
)

// FormatPercent renders a percent rounded to a whole number, e.g. "23%".
func FormatPercent(v float64) string {
	return strconv.Itoa(int(math.Round(max(0, v)))) + "%"
}

// FormatProcessCPU renders a process's CPU percent (of one logical CPU, so it can pass 100) with one decimal below 10, as Activity Monitor does: "0.4%", "12%", "250%".
func FormatProcessCPU(v float64) string {
	v = max(0, v)
	if v < 10 {
		return strconv.FormatFloat(v, 'f', 1, 64) + "%"
	}
	return strconv.Itoa(int(math.Round(v))) + "%"
}

// FormatBytes renders a byte count as the file manager counts it (format.Bytes), rounding a fractional one.
func FormatBytes(v float64) string {
	if math.IsNaN(v) {
		return format.Bytes(0)
	}
	return format.Bytes(int64(math.Round(v)))
}

// FormatRate renders a throughput in bytes per second, e.g. "1.2 MB/s".
func FormatRate(bytesPerSecond float64) string { return FormatBytes(bytesPerSecond) + "/s" }

// FormatLoad renders the 1, 5 and 15 minute load averages, e.g. "2.31 / 1.98 / 1.75".
func FormatLoad(l sysinfo.LoadAverage) string {
	return strings.Join([]string{
		strconv.FormatFloat(l.One, 'f', 2, 64), strconv.FormatFloat(l.Five, 'f', 2, 64), strconv.FormatFloat(l.Fifteen, 'f', 2, 64),
	}, " / ")
}

// FormatMinutes renders a battery time estimate, e.g. "45 分钟", "2 小时 5 分钟", "3 小时".
func FormatMinutes(minutes float64) string {
	m := int(math.Round(max(0, minutes)))
	if m < 60 {
		return fmt.Sprintf("%d 分钟", m)
	}
	hours, rest := m/60, m%60
	if rest == 0 {
		return fmt.Sprintf("%d 小时", hours)
	}
	return fmt.Sprintf("%d 小时 %d 分钟", hours, rest)
}

// FormatUptime renders how long the system has been up, e.g. "12 分钟", "5 小时 3 分钟", "3 天 4 小时".
func FormatUptime(d time.Duration) string {
	minutes := int64(max(0, d) / time.Minute)
	if minutes < 1 {
		return "不到 1 分钟"
	}
	if minutes < 60 {
		return fmt.Sprintf("%d 分钟", minutes)
	}
	hours := minutes / 60
	if hours < 24 {
		if rest := minutes % 60; rest != 0 {
			return fmt.Sprintf("%d 小时 %d 分钟", hours, rest)
		}
		return fmt.Sprintf("%d 小时", hours)
	}
	days, rest := hours/24, hours%24
	if rest == 0 {
		return fmt.Sprintf("%d 天", days)
	}
	return fmt.Sprintf("%d 天 %d 小时", days, rest)
}

// FormatBootTime renders when the system started in local time, e.g. "11月11日 09:12"; "" for an unknown time.
func FormatBootTime(t time.Time) string {
	if t.IsZero() || t.UTC().Year() <= 1 {
		return ""
	}
	t = t.Local()
	return fmt.Sprintf("%d月%d日 %s", int(t.Month()), t.Day(), format.Clock(t))
}

// BatteryStateText describes what the battery is doing, or "" when unknown.
func BatteryStateText(s sysinfo.BatteryState) string {
	switch s {
	case sysinfo.BatteryCharging:
		return "正在充电"
	case sysinfo.BatteryDischarging:
		return "正在使用电池"
	case sysinfo.BatteryFull:
		return "已充满"
	case sysinfo.BatteryNotCharging:
		return "已接通电源，未在充电"
	}
	return ""
}

// BatteryTimeText is the time estimate for the battery's state, e.g. "剩余约 3 小时 5 分钟" or "充满还需 38 分钟"; "" when there is none.
func BatteryTimeText(s sysinfo.BatteryState, minutes *int) string {
	if minutes == nil || *minutes <= 0 {
		return ""
	}
	switch s {
	case sysinfo.BatteryDischarging:
		return "剩余约 " + FormatMinutes(float64(*minutes))
	case sysinfo.BatteryCharging:
		return "充满还需 " + FormatMinutes(float64(*minutes))
	}
	return ""
}

// BatteryCaption is the battery card's line under the charge, e.g. "正在充电 · 充满还需 38 分钟"; "" when neither part is known.
func BatteryCaption(b sysinfo.BatteryStats) string {
	var parts []string
	for _, s := range []string{BatteryStateText(b.State), BatteryTimeText(b.State, b.MinutesRemaining)} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " · ")
}

// PressureText names a memory pressure level as Activity Monitor's graph colours it.
func PressureText(p sysinfo.MemoryPressure) string {
	switch p {
	case sysinfo.PressureWarning:
		return "偏高"
	case sysinfo.PressureCritical:
		return "紧张"
	}
	return "正常"
}

// CoresText describes the CPU's cores, e.g. "12 核（8 性能核 + 4 能效核）" or "8 核 16 线程"; "" when unknown.
func CoresText(info sysinfo.SystemInfo) string {
	physical, logical := info.PhysicalCores, info.LogicalCores
	if physical <= 0 && logical <= 0 {
		return ""
	}
	if physical <= 0 {
		return fmt.Sprintf("%d 线程", logical)
	}
	text := fmt.Sprintf("%d 核", physical)
	if info.PerformanceCores > 0 && info.EfficiencyCores > 0 {
		text += fmt.Sprintf("（%d 性能核 + %d 能效核）", info.PerformanceCores, info.EfficiencyCores)
	}
	if logical > physical {
		text += fmt.Sprintf(" %d 线程", logical)
	}
	return text
}

// CPUFoot is the CPU card's last line, the model and the cores, e.g. "Apple M4 Max · 12 核（8 性能核 + 4 能效核）"; "" when neither is known.
func CPUFoot(info sysinfo.SystemInfo) string {
	var parts []string
	for _, s := range []string{info.CPUModel, CoresText(info)} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " · ")
}

// OSText renders the system's name and version, e.g. "macOS 26.1" or "Windows 11 Pro 24H2"; the build number has its own row on the 系统 card so the headline fits on one line.
func OSText(info sysinfo.SystemInfo) string {
	var parts []string
	for _, s := range []string{info.OSName, info.OSVersion} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " ")
}

// UptimeText is the 系统 card's 已运行 row: 无法读取 without a boot time, 正在读取… before the first sample, else the time from boot to the latest sample, so it moves with the samples.
func UptimeText(info sysinfo.SystemInfo, latest *sysinfo.Vitals) string {
	switch {
	case FormatBootTime(info.BootTime) == "":
		return "无法读取"
	case latest == nil:
		return "正在读取…"
	}
	return FormatUptime(latest.At.Sub(info.BootTime))
}

// CoresLabel is what assistive technology says for the per-core bars, e.g. "各核心占用：10%、40%、5%、39%".
func CoresLabel(perCore []float64) string {
	parts := make([]string, len(perCore))
	for i, p := range perCore {
		parts[i] = FormatPercent(p)
	}
	return "各核心占用：" + strings.Join(parts, "、")
}

// Level is how full a meter is, which colours its fill.
type Level uint8

// The levels.
const (
	LevelNormal Level = iota
	LevelWarning
	LevelCritical
)

// UsageLevel is the level of a disk that is percent used: warning from 85, critical from 95.
func UsageLevel(percent float64) Level {
	switch {
	case percent >= 95:
		return LevelCritical
	case percent >= 85:
		return LevelWarning
	}
	return LevelNormal
}

// PressureLevel is the memory meter's level from the kernel's memory pressure.
func PressureLevel(p sysinfo.MemoryPressure) Level {
	switch p {
	case sysinfo.PressureCritical:
		return LevelCritical
	case sysinfo.PressureWarning:
		return LevelWarning
	}
	return LevelNormal
}

// BatteryLevel is critical while the battery runs the machine at 10% or less.
func BatteryLevel(b sysinfo.BatteryStats) Level {
	if b.State == sysinfo.BatteryDischarging && b.Percent <= 10 {
		return LevelCritical
	}
	return LevelNormal
}

// DiskUsedPercent is the share of the system volume in use, 0 to 100.
func DiskUsedPercent(d sysinfo.DiskUsage) float64 {
	if d.Total == 0 {
		return 0
	}
	return float64(d.Used) / float64(d.Total) * 100
}

// SwapLabel names the swap space as the system does: 页面文件 on Windows, 交换内存 elsewhere.
func SwapLabel(goos string) string {
	if goos == "windows" {
		return "页面文件"
	}
	return "交换内存"
}

// ProcessFootnote is the note under the process lists: what the CPU figure counts on this system, and that Spruce never ends a process.
func ProcessFootnote(goos string) string {
	unit := "CPU 按单个处理器核心计算，100% 即占满一个核心，与活动监视器一致。其他用户和系统的进程（如 WindowServer）无法读取，不在列表中。"
	if goos == "windows" {
		unit = "CPU 按单个处理器核心计算，100% 即占满一个核心；任务管理器按全部核心显示，数值会更低。受保护的系统进程读不到 CPU 占用。"
	}
	return unit + "Spruce 只显示进程，不会结束进程。"
}

// ProcessCPU is a process list's CPU cell, "—" when unknown.
func ProcessCPU(p sysinfo.ProcessUsage) string {
	if p.CPUPercent == nil {
		return "—"
	}
	return FormatProcessCPU(*p.CPUPercent)
}

// ProcessMemory is a process list's memory cell, "—" when unknown.
func ProcessMemory(p sysinfo.ProcessUsage) string {
	if p.Memory == nil {
		return "—"
	}
	return format.Bytes(int64(*p.Memory))
}

// MaxSampleGap is the longest gap between two samples a chart draws a line across; a longer one (sampling stopped while the page was away) leaves the line broken.
const MaxSampleGap = 5 * time.Second

// Chart is what a sparkline draws: X in Unix seconds, oldest first, and one column per series with NaN where a value is missing.
type Chart struct {
	X []float64
	Y [][]float64
}

// Picker reads one series' value from a sample; ok is false where it could not be read.
type Picker func(v sysinfo.Vitals) (value float64, ok bool)

// ChartData turns samples (oldest first) into a Chart with one column per picker. A NaN row is inserted into a gap longer than MaxSampleGap so the line breaks there.
func ChartData(samples []sysinfo.Vitals, pickers ...Picker) Chart {
	c := Chart{Y: make([][]float64, len(pickers))}
	gap := MaxSampleGap.Seconds()
	var prev float64
	has := false
	for _, v := range samples {
		if v.At.IsZero() {
			continue
		}
		x := float64(v.At.UnixNano()) / 1e9
		if has && x-prev > gap {
			c.X = append(c.X, (prev+x)/2)
			for i := range c.Y {
				c.Y[i] = append(c.Y[i], math.NaN())
			}
		}
		c.X = append(c.X, x)
		for i, pick := range pickers {
			y := math.NaN()
			if value, ok := pick(v); ok {
				y = value
			}
			c.Y[i] = append(c.Y[i], y)
		}
		prev, has = x, true
	}
	return c
}

// Point is a point of a chart in its own units.
type Point struct{ X, Y float64 }

// Segments splits series i into runs of points between missing values: each is drawn as one line, so the line breaks where a value is missing.
func (c Chart) Segments(i int) [][]Point {
	var out [][]Point
	var cur []Point
	for j, x := range c.X {
		y := c.Y[i][j]
		if math.IsNaN(y) || math.IsInf(y, 0) {
			if len(cur) > 0 {
				out = append(out, cur)
				cur = nil
			}
			continue
		}
		cur = append(cur, Point{x, y})
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

// Top is the top of the chart's y axis: fixed when fixed is above 0 (100 for percentages), else 15% above the highest value but never below minTop.
func (c Chart) Top(fixed, minTop float64) float64 {
	if fixed > 0 {
		return fixed
	}
	top := 0.0
	for _, col := range c.Y {
		for _, y := range col {
			if !math.IsNaN(y) && !math.IsInf(y, 0) {
				top = max(top, y)
			}
		}
	}
	return max(top, minTop) * 1.15
}

// HistoryLength is how many samples the page keeps: two minutes at one every two seconds, like the sampler.
const HistoryLength = 60

// AppendSample returns samples with next added, oldest first, keeping the newest HistoryLength. A sample no newer than the last one (the history the sampler sends again when the page watches again) returns samples itself. It never changes samples' backing array.
func AppendSample(samples []sysinfo.Vitals, next sysinfo.Vitals) []sysinfo.Vitals {
	if n := len(samples); n > 0 && !next.At.After(samples[n-1].At) {
		return samples
	}
	keep := samples
	if len(keep) >= HistoryLength {
		keep = keep[len(keep)-HistoryLength+1:]
	}
	out := make([]sysinfo.Vitals, 0, len(keep)+1)
	out = append(out, keep...)
	return append(out, next)
}
