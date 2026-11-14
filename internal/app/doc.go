// Package app is the control layer the UI calls: Maintenance, Health, History, Permissions, Settings, Files, Cleanup, Uninstall, Space and Sysinfo, each over engine interfaces. It imports no UI framework, so its tests run without one.
//
// Every error a method returns is a sentence in Simplified Chinese for the page to show; the technical cause goes to the log. Long work takes a context, which the page cancels when it goes away, and reports progress through a callback (nil reports nothing) on the work's own goroutine; the UI moves it onto its own goroutine (internal/ui/async). The DTOs keep their json tags from the time a web page read them; they serve as view models.
//
// # Pages
//
// A cleaner page (智能扫描, 系统垃圾, 开发者垃圾, 安装包) has no controller of its own: its engine package implements cleanup.Scanner and main.go registers it with RegisterCleaner under its CleanerID, and the page calls Cleanup.Scan and Cleanup.Clean with that ID. Cleanup records each clean in the history under the cleaner's kind. 智能扫描 (CleanerSmartScan) is internal/engine/smartscan over instances of the other three cleaners of its own; its categories carry their source and its history lines are per source. 应用卸载 and 空间透视 have flows of their own in Uninstall and Space; 健康监测 reads Sysinfo.
//
// The controllers depend on engine interfaces only; main.go builds the engine implementations (maintenance.PlatformTasks, elevate.NewLauncher, health.New, health.NewProcessSource, health.OpenSystemMonitor, history.Open, settings.Open, trash.System, the cleaners, smartscan.New, uninstall.New, sysinfo.New) and passes them in.
package app
