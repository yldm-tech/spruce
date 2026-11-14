package app

import (
	"context"

	"github.com/yldm-tech/spruce/internal/engine/execx"
	"github.com/yldm-tech/spruce/internal/engine/maintenance"
	"github.com/yldm-tech/spruce/internal/host/fulldisk"
)

// Permissions reports system permissions and guides the user to them, through internal/host/fulldisk. It also implements maintenance.PermissionChecker for the engine.
type Permissions struct {
	// Runner opens System Settings and Finder for the guide.
	Runner execx.Runner
}

var _ maintenance.PermissionChecker = Permissions{}

// FullDiskAccess reports whether the app has Full Disk Access (macOS), which the 优化应用数据库 task needs to reach Safari's and Mail's databases. Windows has no such permission: "notRequired". The error is always nil; the signature is maintenance.PermissionChecker's.
func (Permissions) FullDiskAccess() (maintenance.PermissionState, error) {
	return permissionState(fulldisk.Check()), nil
}

// GuideFullDiskAccess opens System Settings at Full Disk Access with Spruce.app shown in Finder to drag into the list, and returns once the user granted it, closed System Settings, or ctx ended. The error is the reason System Settings could not be opened.
func (p Permissions) GuideFullDiskAccess(ctx context.Context) (maintenance.PermissionState, error) {
	st, err := fulldisk.Guide(ctx, p.Runner)
	return permissionState(st), err
}

// permissionState maps fulldisk's answer to the engine's PermissionState.
func permissionState(st fulldisk.State) maintenance.PermissionState {
	switch st {
	case fulldisk.Granted:
		return maintenance.PermissionGranted
	case fulldisk.Denied:
		return maintenance.PermissionDenied
	case fulldisk.NotRequired:
		return maintenance.PermissionNotRequired
	}
	return maintenance.PermissionUnknown
}
