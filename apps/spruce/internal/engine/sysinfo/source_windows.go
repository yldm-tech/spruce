package sysinfo

import (
	"context"
	"os"
	"strings"

	"github.com/shirou/gopsutil/v4/host"
	"golang.org/x/sys/windows/registry"
)

// systemVolume is the root of %SystemDrive%, normally C:\.
func systemVolume() string {
	drive := os.Getenv("SystemDrive")
	if len(drive) != 2 || drive[1] != ':' {
		drive = "C:"
	}
	return drive + `\`
}

// loadAverage: Windows has no load average.
func loadAverage(context.Context) (*LoadAverage, error) { return nil, nil }

// memoryPressure: Windows reports no memory pressure level.
func memoryPressure() (MemoryPressure, error) { return "", nil }

// platformInfo fills the edition ("Windows 11 Pro"), the feature update ("24H2") and build from gopsutil, and the model from the BIOS values Windows copies to the registry.
func platformInfo(ctx context.Context, info *SystemInfo) {
	if platform, _, display, err := host.PlatformInformationWithContext(ctx); err == nil {
		info.OSName = strings.TrimSpace(strings.TrimPrefix(platform, "Microsoft "))
		info.OSVersion = display
	}
	// "10.0.26100.2314 Build 26100.2314"
	if kernel, err := host.KernelVersionWithContext(ctx); err == nil {
		if _, build, ok := strings.Cut(kernel, "Build "); ok {
			info.OSBuild = strings.TrimSpace(build)
		}
	}
	info.Model = biosModel()
}

// biosModel is "<SystemManufacturer> <SystemProductName>" from HKLM\HARDWARE\DESCRIPTION\System\BIOS, e.g. "LENOVO 21HD", leaving out what is missing or a vendor placeholder.
func biosModel() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\BIOS`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	var parts []string
	for _, name := range []string{"SystemManufacturer", "SystemProductName"} {
		v, _, err := k.GetStringValue(name)
		v = strings.TrimSpace(v)
		if err != nil || v == "" || isPlaceholder(v) {
			continue
		}
		parts = append(parts, v)
	}
	return strings.Join(parts, " ")
}

// isPlaceholder recognises the filler some firmware leaves in its SMBIOS strings.
func isPlaceholder(v string) bool {
	switch strings.ToLower(v) {
	case "to be filled by o.e.m.", "system manufacturer", "system product name", "default string", "not applicable", "none":
		return true
	}
	return false
}
