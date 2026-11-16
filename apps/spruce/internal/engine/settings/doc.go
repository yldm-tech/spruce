// Package settings keeps the user's preferences in one JSON file in the app's data folder and tells interested parts of the app when they change. It does not import any UI framework; internal/app saves through the Store and the UI's settings store follows Subscribe.
//
//	// Open returns a Store keeping its settings in the file at path; home expands a leading "~" in paths. Open performs no I/O: the file is read on first use and written on the first change.
//	func Open(path, home string, defaults func() AppSettings) (*Store, error)
//
//	func (s *Store) Get() AppSettings                              // the current settings, defaults where the file is missing or unreadable
//	func (s *Store) Set(next AppSettings) (AppSettings, error)     // normalizes, writes atomically, notifies; returns what was stored
//	func (s *Store) Update(fn func(*AppSettings)) (AppSettings, error)
//	func (s *Store) Reset() (AppSettings, error)                   // back to the defaults, keeping a longer RetentionDays
//	func (s *Store) Subscribe() (<-chan AppSettings, func())       // the current settings at once, then every change
//
// # Normalizing
//
// Every value that enters the Store, from the file or from the page, goes through Normalize: numbers are clamped to their documented ranges, paths are made absolute (a leading "~" is the home folder) and cleaned, relative or empty paths are dropped, duplicates removed and lists capped. The page therefore never has to reject input: it shows what Set returned.
//
// # File format
//
// One JSON object (AppSettings) with "version": SchemaVersion. Fields missing from the file keep their defaults, so adding a field is backward compatible; a file written by a newer version is read as far as it is understood and rewritten with this version's fields on the next change. A file that does not parse is logged and replaced by the defaults on the next change. Writes go to a temporary file in the same folder, are synced and renamed over the old file, so a crash leaves the old or the new settings, never a mix.
//
// # Defaults
//
// Defaults(home) is the platform's defaults: CPU alert at 40 % of one core for 10 minutes, enabled; developer project roots among ~/Workspace, ~/Projects, ~/Developer, ~/code and ~/src (and %USERPROFILE%\source\repos on Windows) that exist; build output older than 30 days; installers in Downloads and Desktop; large files from 500 MB; history kept 180 days; confirm before cleaning (no longer read); keep running in the menu bar when the window closes; no low-disk alert, its threshold 20 GB; scheduled checks on; every automatic rule off and the weekly summary on.
//
// # Versions
//
// Version 2 added Background, Automation and General's KeepInTray, LowDiskAlert and LowDiskGB. A version 1 file decodes over the defaults, so it gets their defaults; Normalize then stamps version 2. A version 1 build reading a version 2 file ignores the new fields and drops them on its next write, an accepted downgrade.
package settings
