package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/uninstall"
)

// UninstallEngine is what the Uninstall service needs of the engine; *uninstall.Engine implements it, tests pass a fake.
type UninstallEngine interface {
	Apps(ctx context.Context) ([]uninstall.App, error)
	Details(ctx context.Context, emit func(uninstall.Details)) error
	Plan(ctx context.Context, appID string) (uninstall.Plan, error)
	Remove(ctx context.Context, req uninstall.Request, progress func(cleanup.Progress)) (uninstall.Result, error)
	RunUninstaller(ctx context.Context, appID string, started func()) (uninstall.UninstallerResult, error)
	CheckRemoved(appID string) (uninstall.UninstallerResult, error)
}

var _ UninstallEngine = (*uninstall.Engine)(nil)

// UninstallUsage is what is known about when an app was last used.
type UninstallUsage string

const (
	// UninstallUsed means lastUsed is set.
	UninstallUsed UninstallUsage = "used"
	// UninstallNeverUsed means Spotlight knows the app but has no record of it being opened.
	UninstallNeverUsed UninstallUsage = "never"
	// UninstallUsageUnknown means there is no information.
	UninstallUsageUnknown UninstallUsage = "unknown"
)

// UninstallApp is one app of the 应用卸载 list.
type UninstallApp struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	// BundleID is the app's bundle identifier (macOS).
	BundleID string `json:"bundleId,omitempty"`
	// Publisher is the program's publisher (Windows).
	Publisher string `json:"publisher,omitempty"`
	// Path is the .app bundle (macOS) or the install folder, when known (Windows).
	Path string `json:"path,omitempty"`
	// AppStore marks a Mac App Store app.
	AppStore bool `json:"appStore"`
	// Running is set when the app runs now; on macOS it cannot be uninstalled until it quits.
	Running bool `json:"running"`
	// InstallDate is when the program was installed (Windows), when it says.
	InstallDate time.Time `json:"installDate,omitzero"`
}

// UninstallAppDetails is one update of an app's details, streamed by Measure. Only the fields it carries are set.
type UninstallAppDetails struct {
	ID string `json:"id"`
	// Size is the space the app takes, in bytes.
	Size *int64 `json:"size,omitempty"`
	// Usage, with LastUsed when used.
	Usage    UninstallUsage `json:"usage,omitempty"`
	LastUsed time.Time      `json:"lastUsed,omitzero"`
	// Icon is a data: URL of a PNG.
	Icon string `json:"icon,omitempty"`
}

// UninstallLeftoverKind is where a leftover lies.
type UninstallLeftoverKind string

const (
	UninstallKindApp             UninstallLeftoverKind = "app"
	UninstallKindSupport         UninstallLeftoverKind = "support"
	UninstallKindCaches          UninstallLeftoverKind = "caches"
	UninstallKindPreferences     UninstallLeftoverKind = "preferences"
	UninstallKindContainers      UninstallLeftoverKind = "containers"
	UninstallKindGroupContainers UninstallLeftoverKind = "groupContainers"
	UninstallKindSavedState      UninstallLeftoverKind = "savedState"
	UninstallKindHTTPStorage     UninstallLeftoverKind = "httpStorage"
	UninstallKindWebKit          UninstallLeftoverKind = "webkit"
	UninstallKindLogs            UninstallLeftoverKind = "logs"
	UninstallKindCookies         UninstallLeftoverKind = "cookies"
	UninstallKindLaunchAgents    UninstallLeftoverKind = "launchAgents"
	UninstallKindAppData         UninstallLeftoverKind = "appData"
	UninstallKindLocalAppData    UninstallLeftoverKind = "localAppData"
	UninstallKindPrograms        UninstallLeftoverKind = "programs"
)

// UninstallLeftover is one file or folder offered with an app.
type UninstallLeftover struct {
	ID   string                `json:"id"`
	Path string                `json:"path"`
	Kind UninstallLeftoverKind `json:"kind"`
	Size int64                 `json:"size"`
	Dir  bool                  `json:"dir"`
	// Selected is whether it starts checked.
	Selected bool `json:"selected"`
	// Note is an optional short line in Simplified Chinese.
	Note string `json:"note,omitempty"`
}

// UninstallPlan is what uninstalling an app would move to the Trash.
type UninstallPlan struct {
	AppID string `json:"appId"`
	// Bundle is the app itself (macOS); null on Windows.
	Bundle    *UninstallLeftover  `json:"bundle"`
	Leftovers []UninstallLeftover `json:"leftovers"`
	// Denied counts locations that could not be read, so the list may be incomplete.
	Denied  int64 `json:"denied"`
	Running bool  `json:"running"`
	// SharedWith is another installed copy of the app whose files the leftovers may be; they then start unchecked.
	SharedWith string `json:"sharedWith,omitempty"`
}

// UninstallRequest names what Remove moves to the Trash: the planned app's bundle (always, on macOS) and the leftovers by ID.
type UninstallRequest struct {
	AppID       string   `json:"appId"`
	LeftoverIDs []string `json:"leftoverIds"`
}

// UninstallOutcome is what Remove did.
type UninstallOutcome struct {
	App string `json:"app"`
	// BundleRemoved is set when the app is in the Trash (macOS).
	BundleRemoved bool `json:"bundleRemoved"`
	// BundleFailure explains why the app stayed; its leftovers are then left alone too.
	BundleFailure string              `json:"bundleFailure,omitempty"`
	Result        cleanup.CleanResult `json:"result"`
}

// UninstallerOutcome is what running a Windows program's uninstaller achieved.
type UninstallerOutcome struct {
	// Removed is set when the program is gone from the installed programs.
	Removed bool `json:"removed"`
	// Waited is false when the page stopped waiting before the uninstaller exited.
	Waited bool `json:"waited"`
}

// UninstallerStatus is sent once the uninstaller runs.
type UninstallerStatus struct {
	Running bool `json:"running"`
}

// Uninstall is the controller of the 应用卸载 page: list → pick → leftovers → remove. It records each removal in the history.
type Uninstall struct {
	eng     UninstallEngine
	history history.Store
	home    string
	log     *slog.Logger
}

// NewUninstall returns the service over eng, recording removals in store; home shortens paths in the history.
func NewUninstall(eng UninstallEngine, store history.Store, home string, log *slog.Logger) *Uninstall {
	return &Uninstall{eng: eng, history: store, home: home, log: log}
}

// Apps lists the installed apps, sorted by name, without their sizes, icons and last use (Measure streams those).
func (u *Uninstall) Apps(ctx context.Context) ([]UninstallApp, error) {
	apps, err := u.eng.Apps(ctx)
	if err != nil {
		return nil, u.userError(ctx, "list apps", err, "无法读取已安装的应用，详情已记录到日志")
	}
	out := make([]UninstallApp, len(apps))
	for i, a := range apps {
		out[i] = UninstallApp{
			ID: a.ID, Name: a.Name, Version: a.Version, BundleID: a.BundleID, Publisher: a.Publisher,
			Path: a.Path, AppStore: a.AppStore, Running: a.Running, InstallDate: a.InstallDate,
		}
	}
	return out, nil
}

// Measure reports the size, icon and last use of every app of the last Apps call to details (nil reports nothing) as each becomes known, one call per app, and returns when all are measured. Cancelling ctx stops it.
func (u *Uninstall) Measure(ctx context.Context, details func(UninstallAppDetails)) error {
	err := u.eng.Details(ctx, func(d uninstall.Details) {
		if details != nil {
			details(detailsView(d))
		}
	})
	if err != nil {
		return u.userError(ctx, "measure apps", err, "无法读取应用的大小，详情已记录到日志")
	}
	return nil
}

// Leftovers finds what the app left in the user's folders and measures it with the app; the result is what Remove picks from. On Windows it answers only after the program's uninstaller removed it.
func (u *Uninstall) Leftovers(ctx context.Context, appID string) (UninstallPlan, error) {
	p, err := u.eng.Plan(ctx, appID)
	if err != nil {
		return UninstallPlan{}, u.userError(ctx, "plan", err, "没能查找相关文件，详情已记录到日志")
	}
	out := UninstallPlan{AppID: p.AppID, Leftovers: make([]UninstallLeftover, len(p.Leftovers)), Denied: p.Denied, Running: p.Running, SharedWith: p.SharedWith}
	if p.Bundle != nil {
		b := leftoverView(*p.Bundle)
		out.Bundle = &b
	}
	for i, l := range p.Leftovers {
		out.Leftovers[i] = leftoverView(l)
	}
	return out, nil
}

func detailsView(d uninstall.Details) UninstallAppDetails {
	out := UninstallAppDetails{ID: d.ID, Usage: UninstallUsage(d.Usage), LastUsed: d.LastUsed, Icon: d.Icon}
	if d.HasSize {
		size := d.Size
		out.Size = &size
	}
	return out
}

func leftoverView(l uninstall.Leftover) UninstallLeftover {
	return UninstallLeftover{ID: l.ID, Path: l.Path, Kind: UninstallLeftoverKind(l.Kind), Size: l.Size, Dir: l.Dir, Selected: l.Selected, Note: l.Note}
}

// Remove moves the planned app (macOS) and the chosen leftovers to the Trash, reports progress (throttled; nil reports nothing) and records the removal in the history. The app goes first; when it cannot be moved its leftovers stay too and BundleFailure says why. Cancelling ctx stops after the current item.
func (u *Uninstall) Remove(ctx context.Context, req UninstallRequest, progress func(cleanup.Progress)) (UninstallOutcome, error) {
	res, err := u.eng.Remove(ctx, uninstall.Request{AppID: req.AppID, LeftoverIDs: req.LeftoverIDs}, throttled(progress))
	if err != nil {
		return UninstallOutcome{}, u.userError(ctx, "remove", err, "卸载没能开始，详情已记录到日志")
	}
	out := UninstallOutcome{App: res.App, BundleRemoved: res.BundleRemoved, BundleFailure: res.BundleFailure, Result: res.Clean}
	if out.Result.Failed == nil {
		out.Result.Failed = []cleanup.Failure{}
	}
	if out.Result.Categories == nil {
		out.Result.Categories = []cleanup.CategoryOutcome{}
	}
	if res.BundleFailure != "" {
		u.log.Info("app left in place", "app", res.App, "reason", res.BundleFailure)
	}
	for _, f := range res.Clean.Failed {
		u.log.Info("uninstall left an item", "path", f.Path, "reason", f.Reason, "detail", f.Detail)
	}
	if res.Clean.Removed+len(res.Clean.Failed) > 0 || res.BundleFailure != "" {
		u.record(history.Run{
			ID:        newRunID(res.Clean.StartedAt),
			Kind:      history.KindUninstall,
			StartedAt: res.Clean.StartedAt,
			EndedAt:   res.Clean.StartedAt.Add(res.Clean.Duration),
			Summary:   &history.Totals{Freed: res.Clean.Freed, Trashed: res.Clean.Trashed, Items: res.Clean.Removed, Failed: len(res.Clean.Failed)},
			Details:   u.removeDetails(res),
		})
	}
	return out, nil
}

// RunUninstaller runs a Windows program's own uninstaller, so its UI and the UAC prompt appear, reports one status once it runs (nil reports nothing), waits for it to exit and checks whether the program is gone; cancelling ctx stops the wait (never the uninstaller). The run is recorded in the history.
func (u *Uninstall) RunUninstaller(ctx context.Context, appID string, status func(UninstallerStatus)) (UninstallerOutcome, error) {
	started := time.Now()
	res, err := u.eng.RunUninstaller(ctx, appID, func() {
		if status != nil {
			status(UninstallerStatus{Running: true})
		}
	})
	if err != nil {
		return UninstallerOutcome{}, u.userError(ctx, "run uninstaller", err, "没能运行卸载程序，详情已记录到日志")
	}
	name := res.App
	line := fmt.Sprintf("已运行「%s」的卸载程序，它已从已安装的程序中移除", name)
	switch {
	case !res.Waited && !res.Removed:
		line = fmt.Sprintf("已打开「%s」的卸载程序，没有等它结束", name)
	case !res.Removed:
		line = fmt.Sprintf("已运行「%s」的卸载程序，但它仍在已安装的程序中", name)
	}
	items := 0
	if res.Removed {
		items = 1
	}
	u.record(history.Run{
		ID: newRunID(started), Kind: history.KindUninstall, StartedAt: started, EndedAt: time.Now(),
		Summary: &history.Totals{Items: items}, Details: []string{line},
	})
	return UninstallerOutcome{Removed: res.Removed, Waited: res.Waited}, nil
}

// CheckRemoved checks again whether a Windows program is gone, for an uninstaller that went on after the process Spruce started exited.
func (u *Uninstall) CheckRemoved(ctx context.Context, appID string) (UninstallerOutcome, error) {
	res, err := u.eng.CheckRemoved(appID)
	if err != nil {
		return UninstallerOutcome{}, u.userError(ctx, "check removed", err, "没能读取已安装的程序，详情已记录到日志")
	}
	return UninstallerOutcome{Removed: res.Removed, Waited: res.Waited}, nil
}

func (u *Uninstall) record(run history.Run) {
	if err := u.history.Append(run); err != nil {
		// The removal already happened; failing the call would tell the user it did not.
		u.log.Error("record uninstall in history", "err", err)
	}
}

// removeDetails are the history lines of a removal: the app, the leftovers, then the items left in place.
func (u *Uninstall) removeDetails(res uninstall.Result) []string {
	bin := binName()
	var lines []string
	switch {
	case res.BundleRemoved:
		lines = append(lines, fmt.Sprintf("已将「%s」移到%s", res.App, bin))
	case res.BundleFailure != "":
		lines = append(lines, res.BundleFailure)
	}
	moved, failed := 0, 0
	for _, c := range res.Clean.Categories {
		if c.CategoryID == string(uninstall.KindApp) {
			continue
		}
		moved += c.Removed
		failed += c.Failed
	}
	if moved > 0 {
		lines = append(lines, fmt.Sprintf("「%s」的相关文件：已移到%s %d 项", res.App, bin, moved))
	}
	if failed > 0 {
		lines = append(lines, fmt.Sprintf("「%s」的相关文件：%d 项未能处理", res.App, failed))
	}
	for i, f := range res.Clean.Failed {
		if i == maxDetailFailures {
			lines = append(lines, fmt.Sprintf("另有 %d 项未能处理", len(res.Clean.Failed)-maxDetailFailures))
			break
		}
		lines = append(lines, shortPath(u.home, f.Path)+"："+f.Reason)
	}
	if res.Clean.Interrupted {
		lines = append(lines, "卸载在中途停止，其余项目没有改动")
	}
	return lines
}

// userError turns an engine error into the sentence the page shows; the cause goes to the log.
func (u *Uninstall) userError(ctx context.Context, op string, err error, fallback string) error {
	if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		return errors.New("已取消")
	}
	u.log.Warn("uninstall failed", "op", op, "err", err)
	var ue *cleanup.UserError
	switch {
	case errors.As(err, &ue):
		return errors.New(ue.Message)
	case errors.Is(err, uninstall.ErrBusy):
		return errors.New("已有卸载正在进行，请等它结束后再试")
	case errors.Is(err, uninstall.ErrUnknownApp):
		return errors.New("应用列表已更新，请刷新后重新选择")
	case errors.Is(err, uninstall.ErrStalePlan), errors.Is(err, cleanup.ErrStale), errors.Is(err, cleanup.ErrNoScan):
		return errors.New("相关文件已过期，请重新选择应用")
	case errors.Is(err, uninstall.ErrNothingSelected), errors.Is(err, cleanup.ErrNothingSelected):
		return errors.New("没有选择任何项目")
	case errors.Is(err, uninstall.ErrNotUninstalled):
		return errors.New("请先运行它的卸载程序，卸载完成后再清理相关文件")
	case errors.Is(err, errors.ErrUnsupported):
		if runtime.GOOS == "darwin" {
			return errors.New("macOS 上的应用直接移到废纸篓即可，不需要卸载程序")
		}
	case errors.Is(err, fs.ErrNotExist):
		return errors.New("这个应用已经不存在了，请刷新列表")
	}
	return errors.New(fallback)
}

// shortPath replaces home at the start of p with "~".
func shortPath(home, p string) string {
	if home == "" {
		return p
	}
	if rel, err := filepath.Rel(home, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return filepath.Join("~", rel)
	}
	return p
}
