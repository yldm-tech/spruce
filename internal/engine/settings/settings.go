package settings

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// SchemaVersion is the version of the file format this build writes. Version 2 added Background, Automation and General's KeepInTray, LowDiskAlert and LowDiskGB; a version 1 file reads with their defaults.
const SchemaVersion = 2

// AppSettings is every user preference.
type AppSettings struct {
	// Version is the schema version of the file; the Store sets it.
	Version int             `json:"version"`
	Health  HealthSettings  `json:"health"`
	Cleanup CleanupSettings `json:"cleanup"`
	History HistorySettings `json:"history"`
	General GeneralSettings `json:"general"`
	// Background is the scheduled read-only checks.
	Background BackgroundSettings `json:"background"`
	// Automation is the automatic rules and their weekly summary.
	Automation AutomationSettings `json:"automation"`
}

// HealthSettings tunes the CPU monitor of 健康监测 and the alert banner.
type HealthSettings struct {
	// CPUThreshold is the alert threshold in percent of one logical CPU, MinCPUThreshold to MaxCPUThreshold.
	CPUThreshold int `json:"cpuThreshold"`
	// SustainMinutes is how long a process must stay above the threshold before it alerts, MinSustainMinutes to MaxSustainMinutes.
	SustainMinutes int `json:"sustainMinutes"`
	// Enabled turns the monitor on.
	Enabled bool `json:"enabled"`
}

// CleanupSettings tunes the cleaner pages.
type CleanupSettings struct {
	// Exclusions are paths and patterns no cleaner touches (fsx.Excluded).
	Exclusions []string `json:"exclusions"`
	// DevProjectRoots are the folders 开发者垃圾 searches for projects.
	DevProjectRoots []string `json:"devProjectRoots"`
	// DevArtifactMinAgeDays: build output of projects untouched for fewer days is not offered.
	DevArtifactMinAgeDays int `json:"devArtifactMinAgeDays"`
	// InstallerRoots are the folders 安装包 searches.
	InstallerRoots []string `json:"installerRoots"`
	// LargeFileMinMB is the size from which 空间透视 calls a file large.
	LargeFileMinMB int `json:"largeFileMinMB"`
}

// HistorySettings tunes 操作历史.
type HistorySettings struct {
	// RetentionDays: runs older than this are pruned, MinRetentionDays to MaxRetentionDays.
	RetentionDays int `json:"retentionDays"`
}

// GeneralSettings are app-wide preferences.
type GeneralSettings struct {
	// LaunchAtLogin starts Spruce when the user logs in.
	LaunchAtLogin bool `json:"launchAtLogin"`
	// ConfirmBeforeClean is kept in the file for older builds and no longer read: trash moves have undo and irreversible actions always confirm.
	ConfirmBeforeClean bool `json:"confirmBeforeClean"`
	// KeepInTray keeps Spruce in the menu bar when its window closes (macOS; default true).
	KeepInTray bool `json:"keepInTray"`
	// LowDiskAlert posts a notification when the system volume's free space drops below LowDiskGB (default false).
	LowDiskAlert bool `json:"lowDiskAlert"`
	// LowDiskGB is the low-disk threshold in GB, MinLowDiskGB to MaxLowDiskGB (default 20).
	LowDiskGB int `json:"lowDiskGB"`
}

// BackgroundSettings tune the scheduled checks.
type BackgroundSettings struct {
	// Enabled turns scheduled checks on (default true).
	Enabled bool `json:"enabled"`
}

// AutomationSettings are the automatic rules (macOS; all default false) and the weekly summary (default true).
type AutomationSettings struct {
	AppCaches           bool `json:"appCaches"`
	Installers          bool `json:"installers"`
	BuildOutput         bool `json:"buildOutput"`
	IncompleteDownloads bool `json:"incompleteDownloads"`
	WeeklySummary       bool `json:"weeklySummary"`
}

// Ranges Normalize clamps to.
const (
	MinCPUThreshold      = 20
	MaxCPUThreshold      = 400
	MinSustainMinutes    = 1
	MaxSustainMinutes    = 120
	MinDevArtifactMinAge = 0
	MaxDevArtifactMinAge = 3650
	MinLargeFileMB       = 10
	MaxLargeFileMB       = 1024 * 1024
	MinRetentionDays     = 7
	MaxRetentionDays     = 3650
	MaxPaths             = 200
	MinLowDiskGB         = 1
	MaxLowDiskGB         = 500
)

// Defaults returns this platform's defaults for the user whose home folder is home. It checks which developer folders exist, so it reads the disk.
func Defaults(home string) AppSettings {
	var devRoots []string
	for _, rel := range devRootCandidates() {
		p := filepath.Join(home, rel)
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			devRoots = append(devRoots, p)
		}
	}
	return AppSettings{
		Version: SchemaVersion,
		Health:  HealthSettings{CPUThreshold: 40, SustainMinutes: 10, Enabled: true},
		Cleanup: CleanupSettings{
			Exclusions:            []string{},
			DevProjectRoots:       nonNil(devRoots),
			DevArtifactMinAgeDays: 30,
			InstallerRoots:        installerRoots(home),
			LargeFileMinMB:        500,
		},
		History:    HistorySettings{RetentionDays: 180},
		General:    GeneralSettings{LaunchAtLogin: false, ConfirmBeforeClean: true, KeepInTray: true, LowDiskAlert: false, LowDiskGB: 20},
		Background: BackgroundSettings{Enabled: true},
		Automation: AutomationSettings{WeeklySummary: true},
	}
}

// Normalize returns s with every value in range and every path list cleaned; see the package documentation.
func Normalize(s AppSettings, home string) AppSettings {
	s.Version = SchemaVersion
	s.Health.CPUThreshold = clamp(s.Health.CPUThreshold, MinCPUThreshold, MaxCPUThreshold)
	s.Health.SustainMinutes = clamp(s.Health.SustainMinutes, MinSustainMinutes, MaxSustainMinutes)
	s.Cleanup.Exclusions = cleanList(s.Cleanup.Exclusions, home, true)
	s.Cleanup.DevProjectRoots = cleanList(s.Cleanup.DevProjectRoots, home, false)
	s.Cleanup.InstallerRoots = cleanList(s.Cleanup.InstallerRoots, home, false)
	s.Cleanup.DevArtifactMinAgeDays = clamp(s.Cleanup.DevArtifactMinAgeDays, MinDevArtifactMinAge, MaxDevArtifactMinAge)
	s.Cleanup.LargeFileMinMB = clamp(s.Cleanup.LargeFileMinMB, MinLargeFileMB, MaxLargeFileMB)
	s.History.RetentionDays = clamp(s.History.RetentionDays, MinRetentionDays, MaxRetentionDays)
	s.General.LowDiskGB = clamp(s.General.LowDiskGB, MinLowDiskGB, MaxLowDiskGB)
	return s
}

func clamp(v, lo, hi int) int { return min(max(v, lo), hi) }

// cleanList expands "~", cleans and dedupes paths (case-insensitively), dropping relative ones. With patterns, entries that are not absolute but have no separator (names and globs such as "node_modules" or "*.photoslibrary") are kept as they are.
func cleanList(list []string, home string, patterns bool) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, p := range list {
		p = strings.TrimSpace(p)
		if home != "" && (p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`)) {
			p = filepath.Join(home, p[1:])
		}
		switch {
		case p == "":
			continue
		case filepath.IsAbs(p):
			p = filepath.Clean(p)
		case patterns && !strings.ContainsAny(p, `/\`):
		default:
			continue
		}
		key := strings.ToLower(p)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
		if len(out) == MaxPaths {
			break
		}
	}
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return slices.Clip(s)
}
