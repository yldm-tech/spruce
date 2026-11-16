package main

import (
	"testing"
	"time"

	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/ui/cleanflow/cleanflowfake"
	"spruce/apps/spruce/internal/ui/pages/installers"
	"spruce/apps/spruce/internal/ui/shell"
	"spruce/apps/spruce/internal/ui/store/storefake"
)

// installerCategories is a 安装包 check with the engine's categories (internal/engine/installers): old disk images, packages, archives and system images, checked, and recent or attached ones, unchecked.
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

// TestScreensInstallers shows the 安装包 drill-down view with 概览's check, the recent ones expanded, and the view without folders to search, for comparison with the design.
func TestScreensInstallers(t *testing.T) {
	const wait = 10 * time.Second
	fakes := storefake.New(t.TempDir())
	fakes.Settings.Cleanup.InstallerRoots = []string{"/Users/me/Downloads", "/Users/me/Desktop"}
	fakes.Overview = storefake.NewOverview(drillState("installers", installerCategories()...))
	a := screensApp(t, fakes, shell.Deps{Installers: installers.Deps{Files: &cleanflowfake.Files{}}}, "installers")
	a.WaitFor("id:category-list", wait)
	a.Screenshot("installers")
	a.Press("id:expand-installers:recent-installers")
	a.WaitFor("id:items-installers:recent-installers", wait)
	a.Screenshot("installers-expanded")

	none := storefake.New(t.TempDir())
	none.Settings.Cleanup.InstallerRoots = []string{}
	none.Overview = storefake.NewOverview(drillState("installers"))
	b := screensApp(t, none, shell.Deps{Installers: installers.Deps{Files: &cleanflowfake.Files{}}}, "installers")
	b.WaitFor("id:drill-settings-link", wait)
	b.Screenshot("installers-no-folders")
}
