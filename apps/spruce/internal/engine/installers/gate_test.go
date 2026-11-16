package installers

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"sync"
	"testing"
)

// TestMayReadSkipsRoots: a root or sub-folder the read gate refuses is neither listed nor counted as denied, and the scan goes on without an error.
func TestMayReadSkipsRoots(t *testing.T) {
	fx := newFixture(t, false)
	write(t, fx.home, "Downloads/a.dmg", 0, 30*day)
	write(t, fx.home, "Desktop/b.pkg", 0, 30*day)
	write(t, fx.home, "Desktop/old/c.dmg", 0, 30*day)
	var (
		mu    sync.Mutex
		asked []string
	)
	fx.env.MayRead = func(p string) bool {
		mu.Lock()
		defer mu.Unlock()
		asked = append(asked, p)
		return p != fx.desktop
	}
	_, res := fx.scan(t)
	got := fx.contents(t, res)
	if !slices.Equal(got[catDiskImages], []string{"Downloads/a.dmg"}) || len(got[catPackages]) != 0 || res.Denied != 0 {
		t.Errorf("contents %v, denied %d", got, res.Denied)
	}
	if !slices.Contains(asked, fx.downloads) || !slices.Contains(asked, fx.desktop) {
		t.Errorf("asked %v", asked)
	}

	// A refused sub-folder is skipped too.
	fx.env.MayRead = func(p string) bool { return p != filepath.Join(fx.desktop, "old") }
	_, res = fx.scan(t)
	got = fx.contents(t, res)
	if !slices.Equal(got[catDiskImages], []string{"Downloads/a.dmg"}) || !slices.Equal(got[catPackages], []string{"Desktop/b.pkg"}) {
		t.Errorf("contents %v", got)
	}
}

// TestMountedUnknownWarns: when the attached images cannot be listed, 磁盘映像 carries a warning, which keeps automatic rules away; the other categories and a working listing carry none.
func TestMountedUnknownWarns(t *testing.T) {
	fx := newFixture(t, false)
	write(t, fx.home, "Downloads/a.dmg", 0, 30*day)
	write(t, fx.home, "Downloads/b.pkg", 0, 30*day)
	_, res := fx.scan(t)
	for _, c := range res.Categories {
		if c.Warning != "" {
			t.Errorf("%s: warning %q with a working list", c.ID, c.Warning)
		}
	}
	fx.env.Mounted = func(context.Context) ([]string, error) { return nil, errors.New("hdiutil failed") }
	_, res = fx.scan(t)
	if c := category(t, res, catDiskImages); c.Warning != "无法确认哪些磁盘映像正在使用" || !c.Selected {
		t.Errorf("disk images: warning %q, selected %v", c.Warning, c.Selected)
	}
	if c := category(t, res, catPackages); c.Warning != "" {
		t.Errorf("packages warned: %q", c.Warning)
	}
}
