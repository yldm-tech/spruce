package settings

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/history"
	"spruce/apps/spruce/internal/engine/maintenance"
	"spruce/apps/spruce/internal/engine/rules"
	"spruce/apps/spruce/internal/engine/settings"
	"spruce/apps/spruce/internal/ui/format"
)

// Field is how one control reads and writes its part of the settings.
type Field[T any] struct {
	Get func(settings.AppSettings) T
	Put func(settings.AppSettings, T) settings.AppSettings
}

// The fields of the page's controls.
var (
	FieldLaunchAtLogin = Field[bool]{
		Get: func(s settings.AppSettings) bool { return s.General.LaunchAtLogin },
		Put: func(s settings.AppSettings, v bool) settings.AppSettings { s.General.LaunchAtLogin = v; return s },
	}
	FieldKeepInTray = Field[bool]{
		Get: func(s settings.AppSettings) bool { return s.General.KeepInTray },
		Put: func(s settings.AppSettings, v bool) settings.AppSettings { s.General.KeepInTray = v; return s },
	}
	FieldBackgroundEnabled = Field[bool]{
		Get: func(s settings.AppSettings) bool { return s.Background.Enabled },
		Put: func(s settings.AppSettings, v bool) settings.AppSettings { s.Background.Enabled = v; return s },
	}
	FieldLowDiskAlert = Field[bool]{
		Get: func(s settings.AppSettings) bool { return s.General.LowDiskAlert },
		Put: func(s settings.AppSettings, v bool) settings.AppSettings { s.General.LowDiskAlert = v; return s },
	}
	FieldLowDiskGB = Field[int]{
		Get: func(s settings.AppSettings) int { return s.General.LowDiskGB },
		Put: func(s settings.AppSettings, v int) settings.AppSettings { s.General.LowDiskGB = v; return s },
	}
	FieldWeeklySummary = Field[bool]{
		Get: func(s settings.AppSettings) bool { return s.Automation.WeeklySummary },
		Put: func(s settings.AppSettings, v bool) settings.AppSettings { s.Automation.WeeklySummary = v; return s },
	}
	FieldHealthEnabled = Field[bool]{
		Get: func(s settings.AppSettings) bool { return s.Health.Enabled },
		Put: func(s settings.AppSettings, v bool) settings.AppSettings { s.Health.Enabled = v; return s },
	}
	FieldCPUThreshold = Field[int]{
		Get: func(s settings.AppSettings) int { return s.Health.CPUThreshold },
		Put: func(s settings.AppSettings, v int) settings.AppSettings { s.Health.CPUThreshold = v; return s },
	}
	FieldSustainMinutes = Field[int]{
		Get: func(s settings.AppSettings) int { return s.Health.SustainMinutes },
		Put: func(s settings.AppSettings, v int) settings.AppSettings { s.Health.SustainMinutes = v; return s },
	}
	FieldExclusions = Field[[]string]{
		Get: func(s settings.AppSettings) []string { return s.Cleanup.Exclusions },
		Put: func(s settings.AppSettings, v []string) settings.AppSettings { s.Cleanup.Exclusions = v; return s },
	}
	FieldDevProjectRoots = Field[[]string]{
		Get: func(s settings.AppSettings) []string { return s.Cleanup.DevProjectRoots },
		Put: func(s settings.AppSettings, v []string) settings.AppSettings { s.Cleanup.DevProjectRoots = v; return s },
	}
	FieldDevArtifactMinAgeDays = Field[int]{
		Get: func(s settings.AppSettings) int { return s.Cleanup.DevArtifactMinAgeDays },
		Put: func(s settings.AppSettings, v int) settings.AppSettings {
			s.Cleanup.DevArtifactMinAgeDays = v
			return s
		},
	}
	FieldInstallerRoots = Field[[]string]{
		Get: func(s settings.AppSettings) []string { return s.Cleanup.InstallerRoots },
		Put: func(s settings.AppSettings, v []string) settings.AppSettings { s.Cleanup.InstallerRoots = v; return s },
	}
	FieldLargeFileMinMB = Field[int]{
		Get: func(s settings.AppSettings) int { return s.Cleanup.LargeFileMinMB },
		Put: func(s settings.AppSettings, v int) settings.AppSettings { s.Cleanup.LargeFileMinMB = v; return s },
	}
	FieldRetentionDays = Field[int]{
		Get: func(s settings.AppSettings) int { return s.History.RetentionDays },
		Put: func(s settings.AppSettings, v int) settings.AppSettings { s.History.RetentionDays = v; return s },
	}
)

// RuleField is the on/off field of the automatic rule id.
func RuleField(id rules.ID) Field[bool] {
	at := func(a *settings.AutomationSettings) *bool {
		switch id {
		case rules.AppCaches:
			return &a.AppCaches
		case rules.Installers:
			return &a.Installers
		case rules.BuildOutput:
			return &a.BuildOutput
		}
		return &a.IncompleteDownloads
	}
	return Field[bool]{
		Get: func(s settings.AppSettings) bool { return *at(&s.Automation) },
		Put: func(s settings.AppSettings, v bool) settings.AppSettings { *at(&s.Automation) = v; return s },
	}
}

// RuleDescription is what an automatic rule does, as 设置 → 自动清理 says under its label.
func RuleDescription(id rules.ID) string {
	trash := format.TrashName()
	switch id {
	case rules.AppCaches:
		return "每周把 30 天没有改动的应用缓存移到" + trash + "。正在运行的应用的缓存不会处理。"
	case rules.Installers:
		return "每天把「下载」里超过 7 天、对应的应用已经装好的安装包和磁盘映像（.dmg、.pkg）移到" + trash + "。压缩包、系统映像和其他文件夹里的不会处理，正在挂载或无法确认是否挂载的也不会处理。"
	case rules.BuildOutput:
		return "每周把 90 天没有改动的项目里的 node_modules、target 等构建产物移到" + trash + "。有未提交改动、无法确定 Git 状态，或构建产物文件夹里有纳入版本控制的文件的项目不会处理；不是 Git 仓库的项目按最后改动时间判断。"
	case rules.IncompleteDownloads:
		return "每天把超过 7 天没有变化的未完成下载移到" + trash + "。"
	}
	return ""
}

// RuleStatusText is the grey line under an automatic rule: when it is on, its last run (which does not depend on a check result) or that it has not run yet; when it is off, what it would take from the current result, or that a check will tell while there is no result (hasResult false) or the controller does not report the rule (st nil).
func RuleStatusText(on bool, st *app.RuleStatus, hasResult bool, now time.Time) string {
	switch {
	case on && st != nil && !st.LastRun.IsZero():
		return "上次运行：" + format.RelativeTime(st.LastRun, now) + "，移到" + format.TrashName() + " " + format.Bytes(st.LastTrashed)
	case on:
		return "还没有运行过，下次后台检查时运行"
	case st == nil || !hasResult:
		return "检查后会显示符合条件的项目"
	}
	return "现在符合条件的：" + strconv.Itoa(st.Matching) + " 项，" + format.Bytes(st.MatchingBytes)
}

// RuleStatusOf returns the status of rule id in st, nil when the controller does not report the rule. It does not depend on st.Result: the last run of a rule is known without a check result.
func RuleStatusOf(st app.OverviewState, id rules.ID) *app.RuleStatus {
	for i := range st.Rules {
		if st.Rules[i].ID == string(id) {
			return &st.Rules[i]
		}
	}
	return nil
}

// maxWhole is the largest whole number a field takes, JavaScript's Number.MAX_SAFE_INTEGER as the React page had it.
const maxWhole = 1<<53 - 1

// ParseWhole returns the whole number text spells (digits only, surrounding spaces ignored) and whether it is one.
func ParseWhole(text string) (int, bool) {
	t := strings.TrimSpace(text)
	if t == "" {
		return 0, false
	}
	for _, c := range t {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(t, 10, 64)
	if err != nil || n > maxWhole {
		return 0, false
	}
	return int(n), true
}

// InRange reports whether n is within r.
func InRange(n int, r app.SettingRange) bool { return n >= r.Min && n <= r.Max }

// RetentionDrops counts the runs that keeping days days of history deletes as of now, as history.Store.Prune does.
func RetentionDrops(runs []history.Run, days int, now time.Time) int {
	cutoff := history.RetentionCutoff(now, days)
	n := 0
	for _, r := range runs {
		if r.StartedAt.Before(cutoff) {
			n++
		}
	}
	return n
}

// RetentionText is what the question before lowering 保留天数 to days says it deletes: drops runs, or, with drops < 0, the runs that could not be counted.
func RetentionText(days, drops int, trash string) string {
	what := "更早的操作记录"
	if drops >= 0 {
		what = "更早的 " + strconv.Itoa(drops) + " 条操作记录"
	}
	return "只保留最近 " + strconv.Itoa(days) + " 天，" + what + "会立即删除，无法恢复。这些记录中移到" + trash + "的项目也不能再在操作历史里放回原处。"
}

// RangeText is what a number field shows when its text is not a whole number within r.
func RangeText(r app.SettingRange) string {
	return "请输入 " + strconv.Itoa(r.Min) + " 到 " + strconv.Itoa(r.Max) + " 之间的整数"
}

// SameList reports whether a and b hold the same paths in the same order.
func SameList(a, b []string) bool { return slices.Equal(a, b) }

// ContainsFold reports whether list holds path, regardless of case, as the settings package compares paths.
func ContainsFold(list []string, path string) bool {
	return slices.ContainsFunc(list, func(p string) bool { return strings.EqualFold(p, path) })
}

// Without returns a new list holding list's paths except path.
func Without(list []string, path string) []string {
	out := make([]string, 0, len(list))
	for _, p := range list {
		if p != path {
			out = append(out, p)
		}
	}
	return out
}

// With returns a new list holding list's paths and then path.
func With(list []string, path string) []string {
	return append(slices.Clone(list), path)
}

// Version is the 关于 section's version line: the version, or 开发版 in a development build.
func Version(v string) string {
	if v == "" {
		v = "开发版"
	}
	return "版本 " + v
}

// PermissionText is how the 权限 section names a Full Disk Access state.
func PermissionText(st maintenance.PermissionState) string {
	switch st {
	case maintenance.PermissionGranted:
		return "已授权"
	case maintenance.PermissionDenied:
		return "未授权"
	case maintenance.PermissionNotRequired:
		return "不需要"
	}
	return "无法确定"
}

// License is one third-party library Spruce ships, for 关于 → 开源许可.
type License struct {
	Name string
	// License is its SPDX licence identifier.
	License string
}

// Licenses are the third-party modules linked into the app on macOS or Windows (go list -deps of the app for each target) and the bundled fonts, kept by hand with the dependencies, each with the licences of the code the app links from it: every one is permissive. NSGo is Apache-2.0, and the packages the app links carry Tailwind's palette (MIT), Ebitengine's Kage compiler as MyGo forked it (Apache-2.0 and MIT) and CLDR tables (Unicode-3.0), as its NOTICE says; plist's first licence is BSD-2-Clause with the FreeBSD "views and conclusions" clause, BSD-2-Clause-Views. The full texts ship in resources/THIRD_PARTY_NOTICES.txt, which scripts/notices.sh writes.
var Licenses = []License{
	{"NSGo", "Apache-2.0 AND MIT AND Unicode-3.0"},
	{"purego", "Apache-2.0"},
	{"gopsutil", "BSD-3-Clause"},
	{"go-sysconf", "BSD-3-Clause"},
	{"battery", "MIT"},
	{"plist", "BSD-2-Clause-Views AND BSD-3-Clause"},
	{"wmi", "MIT"},
	{"go-ole", "MIT"},
	{"go-yaml", "MIT AND Apache-2.0"},
	{"Go x/sys", "BSD-3-Clause"},
	{"Go x/text", "BSD-3-Clause"},
	{"Go x/image", "BSD-3-Clause"},
	{"Inter 字体", "OFL-1.1"},
}
