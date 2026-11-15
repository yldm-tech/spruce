package schedule

import (
	"context"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGetSystemPowerStatus = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetSystemPowerStatus")

// Power asks kernel32 GetSystemPowerStatus; Windows has no thermal state to read.
func (systemConditions) Power(ctx context.Context) (Power, error) {
	if err := ctx.Err(); err != nil {
		return Power{}, err
	}
	if err := procGetSystemPowerStatus.Find(); err != nil {
		return Power{}, fmt.Errorf("schedule: GetSystemPowerStatus: %w", err)
	}
	var s systemPowerStatus
	if r, _, err := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&s))); r == 0 {
		return Power{}, fmt.Errorf("schedule: GetSystemPowerStatus: %w", err)
	}
	return s.power(), nil
}
