// Package settingsfake is the 设置 page's services for tests: settings that clamp, fail and reset as a test says, announced to the store as settings.Store announces them, and Files that records what it reveals. Only tests import it.
package settingsfake

import (
	"context"
	"slices"
	"sync"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/settings"
	"github.com/yldm-tech/spruce/internal/ui/store"
)

// Limits are the ranges of the React page's tests: the settings package's, with three folders a list at most.
var Limits = app.SettingsLimits{
	CPUThreshold:          app.SettingRange{Min: settings.MinCPUThreshold, Max: settings.MaxCPUThreshold},
	SustainMinutes:        app.SettingRange{Min: settings.MinSustainMinutes, Max: settings.MaxSustainMinutes},
	DevArtifactMinAgeDays: app.SettingRange{Min: settings.MinDevArtifactMinAge, Max: settings.MaxDevArtifactMinAge},
	LargeFileMinMB:        app.SettingRange{Min: settings.MinLargeFileMB, Max: settings.MaxLargeFileMB},
	RetentionDays:         app.SettingRange{Min: settings.MinRetentionDays, Max: settings.MaxRetentionDays},
	MaxPaths:              3,
}

// Defaults are the settings of the React page's tests.
func Defaults() settings.AppSettings {
	return settings.AppSettings{
		Version: settings.SchemaVersion,
		Health:  settings.HealthSettings{CPUThreshold: 40, SustainMinutes: 10, Enabled: true},
		Cleanup: settings.CleanupSettings{
			Exclusions:            []string{"/Users/me/Keep"},
			DevProjectRoots:       []string{"/Users/me/Projects"},
			DevArtifactMinAgeDays: 30,
			InstallerRoots:        []string{"/Users/me/Downloads", "/Users/me/Desktop"},
			LargeFileMinMB:        500,
		},
		History: settings.HistorySettings{RetentionDays: 180},
		General: settings.GeneralSettings{LaunchAtLogin: false, ConfirmBeforeClean: true},
	}
}

// Settings is store.SettingsService and store.SettingsFeed over Stored. Set its fields before the page starts, or through its methods while a test runs.
type Settings struct {
	mu sync.Mutex
	// Stored is what Get answers and what Update and Reset change.
	Stored settings.AppSettings
	// ResetTo is what Reset stores.
	ResetTo settings.AppSettings
	// Clamp, when set, turns what Update receives into what it stores, as the settings package clamps.
	Clamp func(settings.AppSettings) settings.AppSettings
	// UpdateErr and ResetErr, when set, are what Update and Reset fail with, storing nothing.
	UpdateErr, ResetErr error
	// ResetRelease, when set, holds each Reset until it receives.
	ResetRelease chan struct{}
	// Fail, when set, is what Update fails with for the settings it receives (nil stores them).
	Fail func(settings.AppSettings) error
	// Wait, when set, holds each Update, with what it received, before it answers.
	Wait func(settings.AppSettings)
	// About is what About answers.
	AboutInfo app.AppAbout

	updates  []settings.AppSettings
	answered int
	resets   int
	gets     int
	ch       chan settings.AppSettings
}

// New returns settings holding Defaults, resetting to Defaults, with the version 0.1.0 and dataDir as the data folder.
func New(dataDir string) *Settings {
	return &Settings{Stored: Defaults(), ResetTo: Defaults(), AboutInfo: app.AppAbout{Version: "0.1.0", DataDir: dataDir},
		ch: make(chan settings.AppSettings, 1)}
}

// Use puts s in d, in place of the settings d had.
func (s *Settings) Use(d store.Deps) store.Deps {
	d.Settings, d.SettingsFeed = s, s
	return d
}

// SetUpdateErr changes what Update fails with from now on.
func (s *Settings) SetUpdateErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.UpdateErr = err
}

// SetClamp changes how Update clamps from now on.
func (s *Settings) SetClamp(fn func(settings.AppSettings) settings.AppSettings) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Clamp = fn
}

// Change changes the stored settings as another source would (System Settings turning off the login item, another window) and announces them.
func (s *Settings) Change(fn func(*settings.AppSettings)) {
	s.mu.Lock()
	fn(&s.Stored)
	v := clone(s.Stored)
	s.mu.Unlock()
	s.announce(v)
}

// Updates returns what Update received, oldest first.
func (s *Settings) Updates() []settings.AppSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.updates)
}

// Last returns what Update received last, and whether it was called.
func (s *Settings) Last() (settings.AppSettings, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.updates) == 0 {
		return settings.AppSettings{}, false
	}
	return s.updates[len(s.updates)-1], true
}

// Answered counts the calls to Update that have answered (or are answering, past Wait).
func (s *Settings) Answered() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.answered
}

// Resets counts the calls to Reset.
func (s *Settings) Resets() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resets
}

// Gets counts the calls to Get.
func (s *Settings) Gets() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gets
}

// Get implements store.SettingsService.
func (s *Settings) Get() settings.AppSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gets++
	return clone(s.Stored)
}

// Update implements store.SettingsService.
func (s *Settings) Update(next settings.AppSettings) (settings.AppSettings, error) {
	s.mu.Lock()
	s.updates = append(s.updates, clone(next))
	wait := s.Wait
	s.mu.Unlock()
	if wait != nil {
		wait(next)
	}
	s.mu.Lock()
	s.answered++
	err := s.UpdateErr
	if err == nil && s.Fail != nil {
		err = s.Fail(next)
	}
	if err != nil {
		stored := clone(s.Stored)
		s.mu.Unlock()
		return stored, err
	}
	if s.Clamp != nil {
		next = s.Clamp(next)
	}
	s.Stored = clone(next)
	s.mu.Unlock()
	s.announce(next)
	return clone(next), nil
}

// Reset implements store.SettingsService.
func (s *Settings) Reset() (settings.AppSettings, error) {
	s.mu.Lock()
	s.resets++
	release := s.ResetRelease
	s.mu.Unlock()
	if release != nil {
		<-release
	}
	s.mu.Lock()
	if err := s.ResetErr; err != nil {
		stored := clone(s.Stored)
		s.mu.Unlock()
		return stored, err
	}
	s.Stored = clone(s.ResetTo)
	stored := clone(s.Stored)
	s.mu.Unlock()
	s.announce(stored)
	return stored, nil
}

// Limits implements store.SettingsService.
func (s *Settings) Limits() app.SettingsLimits { return Limits }

// About implements store.SettingsService.
func (s *Settings) About() (app.AppAbout, error) { return s.AboutInfo, nil }

// Subscribe implements store.SettingsFeed: the current settings first, as settings.Store sends them, then every change.
func (s *Settings) Subscribe() (<-chan settings.AppSettings, func()) {
	s.announce(s.Get())
	return s.ch, func() {}
}

// announce sends v to the store, replacing a change it has not read yet.
func (s *Settings) announce(v settings.AppSettings) {
	select {
	case <-s.ch:
	default:
	}
	s.ch <- v
}

func clone(v settings.AppSettings) settings.AppSettings {
	v.Cleanup.Exclusions = slices.Clone(v.Cleanup.Exclusions)
	v.Cleanup.DevProjectRoots = slices.Clone(v.Cleanup.DevProjectRoots)
	v.Cleanup.InstallerRoots = slices.Clone(v.Cleanup.InstallerRoots)
	return v
}

// Files is the page's Files: Reveal records each path and fails with RevealErr.
type Files struct {
	mu        sync.Mutex
	RevealErr error
	revealed  []string
}

// Revealed returns the paths Reveal received, oldest first.
func (f *Files) Revealed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.revealed)
}

// Reveal implements settings.Files.
func (f *Files) Reveal(_ context.Context, path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revealed = append(f.revealed, path)
	return f.RevealErr
}
