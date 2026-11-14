package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/ui/cleanflow/cleanflowfake"
	"github.com/yldm-tech/spruce/internal/ui/pages/devjunk"
	"github.com/yldm-tech/spruce/internal/ui/shell"
	"github.com/yldm-tech/spruce/internal/ui/store/storefake"
)

// devJunkRoots are the project folders the screens search, as the reference shows two.
var devJunkRoots = []string{"/Users/me/Workspace", "/Users/me/Developer"}

// devJunkCategories is a 开发者垃圾 scan as the design shows it: tool caches (one cleaned by its own command, one going to the Trash with a warning, one checked in part) and the build output of a few projects, the newest left unchecked.
func devJunkCategories() []cleanup.Category {
	mod := time.Now().Add(-40 * 24 * time.Hour)
	cat := func(id, title, desc, icon string, kind cleanup.CategoryKind, d cleanup.DeletionMode, selected bool, paths []string, size int64) cleanup.Category {
		c := cleanup.Category{ID: id, Title: title, Description: desc, Icon: icon, Kind: kind, Deletion: d, Selected: selected}
		for i, p := range paths {
			it := cleanup.Item{ID: cleanup.ItemID(p), Path: p, Size: size - int64(i)*37_000_000, ModTime: mod, Dir: true, Selected: selected}
			c.Items = append(c.Items, it)
			c.Size += it.Size
		}
		return c
	}
	derived := cat("tool-xcode-derived-data", "Xcode 编译缓存", "DerivedData 里的编译中间文件和索引，下次编译时会重新生成", "xcode", cleanup.KindDevCache, cleanup.DeletePermanent, true,
		[]string{"/Users/me/Library/Developer/Xcode/DerivedData/Spruce-abc", "/Users/me/Library/Developer/Xcode/DerivedData/Demo-def"}, 1_840_000_000)
	archives := cat("tool-xcode-archives", "Xcode 归档", "Xcode 打包时留下的归档", "archive", cleanup.KindDevCache, cleanup.DeleteTrash, false,
		[]string{"/Users/me/Library/Developer/Xcode/Archives/2026-03-02"}, 412_000_000)
	archives.Warning = "包含已发布版本的符号文件，删除后无法再符号化这些版本的崩溃报告；会移到废纸篓"
	support := cat("tool-xcode-device-support", "设备支持文件", "连接真机调试时复制的系统符号，再次连接时会重新复制；每个平台最新的版本默认保留", "simulator", cleanup.KindDevCache, cleanup.DeletePermanent, false,
		[]string{"/Users/me/Library/Developer/Xcode/iOS DeviceSupport/iPhone15,2 18.2", "/Users/me/Library/Developer/Xcode/iOS DeviceSupport/iPhone15,2 17.5"}, 3_100_000_000)
	support.Items[0].Note = "这个平台最新的版本，默认保留"
	support.Items[1].Selected = true
	npm := cat("tool-npm", "npm 缓存", "运行 npm cache clean --force，清空 npm 下载的包", "node", cleanup.KindDevCache, cleanup.DeletePermanent, true,
		[]string{"/Users/me/.npm/_cacache"}, 920_000_000)
	npm.Command = "npm cache clean --force"
	gobuild := cat("tool-go-build", "Go 编译缓存", "go build 的编译缓存，下次编译时重新生成", "build", cleanup.KindDevCache, cleanup.DeletePermanent, true,
		[]string{"/Users/me/Library/Caches/go-build"}, 2_600_000_000)
	nodeModules := cat("artifact-node-modules", "node_modules", "安装的 npm 依赖，在项目里重新安装（npm install）即可恢复", "node", cleanup.KindProjectArtifact, cleanup.DeletePermanent, false,
		[]string{"/Users/me/Workspace/shop/node_modules", "/Users/me/Workspace/blog/node_modules", "/Users/me/Developer/demo/node_modules"}, 640_000_000)
	nodeModules.Items[0].Note = "shop · 最近 30 天内有改动"
	nodeModules.Items[1].Selected, nodeModules.Items[1].Note = true, "blog"
	nodeModules.Items[2].Selected, nodeModules.Items[2].Note = true, "demo"
	rust := cat("artifact-rust-target", "Rust target", "cargo 的编译输出，下次编译时重新生成", "build", cleanup.KindProjectArtifact, cleanup.DeletePermanent, true,
		[]string{"/Users/me/Workspace/engine/target"}, 4_200_000_000)
	return []cleanup.Category{derived, archives, support, npm, gobuild, nodeModules, rust}
}

// TestScreensDevJunk shows the 开发者垃圾 page idle, scanning, with its two sections, asking, cleaning and done, for comparison with the design.
func TestScreensDevJunk(t *testing.T) {
	scanRelease, cleanRelease := make(chan struct{}), make(chan struct{})
	result := cleanflowfake.Result(devJunkCategories()...)
	c := &cleanflowfake.Cleaner{
		OnScan: func(ctx context.Context, _ app.CleanerID, progress func(cleanup.Progress)) (cleanup.ScanResult, error) {
			progress(cleanup.Progress{Phase: cleanup.PhaseScanning, Path: "/Users/me/Workspace/shop/node_modules", Done: 9, Total: 24, Bytes: 5_310_000_000})
			select {
			case <-scanRelease:
			case <-ctx.Done():
				return cleanup.ScanResult{}, ctx.Err()
			}
			return result, nil
		},
		OnClean: func(ctx context.Context, _ app.CleanerID, req cleanup.CleanRequest, progress func(cleanup.Progress)) (cleanup.CleanResult, error) {
			progress(cleanup.Progress{Phase: cleanup.PhaseCleaning, Path: "/Users/me/Library/Developer/Xcode/DerivedData/Spruce-abc", Done: 3, Total: 8, Bytes: 2_760_000_000})
			select {
			case <-cleanRelease:
			case <-ctx.Done():
			}
			return cleanup.CleanResult{Freed: 12_400_000_000, Removed: 8,
				Categories: []cleanup.CategoryOutcome{
					{CategoryID: "tool-xcode-derived-data", Title: "Xcode 编译缓存", Removed: 2, Freed: 3_640_000_000},
					{CategoryID: "tool-xcode-device-support", Title: "设备支持文件", Removed: 1, Freed: 3_063_000_000},
					{CategoryID: "tool-npm", Title: "npm 缓存", Removed: 1, Freed: 920_000_000},
					{CategoryID: "tool-go-build", Title: "Go 编译缓存", Removed: 1, Freed: 2_600_000_000},
					{CategoryID: "artifact-node-modules", Title: "node_modules", Removed: 2, Freed: 1_169_000_000},
					{CategoryID: "artifact-rust-target", Title: "Rust target", Removed: 1, Freed: 4_200_000_000},
				}}, nil
		}}
	fakes := storefake.New(t.TempDir())
	fakes.Settings.Cleanup.DevProjectRoots = devJunkRoots
	a := screensApp(t, fakes, shell.Deps{DevJunk: devjunk.Deps{Cleanup: c, Files: &cleanflowfake.Files{}}}, "dev-junk")
	a.WaitFor("id:roots-list", 10*time.Second)
	a.Screenshot("dev-junk-idle")

	a.Press("id:scan")
	a.WaitFor("text:已找到 5.3 GB", 10*time.Second)
	a.Screenshot("dev-junk-scanning")

	close(scanRelease)
	a.WaitFor("id:list-tools", 10*time.Second)
	a.Screenshot("dev-junk-results")
	a.Scroll("id:list-projects", 2000)
	a.Press("id:expand-artifact-node-modules")
	a.WaitFor("id:items-artifact-node-modules", 10*time.Second)
	a.Scroll("id:items-artifact-node-modules", 2000)
	a.Screenshot("dev-junk-results-projects")

	a.Press("id:clean")
	a.WaitFor("id:confirm", 10*time.Second)
	a.Screenshot("dev-junk-confirm")

	a.Press("id:confirm-ok")
	a.WaitFor(fmt.Sprintf("id:clean-overlay >> text:%s", "共 8 项；停止后，正在运行的命令会先完成"), 10*time.Second)
	a.WaitFor("id:clean-overlay >> text:已处理 3/8 · 已释放 2.8 GB", 10*time.Second)
	a.Screenshot("dev-junk-cleaning")

	close(cleanRelease)
	a.WaitFor("id:done", 10*time.Second)
	a.Screenshot("dev-junk-done")
	a.Press("id:done-close")
	a.WaitForGone("id:done", 10*time.Second)
}

// TestScreensDevJunkNoFolders shows the idle page without a project folder, and a scan that found only tool caches.
func TestScreensDevJunkNoFolders(t *testing.T) {
	tools := devJunkCategories()[:5]
	c := &cleanflowfake.Cleaner{OnScan: func(context.Context, app.CleanerID, func(cleanup.Progress)) (cleanup.ScanResult, error) {
		r := cleanflowfake.Result(tools...)
		r.Denied = 2
		return r, nil
	}}
	fakes := storefake.New(t.TempDir())
	fakes.Settings.Cleanup.DevProjectRoots = []string{}
	a := screensApp(t, fakes, shell.Deps{DevJunk: devjunk.Deps{Cleanup: c, Files: &cleanflowfake.Files{}}}, "dev-junk")
	a.WaitFor("text:还没有设置项目文件夹，这次只扫描开发工具的缓存。", 10*time.Second)
	a.Screenshot("dev-junk-no-folders")
	a.Press("id:scan")
	a.WaitFor("id:roots-notice", 10*time.Second)
	a.Screenshot("dev-junk-tools-only")
}
