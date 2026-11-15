package growth

import "golang.org/x/sys/windows"

// UserFolders returns where Windows keeps the user's Downloads, Desktop and Documents, by watch ID, as the known folders say: the user may have moved them off the home folder, and OneDrive's folder backup moves Desktop and Documents under %USERPROFILE%\OneDrive. A folder Windows cannot name is left out, so DefaultWatches falls back to the home folder's own.
func UserFolders() map[string]string {
	out := map[string]string{}
	for id, f := range map[string]*windows.KNOWNFOLDERID{
		"downloads": windows.FOLDERID_Downloads,
		"desktop":   windows.FOLDERID_Desktop,
		"documents": windows.FOLDERID_Documents,
	} {
		if dir, err := windows.KnownFolderPath(f, windows.KF_FLAG_DEFAULT); err == nil && dir != "" {
			out[id] = dir
		}
	}
	return out
}
