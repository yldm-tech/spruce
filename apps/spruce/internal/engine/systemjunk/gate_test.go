package systemjunk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"spruce/apps/spruce/internal/engine/trash"
)

// TestMayReadSkipsDownloads: a folder the read gate refuses is neither listed nor counted as denied, and the scan goes on without an error.
func TestMayReadSkipsDownloads(t *testing.T) {
	fx := newFixture(t)
	fx.env.volumes = t.TempDir()
	darwinHome(t, fx.home)
	write(t, fx.home, "Downloads/a.crdownload", 2000, 72*time.Hour)
	downloads := filepath.Join(fx.home, "Downloads")
	var (
		mu    sync.Mutex
		asked []string
	)
	fx.env.MayRead = func(p string) bool {
		mu.Lock()
		defer mu.Unlock()
		asked = append(asked, p)
		return p != downloads
	}
	res := scan(t, newCleaner(fx.env, darwinSpecs))
	if slices.Contains(ids(res), catIncompleteDownload) {
		t.Errorf("Downloads was read: %v", ids(res))
	}
	if !slices.Contains(ids(res), catAppCaches) || res.Denied != 0 {
		t.Errorf("categories %v, denied %d", ids(res), res.Denied)
	}
	if !slices.Contains(asked, downloads) || !slices.Contains(asked, filepath.Join(fx.home, "Library", "Caches")) {
		t.Errorf("asked %v", asked)
	}

	// Allowed again, the same scan finds the download.
	fx.env.MayRead = func(string) bool { return true }
	res = scan(t, newCleaner(fx.env, darwinSpecs))
	if got := rels(t, fx.home, category(t, res, catIncompleteDownload)); !slices.Contains(got, "Downloads/a.crdownload") {
		t.Errorf("downloads %v", got)
	}
}

// TestExcludedFolderIsNotListed: an excluded folder is not read at all, so the read gate is not asked about it either.
func TestExcludedFolderIsNotListed(t *testing.T) {
	fx := newFixture(t)
	fx.env.volumes = t.TempDir()
	write(t, fx.home, "Downloads/a.crdownload", 2000, 72*time.Hour)
	downloads := filepath.Join(fx.home, "Downloads")
	fx.exclusions = []string{downloads}
	fx.env.MayRead = func(p string) bool {
		if p == downloads {
			t.Errorf("gate asked about an excluded folder")
		}
		return true
	}
	res := scan(t, newCleaner(fx.env, darwinSpecs))
	if slices.Contains(ids(res), catIncompleteDownload) {
		t.Errorf("categories %v", ids(res))
	}
}

// countingBin counts the measurements of the Trash it wraps.
type countingBin struct {
	trash.Bin
	sized atomic.Int32
}

func (b *countingBin) Size(ctx context.Context) (trash.TrashUsage, error) {
	b.sized.Add(1)
	return b.Bin.Size(ctx)
}

// TestMayReadGatesTheTrash: the Trash folder on a mounted volume lies in a prompting folder, so the read gate is asked about it before the Trash is measured; when it refuses, the Trash is not measured at all and the rest of the scan goes on.
func TestMayReadGatesTheTrash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	fx := newFixture(t)
	darwinHome(t, fx.home)
	volumes := t.TempDir()
	mkdir(t, volumes, "USB")
	mkdir(t, volumes, "Share")
	if err := os.Symlink("/", filepath.Join(volumes, "Macintosh HD")); err != nil {
		t.Fatal(err)
	}
	uid := strconv.Itoa(os.Getuid())
	usb := filepath.Join(volumes, "USB", ".Trashes", uid)
	share := filepath.Join(volumes, "Share", ".Trashes", uid)
	bin := &countingBin{Bin: fx.bin}
	fx.env.Bin = bin
	fx.env.volumes = volumes
	var (
		mu    sync.Mutex
		asked []string
	)
	gate := func(refuse string) func(string) bool {
		return func(p string) bool {
			mu.Lock()
			defer mu.Unlock()
			asked = append(asked, p)
			return p != refuse
		}
	}

	fx.env.MayRead = gate(usb)
	res := scan(t, newCleaner(fx.env, darwinSpecs))
	if n := bin.sized.Load(); n != 0 {
		t.Errorf("Trash measured %d times with a volume refused", n)
	}
	if slices.Contains(ids(res), catTrash) || !slices.Contains(ids(res), catAppCaches) {
		t.Errorf("categories %v", ids(res))
	}
	if !slices.Contains(asked, usb) || !slices.Contains(asked, fx.env.TrashPath) {
		t.Errorf("asked %v", asked)
	}
	for _, p := range asked {
		if strings.HasPrefix(p, filepath.Join(volumes, "Macintosh HD")) {
			t.Errorf("asked about the startup volume's link: %s", p)
		}
	}

	// The home Trash refused keeps it unmeasured too.
	fx.env.MayRead = gate(fx.env.TrashPath)
	scan(t, newCleaner(fx.env, darwinSpecs))
	if n := bin.sized.Load(); n != 0 {
		t.Errorf("Trash measured %d times with the home Trash refused", n)
	}

	// Every Trash folder allowed, the Trash is measured as before.
	asked = nil
	fx.env.MayRead = gate("")
	res = scan(t, newCleaner(fx.env, darwinSpecs))
	if n := bin.sized.Load(); n != 1 || !slices.Contains(ids(res), catTrash) {
		t.Errorf("measured %d times, categories %v", n, ids(res))
	}
	if !slices.Contains(asked, usb) || !slices.Contains(asked, share) {
		t.Errorf("asked %v", asked)
	}

	// Without a gate nothing is asked and the Trash is measured.
	fx.env.MayRead = nil
	res = scan(t, newCleaner(fx.env, darwinSpecs))
	if n := bin.sized.Load(); n != 2 || !slices.Contains(ids(res), catTrash) {
		t.Errorf("measured %d times, categories %v", n, ids(res))
	}
}

// TestRunningUnknownWarns: when the running apps cannot be listed, the app and browser caches carry a warning, which keeps automatic rules away; a listing that works leaves them without one.
func TestRunningUnknownWarns(t *testing.T) {
	fx := newFixture(t)
	darwinHome(t, fx.home)
	res := scan(t, newCleaner(fx.env, darwinSpecs))
	for _, id := range []string{catAppCaches, catBrowserCaches} {
		if w := category(t, res, id).Warning; w != "" {
			t.Errorf("%s: warning %q with a working list", id, w)
		}
	}
	fx.env.Running = func(context.Context) (Running, error) { return nil, errors.New("lsappinfo failed") }
	res = scan(t, newCleaner(fx.env, darwinSpecs))
	for _, id := range []string{catAppCaches, catBrowserCaches} {
		c := category(t, res, id)
		if c.Warning != "无法确认哪些应用正在运行" || !c.Selected {
			t.Errorf("%s: warning %q, selected %v", id, c.Warning, c.Selected)
		}
	}
	for _, c := range res.Categories {
		if c.ID != catAppCaches && c.ID != catBrowserCaches && c.Warning == "无法确认哪些应用正在运行" {
			t.Errorf("%s warned too", c.ID)
		}
	}
	// The scan of a cancelled listing is still an error, as before.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := newCleaner(fx.env, darwinSpecs).Scan(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("err %v", err)
	}
}
