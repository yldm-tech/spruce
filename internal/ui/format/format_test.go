package format

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestBytes(t *testing.T) {
	for _, c := range []struct {
		n      int64
		binary bool
		want   string
	}{
		{0, false, "0 B"},
		{999, false, "999 B"},
		{1500, false, "2 KB"},
		{1_234_567, false, "1.2 MB"},
		{4_800_000_000, false, "4.8 GB"},
		{2_000_000_000, false, "2 GB"},
		{999_999, false, "1 MB"},
		{-5, false, "0 B"},
		{1024, true, "1 KB"},
		{1024 * 1024 * 1024 * 3 / 2, true, "1.5 GB"},
		{1023, true, "1023 B"},
	} {
		if got := BytesIn(c.n, c.binary); got != c.want {
			t.Errorf("BytesIn(%d, %v) = %q, want %q", c.n, c.binary, got, c.want)
		}
	}
}

func TestDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0:                  "不到 1 秒",
		12 * time.Second:   "12 秒",
		200 * time.Second:  "3 分 20 秒",
		180 * time.Second:  "3 分钟",
		725 * time.Second:  "12 分钟",
		3900 * time.Second: "1 小时 5 分",
		7200 * time.Second: "2 小时",
		-time.Second:       "不到 1 秒",
	} {
		if got := Duration(d); got != want {
			t.Errorf("Duration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestRelativeTime(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.Local)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	for _, c := range []struct {
		t    time.Time
		want string
	}{
		{ago(10 * time.Second), "刚刚"},
		{ago(5 * time.Minute), "5分钟前"},
		{ago(3 * time.Hour), "3小时前"},
		{time.Date(2026, 11, 13, 9, 0, 0, 0, time.Local), "昨天"},
		{time.Date(2026, 11, 12, 9, 0, 0, 0, time.Local), "前天"},
		{time.Date(2026, 11, 11, 9, 0, 0, 0, time.Local), "3天前"},
		{time.Date(2026, 10, 2, 0, 0, 0, 0, time.Local), "10月2日"},
		{time.Date(2025, 11, 2, 0, 0, 0, 0, time.Local), "2025年11月2日"},
		{now.Add(time.Hour), "11月14日"},
		{time.Time{}, ""},
		{time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC), ""},
	} {
		if got := RelativeTime(c.t, now); got != c.want {
			t.Errorf("RelativeTime(%v) = %q, want %q", c.t, got, c.want)
		}
	}
}

func TestClockAndThousands(t *testing.T) {
	if got := Clock(time.Date(2026, 1, 2, 9, 5, 0, 0, time.UTC)); got != "09:05" {
		t.Errorf("Clock = %q", got)
	}
	for n, want := range map[int64]string{0: "0", 999: "999", 1000: "1,000", 1234567: "1,234,567", -12345: "-12,345"} {
		if got := Thousands(n); got != want {
			t.Errorf("Thousands(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestPaths(t *testing.T) {
	if got := TruncateMiddle("short", 10); got != "short" {
		t.Errorf("short: %q", got)
	}
	got := TruncateMiddle("/Users/me/Library/Caches/com.example.app/data.bin", 24)
	if utf8.RuneCountInString(got) != 24 || !strings.HasSuffix(got, "data.bin") {
		t.Errorf("TruncateMiddle = %q", got)
	}
	if got := TruncateMiddle("一二三四五六七八九十", 5); got != "一…八九十" {
		t.Errorf("runes: %q", got)
	}
	for in, want := range map[string]string{"/a/b/c.txt": "c.txt", `C:\a\b\`: "b", "name": "name"} {
		if got := BaseName(in); got != want {
			t.Errorf("BaseName(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"/a/b/c.txt": "/a/b", "/a": "/", `C:\a\b`: `C:\a`, "name": ""} {
		if got := ParentPath(in); got != want {
			t.Errorf("ParentPath(%q) = %q, want %q", in, got, want)
		}
	}
}
