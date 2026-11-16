package growth

// UserFolders returns nothing on macOS: Downloads, Desktop and Documents are always in the home folder there.
func UserFolders() map[string]string { return nil }
