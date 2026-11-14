package installers

import (
	"errors"
	"io/fs"
	"slices"
	"syscall"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/execx/execxtest"
)

const hdiutilOutput = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>framework</key>
	<string>671</string>
	<key>images</key>
	<array>
		<dict>
			<key>image-path</key>
			<string>/Users/me/Downloads/Firefox 132.0.dmg</string>
			<key>image-type</key>
			<string>UDIF read-only compressed (zlib)</string>
			<key>writeable</key>
			<false/>
		</dict>
		<dict>
			<key>image-path</key>
			<string>/Users/me/Downloads/Other.dmg</string>
		</dict>
	</array>
</dict>
</plist>`

func TestAttachedImages(t *testing.T) {
	f := execxtest.New()
	f.On("/usr/bin/hdiutil", "info", "-plist").Return(hdiutilOutput, 0)
	got, err := attachedImages(t.Context(), f)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"/Users/me/Downloads/Firefox 132.0.dmg", "/Users/me/Downloads/Other.dmg"}) {
		t.Errorf("images %v", got)
	}
	f.On("/usr/bin/hdiutil", "info", "-plist").Return("not a plist", 0)
	if _, err := attachedImages(t.Context(), f); err == nil {
		t.Error("garbage parsed")
	}
	f.On("/usr/bin/hdiutil", "info", "-plist").Fail(errors.New("no hdiutil"))
	if _, err := attachedImages(t.Context(), f); err == nil {
		t.Error("no error")
	}
	// No images attached: an empty list.
	f.On("/usr/bin/hdiutil", "info", "-plist").Return(`<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>images</key><array/></dict></plist>`, 0)
	if got, err := attachedImages(t.Context(), f); err != nil || len(got) != 0 {
		t.Errorf("empty: %v %v", got, err)
	}
}

// statInfo is a FileInfo with a given modification time and Stat_t.
type statInfo struct {
	fs.FileInfo
	mod time.Time
	st  *syscall.Stat_t
}

func (i statInfo) ModTime() time.Time { return i.mod }
func (i statInfo) Sys() any           { return i.st }

func TestAddedPrefersTheLaterTime(t *testing.T) {
	mod := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	born := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	st := &syscall.Stat_t{Birthtimespec: syscall.NsecToTimespec(born.UnixNano()), Flags: ufHidden}
	info := statInfo{mod: mod, st: st}
	if got := added(info); !got.Equal(born) {
		t.Errorf("added %v, want the birth time %v", got, born)
	}
	if !hidden(info) {
		t.Error("UF_HIDDEN not seen")
	}
	st.Birthtimespec = syscall.NsecToTimespec(mod.Add(-time.Hour).UnixNano())
	st.Flags = 0
	if got := added(info); !got.Equal(mod) || hidden(info) {
		t.Errorf("added %v, hidden %v", got, hidden(info))
	}
}
