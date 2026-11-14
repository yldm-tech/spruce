package services

import (
	"context"
	"errors"

	"github.com/egoist/mygo/plugins/permissionflow"
	"github.com/yldm-tech/spruce/internal/engine/maintenance"
)

// Permissions is the bound service for system permissions, backed by mygo's permissionflow plugin. It also implements maintenance.PermissionChecker for the engine.
type Permissions struct{}

var _ maintenance.PermissionChecker = Permissions{}

// FullDiskAccess reports whether the app has Full Disk Access (macOS), which the 优化应用数据库 task needs to reach Safari's and Mail's databases. Windows has no such permission: "notRequired".
func (Permissions) FullDiskAccess() (maintenance.PermissionState, error) {
	return permissionState(permissionflow.Check(permissionflow.FullDiskAccess))
}

// GuideFullDiskAccess opens System Settings at Full Disk Access with a panel showing the app to drag into the list, and returns once the user granted it, closed the panel or System Settings, or the page went away.
func (Permissions) GuideFullDiskAccess(ctx context.Context) (maintenance.PermissionState, error) {
	st, err := permissionflow.Guide(ctx, permissionflow.FullDiskAccess, permissionflow.GuideOptions{})
	if errors.Is(err, context.Canceled) {
		return permissionState(st, nil)
	}
	return permissionState(st, err)
}

// permissionState maps permissionflow's answer to the engine's PermissionState.
func permissionState(st permissionflow.Status, err error) (maintenance.PermissionState, error) {
	if errors.Is(err, errors.ErrUnsupported) {
		return maintenance.PermissionNotRequired, nil
	}
	if err != nil {
		return maintenance.PermissionUnknown, err
	}
	switch st {
	case permissionflow.Granted:
		return maintenance.PermissionGranted, nil
	case permissionflow.Denied, permissionflow.NotDetermined:
		return maintenance.PermissionDenied, nil
	}
	return maintenance.PermissionUnknown, nil
}
