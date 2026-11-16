package uninstall

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/engine/execx"
	"spruce/apps/spruce/internal/engine/fsx"
)

// Errors of the Engine's methods; the services layer turns them into sentences.
var (
	// ErrUnknownApp is returned for an app ID the last Apps call did not list.
	ErrUnknownApp = errors.New("uninstall: the app is not in the last list")
	// ErrStalePlan is returned by Remove for a request that does not match the last Plan.
	ErrStalePlan = errors.New("uninstall: the request does not match the last plan")
	// ErrNothingSelected is returned by Remove for a request that removes nothing.
	ErrNothingSelected = errors.New("uninstall: nothing selected")
	// ErrBusy is returned while another Remove or uninstaller runs.
	ErrBusy = errors.New("uninstall: a removal is already in progress")
	// ErrNotUninstalled is returned by Plan on Windows for a program whose uninstaller has not removed it yet: its folders are still the installed program's.
	ErrNotUninstalled = errors.New("uninstall: the program is still installed")
)

// minNameLen is the shortest app name leftovers may match by: shorter names are too likely to be someone else's folder.
const minNameLen = 3

// SelfBundleID is Spruce's own bundle ID (the id in nsgo.yaml); the list leaves it, and IDs below it, out.
const SelfBundleID = "ai.yldm.spruce"

// Usage is what is known about when an app was last used.
type Usage string

const (
	// UsageUsed means LastUsed is set.
	UsageUsed Usage = "used"
	// UsageNever means Spotlight knows the app but has no record of it being opened.
	UsageNever Usage = "never"
	// UsageUnknown means there is no information (not indexed, or Windows).
	UsageUnknown Usage = "unknown"
)

// App is one installed app.
type App struct {
	// ID is stable for the app's bundle path (macOS) or registry key (Windows).
	ID      string
	Name    string
	Version string
	// BundleID is the CFBundleIdentifier (macOS).
	BundleID string
	// Publisher is the registry's Publisher (Windows).
	Publisher string
	// Path is the .app bundle (macOS) or the install folder when the registry names one that exists (Windows).
	Path string
	// AppStore marks a Mac App Store app.
	AppStore bool
	// Running is set when a process runs from inside Path.
	Running bool
	// InstallDate is the registry's InstallDate (Windows); zero when unknown.
	InstallDate time.Time
	// EstimatedSize is the registry's EstimatedSize in bytes (Windows); 0 when unknown.
	EstimatedSize int64

	// iconFile is the bundle's .icns (macOS) or the DisplayIcon file (Windows).
	iconFile string
	// platformApp holds what only one platform knows of an app (apps_darwin.go, apps_windows.go).
	platformApp
}

// Details is one update of an app's lazily measured details. Only the fields it carries are set: Size when HasSize, Usage (with LastUsed when used) when Usage is not empty, Icon when not empty.
type Details struct {
	ID      string
	HasSize bool
	Size    int64
	Usage   Usage
	// LastUsed is set with UsageUsed.
	LastUsed time.Time
	// Icon is a data: URL of a PNG.
	Icon string
}

// LeftoverKind is where a leftover lies, which the page names.
type LeftoverKind string

const (
	// KindApp is the app bundle itself (macOS).
	KindApp LeftoverKind = "app"
	// KindSupport is ~/Library/Application Support/<id or name>.
	KindSupport LeftoverKind = "support"
	// KindCaches is ~/Library/Caches/<id or name>.
	KindCaches LeftoverKind = "caches"
	// KindPreferences is ~/Library/Preferences/<id>.plist, <id>.*.plist and ByHost/<id>.*.plist.
	KindPreferences LeftoverKind = "preferences"
	// KindContainers is ~/Library/Containers/<id>.
	KindContainers LeftoverKind = "containers"
	// KindGroupContainers is ~/Library/Group Containers/<id> or *.<id>.
	KindGroupContainers LeftoverKind = "groupContainers"
	// KindSavedState is ~/Library/Saved Application State/<id>.savedState.
	KindSavedState LeftoverKind = "savedState"
	// KindHTTPStorage is ~/Library/HTTPStorages/<id>.
	KindHTTPStorage LeftoverKind = "httpStorage"
	// KindWebKit is ~/Library/WebKit/<id>.
	KindWebKit LeftoverKind = "webkit"
	// KindLogs is ~/Library/Logs/<name or id>.
	KindLogs LeftoverKind = "logs"
	// KindCookies is ~/Library/Cookies/<id>.binarycookies.
	KindCookies LeftoverKind = "cookies"
	// KindLaunchAgents is a ~/Library/LaunchAgents plist that starts the app's code at login.
	KindLaunchAgents LeftoverKind = "launchAgents"
	// KindAppData is %APPDATA%\<publisher>\<name> or %APPDATA%\<name> (Windows).
	KindAppData LeftoverKind = "appData"
	// KindLocalAppData is the same under %LOCALAPPDATA% (Windows).
	KindLocalAppData LeftoverKind = "localAppData"
	// KindPrograms is the install folder under %LOCALAPPDATA%\Programs the uninstaller left (Windows).
	KindPrograms LeftoverKind = "programs"
)

// kindOrder is the order the page lists leftovers in.
var kindOrder = []LeftoverKind{
	KindApp, KindSupport, KindCaches, KindPreferences, KindContainers, KindGroupContainers, KindSavedState,
	KindHTTPStorage, KindWebKit, KindLogs, KindCookies, KindLaunchAgents, KindPrograms, KindLocalAppData, KindAppData,
}

// Candidate is a path a leftover rule matched, before it is checked and measured.
type Candidate struct {
	Path string
	Kind LeftoverKind
	// Root is the folder it may be moved to the Trash from.
	Root string
	// Selected is whether it starts checked.
	Selected bool
	// Note is an optional short line in Simplified Chinese.
	Note string
}

// Leftover is one file or folder the page offers with the app.
type Leftover struct {
	ID       string
	Path     string
	Kind     LeftoverKind
	Size     int64
	Dir      bool
	Selected bool
	Note     string
}

// Plan is what removing an app would move to the Trash.
type Plan struct {
	AppID string
	// Bundle is the app itself (macOS); nil on Windows, where the vendor's uninstaller removes the program.
	Bundle *Leftover
	// Leftovers are sorted by kind, then path.
	Leftovers []Leftover
	// Denied counts locations that could not be read for lack of permission, so the list may be incomplete.
	Denied int64
	// Running is set when the app runs now (macOS: it cannot be removed until it quits).
	Running bool
	// SharedWith is the path of another installed copy with the same bundle ID or name, whose data the leftovers may be; they then start unchecked.
	SharedWith string
}

// Request names what Remove moves: the bundle of the planned app (always, on macOS) and the leftovers by ID.
type Request struct {
	AppID       string
	LeftoverIDs []string
}

// Result is what Remove did.
type Result struct {
	// App is the app's name.
	App string
	// BundleRemoved is set when the bundle is in the Trash (or was already gone).
	BundleRemoved bool
	// BundleFailure is the sentence explaining why the bundle stayed; the leftovers are then left alone.
	BundleFailure string
	// Clean covers the bundle and the leftovers.
	Clean cleanup.CleanResult
}

// UninstallerResult is what running a Windows program's uninstaller achieved.
type UninstallerResult struct {
	// App is the program's name.
	App string
	// Removed is set when the program's registry entry is gone.
	Removed bool
	// Waited is set when the uninstaller exited; it is false when the wait was stopped.
	Waited bool
}

// Config configures an Engine.
type Config struct {
	// Home is the user's home folder.
	Home string
	// IconCache is the folder converted icons are cached in (created on first use).
	IconCache string
	// Runner runs mdls and sips (macOS).
	Runner execx.Runner
	// Deleter moves items to the Trash; nil means cleanup.SystemDeleter.
	Deleter cleanup.Deleter
	// Exclusions returns the user's exclusions (settings.Cleanup.Exclusions); nil means none.
	Exclusions func() []string
	// Processes returns the executable paths of the running processes; nil means every process gopsutil can see.
	Processes func(ctx context.Context) ([]string, error)
	// AppDirs are the folders searched for apps (macOS); nil means /Applications and ~/Applications.
	AppDirs []string
	// Self is the running executable; the bundle holding it is left out of the list.
	Self string
}

// Engine lists, plans and removes apps. It is safe for concurrent use.
type Engine struct {
	cfg  Config
	del  *recordingDeleter
	exec *cleanup.Executor

	// busy is held by Remove and RunUninstaller.
	busy sync.Mutex

	mu    sync.Mutex
	apps  map[string]App
	sizes map[string]int64
	plan  *planState
	// uninstalled holds the Windows programs whose uninstaller removed them.
	uninstalled map[string]bool
}

// planState is the last Plan as Remove needs it.
type planState struct {
	app    App
	bundle *Leftover
	// cats maps a leftover ID to its category.
	cats map[string]string
}

// bundleCategory is the category of the app bundle in the executor's scan.
const bundleCategory = "app"

// New returns an Engine.
func New(cfg Config) *Engine {
	if cfg.Deleter == nil {
		cfg.Deleter = cleanup.SystemDeleter()
	}
	if cfg.Processes == nil {
		cfg.Processes = systemProcesses
	}
	del := &recordingDeleter{Deleter: cfg.Deleter, errs: make(map[string]error)}
	return &Engine{
		cfg:         cfg,
		del:         del,
		exec:        cleanup.NewExecutor(del),
		apps:        make(map[string]App),
		sizes:       make(map[string]int64),
		uninstalled: make(map[string]bool),
	}
}

// Apps lists the installed apps, sorted by name, and remembers them for the other methods.
func (e *Engine) Apps(ctx context.Context) ([]App, error) {
	apps, err := e.listApps(ctx)
	if err != nil {
		return nil, err
	}
	excl := e.exclusions()
	apps = slices.DeleteFunc(apps, func(a App) bool { return a.Path != "" && fsx.Excluded(a.Path, excl) })
	if exes, err := e.cfg.Processes(ctx); err == nil {
		for i := range apps {
			apps[i].Running = runningIn(exes, apps[i].Path)
		}
	}
	slices.SortStableFunc(apps, func(a, b App) int {
		return cmp.Or(cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), cmp.Compare(a.ID, b.ID))
	})
	m := make(map[string]App, len(apps))
	for _, a := range apps {
		m[a.ID] = a
	}
	e.mu.Lock()
	// A program its uninstaller removed is no longer listed, but the page may still plan its leftovers.
	for id := range e.uninstalled {
		if _, ok := m[id]; !ok {
			if old, ok := e.apps[id]; ok {
				m[id] = old
			}
		}
	}
	e.apps = m
	e.mu.Unlock()
	return apps, nil
}

// Details measures the apps of the last list and calls emit (never concurrently) with each detail as it is known: when each was last used, its icon and its size. It returns when everything is measured or ctx ends.
func (e *Engine) Details(ctx context.Context, emit func(Details)) error {
	e.mu.Lock()
	apps := make([]App, 0, len(e.apps))
	for _, a := range e.apps {
		apps = append(apps, a)
	}
	e.mu.Unlock()
	slices.SortFunc(apps, func(a, b App) int { return cmp.Compare(a.Name, b.Name) })
	var mu sync.Mutex
	send := func(d Details) {
		if d.HasSize {
			e.mu.Lock()
			e.sizes[d.ID] = d.Size
			e.mu.Unlock()
		}
		mu.Lock()
		defer mu.Unlock()
		if ctx.Err() == nil {
			emit(d)
		}
	}
	return e.details(ctx, apps, send)
}

// Plan finds the leftovers of the app, measures them and the bundle, and remembers the result for Remove.
func (e *Engine) Plan(ctx context.Context, appID string) (Plan, error) {
	e.mu.Lock()
	app, ok := e.apps[appID]
	others := make([]App, 0, len(e.apps))
	for _, a := range e.apps {
		if a.ID != appID {
			others = append(others, a)
		}
	}
	size, sized := e.sizes[appID]
	uninstalled := e.uninstalled[appID]
	e.mu.Unlock()
	if !ok {
		return Plan{}, ErrUnknownApp
	}
	if runtime.GOOS == "windows" && !uninstalled {
		return Plan{}, ErrNotUninstalled
	}

	plan := Plan{AppID: appID, Leftovers: []Leftover{}}
	if exes, err := e.cfg.Processes(ctx); err == nil {
		plan.Running = runningIn(exes, app.Path)
	}
	excl := e.exclusions()
	var cats []cleanup.Category
	catIndex := make(map[string]int)
	addItem := func(c Candidate, info fs.FileInfo, size int64) Leftover {
		item := cleanup.NewItem(c.Path, info, size)
		id := string(c.Kind)
		i, ok := catIndex[id]
		if !ok {
			i = len(cats)
			catIndex[id] = i
			cats = append(cats, cleanup.Category{ID: id, Title: id, Kind: cleanup.KindLeftover, Deletion: cleanup.DeleteTrash})
		}
		cats[i].Items = append(cats[i].Items, item)
		if !slices.Contains(cats[i].Roots, c.Root) {
			cats[i].Roots = append(cats[i].Roots, c.Root)
		}
		return Leftover{ID: item.ID, Path: c.Path, Kind: c.Kind, Size: size, Dir: item.Dir, Selected: c.Selected, Note: c.Note}
	}

	if c, ok := e.bundleCandidate(app); ok {
		info, err := os.Lstat(c.Path)
		if err != nil {
			return Plan{}, fmt.Errorf("uninstall: %w", err)
		}
		// The size Details measured is reused unless the bundle must be walked anyway, to look for the user's exclusions in it.
		if len(excl) > 0 || !sized {
			measured, holds := e.measure(ctx, c.Path, excl, &plan.Denied)
			if holds {
				return Plan{}, cleanup.NewUserError(fmt.Sprintf("「%s」里有你在设置中排除的项目，Spruce 不会移动它", app.Name), nil)
			}
			size = measured
		}
		b := addItem(c, info, size)
		plan.Bundle = &b
	}

	cands, denied := e.leftovers(ctx, app)
	plan.Denied += denied
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	seen := make(map[string]bool)
	for _, c := range cands {
		if seen[c.Path] || fsx.Excluded(c.Path, excl) || (plan.Bundle != nil && c.Path == plan.Bundle.Path) {
			continue
		}
		seen[c.Path] = true
		info, err := os.Lstat(c.Path)
		if err != nil {
			if errors.Is(err, fs.ErrPermission) {
				plan.Denied++
			}
			continue
		}
		size, holds := e.measure(ctx, c.Path, excl, &plan.Denied)
		if holds {
			// Moving the folder would take the excluded item along.
			continue
		}
		plan.Leftovers = append(plan.Leftovers, addItem(c, info, size))
	}
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	if other, ok := sharedWith(app, others); ok {
		plan.SharedWith = other
		for i := range plan.Leftovers {
			plan.Leftovers[i].Selected = false
		}
	}
	slices.SortStableFunc(plan.Leftovers, func(a, b Leftover) int {
		return cmp.Or(cmp.Compare(slices.Index(kindOrder, a.Kind), slices.Index(kindOrder, b.Kind)), cmp.Compare(a.Path, b.Path))
	})

	if err := e.exec.Remember(cleanup.NewResult(cats, time.Now())); err != nil {
		return Plan{}, err
	}
	st := &planState{app: app, bundle: plan.Bundle, cats: make(map[string]string, len(plan.Leftovers))}
	for _, l := range plan.Leftovers {
		st.cats[l.ID] = string(l.Kind)
	}
	e.mu.Lock()
	e.plan = st
	e.mu.Unlock()
	return plan, nil
}

// measure returns the size of path and whether anything inside it is one of the user's exclusions, adding the folders it could not read to denied.
func (e *Engine) measure(ctx context.Context, path string, excl []string, denied *int64) (int64, bool) {
	var holds atomic.Bool
	opts := fsx.SizeOptions{Workers: 4}
	if len(excl) > 0 {
		opts.Skip = func(p string) bool {
			if fsx.Excluded(p, excl) {
				holds.Store(true)
				return true
			}
			return false
		}
	}
	res, err := fsx.DirSize(ctx, path, opts)
	*denied += res.Denied
	if err != nil && errors.Is(err, fs.ErrPermission) {
		*denied++
	}
	return res.Bytes, holds.Load()
}

// Remove moves what req names from the last Plan to the Trash: first the bundle (macOS), and only once it is there the selected leftovers. Progress counts every item of the request. It returns an error, and moves nothing, when the request does not match the last Plan, nothing is selected, or the app is running (a *cleanup.UserError).
func (e *Engine) Remove(ctx context.Context, req Request, progress func(cleanup.Progress)) (Result, error) {
	if !e.busy.TryLock() {
		return Result{}, ErrBusy
	}
	defer e.busy.Unlock()
	if progress == nil {
		progress = func(cleanup.Progress) {}
	}
	e.mu.Lock()
	st := e.plan
	e.mu.Unlock()
	if st == nil || st.app.ID != req.AppID {
		return Result{}, ErrStalePlan
	}
	var catIDs []string
	for _, id := range req.LeftoverIDs {
		cat, ok := st.cats[id]
		if !ok {
			return Result{}, fmt.Errorf("%w: leftover %q", ErrStalePlan, id)
		}
		if !slices.Contains(catIDs, cat) {
			catIDs = append(catIDs, cat)
		}
	}
	if st.bundle == nil && len(req.LeftoverIDs) == 0 {
		return Result{}, ErrNothingSelected
	}
	if st.bundle != nil {
		exes, err := e.cfg.Processes(ctx)
		switch {
		case err != nil:
			return Result{}, cleanup.NewUserError(fmt.Sprintf("无法确认「%s」是否正在运行，请稍后再试", st.app.Name), err)
		case runningIn(exes, st.app.Path):
			return Result{}, cleanup.NewUserError(fmt.Sprintf("请先退出「%s」再卸载", st.app.Name), nil)
		}
	}

	res := Result{App: st.app.Name}
	total := len(req.LeftoverIDs)
	if st.bundle != nil {
		total++
	}
	offset, base := 0, int64(0)
	relay := func(p cleanup.Progress) {
		p.Done += offset
		p.Total = total
		p.Bytes += base
		progress(p)
	}
	started := time.Now()
	if st.bundle != nil {
		r, err := e.exec.Clean(ctx, cleanup.CleanRequest{CategoryIDs: []string{bundleCategory}}, relay)
		if err != nil {
			return Result{}, err
		}
		res.Clean = r
		if r.Removed+r.Skipped == 0 {
			res.BundleFailure = bundleFailure(st.app.Name, e.del.err(st.bundle.Path), r)
			res.Clean.Duration = time.Since(started)
			return res, nil
		}
		res.BundleRemoved = true
		offset, base = 1, r.Trashed
	}
	if len(req.LeftoverIDs) > 0 {
		r, err := e.exec.Clean(ctx, cleanup.CleanRequest{CategoryIDs: catIDs, ItemIDs: req.LeftoverIDs}, relay)
		if err != nil {
			return Result{}, err
		}
		if st.bundle != nil {
			res.Clean = mergeClean(res.Clean, r)
		} else {
			res.Clean = r
		}
	}
	res.Clean.StartedAt = started
	res.Clean.Duration = time.Since(started)
	// What the plan offered is gone or failed; the page plans again before another removal.
	e.mu.Lock()
	if e.plan == st {
		e.plan = nil
	}
	e.mu.Unlock()
	e.exec.Forget()
	return res, nil
}

// RunUninstaller runs the uninstaller of a Windows program, calls started once it runs, waits for it to exit (until ctx ends, which stops the wait but never the uninstaller) and reads the registry again. On macOS it returns errors.ErrUnsupported.
func (e *Engine) RunUninstaller(ctx context.Context, appID string, started func()) (UninstallerResult, error) {
	if !e.busy.TryLock() {
		return UninstallerResult{}, ErrBusy
	}
	defer e.busy.Unlock()
	app, err := e.app(appID)
	if err != nil {
		return UninstallerResult{}, err
	}
	if started == nil {
		started = func() {}
	}
	res, err := e.runUninstaller(ctx, app, started)
	res.App = app.Name
	if err != nil {
		return res, err
	}
	if res.Removed {
		e.markUninstalled(appID)
	}
	return res, nil
}

// CheckRemoved reads the registry again for a Windows program, for an uninstaller that went on after the process Spruce started exited. On macOS it returns errors.ErrUnsupported.
func (e *Engine) CheckRemoved(appID string) (UninstallerResult, error) {
	app, err := e.app(appID)
	if err != nil {
		return UninstallerResult{}, err
	}
	removed, err := e.removedNow(app)
	if err != nil {
		return UninstallerResult{}, err
	}
	if removed {
		e.markUninstalled(appID)
	}
	return UninstallerResult{App: app.Name, Removed: removed, Waited: true}, nil
}

func (e *Engine) app(id string) (App, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	app, ok := e.apps[id]
	if !ok {
		return App{}, ErrUnknownApp
	}
	return app, nil
}

func (e *Engine) markUninstalled(id string) {
	e.mu.Lock()
	e.uninstalled[id] = true
	e.mu.Unlock()
}

func (e *Engine) exclusions() []string {
	if e.cfg.Exclusions == nil {
		return nil
	}
	return e.cfg.Exclusions()
}

// sharedWith returns the path of another app with the same bundle ID or name as app.
func sharedWith(app App, others []App) (string, bool) {
	for _, o := range others {
		if (app.BundleID != "" && o.BundleID == app.BundleID) || strings.EqualFold(o.Name, app.Name) {
			return cmp.Or(o.Path, o.Name), true
		}
	}
	return "", false
}

// mergeClean adds the outcome of the leftovers' clean b to the bundle's a.
func mergeClean(a, b cleanup.CleanResult) cleanup.CleanResult {
	a.Freed += b.Freed
	a.Trashed += b.Trashed
	a.Removed += b.Removed
	a.Skipped += b.Skipped
	a.Failed = append(a.Failed, b.Failed...)
	a.Categories = append(a.Categories, b.Categories...)
	a.Partial = a.Partial || b.Partial
	a.Interrupted = a.Interrupted || b.Interrupted
	return a
}

// trashName is what the system calls its Trash.
func trashName() string {
	if runtime.GOOS == "windows" {
		return "回收站"
	}
	return "废纸篓"
}

// bundleFailure is the sentence the page shows when the bundle could not be moved: an app installed by an administrator (most App Store apps and those from .pkg installers) needs one, which this page does not ask for, so it sends the user to Finder, which does.
func bundleFailure(name string, err error, r cleanup.CleanResult) string {
	bin := trashName()
	if errors.Is(err, fs.ErrPermission) {
		return fmt.Sprintf("需要管理员权限才能把「%s」移到%s。请在访达中把它拖到%s，系统会请你输入管理员密码。", name, bin, bin)
	}
	reason := "移动失败"
	if len(r.Failed) > 0 {
		reason = r.Failed[0].Reason
	}
	if r.Interrupted && len(r.Failed) == 0 {
		return fmt.Sprintf("卸载在开始前停止了，「%s」没有改动。", name)
	}
	return fmt.Sprintf("没能把「%s」移到%s（%s）。可以在访达中把它拖到%s。", name, bin, reason, bin)
}

// recordingDeleter remembers the last Trash error of each path, so Remove can tell a permission problem from others.
type recordingDeleter struct {
	cleanup.Deleter
	mu   sync.Mutex
	errs map[string]error
}

func (d *recordingDeleter) Trash(ctx context.Context, path string, roots []string) (cleanup.Trashed, error) {
	to, err := d.Deleter.Trash(ctx, path, roots)
	d.mu.Lock()
	d.errs[path] = err
	d.mu.Unlock()
	return to, err
}

func (d *recordingDeleter) err(path string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.errs[path]
}

// runningIn reports whether one of exes lies inside dir.
func runningIn(exes []string, dir string) bool {
	if dir == "" {
		return false
	}
	for _, exe := range exes {
		if inside(exe, dir) {
			return true
		}
	}
	return false
}

// inside reports whether p is strictly inside dir, by whole components (case-insensitively on Windows).
func inside(p, dir string) bool {
	p, dir = filepath.Clean(p), filepath.Clean(dir)
	if runtime.GOOS == "windows" {
		p, dir = strings.ToLower(p), strings.ToLower(dir)
	}
	return strings.HasPrefix(p, dir+string(filepath.Separator))
}
