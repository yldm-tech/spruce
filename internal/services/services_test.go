package services

import (
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/permissionflow"
	"github.com/yldm-tech/spruce/internal/engine/maintenance"
	"github.com/yldm-tech/spruce/internal/engine/taskresult"
)

func TestPermissionState(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		st   permissionflow.Status
		err  error
		want maintenance.PermissionState
		fail bool
	}{
		{permissionflow.Granted, nil, maintenance.PermissionGranted, false},
		{permissionflow.Denied, nil, maintenance.PermissionDenied, false},
		{permissionflow.NotDetermined, nil, maintenance.PermissionDenied, false},
		{permissionflow.Unknown, nil, maintenance.PermissionUnknown, false},
		{permissionflow.Unknown, errors.ErrUnsupported, maintenance.PermissionNotRequired, false},
		{permissionflow.Unknown, boom, maintenance.PermissionUnknown, true},
	}
	for _, c := range cases {
		got, err := permissionState(c.st, c.err)
		if got != c.want || (err != nil) != c.fail {
			t.Errorf("permissionState(%v, %v) = %q, %v; want %q", c.st, c.err, got, err, c.want)
		}
	}
}

func TestSummarize(t *testing.T) {
	start := time.Date(2026, 11, 14, 1, 0, 0, 0, time.UTC)
	s := summarize("id", start, start.Add(time.Minute), []taskresult.Result{
		{TaskID: "a", Outcome: taskresult.Applied},
		{TaskID: "b", Outcome: taskresult.Applied},
		{TaskID: "c", Outcome: taskresult.Skipped},
		{TaskID: "d", Outcome: taskresult.Failed},
		{TaskID: "e", Outcome: taskresult.Unchanged},
		{TaskID: "f", Outcome: taskresult.Unavailable},
	})
	if s.Applied != 2 || s.Skipped != 1 || s.Failed != 1 || s.Unchanged != 1 || s.Unavailable != 1 || len(s.Results) != 6 {
		t.Errorf("summary %+v", s)
	}
}

func TestNewRunID(t *testing.T) {
	id := newRunID(time.Date(2026, 11, 14, 1, 4, 12, 0, time.FixedZone("JST", 9*3600)))
	if !regexp.MustCompile(`^20261113T160412Z-[0-9a-f]{8}$`).MatchString(id) {
		t.Errorf("id %q", id)
	}
}
