package main

import (
	"testing"
	"time"

	"spruce/apps/spruce/internal/engine/cleanup"
	engdev "spruce/apps/spruce/internal/engine/devjunk"
	"spruce/apps/spruce/internal/ui/cleanflow/cleanflowfake"
	"spruce/apps/spruce/internal/ui/pages/devjunk"
	"spruce/apps/spruce/internal/ui/shell"
	"spruce/apps/spruce/internal/ui/store/storefake"
)

// devJunkRoots are the project folders the screens search, as the reference shows two.
var devJunkRoots = []string{"/Users/me/Workspace", "/Users/me/Developer"}

// devTools are a 开发者 check's tool caches: one cleaned by its own command, one going to the Trash with a warning, one checked in part.
func devTools() []cleanup.Category {
	mod := time.Now().Add(-40 * 24 * time.Hour)
	cat := func(id, title, desc, icon string, d cleanup.DeletionMode, selected bool, paths []string, size int64) cleanup.Category {
		c := cleanup.Category{ID: id, Title: title, Description: desc, Icon: icon, Kind: cleanup.KindDevCache, Deletion: d, Selected: selected}
		for i, p := range paths {
			it := cleanup.Item{ID: cleanup.ItemID(p), Path: p, Size: size - int64(i)*37_000_000, ModTime: mod, Dir: true, Selected: selected}
			c.Items = append(c.Items, it)
			c.Size += it.Size
		}
		return c
	}
	derived := cat("tool-xcode-derived-data", "Xcode 编译缓存", "DerivedData 里的编译中间文件和索引，下次编译时会重新生成", "xcode", cleanup.DeletePermanent, true,
		[]string{"/Users/me/Library/Developer/Xcode/DerivedData/Spruce-abc", "/Users/me/Library/Developer/Xcode/DerivedData/Demo-def"}, 1_840_000_000)
	archives := cat("tool-xcode-archives", "Xcode 归档", "Xcode 打包时留下的归档", "archive", cleanup.DeleteTrash, false,
		[]string{"/Users/me/Library/Developer/Xcode/Archives/2026-03-02"}, 412_000_000)
	archives.Warning = "包含已发布版本的符号文件，删除后无法再符号化这些版本的崩溃报告；会移到废纸篓"
	support := cat("tool-xcode-device-support", "设备支持文件", "连接真机调试时复制的系统符号，再次连接时会重新复制；每个平台最新的版本默认保留", "simulator", cleanup.DeletePermanent, false,
		[]string{"/Users/me/Library/Developer/Xcode/iOS DeviceSupport/iPhone15,2 18.2", "/Users/me/Library/Developer/Xcode/iOS DeviceSupport/iPhone15,2 17.5"}, 3_100_000_000)
	support.Items[0].Note = "这个平台最新的版本，默认保留"
	support.Items[1].Selected = true
	npm := cat("tool-npm", "npm 缓存", "运行 npm cache clean --force，清空 npm 下载的包", "node", cleanup.DeletePermanent, true, []string{"/Users/me/.npm/_cacache"}, 920_000_000)
	npm.Command = "npm cache clean --force"
	gobuild := cat("tool-go-build", "Go 编译缓存", "go build 的编译缓存，下次编译时重新生成", "build", cleanup.DeletePermanent, true, []string{"/Users/me/Library/Caches/go-build"}, 2_600_000_000)
	return []cleanup.Category{derived, archives, support, npm, gobuild}
}

// devArtifacts are the projects' build output: idle and clean (checked), not a repository (checked), recent, Git unknown, with uncommitted changes (held) and with tracked files (held).
func devArtifacts() []cleanup.Category {
	day := 24 * time.Hour
	now := time.Now()
	art := func(project, root, name string, size int64, git cleanup.GitState, hold string, idle time.Duration) cleanup.Item {
		p := project + "/" + name
		recent := idle < 30*day
		note := project[len(root)+1:]
		if recent {
			note += " · 最近 30 天内有改动"
		}
		return cleanup.Item{ID: cleanup.ItemID(p), Path: p, Size: size, ModTime: now.Add(-idle), Dir: true, Project: project, Git: git, Hold: hold, Note: note,
			Selected: hold == "" && !recent && (git == cleanup.GitClean || git == cleanup.GitNone)}
	}
	ws, dev := devJunkRoots[0], devJunkRoots[1]
	nm := cleanup.Category{ID: "artifact-node-modules", Title: "node_modules", Description: "安装的 npm 依赖，在项目里重新安装（npm install）即可恢复", Icon: "node",
		Kind: cleanup.KindProjectArtifact, Deletion: cleanup.DeletePermanent, Items: []cleanup.Item{
			art(ws+"/shop", ws, "node_modules", 1_640_000_000, cleanup.GitClean, "", 120*day),
			art(ws+"/blog", ws, "node_modules", 920_000_000, cleanup.GitDirty, "项目有未提交的改动", 95*day),
			art(dev+"/demo", dev, "node_modules", 610_000_000, cleanup.GitNone, "", 200*day),
			art(ws+"/site/web", ws, "node_modules", 480_000_000, cleanup.GitUnknown, "", 12*day),
		}}
	next := cleanup.Category{ID: "artifact-next", Title: ".next", Description: "Next.js 的构建输出，下次构建时重新生成", Icon: "build",
		Kind: cleanup.KindProjectArtifact, Deletion: cleanup.DeletePermanent, Items: []cleanup.Item{
			art(ws+"/shop", ws, ".next", 310_000_000, cleanup.GitClean, "", 120*day),
			art(ws+"/blog", ws, ".next", 140_000_000, cleanup.GitDirty, "项目有未提交的改动", 95*day),
		}}
	rust := cleanup.Category{ID: "artifact-rust-target", Title: "Rust target", Description: "cargo 的编译输出，下次编译时重新生成", Icon: "build",
		Kind: cleanup.KindProjectArtifact, Deletion: cleanup.DeletePermanent, Items: []cleanup.Item{
			art(ws+"/engine", ws, "target", 4_200_000_000, cleanup.GitClean, "", 150*day),
			art(dev+"/vendor-lib", dev, "target", 260_000_000, cleanup.GitClean, "构建产物文件夹里有纳入版本控制的文件", 300*day),
		}}
	return []cleanup.Category{nm, next, rust}
}

// devState is the check over cats with its projects grouped, as 概览's controller publishes it.
func devState(cats ...cleanup.Category) *storefake.Overview {
	st := drillState("dev-junk", cats...)
	st.Result.Projects = engdev.GroupProjects(st.Result.Scan)
	return storefake.NewOverview(st)
}

// TestScreensDevJunk shows the 开发者 drill-down view: 项目 with each project's Git status, the ones with uncommitted changes, one expanded, 工具缓存, and 项目 without project folders, for comparison with the design.
func TestScreensDevJunk(t *testing.T) {
	const wait = 10 * time.Second
	fakes := storefake.New(t.TempDir())
	fakes.Settings.Cleanup.DevProjectRoots = devJunkRoots
	fakes.Overview = devState(append(devTools(), devArtifacts()...)...)
	a := screensApp(t, fakes, shell.Deps{DevJunk: devjunk.Deps{Files: &cleanflowfake.Files{}}}, "dev-junk")
	a.WaitFor("id:projects", wait)
	a.Screenshot("dev-projects")
	a.Press("id:clean")
	a.WaitFor("id:confirm-projects", wait)
	a.Screenshot("dev-projects-confirm")
	a.Press("id:confirm-cancel")
	a.WaitForGone("id:confirm", wait)

	a.Press("id:projects-sort-active")
	a.WaitUntil(wait, func() bool { return a.Find("id:projects-sort-active").Props.Pressed })
	a.Screenshot("dev-projects-recent")
	a.Press("id:projects-sort-size")

	a.Press("id:projects-filter-dirty")
	blog := "id:project-" + cleanup.ItemID(devJunkRoots[0]+"/blog")
	a.WaitUntil(wait, func() bool { return len(a.Query("id:projects >> role:checkbox")) == 1 })
	a.Press(blog + "-expand")
	a.WaitFor(blog+"-items", wait)
	a.Screenshot("dev-projects-dirty")

	a.Press("id:tab-tools")
	a.WaitFor("id:list-tools", wait)
	a.Screenshot("dev-tools")

	none := storefake.New(t.TempDir())
	none.Settings.Cleanup.DevProjectRoots = []string{}
	none.Overview = devState(devTools()...)
	b := screensApp(t, none, shell.Deps{DevJunk: devjunk.Deps{Files: &cleanflowfake.Files{}}}, "dev-junk")
	b.WaitFor("id:projects-roots", wait)
	b.Screenshot("dev-projects-roots")
}
