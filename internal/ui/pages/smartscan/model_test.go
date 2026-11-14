package smartscan

import (
	"math"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/ui/cleanflow"
)

type item struct {
	name     string
	size     int64
	selected bool
}

func category(id, source string, selected bool, items ...item) cleanup.Category {
	c := cleanup.Category{ID: source + ":" + id, Title: id, Icon: "cache", Kind: cleanup.KindCache, Deletion: cleanup.DeletePermanent,
		Selected: selected, Source: source}
	for _, it := range items {
		c.Items = append(c.Items, cleanup.Item{ID: it.name, Path: "/x/" + it.name, Size: it.size, Selected: it.selected})
		c.Size += it.size
	}
	return c
}

// scan is sources.test.ts's SCAN.
func scan() cleanup.ScanResult {
	return cleanup.ScanResult{
		Categories: []cleanup.Category{
			category("caches", "system-junk", true, item{"a", 100, false}, item{"b", 50, false}),
			category("running", "system-junk", false, item{"c", 30, false}),
			category("support", "dev-junk", false, item{"old", 200, true}, item{"new", 300, false}),
		},
		Size: 680, Items: 5,
		Sources: []cleanup.SourceState{
			{ID: "system-junk", Status: cleanup.SourceDone, Bytes: 180, Items: 3},
			{ID: "dev-junk", Status: cleanup.SourceDone, Bytes: 500, Items: 2},
			{ID: "installers", Status: cleanup.SourceFailed, Error: "没能扫描完成"},
		},
	}
}

// Ported from sources.test.ts: splits the scan by source, in scan order, with what starts checked.
func TestSummarize(t *testing.T) {
	type row struct {
		id           app.CleanerID
		items        int
		bytes        int64
		recItems     int
		recBytes     int64
		failed, seen bool
	}
	var got []row
	for _, s := range Summarize(scan()) {
		got = append(got, row{s.ID, s.Items, s.Bytes, s.Recommended.Count, s.Recommended.Bytes, s.Failed(), s.State != nil})
	}
	want := []row{
		{app.CleanerSystemJunk, 3, 180, 2, 150, false, true},
		{app.CleanerDevJunk, 2, 500, 1, 200, false, true},
		{app.CleanerInstallers, 0, 0, 0, 0, true, true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("summarize\n got %+v\nwant %+v", got, want)
	}
	if got := RecommendedTotals(scan()); got != (cleanflow.Totals{Count: 3, Bytes: 350}) {
		t.Errorf("recommended %+v", got)
	}
}

func TestSummarizeWithoutSources(t *testing.T) {
	for _, s := range Summarize(cleanup.ScanResult{}) {
		if s.State != nil || s.Failed() || s.Items != 0 || s.Categories == nil {
			t.Errorf("%s %+v", s.ID, s)
		}
	}
}

// Ported from sources.test.ts: waits before the first event.
func TestSourceRowsBeforeTheFirstEvent(t *testing.T) {
	for _, r := range SourceRows(cleanup.Progress{}) {
		if r.Status != StatusWaiting || r.Known {
			t.Errorf("%s %+v", r.ID, r)
		}
		if got := RowStatusText(r); got != "等待开始…" {
			t.Errorf("%s %q", r.ID, got)
		}
	}
	if _, known := OverallFraction(cleanup.Progress{}); known {
		t.Error("overall known before the first event")
	}
}

// Ported from sources.test.ts: reads each source's state from the event.
func TestSourceRows(t *testing.T) {
	p := cleanup.Progress{Phase: cleanup.PhaseScanning, Done: 1250, Total: 3000, Bytes: 10, Sources: []cleanup.SourceState{
		{ID: "system-junk", Status: cleanup.SourceDone, Done: 4, Total: 4, Bytes: 10, Items: 2},
		{ID: "dev-junk", Status: cleanup.SourceScanning, Done: 1, Total: 4},
		{ID: "installers", Status: cleanup.SourceScanning},
	}}
	type row struct {
		id       app.CleanerID
		status   RowStatus
		fraction float64
		known    bool
	}
	var got []row
	for _, r := range SourceRows(p) {
		got = append(got, row{r.ID, r.Status, r.Fraction, r.Known})
	}
	want := []row{
		{app.CleanerSystemJunk, "done", 1, true},
		{app.CleanerDevJunk, "scanning", 0.25, true},
		{app.CleanerInstallers, "scanning", 0, false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rows\n got %+v\nwant %+v", got, want)
	}
	if f, known := OverallFraction(p); !known || math.Abs(f-1250.0/3000) > 1e-9 {
		t.Errorf("overall %v %v", f, known)
	}
}

func TestRowStatusText(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("expectations use SI sizes")
	}
	for _, tc := range []struct {
		row  SourceRow
		want string
	}{
		{SourceRow{Status: "scanning"}, "正在扫描… 已找到 0 B"},
		{SourceRow{Status: "done", Items: 4, Bytes: 2_000_000}, "完成，找到 4 项，共 2 MB"},
		{SourceRow{Status: "done"}, "完成，没有找到可以清理的内容"},
		{SourceRow{Status: "failed", Error: "需要权限"}, "需要权限"},
		{SourceRow{Status: "failed"}, "没能扫描完成"},
	} {
		if got := RowStatusText(tc.row); got != tc.want {
			t.Errorf("%+v: %q, want %q", tc.row, got, tc.want)
		}
	}
}

// Ported from sources.test.ts: warns about a disk with little space left.
func TestDiskLow(t *testing.T) {
	for _, tc := range []struct {
		free, total int64
		low         bool
	}{
		{200e9, 1000e9, false},
		{50e9, 1000e9, true},
		{15e9, 100e9, true},
		{0, 0, false},
	} {
		if got := DiskLow(app.SpaceVolume{Free: tc.free, Total: tc.total}); got != tc.low {
			t.Errorf("%d of %d: low %v", tc.free, tc.total, got)
		}
	}
	if runtime.GOOS != "windows" {
		if got := DiskNote(app.SpaceVolume{Path: "/", Name: "Macintosh HD", Total: 1000e9, Free: 120e9}); got != "Macintosh HD 共 1 TB" {
			t.Errorf("note %q", got)
		}
		if got := DiskNote(app.SpaceVolume{Path: "/", Total: 100e9, Free: 5e9}); got != "/ 共 100 GB，空间不多了" {
			t.Errorf("low note %q", got)
		}
	}
	if got := UsedFraction(app.SpaceVolume{Total: 1000e9, Free: 120e9}); math.Abs(got-0.88) > 1e-9 {
		t.Errorf("used %v", got)
	}
}

func TestSystemVolume(t *testing.T) {
	if SystemVolume(app.SpaceLocations{}) != nil {
		t.Error("a volume without disks")
	}
	l := app.SpaceLocations{Volumes: []app.SpaceVolume{{Path: "/Volumes/X"}, {Path: "/", Startup: true}}}
	if got := SystemVolume(l); got == nil || got.Path != "/" {
		t.Errorf("startup %+v", got)
	}
	l.Volumes[1].Startup = false
	if got := SystemVolume(l); got == nil || got.Path != "/Volumes/X" {
		t.Errorf("first %+v", got)
	}
}

// Ported from sources.test.ts: offers 维护 when it never ran or ran more than 30 days ago.
func TestMaintenanceDue(t *testing.T) {
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	run := func(kind history.Kind, ended time.Time) history.Run {
		return history.Run{ID: ended.String(), Kind: kind, StartedAt: ended, EndedAt: ended}
	}
	runs := []history.Run{
		run(history.KindSystemJunk, time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)),
		run(history.KindMaintenance, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)),
	}
	last := LastMaintenance(runs)
	if last == nil || !last.EndedAt.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("last %+v", last)
	}
	if MaintenanceDue(last, now) {
		t.Error("due 13 days after")
	}
	old := run(history.KindMaintenance, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	if !MaintenanceDue(&old, now) {
		t.Error("not due 44 days after")
	}
	if !MaintenanceDue(nil, now) {
		t.Error("not due without a run")
	}
	if LastMaintenance(runs[:1]) != nil {
		t.Error("a 维护 run among cleans")
	}
	if got := MaintenanceNote(nil, true); got != "做一次轻量保养，不会动你的文档和照片" {
		t.Errorf("never %q", got)
	}
	if got := MaintenanceNote(&old, true); got != "已经超过 30 天没有维护了，可以再做一次" {
		t.Errorf("due %q", got)
	}
	if got := MaintenanceValue(nil, now); got != "还没有做过维护" {
		t.Errorf("value %q", got)
	}
	recent := run(history.KindMaintenance, now.Add(-3*24*time.Hour))
	if got := MaintenanceValue(&recent, now); got != "上次维护：3天前" {
		t.Errorf("value %q", got)
	}
}

func TestSourceNoteAndTotalSub(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("expectations use SI sizes")
	}
	s := Summarize(scan())
	if got := SourceNote(s[0]); got != "3 项，推荐清理 2 项（150 B）" {
		t.Errorf("note %q", got)
	}
	if got := SourceNote(s[2]); got != "没有找到可以清理的内容" {
		t.Errorf("empty note %q", got)
	}
	if got := TotalSub(scan()); got != "已按推荐勾选 3 项。一共找到 5 项（680 B），默认不选的可能还要用，可以在下面逐项查看。" {
		t.Errorf("sub %q", got)
	}
}
