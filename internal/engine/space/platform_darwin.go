package space

import "path/filepath"

// foldPaths: paths of the tree compare case-sensitively on macOS, as fsx compares allowed roots, so a path spelled differently from the scan is refused rather than guessed at.
const foldPaths = false

// SystemLocations are the folders outside the home folder where the page moves nothing to the Trash: the operating system, the shared libraries and the Unix tree.
func SystemLocations() []string {
	return []string{
		"/System", "/Library", "/usr", "/bin", "/sbin", "/private", "/etc", "/var", "/tmp", "/opt", "/cores", "/dev", "/Network",
	}
}

func startupRoot() string { return "/" }

func appsFolder() string { return "/Applications" }

// isVolumeRoot reports whether a cleaned absolute path is the root of a volume.
func isVolumeRoot(p string) bool {
	return p == "/" || filepath.Dir(p) == "/Volumes" || p == "/System/Volumes/Data"
}

// dataVolume is where the startup disk's data volume is mounted: the same APFS container as "/", whose user folders /Users, /Applications, /Library and /private reach through firmlinks.
const dataVolume = "/System/Volumes/Data"

// planFor returns the folders a scan of root leaves out and the volumes that count as root's. A scan of the whole startup disk walks "/" and enters the data volume through its firmlinks, but not /System/Volumes, which would count the data volume a second time and holds the VM, Preboot and Update volumes, nor the swap files of /private/var/vm.
func planFor(root string) (skip, same []string) {
	if root != "/" {
		return nil, nil
	}
	return []string{"/System/Volumes", "/private/var/vm", "/dev"}, []string{dataVolume}
}
