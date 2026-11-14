package schedule

import (
	"context"
	"fmt"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/yldm-tech/spruce/internal/engine/sysinfo"
)

const foundationPath = "/System/Library/Frameworks/Foundation.framework/Foundation"

// loadFoundation makes NSProcessInfo available to the Objective-C runtime (the app has usually loaded Foundation already).
var loadFoundation = sync.OnceValue(func() error {
	if _, err := purego.Dlopen(foundationPath, purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
		return fmt.Errorf("load Foundation: %w", err)
	}
	return nil
})

var (
	selProcessInfo  = objc.RegisterName("processInfo")
	selResponds     = objc.RegisterName("respondsToSelector:")
	selLowPowerMode = objc.RegisterName("isLowPowerModeEnabled")
	selThermalState = objc.RegisterName("thermalState")
)

// Power reads the battery through sysinfo (ioreg, which needs no permission) and Low Power Mode and the thermal state from NSProcessInfo, in process: no child process and no prompt.
func (c systemConditions) Power(ctx context.Context) (Power, error) {
	b, err := sysinfo.NewSource(c.runner).Battery(ctx)
	if err != nil {
		return Power{}, err
	}
	p := Power{OnAC: batteryOnAC(b)}
	saver, thermal, err := processInfo()
	if err != nil {
		return Power{}, err
	}
	p.Saver, p.Thermal = saver, thermal
	return p, nil
}

// processInfo reads [NSProcessInfo processInfo]. isLowPowerModeEnabled exists from macOS 12 and the app runs from 11, and a message to an unknown selector raises an Objective-C exception that ends the process, so each property is asked for only when the object responds to it; one it does not have reads as off and nominal.
func processInfo() (saver bool, thermal int, err error) {
	if err := loadFoundation(); err != nil {
		return false, 0, err
	}
	pi := objc.ID(objc.GetClass("NSProcessInfo")).Send(selProcessInfo)
	if pi == 0 {
		return false, 0, fmt.Errorf("schedule: no NSProcessInfo")
	}
	if objc.Send[bool](pi, selResponds, selLowPowerMode) {
		saver = objc.Send[bool](pi, selLowPowerMode)
	}
	if objc.Send[bool](pi, selResponds, selThermalState) {
		thermal = int(objc.Send[int64](pi, selThermalState))
	}
	return saver, thermal, nil
}
