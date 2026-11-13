// Package services is the only layer that imports both mygo and the engine. Its exported types are bound with mygo.Bind in main.go, so every exported method of Maintenance, Health, History and Permissions is callable from the page through the client that `mygo generate` writes to src/mygo.ts (method names become lower camel case: Maintenance.Run → Maintenance.run). Keep anything the page must not call unexported or a package-level function.
//
// Bound API (TypeScript view):
//
//	Maintenance.catalog(): Promise<TaskView[]>
//	Maintenance.run(ids: string[], progress: Channel<ProgressEvent>): Promise<RunSummary>
//	Health.alerts(): Promise<Alert[]>
//	Health.openSystemMonitor(): Promise<void>
//	History.list(limit: number): Promise<Run[]>
//	Permissions.fullDiskAccess(): Promise<PermissionState>
//	Permissions.guideFullDiskAccess(): Promise<PermissionState>
//	events.healthAlerts: Event<Alert[]>   // "health:alerts", broadcast by BroadcastAlerts on every change
//
// The services depend on engine interfaces only; main.go builds the engine implementations (maintenance.PlatformTasks, elevate.NewLauncher, health.New, health.NewProcessSource, health.OpenSystemMonitor, history.Open) and passes them in, so this package compiles before those land.
package services
