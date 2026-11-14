package systemjunk

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

// TestMayReadSkipsDownloads: a folder the read gate refuses is neither listed nor counted as denied, and the scan goes on without an error.
func TestMayReadSkipsDownloads(t *testing.T) {
	fx := newFixture(t)
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
