package cli

import (
	"errors"
	"flag"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParseAge(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"12h": 12 * time.Hour,
		"1h":  time.Hour,
		"30d": 30 * 24 * time.Hour,
		"2w":  14 * 24 * time.Hour,
	} {
		got, err := parseAge(in)
		if err != nil || got != want {
			t.Errorf("parseAge(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "d", "30", "0d", "-3d", "+3d", "1.5d", "3m", "30s", "d30", "3 d", "999999999w"} {
		if _, err := parseAge(in); err == nil {
			t.Errorf("parseAge(%q) accepted", in)
		}
	}
}

func TestParse(t *testing.T) {
	fs := newFlags("clean")
	var cats list
	var older age
	var yes bool
	fs.Var(&cats, "category", "")
	fs.Var(&older, "older-than", "")
	fs.BoolVar(&yes, "yes", false, "")
	pos, err := parse(fs, []string{"app-caches", "--yes", "--category", "a:b,c:d", "logs", "--older-than=2w", "--", "--not-a-flag"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(pos, []string{"app-caches", "logs", "--not-a-flag"}) {
		t.Fatalf("positional %q", pos)
	}
	if !yes || !slices.Equal(cats, list{"a:b", "c:d"}) || time.Duration(older) != 14*24*time.Hour {
		t.Fatalf("yes %v, categories %q, older %v", yes, cats, time.Duration(older))
	}

	for args, want := range map[string]string{
		"--force":          "未知的选项 -force",
		"--category":       "选项 -category 需要一个值",
		"--older-than=30":  "无法识别的时长",
		"--yes=maybe":      "选项的值无效",
		"x --older-than x": "无法识别的时长",
	} {
		fs := newFlags("clean")
		fs.Var(&cats, "category", "")
		fs.Var(&older, "older-than", "")
		fs.BoolVar(&yes, "yes", false, "")
		_, err := parse(fs, strings.Fields(args))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("parse(%q) error %v, want %q", args, err, want)
		}
	}

	fs = newFlags("scan")
	if _, err := parse(fs, []string{"--help"}); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("--help: %v", err)
	}
}

func TestWantsJSON(t *testing.T) {
	for _, args := range [][]string{{"scan", "--json"}, {"clean", "-json", "x"}, {"clean", "--json=true"}} {
		if !wantsJSON(args) {
			t.Errorf("wantsJSON(%q) = false", args)
		}
	}
	for _, args := range [][]string{{"scan"}, {"clean", "--", "--json"}, {"clean", "--json=false"}} {
		if wantsJSON(args) {
			t.Errorf("wantsJSON(%q) = true", args)
		}
	}
}

func TestSources(t *testing.T) {
	if err := checkSources([]string{"system-junk", "dev-junk", "installers"}); err != nil {
		t.Fatal(err)
	}
	if err := checkSources([]string{"smart-scan"}); err == nil {
		t.Fatal("an unknown source was accepted")
	}
	if got := sourcesOrNil([]string{"installers", "system-junk", "installers"}); !slices.Equal(got, []string{"system-junk", "installers"}) {
		t.Fatalf("sourcesOrNil = %q", got)
	}
	if got := sourcesOrNil(nil); got != nil {
		t.Fatalf("sourcesOrNil(nil) = %q", got)
	}
}

func TestCells(t *testing.T) {
	for s, want := range map[string]int{"": 0, "scan": 4, "概览": 4, "pnpm 存储": 9, "１": 2} {
		if got := cells(s); got != want {
			t.Errorf("cells(%q) = %d, want %d", s, got, want)
		}
	}
}

func TestRedirected(t *testing.T) {
	const fileTypeChar, fileTypePipe = 2, 3
	for _, c := range []struct {
		handle   uintptr
		fileType uint32
		err      error
		want     bool
	}{
		{0, fileTypeUnknown, errors.New("invalid handle"), false},
		{invalidHandle, fileTypeUnknown, nil, false},
		{0x44, fileTypeUnknown, nil, false},
		{0x44, fileTypeUnknown, errors.New("invalid handle"), false},
		{0x44, fileTypePipe, nil, true},
		{0x48, fileTypeChar, nil, true},
	} {
		if got := redirected(c.handle, c.fileType, c.err); got != c.want {
			t.Errorf("redirected(%#x, %d, %v) = %v, want %v", c.handle, c.fileType, c.err, got, c.want)
		}
	}
}
