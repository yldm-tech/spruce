package folderaccess_test

import (
	"testing"

	"spruce/apps/spruce/internal/host/folderaccess"
)

// TestPromptingDarwin checks the macOS rules on macOS paths, which are absolute only where the separator is a slash.
func TestPromptingDarwin(t *testing.T) {
	home := "/Users/x"
	for path, want := range map[string]string{
		"/Users/x/Desktop":                                           "/Users/x/Desktop",
		"/Users/x/Desktop/a/b":                                       "/Users/x/Desktop",
		"/Users/x/desktop/a":                                         "/Users/x/Desktop",
		"/Users/x/Documents/code/../x":                               "/Users/x/Documents",
		"/Users/x/Downloads":                                         "/Users/x/Downloads",
		"/Users/x/Library/Mobile Documents/c/d":                      "/Users/x/Library/Mobile Documents",
		"/Users/x/Library/CloudStorage/Dropbox":                      "/Users/x/Library/CloudStorage/Dropbox",
		"/Users/x/Library/CloudStorage/GoogleDrive-a@b.c/My Drive/x": "/Users/x/Library/CloudStorage/GoogleDrive-a@b.c",
		"/Volumes/USB":                                               "/Volumes/USB",
		"/Volumes/Backup Disk/a/b":                                   "/Volumes/Backup Disk",
		"/Users/x/Library/CloudStorage":                              "",
		"/Volumes":                                                   "",
		"/Users/x":                                                   "",
		"/Users/x/Library/Caches/a":                                  "",
		"/Users/x/DesktopOld/a":                                      "",
		"/Users/x/code/Desktop":                                      "",
		"Desktop":                                                    "",
		"/Users/y/Desktop":                                           "",
	} {
		got, ok := folderaccess.PromptingOn("darwin", home, path)
		if got != want || ok != (want != "") {
			t.Errorf("%s: %q %v, want %q", path, got, ok, want)
		}
	}
}
