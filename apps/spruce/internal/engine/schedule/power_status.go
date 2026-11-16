package schedule

import (
	"errors"

	"spruce/apps/spruce/internal/engine/sysinfo"
)

// cpuTimes is the idle and total CPU time of the whole machine, in seconds.
type cpuTimes struct {
	idle, total float64
}

// busyPercent is the share of the CPU time between two readings that was not idle, 0 to 100. Pure.
func busyPercent(before, after cpuTimes) (float64, error) {
	dt := after.total - before.total
	di := after.idle - before.idle
	if dt <= 0 || di < 0 {
		return 0, errors.New("schedule: CPU times did not advance")
	}
	return min(max((1-di/dt)*100, 0), 100), nil
}

// batteryOnAC reads macOS's battery: a Mac without a battery is on AC; one with a battery is when it is charging, full or connected without charging. A state the reading did not settle is not AC. Pure.
func batteryOnAC(b sysinfo.BatteryStats) bool {
	if !b.Present {
		return true
	}
	switch b.State {
	case sysinfo.BatteryCharging, sysinfo.BatteryFull, sysinfo.BatteryNotCharging:
		return true
	}
	return false
}

// systemPowerStatus is the Windows SYSTEM_POWER_STATUS structure GetSystemPowerStatus fills in. It is declared without a build tag so its decoding is tested on every platform.
type systemPowerStatus struct {
	ACLineStatus        uint8
	BatteryFlag         uint8
	BatteryLifePercent  uint8
	SystemStatusFlag    uint8
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

// Values of SYSTEM_POWER_STATUS.
const (
	acOnline         = 1
	batteryNoSystem  = 128
	batteryFlagUnset = 255
	statusSaverOn    = 1
)

// power decodes s: ACLineStatus 1 is AC, 0 and 255 (unknown) are not, unless BatteryFlag says the machine has no battery; SystemStatusFlag 1 is battery saver. Pure.
func (s systemPowerStatus) power() Power {
	noBattery := s.BatteryFlag != batteryFlagUnset && s.BatteryFlag&batteryNoSystem != 0
	return Power{OnAC: s.ACLineStatus == acOnline || noBattery, Saver: s.SystemStatusFlag == statusSaverOn}
}
