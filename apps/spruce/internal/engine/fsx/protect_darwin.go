package fsx

import "path/filepath"

// foldRoots: allowed roots compare case-sensitively on macOS, so a path spelled differently from its root is refused rather than guessed at.
const foldRoots = false

func platformPolicy(home string) policy {
	p := policy{
		home: home,
		exact: []string{
			"/Applications", "/Library", "/Library/Caches", "/Library/Logs", "/Library/Application Support",
			"/Users", "/Users/Shared", "/Volumes", "/System", "/private", "/private/var", "/private/tmp", "/private/var/folders",
			"/var", "/tmp", "/opt", "/opt/homebrew", "/usr", "/usr/local", "/cores", "/Network",
		},
		subtree: []string{
			"/System", "/bin", "/sbin", "/usr/bin", "/usr/sbin", "/usr/lib", "/usr/libexec", "/usr/share", "/usr/standalone",
			"/etc", "/private/etc", "/dev", "/private/var/db", "/private/var/root", "/private/var/protected",
			"/Library/Keychains", "/Library/Security", "/Library/Apple", "/Library/Extensions", "/Library/SystemExtensions",
		},
		volumeRoot: func(p string) bool {
			return p == "/" || filepath.Dir(p) == "/Volumes" || p == "/System/Volumes/Data"
		},
	}
	if home != "" {
		p.exact = append(p.exact, joinAll(home,
			"Applications", "Desktop", "Documents", "Downloads", "Movies", "Music", "Pictures", "Public", "Sites",
			".Trash", "Library", "Library/Caches", "Library/Logs", "Library/Application Support", "Library/Containers",
			"Library/Group Containers", "Library/Preferences", "Library/Developer", "Library/Developer/Xcode", "Library/Saved Application State",
		)...)
		p.subtree = append(p.subtree, joinAll(home,
			"Documents", "Desktop", "Pictures",
			".ssh", ".gnupg",
			"Library/Keychains", "Library/Mobile Documents", "Library/CloudStorage", "Library/Mail", "Library/Messages",
			"Library/Photos", "Library/Accounts", "Library/Calendars", "Library/Safari", "Library/IdentityServices",
			"Library/Application Support/AddressBook", "Library/Application Support/MobileSync",
		)...)
	}
	return p
}
