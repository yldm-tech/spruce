package uninstall

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/ui/format"
)

// AppDetails is what Measure has streamed for one app so far.
type AppDetails struct {
	Size     int64
	HasSize  bool
	Usage    app.UninstallUsage
	LastUsed time.Time
	Icon     string
}

// MergeDetails adds streamed updates to what is known of the apps, field by field, in a new map; known is left alone.
func MergeDetails(known map[string]AppDetails, updates []app.UninstallAppDetails) map[string]AppDetails {
	next := make(map[string]AppDetails, len(known)+len(updates))
	for id, d := range known {
		next[id] = d
	}
	for _, u := range updates {
		cur := next[u.ID]
		if u.Size != nil {
			cur.Size, cur.HasSize = *u.Size, true
		}
		if u.Usage != "" {
			cur.Usage, cur.LastUsed = u.Usage, u.LastUsed
		}
		if u.Icon != "" {
			cur.Icon = u.Icon
		}
		next[u.ID] = cur
	}
	return next
}

// SortKey is how the list is ordered. On Windows the third order is by install date, since Windows keeps no record of last use.
type SortKey uint8

// The orders: by name, largest first (the default), most recent first.
const (
	SortName SortKey = iota
	SortSize
	SortRecent
)

// nameOrder orders names as Intl.Collator("zh-CN", {numeric: true, sensitivity: "base"}) does: pinyin for Chinese, digits by value, case and accents ignored, and Chinese before Latin. x/text's collation puts Latin first and cannot reorder scripts (collate.Reorder is not implemented), so where two names first differ in a Chinese and a non-Chinese letter the Chinese one goes first, as ICU's zh tailoring has it.
type nameOrder struct {
	keys map[string][]byte
}

func newNameOrder(apps []app.UninstallApp) nameOrder {
	c := collate.New(language.SimplifiedChinese, collate.Numeric, collate.Loose)
	var buf collate.Buffer
	keys := make(map[string][]byte, len(apps))
	for _, a := range apps {
		keys[a.Name] = slices.Clone(c.KeyFromString(&buf, a.Name))
		buf.Reset()
	}
	return nameOrder{keys: keys}
}

// scriptClass groups a rune for the reordering: what is not a letter first, then Chinese, then other letters.
func scriptClass(r rune) int {
	switch {
	case unicode.Is(unicode.Han, r) || unicode.Is(unicode.Bopomofo, r):
		return 1
	case unicode.IsLetter(r):
		return 2
	}
	return 0
}

func (o nameOrder) compare(a, b string) int {
	ra, rb := []rune(strings.ToLower(a)), []rune(strings.ToLower(b))
	for i := 0; i < len(ra) && i < len(rb); i++ {
		if ra[i] == rb[i] {
			continue
		}
		if ca, cb := scriptClass(ra[i]), scriptClass(rb[i]); ca != cb && ca != 0 && cb != 0 {
			return ca - cb
		}
		break
	}
	return bytes.Compare(o.keys[a], o.keys[b])
}

// known reports whether t is a real time (not the zero time or year 1).
func known(t time.Time) bool { return !t.IsZero() && t.Year() > 1 }

// RecentTime is the time the 最近使用 order uses: the last use on macOS, the install date on Windows; false when unknown.
func RecentTime(a app.UninstallApp, d AppDetails, windows bool) (time.Time, bool) {
	if windows {
		return a.InstallDate, known(a.InstallDate)
	}
	if d.Usage == app.UninstallUsed && known(d.LastUsed) {
		return d.LastUsed, true
	}
	return time.Time{}, false
}

// VisibleApps filters the apps by the search text (name, bundle ID or publisher, ignoring case) and sorts them: by name, largest or most recent first; unknown sizes and times go last, ties by name. apps is left as it is.
func VisibleApps(apps []app.UninstallApp, details map[string]AppDetails, query string, sort SortKey, windows bool) []app.UninstallApp {
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]app.UninstallApp, 0, len(apps))
	for _, a := range apps {
		if q == "" || strings.Contains(strings.ToLower(a.Name), q) || strings.Contains(strings.ToLower(a.BundleID), q) ||
			strings.Contains(strings.ToLower(a.Publisher), q) {
			out = append(out, a)
		}
	}
	names := newNameOrder(out)
	byName := func(a, b app.UninstallApp) int { return names.compare(a.Name, b.Name) }
	switch sort {
	case SortName:
		slices.SortStableFunc(out, byName)
	case SortSize:
		size := func(a app.UninstallApp) int64 {
			if d, ok := details[a.ID]; ok && d.HasSize {
				return d.Size
			}
			return -1
		}
		slices.SortStableFunc(out, func(a, b app.UninstallApp) int {
			if sa, sb := size(a), size(b); sa != sb {
				if sb > sa {
					return 1
				}
				return -1
			}
			return byName(a, b)
		})
	case SortRecent:
		slices.SortStableFunc(out, func(a, b app.UninstallApp) int {
			ta, oka := RecentTime(a, details[a.ID], windows)
			tb, okb := RecentTime(b, details[b.ID], windows)
			switch {
			case oka != okb:
				if oka {
					return -1
				}
				return 1
			case !oka || ta.Equal(tb):
				return byName(a, b)
			case tb.After(ta):
				return 1
			}
			return -1
		})
	}
	return out
}

// UsageText is the 最近使用 line of an app: the relative time, 从未使用, or "" when unknown. On Windows it is the install date.
func UsageText(a app.UninstallApp, d AppDetails, windows bool, now time.Time) string {
	if windows {
		if when := format.RelativeTime(a.InstallDate, now); when != "" {
			return "安装于 " + when
		}
		return ""
	}
	switch d.Usage {
	case app.UninstallUsed:
		if when := format.RelativeTime(d.LastUsed, now); when != "" {
			return when + "使用过"
		}
	case app.UninstallNeverUsed:
		return "从未使用"
	}
	return ""
}

// SizeText is the size of an app as the list shows it while sizes stream in.
func SizeText(d AppDetails, measuring, windows bool) string {
	switch {
	case d.HasSize:
		return format.BytesIn(d.Size, windows)
	case measuring:
		return "正在计算…"
	}
	return "大小未知"
}

// KindLabels are what each kind of leftover is called.
var KindLabels = map[app.UninstallLeftoverKind]string{
	app.UninstallKindApp:             "应用本身",
	app.UninstallKindSupport:         "应用支持文件",
	app.UninstallKindCaches:          "缓存",
	app.UninstallKindPreferences:     "偏好设置",
	app.UninstallKindContainers:      "沙盒容器",
	app.UninstallKindGroupContainers: "共享容器",
	app.UninstallKindSavedState:      "窗口状态",
	app.UninstallKindHTTPStorage:     "网络存储",
	app.UninstallKindWebKit:          "网页数据",
	app.UninstallKindLogs:            "日志",
	app.UninstallKindCookies:         "Cookie",
	app.UninstallKindLaunchAgents:    "登录启动项",
	app.UninstallKindAppData:         "用户数据（Roaming）",
	app.UninstallKindLocalAppData:    "本地数据（Local）",
	app.UninstallKindPrograms:        "安装文件夹",
}

// DefaultChecked is the set of leftovers that start checked.
func DefaultChecked(leftovers []app.UninstallLeftover) map[string]bool {
	out := map[string]bool{}
	for _, l := range leftovers {
		if l.Selected {
			out[l.ID] = true
		}
	}
	return out
}

// CheckedTotals counts and sizes the checked leftovers.
func CheckedTotals(leftovers []app.UninstallLeftover, checked map[string]bool) (count int, size int64) {
	for _, l := range leftovers {
		if checked[l.ID] {
			count++
			size += l.Size
		}
	}
	return count, size
}

// CheckedIDs are the IDs of the checked leftovers, in the list's order.
func CheckedIDs(leftovers []app.UninstallLeftover, checked map[string]bool) []string {
	ids := []string{}
	for _, l := range leftovers {
		if checked[l.ID] {
			ids = append(ids, l.ID)
		}
	}
	return ids
}

// trashName is what the system calls its Trash.
func trashName(windows bool) string {
	if windows {
		return "回收站"
	}
	return "废纸篓"
}

// fileManagerName is the system's file manager.
func fileManagerName(windows bool) string {
	if windows {
		return "资源管理器"
	}
	return "访达"
}

// ConfirmMessage is the question asked before an app goes to the Trash (macOS).
func ConfirmMessage(name string, leftovers int, size int64, windows bool) string {
	what := "「" + name + "」"
	if leftovers > 0 {
		what = fmt.Sprintf("「%s」和勾选的 %d 个相关文件", name, leftovers)
	}
	return fmt.Sprintf("%s（共 %s）会移到%s，可以随时放回。", what, format.BytesIn(size, windows), trashName(windows))
}

// DoneSummary is what a done card shows.
type DoneSummary struct {
	Title, Big string
	Lines      []string
	Succeeded  bool
}

// maxFailureLines bounds the failed items a done card names.
const maxFailureLines = 8

func leftoverLines(r cleanup.CleanResult, bin, skipCategory string) []string {
	var lines []string
	moved, failed := 0, 0
	for _, c := range r.Categories {
		if skipCategory != "" && c.CategoryID == skipCategory {
			continue
		}
		moved += c.Removed
		failed += c.Failed
	}
	if moved > 0 {
		lines = append(lines, fmt.Sprintf("相关文件：%d 项已移到%s", moved, bin))
	}
	if failed > 0 {
		lines = append(lines, fmt.Sprintf("相关文件：%d 项未能处理", failed))
	}
	for _, f := range r.Failed[:min(len(r.Failed), maxFailureLines)] {
		lines = append(lines, format.BaseName(f.Path)+"："+f.Reason)
	}
	if len(r.Failed) > maxFailureLines {
		lines = append(lines, fmt.Sprintf("另有 %d 项未能处理", len(r.Failed)-maxFailureLines))
	}
	if r.Skipped > 0 {
		lines = append(lines, fmt.Sprintf("%d 项在查找后已不存在，已跳过", r.Skipped))
	}
	if r.Interrupted {
		lines = append(lines, "卸载在中途停止，其余项目没有改动")
	}
	if r.Trashed > 0 {
		lines = append(lines, fmt.Sprintf("移到%s的项目在清空%s后才会释放空间", bin, bin))
	}
	return lines
}

// RemoveSummary is the done card after the app (macOS) or its leftovers (Windows) went to the Trash.
func RemoveSummary(o app.UninstallOutcome, windows bool) DoneSummary {
	bin := trashName(windows)
	r := o.Result
	if o.BundleFailure != "" {
		return DoneSummary{Title: "没能卸载「" + o.App + "」", Big: "未卸载", Lines: []string{o.BundleFailure, "它的相关文件也没有改动。"}}
	}
	head, skip, ok := "「"+o.App+"」已移到"+bin, string(app.UninstallKindApp), o.BundleRemoved
	if windows {
		head, skip, ok = "卸载程序已移除「"+o.App+"」", "", r.Removed > 0 || len(r.Failed) == 0
	}
	title := "已卸载「" + o.App + "」"
	if r.Interrupted {
		title = "卸载已中断"
	}
	return DoneSummary{Title: title, Big: format.BytesIn(r.Trashed, windows), Lines: append([]string{head}, leftoverLines(r, bin, skip)...), Succeeded: ok}
}

// UninstalledSummary is the done card of a Windows program its uninstaller removed and that left nothing behind (or whose leftovers the user kept).
func UninstalledSummary(name string, leftoversFound int) DoneSummary {
	second := "没有找到它留下的文件"
	if leftoversFound > 0 {
		second = fmt.Sprintf("留下的 %d 个相关文件没有改动", leftoversFound)
	}
	return DoneSummary{Title: "已卸载「" + name + "」", Big: "1 个程序", Lines: []string{"卸载程序已移除「" + name + "」", second}, Succeeded: true}
}

// FailedSummary is the done card of an uninstall that could not run, with why.
func FailedSummary(reason string) DoneSummary {
	return DoneSummary{Title: "卸载没有进行", Big: "未卸载", Lines: []string{reason}}
}

// ProgressLine is the removal overlay's progress line.
func ProgressLine(p cleanup.Progress, windows bool) string {
	if p.Total > 0 {
		return fmt.Sprintf("已处理 %d/%d · 已释放 %s", p.Done, p.Total, format.BytesIn(p.Bytes, windows))
	}
	return "正在准备…"
}
