package fsx

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// foldRoots: Windows paths are case-insensitive, and the same folder often arrives spelled differently (C:\Users, c:\users).
const foldRoots = true

func platformPolicy(home string) policy {
	sysRoot := envOr("SystemRoot", `C:\Windows`)
	programData := envOr("ProgramData", `C:\ProgramData`)
	p := policy{
		home: home,
		exact: []string{
			sysRoot, filepath.Join(sysRoot, "Temp"), programData,
			envOr("ProgramFiles", `C:\Program Files`), envOr("ProgramFiles(x86)", `C:\Program Files (x86)`),
		},
		subtree: []string{
			filepath.Join(sysRoot, "System32"), filepath.Join(sysRoot, "SysWOW64"), filepath.Join(sysRoot, "WinSxS"),
			filepath.Join(sysRoot, "servicing"), filepath.Join(sysRoot, "Boot"), filepath.Join(sysRoot, "Fonts"),
			envOr("ProgramFiles", `C:\Program Files`), envOr("ProgramFiles(x86)", `C:\Program Files (x86)`),
			filepath.Join(programData, "Microsoft"),
		},
		volumeRoot: func(p string) bool {
			vol := filepath.VolumeName(p)
			return vol != "" && (p == vol || p == vol+`\`)
		},
	}
	if home != "" {
		p.exact = append(p.exact, filepath.Dir(home))
		p.exact = append(p.exact, joinAll(home,
			"Desktop", "Documents", "Downloads", "Music", "Pictures", "Videos", "Favorites", "Contacts", "source", `source\repos`,
			"AppData", `AppData\Local`, `AppData\Roaming`, `AppData\LocalLow`, `AppData\Local\Temp`, `AppData\Local\Packages`, `AppData\Local\Programs`,
		)...)
		p.subtree = append(p.subtree, joinAll(home,
			"Documents", "Desktop", "Pictures", ".ssh", ".gnupg",
			`AppData\Roaming\Microsoft\Credentials`, `AppData\Roaming\Microsoft\Protect`, `AppData\Roaming\Microsoft\Crypto`,
			`AppData\Roaming\Microsoft\SystemCertificates`, `AppData\Local\Microsoft\Credentials`, `AppData\Local\Microsoft\Vault`,
		)...)
	}
	// Known folders the user may have moved off the home folder (to OneDrive or another drive).
	for _, id := range []*windows.KNOWNFOLDERID{windows.FOLDERID_Documents, windows.FOLDERID_Desktop, windows.FOLDERID_Pictures} {
		if dir, err := windows.KnownFolderPath(id, windows.KF_FLAG_DEFAULT); err == nil && dir != "" {
			p.exact = append(p.exact, dir)
			p.subtree = append(p.subtree, dir)
		}
	}
	for _, id := range []*windows.KNOWNFOLDERID{windows.FOLDERID_Downloads, windows.FOLDERID_Music, windows.FOLDERID_Videos} {
		if dir, err := windows.KnownFolderPath(id, windows.KF_FLAG_DEFAULT); err == nil && dir != "" {
			p.exact = append(p.exact, dir)
		}
	}
	for _, env := range []string{"OneDrive", "OneDriveConsumer", "OneDriveCommercial"} {
		if dir := os.Getenv(env); dir != "" && filepath.IsAbs(dir) {
			p.subtree = append(p.subtree, filepath.Clean(dir))
		}
	}
	return p
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" && filepath.IsAbs(v) {
		return filepath.Clean(v)
	}
	return fallback
}
