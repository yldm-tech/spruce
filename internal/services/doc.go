// Package services is the only layer that imports both mygo and the engine. Its exported types are bound with mygo.Bind in main.go, so every exported method of Maintenance, Health, History, Permissions, Settings, Files, Cleanup, Uninstall, Space and Sysinfo is callable from the page through the client that `mygo generate` writes to src/mygo.ts (method names become lower camel case: Maintenance.Run → Maintenance.run). Keep anything the page must not call unexported or a package-level function.
//
// Bound API (TypeScript view):
//
//	Maintenance.catalog(): Promise<TaskView[]>
//	Maintenance.run(ids: string[], progress: Channel<ProgressEvent>): Promise<RunSummary>
//	Health.alerts(): Promise<Alert[]>
//	Health.openSystemMonitor(): Promise<void>
//	History.list(limit: number): Promise<Run[]>
//	History.clear(): Promise<void>
//	Permissions.fullDiskAccess(): Promise<PermissionState>
//	Permissions.guideFullDiskAccess(): Promise<PermissionState>
//	Settings.get(): Promise<AppSettings>
//	Settings.update(next: AppSettings): Promise<AppSettings>   // clamped; the page shows what comes back
//	Settings.reset(): Promise<AppSettings>
//	Settings.limits(): Promise<SettingsLimits>                 // the ranges update clamps to
//	Settings.chooseFolder(title: string): Promise<string>      // "" when cancelled
//	Settings.about(): Promise<AppAbout>
//	Files.reveal(path: string): Promise<void>
//	Files.trashSize(): Promise<TrashUsage>
//	Files.emptyTrash(): Promise<TrashUsage>
//	Cleanup.available(): Promise<CleanerID[]>
//	Cleanup.scan(cleaner: CleanerID, progress: Channel<Progress>): Promise<ScanResult>
//	Cleanup.clean(cleaner: CleanerID, req: CleanRequest, progress: Channel<Progress>): Promise<CleanResult>
//	Uninstall.apps(): Promise<UninstallApp[]>
//	Uninstall.measure(details: Channel<UninstallAppDetails>): Promise<void>
//	Uninstall.leftovers(appID: string): Promise<UninstallPlan>
//	Uninstall.remove(req: UninstallRequest, progress: Channel<Progress>): Promise<UninstallOutcome>
//	Uninstall.runUninstaller(appID: string, status: Channel<UninstallerStatus>): Promise<UninstallerOutcome>
//	Uninstall.checkRemoved(appID: string): Promise<UninstallerOutcome>
//	Space.locations(): Promise<SpaceLocations>
//	Space.chooseFolder(): Promise<string>
//	Space.scan(path: string, progress: Channel<SpaceProgress>): Promise<SpaceSummary>
//	Space.current(): Promise<SpaceSummary | null>
//	Space.children(scanID: string, id: number): Promise<SpaceListing>
//	Space.largeFiles(scanID: string): Promise<SpaceEntry[]>
//	Space.trash(scanID: string, paths: string[]): Promise<SpaceTrashResult>
//	Sysinfo.info(): Promise<SystemInfo>
//	Sysinfo.watch(samples: Channel<Vitals>): Promise<void>
//	events.healthAlerts: Event<Alert[]>          // "health:alerts", broadcast by BroadcastAlerts on every change
//	events.historyAppended: Event<Run>           // "history:appended", broadcast by BroadcastHistory for every recorded run
//	events.settingsChanged: Event<AppSettings>   // "settings:changed", broadcast by BroadcastSettings on every change
//
// Every error a bound method returns is a sentence in Simplified Chinese for the page to show; the technical cause goes to the log.
//
// # Pages
//
// A cleaner page (智能扫描, 系统垃圾, 开发者垃圾, 安装包) does not bind a service of its own: its engine package implements cleanup.Scanner and main.go registers it with RegisterCleaner under its CleanerID, and the page calls Cleanup.scan and Cleanup.clean with that ID. Cleanup records each clean in the history under the cleaner's kind. 智能扫描 (CleanerSmartScan) is internal/engine/smartscan over instances of the other three cleaners of its own; its categories carry their source and its history lines are per source. 应用卸载 and 空间透视 have flows of their own and bind Uninstall and Space; 健康监测 binds Sysinfo. Any other page puts its bound type in internal/services/<page>.go and binds it in its block of main.go.
//
// Bound types share one TypeScript namespace with every type their methods use: do not declare a new exported type under a name the generated client already has (among them Alert, AppAbout, AppSettings, Category, CategoryOutcome, CleanRequest, CleanResult, CleanerID, Failure, Item, Kind, PermissionState, Progress, ProgressEvent, Result, Run, RunSummary, ScanResult, SettingRange, SettingsLimits, SourceState, SourceStatus, SystemInfo, TaskView, Totals, TrashUsage, Vitals and the Space*, Uninstall* types); the generator would rename one of them. src/mygo.ts lists them all.
//
// The services depend on engine interfaces only; main.go builds the engine implementations (maintenance.PlatformTasks, elevate.NewLauncher, health.New, health.NewProcessSource, health.OpenSystemMonitor, history.Open, settings.Open, trash.System, the cleaners, smartscan.New, uninstall.New, sysinfo.New) and passes them in.
package services
