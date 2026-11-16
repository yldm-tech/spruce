package overview

import (
	"slices"
	"testing"
)

// Only macOS has prompting folders (folderaccess.Prompting is never true on Windows), so the skipped paths SkippedNames names are macOS paths.
func TestSkippedNames(t *testing.T) {
	home := "/Users/me"
	got := SkippedNames(home, []string{home + "/Downloads", home + "/Desktop", home + "/Documents", home + "/Library/Mobile Documents",
		home + "/Library/CloudStorage/Dropbox-Personal", "/Volumes/T7", "/Volumes/Backup", home + "/Downloads"})
	want := []string{"下载", "桌面", "文稿", "iCloud 云盘", "Dropbox", "外接磁盘"}
	if !slices.Equal(got, want) {
		t.Errorf("names %v", got)
	}
}
