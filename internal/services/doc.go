// Package services is the only layer that imports both mygo and the engine. Its exported types are bound with mygo.Bind in main.go, so every exported method of Maintenance, Health, History, Permissions, Settings, Files and Cleanup is callable from the page through the client that `mygo generate` writes to src/mygo.ts (method names become lower camel case: Maintenance.Run → Maintenance.run). Keep anything the page must not call unexported or a package-level function.
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
//	Files.reveal(path: string): Promise<void>
//	Files.trashSize(): Promise<TrashUsage>
//	Files.emptyTrash(): Promise<TrashUsage>
//	Cleanup.available(): Promise<CleanerID[]>
//	Cleanup.scan(cleaner: CleanerID, progress: Channel<Progress>): Promise<ScanResult>
//	Cleanup.clean(cleaner: CleanerID, req: CleanRequest, progress: Channel<Progress>): Promise<CleanResult>
//	events.healthAlerts: Event<Alert[]>          // "health:alerts", broadcast by BroadcastAlerts on every change
//	events.settingsChanged: Event<AppSettings>   // "settings:changed", broadcast by BroadcastSettings on every change
//
// Every error a bound method returns is a sentence in Simplified Chinese for the page to show; the technical cause goes to the log.
//
// # Pages
//
// A cleaner page (智能扫描, 系统垃圾, 开发者垃圾, 安装包, and the leftovers of 应用卸载 and the deletions of 空间透视 if they use the cleanup model) does not bind a service of its own: its engine package implements cleanup.Scanner and main.go registers it with RegisterCleaner under its CleanerID, and the page calls Cleanup.scan and Cleanup.clean with that ID. Cleanup records each clean in the history under the cleaner's kind. Any other page puts its bound type in internal/services/<page>.go and binds it in its block of main.go. Bound types share one TypeScript namespace: do not declare types named like the ones above (Category, Item, Progress, Failure, ScanResult, CleanRequest, CleanResult, AppSettings, TrashUsage …); the generator would rename one of them.
//
// The services depend on engine interfaces only; main.go builds the engine implementations (maintenance.PlatformTasks, elevate.NewLauncher, health.New, health.NewProcessSource, health.OpenSystemMonitor, history.Open, settings.Open, trash.System) and passes them in.
package services
