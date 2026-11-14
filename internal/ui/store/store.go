// Package store holds the app-wide state the pages share, as signals in the app's own scope, which lives until the app quits: the settings, the history's generation, the CPU alerts, the Full Disk Access state, the Trash's and the disks' generations, 概览's state, the toast, and whether an overlay blocks the shell. The shell makes it once (New) and provides it with Key; pages read it with Use. Every method runs on the engine goroutine; the work behind it goes to goroutines (package async).
package store

import (
	"context"
	"slices"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/health"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/maintenance"
	"github.com/yldm-tech/spruce/internal/engine/settings"
	"github.com/yldm-tech/spruce/internal/ui/async"
)

// SettingsService is what Settings reads and writes through: *app.Settings.
type SettingsService interface {
	Get() settings.AppSettings
	Update(next settings.AppSettings) (settings.AppSettings, error)
	Reset() (settings.AppSettings, error)
	Limits() app.SettingsLimits
	About() (app.AppAbout, error)
}

// SettingsFeed announces every change of the stored settings, from any source: *settings.Store.
type SettingsFeed interface {
	Subscribe() (<-chan settings.AppSettings, func())
}

// HistoryService is the history: *app.History.
type HistoryService interface {
	List(limit int) ([]history.Run, error)
	Clear() error
	Subscribe() (<-chan history.Run, func(), bool)
}

// AlertService is the CPU monitor: *app.Health.
type AlertService interface {
	Alerts() []health.Alert
	Subscribe() (<-chan []health.Alert, func())
	OpenSystemMonitor(ctx context.Context) error
}

// PermissionService checks and guides Full Disk Access: app.Permissions.
type PermissionService interface {
	FullDiskAccess() (maintenance.PermissionState, error)
	GuideFullDiskAccess(ctx context.Context) (maintenance.PermissionState, error)
}

// LocationService lists the disks and folders of 空间透视's start screen: *app.Space.
type LocationService interface {
	Locations(ctx context.Context) app.SpaceLocations
}

// Deps are the services the stores read.
type Deps struct {
	Settings     SettingsService
	SettingsFeed SettingsFeed
	History      HistoryService
	Alerts       AlertService
	Permissions  PermissionService
	Locations    LocationService
	// Overview is 概览's controller; nil until it is wired (see Overview).
	Overview OverviewService
}

// Stores are the app-wide stores.
type Stores struct {
	Settings   *Settings
	History    *History
	Alerts     *Alerts
	Permission *Permission
	Trash      *Trash
	Locations  *Locations
	Overview   *Overview
	Toasts     *Toasts
	Block      *Block
}

// Key provides the stores to the pages.
var Key = core.NewKey[*Stores]("spruce.stores", nil)

// Use returns the stores the shell provides.
func Use(cx *core.Scope) *Stores { return Key.Use(cx) }

// New makes the stores in cx, the app's own scope, and starts following the settings, the history and the alerts until ctx ends (the app quits).
func New(cx *core.Scope, ctx context.Context, d Deps) *Stores {
	s := &Stores{
		Settings:   newSettings(cx, ctx, d.Settings, d.SettingsFeed),
		History:    newHistory(cx, ctx, d.History),
		Alerts:     newAlerts(cx, ctx, d.Alerts),
		Permission: newPermission(cx, ctx, d.Permissions),
		Trash:      &Trash{gen: core.NewSignal(cx, 0)},
		Locations:  newLocations(cx, ctx, d.Locations),
		Overview:   newOverview(cx, ctx, d.Overview),
		Toasts:     newToasts(cx),
		Block:      &Block{holders: core.NewSignal(cx, 0)},
	}
	return s
}

// Settings mirrors the stored settings, with LaunchAtLogin read from the system, and saves through the service.
type Settings struct {
	cx     *core.Scope
	ctx    context.Context
	svc    SettingsService
	value  *core.Signal[settings.AppSettings]
	loaded *core.Signal[bool]
	gen    *core.Signal[int]
	seq    int
	limits app.SettingsLimits
	about  app.AppAbout
}

func newSettings(cx *core.Scope, ctx context.Context, svc SettingsService, feed SettingsFeed) *Settings {
	s := &Settings{cx: cx, ctx: ctx, svc: svc, value: core.NewSignalFunc(cx, settings.AppSettings{}, nil),
		loaded: core.NewSignal(cx, false), gen: core.NewSignal(cx, 0), limits: svc.Limits()}
	s.about, _ = svc.About()
	ch, unsubscribe := feed.Subscribe()
	go func() {
		defer unsubscribe()
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-ch:
				if !ok {
					return
				}
				// Get adds the login item's state, which the system keeps.
				v := svc.Get()
				cx.Post(func() {
					s.value.Set(v)
					s.loaded.Set(true)
				})
			}
		}
	}()
	return s
}

// Value returns the settings; reading it subscribes.
func (s *Settings) Value() settings.AppSettings { return s.value.Get() }

// Loaded reports whether the settings have been read; until then Value is the zero AppSettings.
func (s *Settings) Loaded() bool { return s.loaded.Get() }

// Limits returns the ranges Save clamps to.
func (s *Settings) Limits() app.SettingsLimits { return s.limits }

// About returns the app's version and data folder.
func (s *Settings) About() app.AppAbout { return s.about }

// Generation counts the resets, so controls drop their drafts.
func (s *Settings) Generation() int { return s.gen.Get() }

// Save shows next at once, stores it on a goroutine and then shows what was stored (clamped), and calls done with it. Only the latest Save's answer replaces Value; done runs for every Save. On an error Value is what the service still holds and err is a sentence to show.
func (s *Settings) Save(next settings.AppSettings, done func(saved settings.AppSettings, err error)) {
	s.seq++
	seq := s.seq
	s.value.Set(next)
	async.Do(s.cx, s.ctx, func(context.Context) (settings.AppSettings, error) { return s.svc.Update(next) },
		func(saved settings.AppSettings, err error) {
			if seq == s.seq {
				s.value.Set(saved)
			}
			if done != nil {
				done(saved, err)
			}
		})
}

// Reset restores every default, then counts a generation and calls done.
func (s *Settings) Reset(done func(error)) {
	s.seq++
	seq := s.seq
	async.Do(s.cx, s.ctx, func(context.Context) (settings.AppSettings, error) { return s.svc.Reset() },
		func(saved settings.AppSettings, err error) {
			if seq == s.seq {
				s.value.Set(saved)
			}
			s.gen.Update(func(g int) int { return g + 1 })
			if done != nil {
				done(err)
			}
		})
}

// History counts the runs the history records, so the pages that show it load it again.
type History struct {
	cx  *core.Scope
	ctx context.Context
	svc HistoryService
	gen *core.Signal[int]
}

func newHistory(cx *core.Scope, ctx context.Context, svc HistoryService) *History {
	h := &History{cx: cx, ctx: ctx, svc: svc, gen: core.NewSignal(cx, 0)}
	if runs, unsubscribe, ok := svc.Subscribe(); ok {
		async.Subscribe(cx, ctx, runs, unsubscribe, func(history.Run) { h.Bump() })
	}
	return h
}

// Generation changes on every recorded run and on Clear; reading it subscribes.
func (h *History) Generation() int { return h.gen.Get() }

// Bump counts a change made outside the history's own announcements.
func (h *History) Bump() { h.gen.Update(func(g int) int { return g + 1 }) }

// Clear deletes every run, then counts a generation and calls done with the error to show.
func (h *History) Clear(done func(error)) {
	async.Do(h.cx, h.ctx, func(context.Context) (struct{}, error) { return struct{}{}, h.svc.Clear() },
		func(_ struct{}, err error) {
			h.Bump()
			if done != nil {
				done(err)
			}
		})
}

// Load lists up to limit runs on a goroutine and calls done in cx, the caller's scope, with ctx the caller's.
func (h *History) Load(cx *core.Scope, ctx context.Context, limit int, done func([]history.Run, error)) {
	async.Do(cx, ctx, func(context.Context) ([]history.Run, error) { return h.svc.List(limit) }, done)
}

// Alerts holds the current CPU alerts, most CPU first.
type Alerts struct {
	cx   *core.Scope
	ctx  context.Context
	svc  AlertService
	list *core.Signal[[]health.Alert]
}

func newAlerts(cx *core.Scope, ctx context.Context, svc AlertService) *Alerts {
	a := &Alerts{cx: cx, ctx: ctx, svc: svc, list: core.NewSignalFunc[[]health.Alert](cx, nil, nil)}
	// The subscription starts first, and a broadcast always wins over the first answer, which may be older.
	broadcast := false
	ch, unsubscribe := svc.Subscribe()
	async.Subscribe(cx, ctx, ch, unsubscribe, func(list []health.Alert) {
		broadcast = true
		a.list.Set(slices.Clone(list))
	})
	async.Do(cx, ctx, func(context.Context) ([]health.Alert, error) { return svc.Alerts(), nil }, func(list []health.Alert, _ error) {
		if !broadcast {
			a.list.Set(list)
		}
	})
	return a
}

// List returns the current alerts; reading it subscribes.
func (a *Alerts) List() []health.Alert { return a.list.Get() }

// OpenSystemMonitor opens Activity Monitor or Task Manager and calls done with the error, if any.
func (a *Alerts) OpenSystemMonitor(done func(error)) {
	async.Do(a.cx, a.ctx, func(ctx context.Context) (struct{}, error) { return struct{}{}, a.svc.OpenSystemMonitor(ctx) },
		func(_ struct{}, err error) {
			if done != nil {
				done(err)
			}
		})
}

// Permission holds whether the app has Full Disk Access, shared by 维护, 智能扫描, 空间透视 and 设置. It changes outside the app (System Settings), so every page that shows it calls Recheck as it opens.
type Permission struct {
	cx      *core.Scope
	ctx     context.Context
	svc     PermissionService
	state   *core.Signal[maintenance.PermissionState]
	checked *core.Signal[bool]
	guiding *core.Signal[bool]
}

func newPermission(cx *core.Scope, ctx context.Context, svc PermissionService) *Permission {
	p := &Permission{cx: cx, ctx: ctx, svc: svc, state: core.NewSignal(cx, maintenance.PermissionUnknown), checked: core.NewSignal(cx, false),
		guiding: core.NewSignal(cx, false)}
	p.Recheck()
	return p
}

// FullDiskAccess returns the last known state, PermissionUnknown until the first check answers (Checked); reading it subscribes.
func (p *Permission) FullDiskAccess() maintenance.PermissionState { return p.state.Get() }

// Checked reports whether a check or the guide has answered yet; until then the pages say 正在检查… and offer no guide. Reading it subscribes.
func (p *Permission) Checked() bool { return p.checked.Get() }

// Guiding reports whether the guide waits in System Settings.
func (p *Permission) Guiding() bool { return p.guiding.Get() }

// Recheck reads the state again.
func (p *Permission) Recheck() {
	async.Do(p.cx, p.ctx, func(context.Context) (maintenance.PermissionState, error) { return p.svc.FullDiskAccess() },
		func(st maintenance.PermissionState, err error) {
			if err == nil {
				p.state.Set(st)
			}
			p.checked.Set(true)
		})
}

// Guide opens System Settings at Full Disk Access and waits there; done receives the state it ended with and the error that kept System Settings from opening. A second call while one waits does nothing.
func (p *Permission) Guide(done func(maintenance.PermissionState, error)) {
	if p.guiding.GetUntracked() {
		return
	}
	p.guiding.Set(true)
	async.Do(p.cx, p.ctx, p.svc.GuideFullDiskAccess, func(st maintenance.PermissionState, err error) {
		p.guiding.Set(false)
		p.state.Set(st)
		p.checked.Set(true)
		if done != nil {
			done(st, err)
		}
	})
}

// Trash counts the changes to the Trash: every clean, uninstall and move to the Trash bumps it, so what shows the Trash's size measures again.
type Trash struct {
	gen *core.Signal[int]
}

// Generation changes on every Bump; reading it subscribes.
func (t *Trash) Generation() int { return t.gen.Get() }

// Bump counts a change.
func (t *Trash) Bump() { t.gen.Update(func(g int) int { return g + 1 }) }

// Locations holds the disks and folders of 空间透视's start screen, read once on first use and again after Bump (智能扫描's 完成 changes the free space).
type Locations struct {
	cx      *core.Scope
	ctx     context.Context
	svc     LocationService
	value   *core.Signal[*app.SpaceLocations]
	loading bool
	seq     int
}

func newLocations(cx *core.Scope, ctx context.Context, svc LocationService) *Locations {
	return &Locations{cx: cx, ctx: ctx, svc: svc, value: core.NewSignalFunc[*app.SpaceLocations](cx, nil, nil)}
}

// Get returns the locations and whether they have been read; the first call starts reading them. Reading it subscribes.
func (l *Locations) Get() (app.SpaceLocations, bool) {
	v := l.value.Get()
	if v == nil {
		l.load()
		return app.SpaceLocations{}, false
	}
	return *v, true
}

func (l *Locations) load() {
	if l.loading {
		return
	}
	l.loading = true
	l.seq++
	seq := l.seq
	async.Do(l.cx, l.ctx, func(ctx context.Context) (app.SpaceLocations, error) { return l.svc.Locations(ctx), nil },
		func(v app.SpaceLocations, _ error) {
			if seq != l.seq {
				return
			}
			l.loading = false
			l.value.Set(&v)
		})
}

// Bump reads the locations again, keeping the old ones until the new arrive.
func (l *Locations) Bump() {
	l.loading = false
	l.load()
}

// Block counts the open overlays; the shell disables the sidebar while any is open. It implements kit.Blocker.
type Block struct {
	holders *core.Signal[int]
}

// Hold blocks the shell until cx is disposed.
func (b *Block) Hold(cx *core.Scope) {
	b.holders.Update(func(n int) int { return n + 1 })
	cx.Cleanup(func() { b.holders.Update(func(n int) int { return n - 1 }) })
}

// Blocked reports whether an overlay is open; reading it subscribes.
func (b *Block) Blocked() bool { return b.holders.Get() > 0 }
