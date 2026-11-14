// Package uninstall is the engine of the 应用卸载 page: it lists the installed apps, measures them, finds the files an app leaves behind and removes an app with the files the user picked. It does not import mygo; internal/services/uninstall.go binds it.
//
// The flow is list → pick → leftovers → remove, one app at a time, rather than the scan-all/clean model of the cleaner pages, so the engine has its own API (Engine) and uses cleanup.Executor only for the deletions: every removal goes to the Trash (Recycle Bin), only for items of the last Plan, re-checked before each move.
//
// # macOS
//
// Apps are the .app bundles in /Applications and ~/Applications, and in their direct subfolders (/Applications/Foo/Foo.app); bundles are never descended into and symlinks are never followed. Apple's own apps (com.apple.*) and Spruce itself are left out. Each bundle's Contents/Info.plist gives its name, bundle ID, version and icon; Contents/_MASReceipt/receipt marks an App Store app. Details then stream in: when it was last used (Spotlight's kMDItemLastUsedDate through mdls), its size (fsx.DirSize) and its icon (the bundle's .icns converted to a PNG by sips and cached under the app's data folder, keyed by bundle ID and the icon's modification time).
//
// Leftovers match the app's exact bundle ID, or its exact name where the rules allow a name, in the user's ~/Library (Application Support, Caches, Preferences and ByHost, Containers, Group Containers, Saved Application State, HTTPStorages, WebKit, Logs, Cookies) and the LaunchAgents whose Label is the bundle ID or whose program lies inside the bundle. Removing an app moves the bundle to the Trash first; only when that worked are the selected leftovers moved. An app that is running cannot be removed.
//
// # Windows
//
// Apps are the entries of the Uninstall keys of HKLM (64- and 32-bit views) and HKCU, without system components, updates and entries lacking a name or an uninstall command (ParseEntry). Removing an app runs its own uninstaller (UninstallString, or msiexec /x for Windows Installer products) through ShellExecuteEx, so UAC and the vendor's UI appear, waits for it to exit and reads the registry again. Only then does the page offer the leftovers, user-level folders under %APPDATA% and %LOCALAPPDATA% named exactly after the program (and its publisher), which go to the Recycle Bin. Store apps are not listed.
package uninstall
