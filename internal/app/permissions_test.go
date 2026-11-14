package app

import (
	"testing"

	"github.com/yldm-tech/spruce/internal/engine/maintenance"
	"github.com/yldm-tech/spruce/internal/host/fulldisk"
)

func TestPermissionState(t *testing.T) {
	cases := map[fulldisk.State]maintenance.PermissionState{
		fulldisk.Granted:     maintenance.PermissionGranted,
		fulldisk.Denied:      maintenance.PermissionDenied,
		fulldisk.Unknown:     maintenance.PermissionUnknown,
		fulldisk.NotRequired: maintenance.PermissionNotRequired,
	}
	for st, want := range cases {
		if got := permissionState(st); got != want {
			t.Errorf("permissionState(%v) = %q, want %q", st, got, want)
		}
	}
}
