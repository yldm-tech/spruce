package main

import (
	"context"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/ui/cleanflow/cleanflowfake"
	"github.com/yldm-tech/spruce/internal/ui/pages/installers"
	"github.com/yldm-tech/spruce/internal/ui/shell"
	"github.com/yldm-tech/spruce/internal/ui/store/storefake"
)

// installerCategories is a 安装包 scan with the engine's categories (internal/engine/installers): old disk images, packages, archives and system images, checked, and recent or attached ones, unchecked.
func installerCategories() []cleanup.Category {
	now := time.Now()
	cat := func(id, title, desc, icon string, selected bool, items ...cleanup.Item) cleanup.Category {
		c := cleanup.Category{ID: id, Title: title, Description: desc, Icon: icon, Kind: cleanup.KindInstaller, Deletion: cleanup.DeleteTrash, Selected: selected, Items: items}
		for i := range c.Items {
			c.Items[i].ID = cleanup.ItemID(c.Items[i].Path)
			c.Items[i].Selected = selected
			c.Size += c.Items[i].Size
		}
		return c
	}
	item := func(path string, size int64, age time.Duration, note string) cleanup.Item {
		return cleanup.Item{Path: path, Size: size, ModTime: now.Add(-age), Note: note}
	}
	day := 24 * time.Hour
	return []cleanup.Category{
		cat("disk-images", "磁盘映像", ".dmg 磁盘映像，装好应用后通常就不再需要", "installer", true,
			item("/Users/me/Downloads/Firefox 130.0.dmg", 140_000_000, 74*day, ""),
			item("/Users/me/Downloads/Docker.dmg", 610_000_000, 120*day, "")),
		cat("installer-packages", "安装器", ".pkg 安装器，装好应用后通常就不再需要", "package", true,
			item("/Users/me/Desktop/node-v22.11.0.pkg", 80_000_000, 105*day, ""),
			item("/Users/me/Downloads/Zoom.pkg", 120_000_000, 45*day, "")),
		cat("archives", "压缩包", "名称像安装包的 .zip 和 .xip 文件", "archive", true,
			item("/Users/me/Downloads/Xcode_16.xip", 3_400_000_000, 160*day, "")),
		cat("system-images", "系统映像", ".iso 光盘映像", "disk", true,
			item("/Users/me/Downloads/ubuntu-24.04-desktop-arm64.iso", 2_900_000_000, 210*day, "")),
		cat("recent-installers", "最近下载或正在使用的安装包", "7 天内下载的，或仍处于挂载状态的磁盘映像，可能还要用", "temp", false,
			item("/Users/me/Downloads/Slack.dmg", 200_000_000, 1*day, "磁盘映像 · 已挂载"),
			item("/Users/me/Downloads/Figma.dmg", 180_000_000, 3*day, "")),
	}
}

// TestScreensInstallers shows the 安装包 page idle, scanning, with results, asking, cleaning and done, for comparison with the design.
func TestScreensInstallers(t *testing.T) {
	scanRelease, cleanRelease := make(chan struct{}), make(chan struct{})
	result := cleanflowfake.Result(installerCategories()...)
	c := &cleanflowfake.Cleaner{
		OnScan: func(ctx context.Context, _ app.CleanerID, progress func(cleanup.Progress)) (cleanup.ScanResult, error) {
			progress(cleanup.Progress{Phase: cleanup.PhaseScanning, Path: "/Users/me/Downloads/Installers", Done: 3, Total: 4, Bytes: 3_640_000_000})
			select {
			case <-scanRelease:
			case <-ctx.Done():
				return cleanup.ScanResult{}, ctx.Err()
			}
			return result, nil
		},
		OnClean: func(ctx context.Context, _ app.CleanerID, req cleanup.CleanRequest, progress func(cleanup.Progress)) (cleanup.CleanResult, error) {
			progress(cleanup.Progress{Phase: cleanup.PhaseCleaning, Path: "/Users/me/Downloads/Docker.dmg", Done: 2, Total: 6, Bytes: 750_000_000})
			select {
			case <-cleanRelease:
			case <-ctx.Done():
			}
			return cleanup.CleanResult{Trashed: 7_250_000_000, Removed: 6, Categories: []cleanup.CategoryOutcome{
				{CategoryID: "disk-images", Title: "磁盘映像", Removed: 2, Trashed: 750_000_000},
				{CategoryID: "installer-packages", Title: "安装器", Removed: 2, Trashed: 200_000_000},
				{CategoryID: "archives", Title: "压缩包", Removed: 1, Trashed: 3_400_000_000},
				{CategoryID: "system-images", Title: "系统映像", Removed: 1, Trashed: 2_900_000_000},
			}}, nil
		}}
	fakes := storefake.New(t.TempDir())
	fakes.Settings.Cleanup.InstallerRoots = []string{"/Users/me/Downloads", "/Users/me/Desktop"}
	a := screensApp(t, fakes, shell.Deps{Installers: installers.Deps{Cleanup: c, Files: &cleanflowfake.Files{}}}, "installers")
	a.WaitFor("role:group 查找的文件夹", 10*time.Second)
	a.Screenshot("installers-idle")

	a.Press("id:scan")
	a.WaitFor("text:已找到 3.6 GB", 10*time.Second)
	a.Screenshot("installers-scanning")

	close(scanRelease)
	a.WaitFor("id:category-list", 10*time.Second)
	a.Screenshot("installers-results")
	a.Press("id:expand-recent-installers")
	a.WaitFor("id:items-recent-installers", 10*time.Second)
	a.Screenshot("installers-results-expanded")

	a.Press("id:clean")
	a.WaitFor("id:confirm", 10*time.Second)
	a.Screenshot("installers-confirm")

	a.Press("id:confirm-ok")
	a.WaitFor("id:clean-overlay >> text:已处理 2/6 · 已释放 750 MB", 10*time.Second)
	a.Screenshot("installers-cleaning")

	close(cleanRelease)
	a.WaitFor("id:done", 10*time.Second)
	a.Screenshot("installers-done")
	a.Press("id:done-close")
	a.WaitForGone("id:done", 10*time.Second)
}

// TestScreensInstallersNoFolders shows the 安装包 page when 设置 lists no folders to search: 扫描 is disabled.
func TestScreensInstallersNoFolders(t *testing.T) {
	fakes := storefake.New(t.TempDir())
	fakes.Settings.Cleanup.InstallerRoots = []string{}
	a := screensApp(t, fakes, shell.Deps{Installers: installers.Deps{Cleanup: &cleanflowfake.Cleaner{}, Files: &cleanflowfake.Files{}}}, "installers")
	a.WaitFor("id:no-folders", 10*time.Second)
	a.Screenshot("installers-no-folders")
}

// TestScreensInstallersEmpty shows the 安装包 page after a scan that found nothing.
func TestScreensInstallersEmpty(t *testing.T) {
	fakes := storefake.New(t.TempDir())
	fakes.Settings.Cleanup.InstallerRoots = []string{"/Users/me/Downloads", "/Users/me/Desktop"}
	a := screensApp(t, fakes, shell.Deps{Installers: installers.Deps{Cleanup: &cleanflowfake.Cleaner{}, Files: &cleanflowfake.Files{}}}, "installers")
	a.WaitFor("id:scan", 10*time.Second)
	a.Press("id:scan")
	a.WaitFor("id:empty", 10*time.Second)
	a.Screenshot("installers-empty")
}
