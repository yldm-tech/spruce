package installers

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// kind is the type of installer, one category each.
type kind int

const (
	kindNone kind = iota
	kindDiskImage
	kindPackage
	kindArchive
	kindSystemImage
)

// label is the type's name, used as the item note in the recent category.
func (k kind) label() string {
	switch k {
	case kindDiskImage:
		return "磁盘映像"
	case kindPackage:
		return "安装器"
	case kindArchive:
		return "压缩包"
	case kindSystemImage:
		return "系统映像"
	}
	return ""
}

// minArchiveSize is the size from which a .zip that looks like an installer is offered: smaller ones are more likely documents or exports than apps.
const minArchiveSize = 10 * 1000 * 1000

// file is what classify knows about one entry.
type file struct {
	name string
	dir  bool
	size int64
	// topOfDownloads is set for an entry directly in the Downloads folder.
	topOfDownloads bool
}

// classify returns the kind of installer f is on macOS or Windows, or kindNone.
func classify(f file, windows bool) kind {
	ext := strings.ToLower(filepath.Ext(f.name))
	if windows {
		if f.dir {
			return kindNone
		}
		switch ext {
		case ".msi", ".msix", ".msixbundle", ".appx", ".appxbundle":
			return kindPackage
		case ".iso":
			return kindSystemImage
		case ".exe":
			// Downloaded executables are nearly always installers; anywhere else a program may be kept to run.
			if f.topOfDownloads || installerLike(f.name) {
				return kindPackage
			}
		}
		return kindNone
	}
	switch ext {
	case ".pkg", ".mpkg":
		// Flat packages are files, older ones bundles.
		return kindPackage
	}
	if f.dir {
		return kindNone
	}
	switch ext {
	case ".dmg":
		return kindDiskImage
	case ".iso":
		return kindSystemImage
	case ".xip":
		return kindArchive
	case ".zip":
		if f.size >= minArchiveSize && installerLike(f.name) {
			return kindArchive
		}
	}
	return kindNone
}

var versionPattern = regexp.MustCompile(`\d+\.\d+|(^|[^a-z])v\d+`)

// installerWords are name tokens that mark an installer: platforms and architectures.
var installerWords = []string{"mac", "macos", "osx", "darwin", "arm64", "aarch64", "x64", "x86", "amd64", "universal", "win32", "win64"}

// installerLike reports whether a file name looks like an installer's: it has a version number, says setup or install, or names a platform or architecture as a word ("VSCode-darwin-universal.zip", "ChromeSetup.exe", "node-v22.11.0.pkg").
func installerLike(name string) bool {
	stem := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
	if strings.Contains(stem, "setup") || strings.Contains(stem, "install") || versionPattern.MatchString(stem) {
		return true
	}
	words := strings.FieldsFunc(stem, func(r rune) bool { return (r < 'a' || r > 'z') && (r < '0' || r > '9') })
	return slices.ContainsFunc(words, func(w string) bool { return slices.Contains(installerWords, w) })
}

var packageExt = regexp.MustCompile(`^\.[A-Za-z]+$`)

// descendable reports whether the search may look inside a sub-folder: not hidden, and not named like a package (an extension of letters only: Some.app, Photos.photoslibrary, Project.xcodeproj), which macOS shows as a single file. A folder like node-v22.1 still qualifies.
func descendable(name string) bool {
	return !strings.HasPrefix(name, ".") && !packageExt.MatchString(filepath.Ext(name))
}
