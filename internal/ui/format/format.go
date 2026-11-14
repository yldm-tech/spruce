// Package format renders numbers, sizes, times and paths the way the pages show them, in Simplified Chinese. It is the Go form of src/components/common/format.ts.
package format

import (
	"math"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// windows is whether sizes and names follow Windows (Explorer, 回收站) rather than macOS (Finder, 废纸篓).
var windows = runtime.GOOS == "windows"

// Bytes renders a byte count the way the system's file manager does, e.g. "1.2 GB": 1000-based on macOS (Finder), 1024-based with the same KB/MB/GB symbols on Windows (Explorer); whole numbers below 1 MB, one decimal from 1 MB up; a negative count is "0 B". The base must match internal/app's spaceMegabyte.
func Bytes(n int64) string { return BytesIn(n, windows) }

// sizeUnits are the symbols Bytes uses, from bytes up.
var sizeUnits = []string{"B", "KB", "MB", "GB", "TB", "PB", "EB"}

// BytesIn is Bytes with the base chosen: binary counts in 1024s.
func BytesIn(n int64, binary bool) string {
	if n <= 0 {
		return "0 B"
	}
	base := 1000.0
	if binary {
		base = 1024
	}
	v := float64(n)
	e := min(int(math.Floor(math.Log(v)/math.Log(base))), len(sizeUnits)-1)
	val := v / math.Pow(base, float64(e))
	decimals := 1
	if v < base*base {
		decimals = 0
	}
	p := math.Pow(10, float64(decimals))
	val = math.Round(val*p) / p
	// Rounding up to a whole next unit ("1000 KB") reads as that unit ("1 MB").
	if val == base && e < len(sizeUnits)-1 {
		val, e = 1, e+1
	}
	return strconv.FormatFloat(val, 'f', -1, 64) + " " + sizeUnits[e]
}

// Duration renders d in Chinese: "不到 1 秒", "12 秒", "3 分 20 秒", "12 分钟" (from 10 minutes the seconds go), "1 小时 5 分", "2 小时".
func Duration(d time.Duration) string {
	seconds := int64(max(d, 0) / time.Second)
	if seconds < 1 {
		return "不到 1 秒"
	}
	if seconds < 60 {
		return strconv.FormatInt(seconds, 10) + " 秒"
	}
	minutes := seconds / 60
	if minutes < 60 {
		rest := seconds % 60
		if rest == 0 || minutes >= 10 {
			return strconv.FormatInt(minutes, 10) + " 分钟"
		}
		return strconv.FormatInt(minutes, 10) + " 分 " + strconv.FormatInt(rest, 10) + " 秒"
	}
	hours := minutes / 60
	rest := minutes % 60
	if rest == 0 {
		return strconv.FormatInt(hours, 10) + " 小时"
	}
	return strconv.FormatInt(hours, 10) + " 小时 " + strconv.FormatInt(rest, 10) + " 分"
}

// RelativeTime renders t relative to now in Chinese, as Intl.RelativeTimeFormat("zh-CN", {numeric: "auto"}) does: "刚刚", "5分钟前", "3小时前", "昨天", "前天", "3天前", then the date (MonthDay). The zero time gives "". Days count calendar days in t's location.
func RelativeTime(t, now time.Time) string {
	if t.IsZero() || t.Year() <= 1 {
		return ""
	}
	diff := now.Sub(t)
	switch {
	case diff < 0:
		return MonthDay(t, now)
	case diff < time.Minute:
		return "刚刚"
	case diff < time.Hour:
		return strconv.Itoa(int(diff/time.Minute)) + "分钟前"
	case diff < 24*time.Hour:
		return strconv.Itoa(int(diff/time.Hour)) + "小时前"
	}
	now = now.In(t.Location())
	day := func(x time.Time) time.Time { return time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, x.Location()) }
	days := max(int(math.Round(day(now).Sub(day(t)).Hours()/24)), 1)
	switch {
	case days == 1:
		return "昨天"
	case days == 2:
		return "前天"
	case days < 7:
		return strconv.Itoa(days) + "天前"
	}
	return MonthDay(t, now)
}

// MonthDay renders t's date: "11月2日" in now's year, "2025年11月2日" in another.
func MonthDay(t, now time.Time) string {
	md := strconv.Itoa(int(t.Month())) + "月" + strconv.Itoa(t.Day()) + "日"
	if t.Year() == now.In(t.Location()).Year() {
		return md
	}
	return strconv.Itoa(t.Year()) + "年" + md
}

// Clock renders t's time of day on a 24-hour clock, "09:05".
func Clock(t time.Time) string { return t.Format("15:04") }

// Thousands renders n with a comma every three digits, "12,345".
func Thousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// TruncateMiddle shortens s to at most max characters (runes) by replacing its middle with "…", keeping 60% of the rest at the end, which for a path is the file name.
func TruncateMiddle(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	keep := max - 1
	tail := int(math.Ceil(float64(keep) * 0.6))
	return string(r[:keep-tail]) + "…" + string(r[len(r)-tail:])
}

// trimSeparators drops the separators at the end of path, of either platform.
func trimSeparators(path string) string { return strings.TrimRight(path, `/\`) }

// lastSeparator is the index of path's last separator, of either platform, or -1.
func lastSeparator(path string) int { return strings.LastIndexAny(path, `/\`) }

// BaseName returns the last component of path, for either separator.
func BaseName(path string) string {
	p := trimSeparators(path)
	return p[lastSeparator(p)+1:]
}

// ParentPath returns path without its last component, for either separator: "/a" gives "/".
func ParentPath(path string) string {
	p := trimSeparators(path)
	i := lastSeparator(p)
	if i <= 0 {
		return p[:i+1]
	}
	return p[:i]
}

// TrashName is what the system calls its Trash: 废纸篓 on macOS, 回收站 on Windows.
func TrashName() string {
	if windows {
		return "回收站"
	}
	return "废纸篓"
}

// FileManagerName is the system's file manager: 访达 on macOS, 资源管理器 on Windows.
func FileManagerName() string {
	if windows {
		return "资源管理器"
	}
	return "访达"
}
