package app

import (
	"testing"
	"time"
)

func TestDiskLow(t *testing.T) {
	for _, tc := range []struct {
		free, total int64
		want        bool
	}{
		{free: 0, total: 0, want: false},
		{free: 100e9, total: 500e9, want: false},
		{free: 19e9, total: 100e9, want: true},  // below 20 GB
		{free: 90e9, total: 1000e9, want: true}, // below a tenth
		{free: 100e9, total: 1000e9, want: false},
	} {
		if got := DiskLow(tc.free, tc.total); got != tc.want {
			t.Errorf("DiskLow(%d, %d) = %v, want %v", tc.free, tc.total, got, tc.want)
		}
	}
}

func TestLastMonday10(t *testing.T) {
	loc := time.FixedZone("JST", 9*3600)
	for _, tc := range []struct{ at, want time.Time }{
		{at: time.Date(2026, 11, 16, 10, 0, 0, 0, loc), want: time.Date(2026, 11, 16, 10, 0, 0, 0, loc)}, // Monday 10:00
		{at: time.Date(2026, 11, 16, 9, 59, 0, 0, loc), want: time.Date(2026, 11, 9, 10, 0, 0, 0, loc)},  // just before
		{at: time.Date(2026, 11, 15, 23, 0, 0, 0, loc), want: time.Date(2026, 11, 9, 10, 0, 0, 0, loc)},  // Sunday
		{at: time.Date(2026, 11, 18, 8, 0, 0, 0, loc), want: time.Date(2026, 11, 16, 10, 0, 0, 0, loc)},  // Wednesday
		{at: time.Date(2027, 1, 1, 12, 0, 0, 0, loc), want: time.Date(2026, 12, 28, 10, 0, 0, 0, loc)},   // across a year
	} {
		if got := lastMonday10(tc.at); !got.Equal(tc.want) {
			t.Errorf("lastMonday10(%v) = %v, want %v", tc.at, got, tc.want)
		}
	}
}

func TestGigabyteFollowsTheFileManager(t *testing.T) {
	if gigabyte("darwin") != 1e9 || gigabyte("windows") != 1<<30 {
		t.Errorf("gigabyte: darwin %d, windows %d", gigabyte("darwin"), gigabyte("windows"))
	}
}

func TestNilGateReadsEverything(t *testing.T) {
	var g *GateRecorder
	if !g.Background("/anywhere") || !g.Foreground("/anywhere") {
		t.Error("a nil gate refused a read")
	}
	g = NewGateRecorder(nil)
	if !g.Background("/anywhere") || len(g.recorded(CheckBackground)) != 0 {
		t.Error("a gate without a store refused or recorded a read")
	}
}
