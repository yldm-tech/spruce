package installers

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/execx"
	"github.com/yldm-tech/spruce/internal/engine/fsx"
	"github.com/yldm-tech/spruce/internal/engine/settings"
)

// Env is what the cleaner reads from the machine. New fills in what is left zero for this platform; tests pass temporary folders and fakes.
type Env struct {
	// Home is the user's home folder.
	Home string
	// Downloads is the Downloads folder, where every .exe counts as an installer on Windows; "" means the platform's.
	Downloads string
	// Settings returns the current settings: InstallerRoots and Exclusions. Required.
	Settings func() settings.AppSettings
	// Mounted returns the paths of the attached disk images; nil means the platform's (hdiutil through Runner on macOS, none on Windows).
	Mounted func(ctx context.Context) ([]string, error)
	// Runner runs hdiutil on macOS when Mounted is nil.
	Runner execx.Runner
	// Deleter moves the items to the Trash; nil means cleanup.SystemDeleter().
	Deleter cleanup.Deleter
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Log receives what the page is not told; nil discards it.
	Log *slog.Logger
	// windows classifies as on Windows; set by New on Windows and by tests.
	windows bool
	// arrived is when a file arrived; nil means added. Tests replace it, since a file's creation time cannot be set back.
	arrived func(fs.FileInfo) time.Time
}

// Category IDs, stable across scans.
const (
	catDiskImages   = "disk-images"
	catPackages     = "installer-packages"
	catArchives     = "archives"
	catSystemImages = "system-images"
	catRecent       = "recent-installers"
)

// minAge is how long ago an installer must have arrived to be checked by default: one downloaded this week may not be installed yet.
const minAge = 7 * 24 * time.Hour

// ErrBusy is returned by Scan and Clean while another scan or clean of the same Cleaner runs.
var ErrBusy = cleanup.ErrBusy

// Cleaner is the cleanup.Scanner of the 安装包 page. It is safe for concurrent use; one scan or clean runs at a time.
type Cleaner struct {
	env  Env
	exec *cleanup.Executor
	busy sync.Mutex
}

var _ cleanup.Scanner = (*Cleaner)(nil)

// New returns the cleaner of this platform.
func New(env Env) *Cleaner {
	env.windows = runtime.GOOS == "windows"
	fillPlatform(&env)
	return newCleaner(env)
}

func newCleaner(env Env) *Cleaner {
	if env.Now == nil {
		env.Now = time.Now
	}
	if env.Log == nil {
		env.Log = slog.New(slog.DiscardHandler)
	}
	if env.Settings == nil {
		env.Settings = func() settings.AppSettings { return settings.AppSettings{} }
	}
	if env.Mounted == nil {
		env.Mounted = func(context.Context) ([]string, error) { return nil, nil }
	}
	if env.arrived == nil {
		env.arrived = added
	}
	return &Cleaner{env: env, exec: cleanup.NewExecutor(env.Deleter)}
}

// found is one installer the walk found.
type found struct {
	path  string
	info  fs.FileInfo
	kind  kind
	size  int64
	added time.Time
}

// Scan searches the installer folders and remembers what it found for Clean.
func (c *Cleaner) Scan(ctx context.Context, progress func(cleanup.Progress)) (cleanup.ScanResult, error) {
	if !c.busy.TryLock() {
		return cleanup.ScanResult{}, ErrBusy
	}
	defer c.busy.Unlock()
	if progress == nil {
		progress = func(cleanup.Progress) {}
	}
	started := c.env.Now()
	s := c.env.Settings().Cleanup
	roots := cleanRoots(s.InstallerRoots)

	mounted := map[string]bool{}
	if paths, err := c.env.Mounted(ctx); err != nil {
		if ctx.Err() != nil {
			return cleanup.ScanResult{}, ctx.Err()
		}
		// Without the list an attached image looks like any other; it is still only moved to the Trash, and macOS refuses to trash a busy one.
		c.env.Log.Warn("list attached disk images", "err", err)
	} else {
		for _, p := range paths {
			mounted[pathKey(p)] = true
		}
	}

	w := &walker{env: c.env, exclusions: s.Exclusions, seen: map[string]bool{}}
	p := cleanup.Progress{Phase: cleanup.PhaseScanning, Total: len(roots)}
	progress(p)
	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return cleanup.ScanResult{}, err
		}
		p.Path = root
		progress(p)
		w.root(ctx, root, sameFolder(root, c.env.Downloads))
		p.Done++
		p.Bytes = w.bytes
		progress(p)
	}
	if err := ctx.Err(); err != nil {
		return cleanup.ScanResult{}, err
	}

	cats := c.categories(w.found, roots, mounted, started)
	res := cleanup.NewResult(cats, started)
	res.Denied = w.denied.Load()
	if err := c.exec.Remember(res); err != nil {
		return cleanup.ScanResult{}, err
	}
	return res, nil
}

// categories sorts what was found into the type categories and the unchecked recent one, dropping anything the Trash would refuse; empty categories are left out.
func (c *Cleaner) categories(list []found, roots []string, mounted map[string]bool, now time.Time) []cleanup.Category {
	tmpl := func(id, title, desc, icon string, selected bool) cleanup.Category {
		return cleanup.Category{
			ID: id, Title: title, Description: desc, Icon: icon, Kind: cleanup.KindInstaller,
			Deletion: cleanup.DeleteTrash, Selected: selected, Roots: roots, Items: []cleanup.Item{},
		}
	}
	pkgDesc := ".pkg 安装器，装好应用后通常就不再需要"
	if c.env.windows {
		pkgDesc = ".exe、.msi 等安装程序，装好应用后通常就不再需要"
	}
	cats := []cleanup.Category{
		tmpl(catDiskImages, "磁盘映像", ".dmg 磁盘映像，装好应用后通常就不再需要", "installer", true),
		tmpl(catPackages, "安装器", pkgDesc, "package", true),
		tmpl(catArchives, "压缩包", "名称像安装包的 .zip 和 .xip 文件", "archive", true),
		tmpl(catSystemImages, "系统映像", ".iso 光盘映像", "disk", true),
		tmpl(catRecent, "最近下载或正在使用的安装包", "7 天内下载的，或仍处于挂载状态的磁盘映像，可能还要用", "temp", false),
	}
	byKind := map[kind]*cleanup.Category{kindDiskImage: &cats[0], kindPackage: &cats[1], kindArchive: &cats[2], kindSystemImage: &cats[3]}
	for _, f := range list {
		if err := fsx.CheckTrashable(f.path, roots); err != nil {
			c.env.Log.Debug("left out an installer the Trash would refuse", "path", f.path, "err", err)
			continue
		}
		it := cleanup.NewItem(f.path, f.info, f.size)
		// The page shows the item's age from ModTime: when the file arrived, which is what the default selection goes by.
		it.ModTime = f.added
		cat := byKind[f.kind]
		attached := f.kind == kindDiskImage && mounted[pathKey(f.path)]
		switch {
		case attached:
			it.Note = f.kind.label() + " · 已挂载"
			cat = &cats[4]
		case now.Sub(f.added) < minAge:
			it.Note = f.kind.label()
			cat = &cats[4]
		}
		cat.Items = append(cat.Items, it)
	}
	out := cats[:0]
	for _, cat := range cats {
		if len(cat.Items) > 0 {
			out = append(out, cat)
		}
	}
	return out
}

// Clean moves the items req names from the last scan to the Trash.
func (c *Cleaner) Clean(ctx context.Context, req cleanup.CleanRequest, progress func(cleanup.Progress)) (cleanup.CleanResult, error) {
	if !c.busy.TryLock() {
		return cleanup.CleanResult{}, ErrBusy
	}
	defer c.busy.Unlock()
	return c.exec.Clean(ctx, req, progress)
}

// walker searches the roots; one scan uses one walker, from one goroutine.
type walker struct {
	env        Env
	exclusions []string
	seen       map[string]bool
	found      []found
	bytes      int64
	denied     atomic.Int64
}

// root searches root and its direct sub-folders.
func (w *walker) root(ctx context.Context, root string, isDownloads bool) {
	for _, e := range w.list(root) {
		if ctx.Err() != nil {
			return
		}
		if w.consider(ctx, e, isDownloads) || !e.info.IsDir() || !descendable(e.name) {
			continue
		}
		for _, sub := range w.list(e.path) {
			if ctx.Err() != nil {
				return
			}
			w.consider(ctx, sub, false)
		}
	}
}

// entry is one child of a folder: a plain file or folder, never a symlink, and not hidden.
type entry struct {
	name string
	path string
	info fs.FileInfo
}

// list returns the visible plain files and folders in dir that are not excluded.
func (w *walker) list(dir string) []entry {
	des, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			w.denied.Add(1)
		}
		return nil
	}
	var out []entry
	for _, de := range des {
		if t := de.Type(); t != 0 && t != fs.ModeDir {
			continue
		}
		if strings.HasPrefix(de.Name(), ".") {
			continue
		}
		info, err := de.Info()
		if err != nil || hidden(info) {
			continue
		}
		p := filepath.Join(dir, de.Name())
		if len(w.exclusions) > 0 && fsx.Excluded(p, w.exclusions) {
			continue
		}
		out = append(out, entry{name: de.Name(), path: p, info: info})
	}
	return out
}

// consider adds e when it is an installer and reports whether it was one.
func (w *walker) consider(ctx context.Context, e entry, topOfDownloads bool) bool {
	f := file{name: e.name, dir: e.info.IsDir(), size: e.info.Size(), topOfDownloads: topOfDownloads}
	k := classify(f, w.env.windows)
	if k == kindNone {
		return false
	}
	key := pathKey(e.path)
	if w.seen[key] {
		return true
	}
	w.seen[key] = true
	size := e.info.Size()
	if f.dir || runtime.GOOS == "darwin" {
		// fsx.DirSize counts allocated blocks on macOS, as Finder does, and walks a bundle package.
		var hit atomic.Bool
		opts := fsx.SizeOptions{Workers: 4}
		if len(w.exclusions) > 0 {
			opts.Skip = func(p string) bool {
				if fsx.Excluded(p, w.exclusions) {
					hit.Store(true)
					return true
				}
				return false
			}
		}
		res, err := fsx.DirSize(ctx, e.path, opts)
		if err != nil || hit.Load() {
			if errors.Is(err, fs.ErrPermission) {
				w.denied.Add(1)
			}
			return true
		}
		size = res.Bytes
	}
	w.bytes += size
	w.found = append(w.found, found{path: e.path, info: e.info, kind: k, size: size, added: w.env.arrived(e.info)})
	return true
}

// added is when the file arrived: the later of its modification time and its creation time. On Windows a copied or extracted file keeps the original's modification time but gets a new creation time, so the modification time alone can make a new file look old; on macOS setting an earlier modification time also moves the birth time back, so there the two rarely differ.
func added(info fs.FileInfo) time.Time {
	t := info.ModTime()
	if b := created(info); b.After(t) {
		t = b
	}
	return t
}

// cleanRoots returns the absolute roots, cleaned and without duplicates.
func cleanRoots(roots []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range roots {
		if !filepath.IsAbs(r) {
			continue
		}
		r = filepath.Clean(r)
		if k := pathKey(r); !seen[k] {
			seen[k] = true
			out = append(out, r)
		}
	}
	return out
}

// pathKey is the key a path is compared by: resolved through symlinks when it exists, and in lower case, as both platforms' default file systems ignore case.
func pathKey(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return strings.ToLower(filepath.Clean(p))
}

// sameFolder reports whether a and b are the same folder.
func sameFolder(a, b string) bool {
	return a != "" && b != "" && pathKey(a) == pathKey(b)
}
