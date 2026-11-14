package devjunk

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/execx"
	"github.com/yldm-tech/spruce/internal/engine/fsx"
	"github.com/yldm-tech/spruce/internal/engine/settings"
)

// measureWorkers bounds how many items are measured at once; each measurement walks with its own pool of fsx.DirSize workers.
const measureWorkers = 4

// Options configures a Cleaner. Home, Runner and Settings are required; the rest default to the real system and exist for tests.
type Options struct {
	// Home is the user's home folder.
	Home   string
	Runner execx.Runner
	// Settings returns the current cleanup settings (project roots, exclusions, minimum age); it is read at the start of every scan.
	Settings func() settings.CleanupSettings
	Log      *slog.Logger

	// GOOS and Getenv describe the platform and environment; default runtime.GOOS and os.Getenv.
	GOOS   string
	Getenv func(string) string
	// LookPath finds a binary by name or checks one by path; default exec.LookPath.
	LookPath func(string) (string, error)
	// Deleter deletes and trashes file items; default cleanup.SystemDeleter.
	Deleter cleanup.Deleter
	// VSCodeRunning reports whether VS Code runs; default looks at the running processes.
	VSCodeRunning func(context.Context) bool
	// Now is the clock for project ages; default time.Now.
	Now func() time.Time
	// Removable and Trashable check where an item may go (default fsx.CheckRemovable and fsx.CheckTrashable).
	Removable func(path string, roots []string) error
	Trashable func(path string, roots []string) error
	// Excluded matches the user's exclusions; default fsx.Excluded.
	Excluded func(path string, exclusions []string) bool
}

// Cleaner is the 开发者垃圾 cleaner. It is safe for concurrent use.
type Cleaner struct {
	opts Options
	env  env
	del  *scopedDeleter
	exec *cleanup.Executor

	cleaning sync.Mutex

	mu   sync.Mutex
	last *snapshot
}

var _ cleanup.Scanner = (*Cleaner)(nil)

// snapshot is what the Cleaner keeps of its last scan beyond the Executor's copy.
type snapshot struct {
	// order is the category IDs in scan order.
	order []string
	// items maps every item ID to its category.
	items map[string]string
	// commands are the categories cleaned by a command, by category ID.
	commands map[string]*commandJob
}

// commandJob is a category cleaned by running its tool's command.
type commandJob struct {
	categoryID string
	itemID     string
	title      string
	path       string
	size       int64
	cmd        execx.Command
	// measure returns what is left after the command ran.
	measure func(ctx context.Context) int64
}

// New returns a Cleaner over opts.
func New(opts Options) *Cleaner {
	if opts.Log == nil {
		opts.Log = slog.New(slog.DiscardHandler)
	}
	if opts.GOOS == "" {
		opts.GOOS = runtime.GOOS
	}
	if opts.Getenv == nil {
		opts.Getenv = os.Getenv
	}
	if opts.LookPath == nil {
		opts.LookPath = exec.LookPath
	}
	if opts.Deleter == nil {
		opts.Deleter = cleanup.SystemDeleter()
	}
	if opts.VSCodeRunning == nil {
		opts.VSCodeRunning = vscodeRunning
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Removable == nil {
		opts.Removable = fsx.CheckRemovable
	}
	if opts.Trashable == nil {
		opts.Trashable = fsx.CheckTrashable
	}
	if opts.Excluded == nil {
		opts.Excluded = fsx.Excluded
	}
	if opts.Settings == nil {
		opts.Settings = func() settings.CleanupSettings { return settings.CleanupSettings{} }
	}
	del := &scopedDeleter{base: opts.Deleter, roots: map[string]string{}}
	return &Cleaner{
		opts: opts,
		env:  env{goos: opts.GOOS, home: opts.Home, getenv: opts.Getenv},
		del:  del,
		exec: cleanup.NewExecutorWith(del, cleanup.ExecutorOptions{Exclusions: func() []string { return opts.Settings().Exclusions }}),
	}
}

// reporter turns the scan's steps into Progress events, never calling progress concurrently.
type reporter struct {
	mu          sync.Mutex
	fn          func(cleanup.Progress)
	done, total int
	bytes       atomic.Int64
}

func (r *reporter) emit(path string, step bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if step {
		r.done++
	}
	r.fn(cleanup.Progress{Phase: cleanup.PhaseScanning, Path: path, Done: r.done, Total: r.total, Bytes: r.bytes.Load()})
}

// at reports the folder being read.
func (r *reporter) at(path string) { r.emit(path, false) }

// step reports a finished step.
func (r *reporter) step() { r.emit("", true) }

// Scan finds the tool caches and the projects' build output. A tool or a root that cannot be read is left out; only a cancelled ctx fails the scan.
func (c *Cleaner) Scan(ctx context.Context, progress func(cleanup.Progress)) (cleanup.ScanResult, error) {
	started := c.opts.Now()
	cfg := c.opts.Settings()
	if progress == nil {
		progress = func(cleanup.Progress) {}
	}
	devices := filepath.Join(c.opts.Home, "Library", "Developer", "CoreSimulator", "Devices")
	loc := locate(ctx, c.env, c.opts.Runner, c.opts.LookPath, devices)
	loc.vscodeRunning = c.opts.VSCodeRunning(ctx)
	tools := catalog(c.env, loc)
	rep := &reporter{fn: progress, total: len(tools) + len(cfg.DevProjectRoots)}
	rep.at("")

	b := &builder{seen: map[string]bool{}, commands: map[string]*commandJob{}, projects: map[string]string{}}
	for _, t := range tools {
		if err := c.scanTool(ctx, t, cfg.Exclusions, rep, b); err != nil {
			return cleanup.ScanResult{}, err
		}
		rep.step()
	}
	if err := c.scanProjects(ctx, cfg, rep, b); err != nil {
		return cleanup.ScanResult{}, err
	}

	res := cleanup.NewResult(b.cats, started)
	res.Denied = b.denied
	files := cleanup.ScanResult{}
	for _, cat := range res.Categories {
		if b.commands[cat.ID] == nil {
			files.Categories = append(files.Categories, cat)
		}
	}
	if err := c.exec.Remember(files); err != nil {
		return cleanup.ScanResult{}, err
	}
	snap := &snapshot{items: map[string]string{}, commands: b.commands}
	for _, cat := range res.Categories {
		snap.order = append(snap.order, cat.ID)
		for _, it := range cat.Items {
			snap.items[it.ID] = cat.ID
		}
	}
	c.del.set(b.projects)
	c.mu.Lock()
	c.last = snap
	c.mu.Unlock()
	return res, nil
}

// builder collects the categories of a scan.
type builder struct {
	mu       sync.Mutex
	cats     []cleanup.Category
	seen     map[string]bool
	commands map[string]*commandJob
	// projects maps an artifact's path to its project folder, its only allowed root.
	projects map[string]string
	denied   int64
}

// add appends cat with the items whose IDs no earlier category has, each starting checked (Item.Selected) when checked says so; cleanup.NewResult then checks the category as a whole when all of them are. A category left empty is dropped.
func (b *builder) add(cat cleanup.Category, checked map[string]bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	cat.Items = slices.DeleteFunc(cat.Items, func(it cleanup.Item) bool { return b.seen[it.ID] })
	if len(cat.Items) == 0 {
		return
	}
	for i := range cat.Items {
		it := &cat.Items[i]
		b.seen[it.ID] = true
		it.Selected = checked[it.ID]
	}
	cat.Selected = false
	b.cats = append(b.cats, cat)
}

// measured is the size of one path.
type measured struct {
	size   int64
	denied int64
	// newest is the latest modification time inside the path.
	newest time.Time
	// excluded is set when one of the user's exclusions lies inside the path, which must then not be deleted as a whole.
	excluded bool
	err      error
}

// measure sizes path, watching for the user's exclusions inside it.
func (c *Cleaner) measure(ctx context.Context, path string, exclusions []string, rep *reporter) measured {
	var hit atomic.Bool
	opts := fsx.SizeOptions{ProgressEvery: 150 * time.Millisecond, Progress: func(p fsx.SizeProgress) { rep.at(p.Path) }}
	if len(exclusions) > 0 {
		opts.Skip = func(p string) bool {
			if c.opts.Excluded(p, exclusions) {
				hit.Store(true)
				return true
			}
			return false
		}
	}
	r, err := fsx.DirSize(ctx, path, opts)
	return measured{size: r.Bytes, denied: r.Denied, newest: r.Newest, excluded: hit.Load(), err: err}
}

// measureAll sizes every path with measureWorkers at a time.
func (c *Cleaner) measureAll(ctx context.Context, paths []string, exclusions []string, rep *reporter) []measured {
	out := make([]measured, len(paths))
	sem := make(chan struct{}, measureWorkers)
	var wg sync.WaitGroup
	for i, p := range paths {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			out[i] = c.measure(ctx, p, exclusions, rep)
			rep.bytes.Add(out[i].size)
		})
	}
	wg.Wait()
	return out
}

// scanTool adds the category of one tool cache, if it holds anything.
func (c *Cleaner) scanTool(ctx context.Context, t *tool, exclusions []string, rep *reporter, b *builder) error {
	if t.command != nil {
		used, err := c.scanCommandTool(ctx, t, exclusions, rep, b)
		if used || err != nil {
			return err
		}
	}
	if t.commandOnly {
		return ctx.Err()
	}
	list := t.list
	if list == nil {
		list = listDirs
	}
	entries, err := list(t)
	if err != nil {
		c.opts.Log.Debug("list tool cache", "tool", t.id, "err", err)
	}
	var keep []entry
	for _, e := range entries {
		if !c.opts.Excluded(e.path, exclusions) && !c.opts.Excluded(e.root, exclusions) {
			keep = append(keep, e)
		}
	}
	paths := make([]string, len(keep))
	for i, e := range keep {
		paths[i] = e.path
	}
	sizes := c.measureAll(ctx, paths, exclusions, rep)
	if err := ctx.Err(); err != nil {
		return err
	}
	cat := c.category("tool-"+t.id, t.title, t.description, t.icon, t.warning, cleanup.KindDevCache, t.deletion())
	checked := map[string]bool{}
	var roots []string
	for i, e := range keep {
		m := sizes[i]
		b.addDenied(m.denied)
		if m.err != nil || m.excluded || m.size == 0 {
			continue
		}
		it := cleanup.NewItem(e.path, e.info, m.size)
		it.Note = e.note
		if it.Dir {
			it.Newest = m.newest
		}
		cat.Items = append(cat.Items, it)
		checked[it.ID] = e.selected
		if !slices.Contains(roots, e.root) {
			roots = append(roots, e.root)
		}
	}
	cat.Roots = roots
	b.add(cat, checked)
	return nil
}

// scanCommandTool adds the tool as one item cleaned by its command. It reports false, adding nothing, when the tool should be listed by its entries instead (its folder is missing or empty, or an exclusion lies inside it).
func (c *Cleaner) scanCommandTool(ctx context.Context, t *tool, exclusions []string, rep *reporter, b *builder) (bool, error) {
	if len(t.dirs) == 0 {
		return false, nil
	}
	dir := t.dirs[0].path
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || c.opts.Excluded(dir, exclusions) {
		return false, nil
	}
	var paths []string
	note := ""
	if t.simulators {
		udids, err := unavailableSimulators(ctx, c.opts.Runner, t.command.Name)
		if err != nil {
			c.opts.Log.Debug("list simulators", "err", err)
		}
		for _, u := range udids {
			paths = append(paths, filepath.Join(dir, u))
		}
		note = fmt.Sprintf("%d 台不可用的模拟器", len(paths))
	} else {
		paths = []string{dir}
	}
	sizes := c.measureAll(ctx, paths, exclusions, rep)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	var total int64
	var newest time.Time
	for _, m := range sizes {
		b.addDenied(m.denied)
		if m.excluded {
			return false, nil
		}
		if m.err == nil {
			total += m.size
			if m.newest.After(newest) {
				newest = m.newest
			}
		}
	}
	if total == 0 {
		return false, nil
	}
	cat := c.category("tool-"+t.id, t.title, t.commandDescription, t.icon, t.warning, cleanup.KindDevCache, cleanup.DeletePermanent)
	cat.Command = commandLabel(*t.command)
	it := cleanup.NewItem(dir, info, total)
	it.Note = note
	it.Newest = newest
	cat.Items = []cleanup.Item{it}
	job := &commandJob{
		categoryID: cat.ID, itemID: it.ID, title: t.title, path: dir, size: total, cmd: *t.command,
		measure: func(ctx context.Context) int64 {
			var left int64
			for _, p := range paths {
				if r, err := fsx.DirSize(ctx, p, fsx.SizeOptions{}); err == nil {
					left += r.Bytes
				}
			}
			return left
		},
	}
	b.mu.Lock()
	b.commands[cat.ID] = job
	b.mu.Unlock()
	b.add(cat, map[string]bool{it.ID: t.selected})
	return true, nil
}

func (b *builder) addDenied(n int64) {
	if n == 0 {
		return
	}
	b.mu.Lock()
	b.denied += n
	b.mu.Unlock()
}

// category starts a Category.
func (c *Cleaner) category(id, title, description, icon, warning string, kind cleanup.CategoryKind, del cleanup.DeletionMode) cleanup.Category {
	return cleanup.Category{ID: id, Title: title, Description: description, Icon: icon, Warning: warning, Kind: kind, Deletion: del}
}

// binName is what the platform calls its Trash.
func (c *Cleaner) binName() string {
	if c.env.windows() {
		return "回收站"
	}
	return "废纸篓"
}

// scanProjects walks the project roots and adds a category per kind of build output (and one more per kind for output that goes to the Trash).
func (c *Cleaner) scanProjects(ctx context.Context, cfg settings.CleanupSettings, rep *reporter, b *builder) error {
	seen := map[string]bool{}
	var arts []sized
	for _, root := range cfg.DevProjectRoots {
		if !filepath.IsAbs(root) || c.opts.Excluded(root, cfg.Exclusions) {
			rep.step()
			continue
		}
		found, denied, err := walkProjects(ctx, filepath.Clean(root), cfg.Exclusions, c.opts.Excluded, rep.at)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			c.opts.Log.Debug("walk project root", "root", root, "err", err)
		}
		b.addDenied(denied)
		var paths []string
		var fresh []artifact
		for _, a := range found {
			if !seen[a.path] {
				seen[a.path] = true
				fresh = append(fresh, a)
				paths = append(paths, a.path)
			}
		}
		sizes := c.measureAll(ctx, paths, cfg.Exclusions, rep)
		if err := ctx.Err(); err != nil {
			return err
		}
		for i, a := range fresh {
			m := sizes[i]
			b.addDenied(m.denied)
			if m.err == nil && !m.excluded && m.size > 0 {
				arts = append(arts, sized{artifact: a, size: m.size})
			}
		}
		rep.step()
	}

	type key struct {
		kind  string
		trash bool
	}
	cats := map[key]*cleanup.Category{}
	checked := map[string]bool{}
	minAge := time.Duration(cfg.DevArtifactMinAgeDays) * 24 * time.Hour
	now := c.opts.Now()
	for _, a := range arts {
		roots := []string{a.project}
		var trash bool
		switch {
		case c.opts.Removable(a.path, roots) == nil:
		case c.opts.Trashable(a.path, roots) == nil:
			trash = true
		default:
			c.opts.Log.Debug("build output in a protected place", "path", a.path)
			continue
		}
		k := key{a.kind, trash}
		cat := cats[k]
		if cat == nil {
			cat = c.artifactCategory(a.kind, trash)
			cats[k] = cat
		}
		it := cleanup.NewItem(a.path, a.info, a.size)
		if !a.active.IsZero() {
			it.ModTime = a.active
		}
		notes := []string{projectLabel(a.root, a.project)}
		recent := minAge > 0 && !a.active.IsZero() && now.Sub(a.active) < minAge
		if recent {
			notes = append(notes, fmt.Sprintf("最近 %d 天内有改动", cfg.DevArtifactMinAgeDays))
		}
		it.Note = strings.Join(notes, " · ")
		cat.Items = append(cat.Items, it)
		if !slices.Contains(cat.Roots, a.project) {
			cat.Roots = append(cat.Roots, a.project)
		}
		checked[it.ID] = !recent
		b.mu.Lock()
		b.projects[a.path] = a.project
		b.mu.Unlock()
	}
	for _, kind := range artifactKinds {
		for _, trash := range []bool{false, true} {
			if cat := cats[key{kind.id, trash}]; cat != nil {
				b.add(*cat, checked)
			}
		}
	}
	return nil
}

// sized is an artifact with its measured size.
type sized struct {
	artifact
	size int64
}

// artifactCategory starts the category of a kind of build output; trash is the variant whose items go to the Trash.
func (c *Cleaner) artifactCategory(kindID string, trash bool) *cleanup.Category {
	var k artifactKind
	for _, ak := range artifactKinds {
		if ak.id == kindID {
			k = ak
		}
	}
	cat := c.category("artifact-"+k.id, k.title, k.description, k.icon, "", cleanup.KindProjectArtifact, cleanup.DeletePermanent)
	if trash {
		places := "文稿、桌面"
		if c.env.windows() {
			places = "文档、桌面"
		}
		cat.ID += "-trash"
		cat.Title = fmt.Sprintf("%s（移到%s）", k.title, c.binName())
		cat.Description = fmt.Sprintf("这些项目在%s等受保护的文件夹里，构建产物会移到%s，不会直接删除", places, c.binName())
		cat.Deletion = cleanup.DeleteTrash
	}
	return &cat
}

// projectLabel is the project's path relative to its root; the root itself shows as its own name.
func projectLabel(root, project string) string {
	rel, err := filepath.Rel(root, project)
	if err != nil || rel == "." {
		return filepath.Base(project)
	}
	return rel
}

// scopedDeleter narrows the allowed roots of each build output folder to its own project folder; other items keep their category's roots.
type scopedDeleter struct {
	base  cleanup.Deleter
	mu    sync.RWMutex
	roots map[string]string
}

func (d *scopedDeleter) set(roots map[string]string) {
	d.mu.Lock()
	d.roots = roots
	d.mu.Unlock()
}

func (d *scopedDeleter) scope(path string, roots []string) []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if r, ok := d.roots[path]; ok {
		return []string{r}
	}
	return roots
}

func (d *scopedDeleter) Remove(ctx context.Context, path string, roots []string) (fsx.Removal, error) {
	return d.base.Remove(ctx, path, d.scope(path, roots))
}

func (d *scopedDeleter) Trash(ctx context.Context, path string, roots []string) (cleanup.Trashed, error) {
	return d.base.Trash(ctx, path, d.scope(path, roots))
}
