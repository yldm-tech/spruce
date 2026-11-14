package trash

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestKeepBothName(t *testing.T) {
	for _, c := range []struct {
		in   string
		n    int
		want string
	}{
		{"/a/report.pdf", 2, "/a/report 2.pdf"},
		{"/a/Photos", 3, "/a/Photos 3"},
		{"/a/Xcode.app", 2, "/a/Xcode 2.app"},
		{"/a/archive.tar.gz", 2, "/a/archive.tar 2.gz"},
		{"/a/.env", 2, "/a/.env 2"},
		{"/a/.config.json", 99, "/a/.config 99.json"},
	} {
		if got := keepBothName(filepath.FromSlash(c.in), c.n); got != filepath.FromSlash(c.want) {
			t.Errorf("keepBothName(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}

func TestDirectlyIn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Trash folders are macOS paths")
	}
	dirs := []string{"/u/.Trash", "/Volumes/X/.Trashes/501"}
	for _, c := range []struct {
		path string
		want bool
	}{
		{"/u/.Trash/a.dmg", true},
		{"/u/.Trash/sub/a.dmg", false},
		{"/u/.Trash", false},
		{"/u/.Trash/../a.dmg", false},
		{"/Volumes/X/.Trashes/501/movie.mov", true},
		{"/u/Library/Mobile Documents/.Trash/a", false},
		{"", false},
		{"u/.Trash/a", false},
	} {
		if got := directlyIn(c.path, dirs); got != c.want {
			t.Errorf("directlyIn(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}
