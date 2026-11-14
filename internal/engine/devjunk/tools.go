package devjunk

import (
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/execx"
)

// Timeouts of the commands this package runs.
const (
	// queryTimeout bounds a command that only answers a question (go env, brew --cache, simctl list).
	queryTimeout = 30 * time.Second
	// cleanTimeout bounds a clean command; brew cleanup on a large cellar and go clean -modcache take minutes.
	cleanTimeout = 15 * time.Minute
)

// env is the platform and environment a catalog is built for. Everything platform specific in this file reads it rather than runtime.GOOS or os.Getenv, so the Windows catalog is tested on macOS.
type env struct {
	goos   string
	home   string
	getenv func(string) string
}

func (e env) windows() bool { return e.goos == "windows" }

// isAbs reports whether p is absolute on e's platform: a drive or UNC path on Windows, a path from / elsewhere.
func (e env) isAbs(p string) bool {
	if !e.windows() {
		return strings.HasPrefix(p, "/")
	}
	if strings.HasPrefix(p, `\\`) {
		return true
	}
	return len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') && (p[0]|0x20 >= 'a' && p[0]|0x20 <= 'z')
}

// dirVar is the folder named by the environment variable name, or fallback when it is unset or not absolute.
func (e env) dirVar(name, fallback string) string {
	if v := strings.TrimSpace(e.getenv(name)); v != "" && e.isAbs(v) {
		return filepath.Clean(v)
	}
	return fallback
}

// localAppData is %LOCALAPPDATA% (Windows).
func (e env) localAppData() string {
	return e.dirVar("LOCALAPPDATA", filepath.Join(e.home, "AppData", "Local"))
}

// appData is %APPDATA% (Windows).
func (e env) appData() string {
	return e.dirVar("APPDATA", filepath.Join(e.home, "AppData", "Roaming"))
}

// profile is %USERPROFILE% on Windows and the home folder elsewhere.
func (e env) profile() string {
	if e.windows() {
		return e.dirVar("USERPROFILE", e.home)
	}
	return e.home
}

// caches is ~/Library/Caches (macOS).
func (e env) caches() string { return filepath.Join(e.home, "Library", "Caches") }

// cargoHome is $CARGO_HOME or ~/.cargo.
func (e env) cargoHome() string { return e.dirVar("CARGO_HOME", filepath.Join(e.profile(), ".cargo")) }

// gradleHome is $GRADLE_USER_HOME or ~/.gradle.
func (e env) gradleHome() string {
	return e.dirVar("GRADLE_USER_HOME", filepath.Join(e.profile(), ".gradle"))
}

// pathList joins PATH entries with the platform's separator.
func (e env) pathList(dirs ...string) string {
	sep := ":"
	if e.windows() {
		sep = ";"
	}
	var keep []string
	for _, d := range dirs {
		if d != "" {
			keep = append(keep, d)
		}
	}
	return strings.Join(keep, sep)
}

// dirOf is the folder of a binary's path on e's platform.
func (e env) dirOf(p string) string {
	seps := "/"
	if e.windows() {
		seps = `\/`
	}
	if i := strings.LastIndexAny(p, seps); i > 0 {
		return p[:i]
	}
	return ""
}

// located is what locate found out by asking the system: the tools' binaries and the folders the tools report.
type located struct {
	// bins maps a tool name (brew, npm, pnpm, go, pip) to its binary's absolute path; a missing tool has no entry.
	bins map[string]string
	// goCache and goModCache are go env GOCACHE and GOMODCACHE ("" without go).
	goCache    string
	goModCache string
	// pnpmStore is pnpm store path ("" without pnpm).
	pnpmStore string
	// brewCache is brew --cache ("" without brew).
	brewCache string
	// xcrun is /usr/bin/xcrun when the selected developer folder has simctl (Xcode, not only the Command Line Tools); "" otherwise.
	xcrun string
	// vscodeRunning is set while VS Code runs.
	vscodeRunning bool
}

// cacheDir is a folder whose entries are offered.
type cacheDir struct {
	path string
	// off leaves its entries unchecked when the tool is checked.
	off bool
}

// lister lists a tool's items itself, for tools whose entries need picking (keep the newest version …).
type lister func(t *tool) ([]entry, error)

// entry is one candidate item before it is measured.
type entry struct {
	path string
	// root is the allowed root for deleting it.
	root     string
	info     fs.FileInfo
	selected bool
	note     string
}

// tool is one tool cache: one category of the page.
type tool struct {
	id          string
	title       string
	description string
	icon        string
	warning     string
	// selected is whether the category starts checked.
	selected bool
	// trash moves the items to the Trash instead of deleting them.
	trash bool
	dirs  []cacheDir
	// list replaces the default listing (the entries of dirs).
	list lister

	// command, when set, cleans the tool as one item by running it; the item is the first of dirs.
	command *execx.Command
	// commandDescription replaces description when the command is used.
	commandDescription string
	// commandOnly: without the command (or when an exclusion lies inside its folder) the tool is not offered.
	commandOnly bool
	// simulators marks the unavailable-simulators tool, which measures the devices simctl reports.
	simulators bool
}

// deletion is how the tool's items go.
func (t *tool) deletion() cleanup.DeletionMode {
	if t.trash {
		return cleanup.DeleteTrash
	}
	return cleanup.DeletePermanent
}

// commandFor builds the clean command for the tool name, or nil when its binary was not found. The binary's folder goes first on PATH, since npm and pnpm are scripts run by the node next to them and a GUI app's PATH does not have it.
func (e env) commandFor(l located, name string, args ...string) *execx.Command {
	bin := l.bins[name]
	if bin == "" {
		return nil
	}
	cmd := &execx.Command{Name: bin, Args: args, Timeout: cleanTimeout, Env: []string{"PATH=" + e.pathList(e.dirOf(bin), e.getenv("PATH"))}}
	if name == "go" {
		// Never download a toolchain to answer or clean.
		cmd.Env = append(cmd.Env, "GOTOOLCHAIN=local")
	}
	return cmd
}

// dirs makes cacheDirs that are all checked with their tool.
func dirs(paths ...string) []cacheDir {
	out := make([]cacheDir, 0, len(paths))
	for _, p := range paths {
		if p != "" {
			out = append(out, cacheDir{path: p})
		}
	}
	return out
}

// firstNonEmpty returns the first of values that is not "".
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// catalog lists the tool caches of e's platform, in the order the page shows them.
func catalog(e env, l located) []*tool {
	var ts []*tool
	add := func(t *tool) { ts = append(ts, t) }
	home := e.home

	if !e.windows() {
		xcode := filepath.Join(home, "Library", "Developer", "Xcode")
		add(&tool{
			id: "xcode-derived-data", title: "Xcode 编译缓存", icon: "xcode", selected: true,
			description: "DerivedData 里的编译中间文件和索引，下次编译时会重新生成",
			dirs:        dirs(filepath.Join(xcode, "DerivedData")),
		})
		add(&tool{
			id: "xcode-archives", title: "Xcode 归档", icon: "archive", trash: true,
			description: "Xcode 打包时留下的归档",
			warning:     "包含已发布版本的符号文件，删除后无法再符号化这些版本的崩溃报告；会移到废纸篓",
			dirs:        dirs(filepath.Join(xcode, "Archives")),
		})
		add(&tool{
			id: "xcode-device-support", title: "设备支持文件", icon: "simulator", selected: true,
			description: "连接真机调试时复制的系统符号，再次连接时会重新复制；每个平台最新的版本默认保留",
			list:        func(*tool) ([]entry, error) { return listDeviceSupport(xcode) },
		})
		add(&tool{
			id: "xcode-simulators", title: "不可用的模拟器", icon: "simulator", selected: true, simulators: true, commandOnly: true,
			commandDescription: "运行 xcrun simctl delete unavailable，删除系统运行时已不存在、无法再启动的模拟器",
			dirs:               dirs(filepath.Join(home, "Library", "Developer", "CoreSimulator", "Devices")),
			command:            simctlCommand(l),
		})
		add(&tool{
			id: "simulator-caches", title: "模拟器缓存", icon: "simulator", selected: true,
			description: "模拟器的系统库缓存，启动模拟器时会重新生成",
			dirs:        dirs(filepath.Join(home, "Library", "Developer", "CoreSimulator", "Caches")),
		})
		add(&tool{
			id: "xcode-cache", title: "Xcode 缓存", icon: "xcode", selected: true,
			description: "Xcode 和 xcodebuild 自己的缓存，会自动重新生成",
			dirs:        dirs(filepath.Join(e.caches(), "com.apple.dt.Xcode"), filepath.Join(e.caches(), "com.apple.dt.xcodebuild")),
		})
		add(&tool{
			id: "homebrew", title: "Homebrew 缓存", icon: "package", selected: true,
			description:        "Homebrew 下载的安装包，重新安装时会再下载",
			commandDescription: "运行 brew cleanup -s --prune=all：删除下载缓存，以及已升级软件的旧版本",
			dirs:               dirs(firstNonEmpty(l.brewCache, filepath.Join(e.caches(), "Homebrew"))),
			command:            e.commandFor(l, "brew", "cleanup", "-s", "--prune=all"),
		})
	}

	npmCache := filepath.Join(home, ".npm", "_cacache")
	if e.windows() {
		npmCache = filepath.Join(e.localAppData(), "npm-cache", "_cacache")
	}
	add(&tool{
		id: "npm", title: "npm 缓存", icon: "node", selected: true,
		description:        "npm 下载的包，安装依赖时会重新下载",
		commandDescription: "运行 npm cache clean --force，清空 npm 下载的包",
		dirs:               dirs(npmCache),
		command:            e.commandFor(l, "npm", "cache", "clean", "--force"),
	})
	add(&tool{
		id: "yarn", title: "Yarn 缓存", icon: "node", selected: true,
		description: "Yarn 下载的包，安装依赖时会重新下载",
		dirs:        dirs(e.pick(filepath.Join(e.caches(), "Yarn"), filepath.Join(e.localAppData(), "Yarn", "Cache"))),
	})
	add(&tool{
		id: "pnpm", title: "pnpm 存储", icon: "node", selected: true, commandOnly: true,
		commandDescription: "运行 pnpm store prune，只删除没有任何项目再引用的包；实际释放的空间可能小于显示的大小",
		dirs:               dirs(l.pnpmStore),
		command:            e.commandFor(l, "pnpm", "store", "prune"),
	})
	add(&tool{
		id: "pnpm-cache", title: "pnpm 元数据缓存", icon: "node", selected: true,
		description: "pnpm 下载的包信息，安装依赖时会重新获取",
		dirs:        dirs(e.pick(filepath.Join(e.caches(), "pnpm"), filepath.Join(e.localAppData(), "pnpm-cache"))),
	})
	add(&tool{
		id: "bun", title: "Bun 缓存", icon: "package", selected: true,
		description: "Bun 下载的包，安装依赖时会重新下载",
		dirs:        dirs(filepath.Join(e.profile(), ".bun", "install", "cache")),
	})
	add(&tool{
		id: "node-gyp", title: "node-gyp 缓存", icon: "node", selected: true,
		description: "编译原生 Node 模块用的头文件，需要时会重新下载",
		dirs:        dirs(e.pick(filepath.Join(e.caches(), "node-gyp"), filepath.Join(e.localAppData(), "node-gyp", "Cache"))),
	})
	add(&tool{
		id: "playwright", title: "Playwright 浏览器", icon: "browser",
		description: "Playwright 和 playwright-go 下载的测试用浏览器",
		warning:     "删除后要重新下载浏览器（npx playwright install）才能运行测试",
		dirs:        dirs(e.userCache("ms-playwright"), e.userCache("ms-playwright-go")),
	})
	add(&tool{
		id: "cypress", title: "Cypress 缓存", icon: "browser",
		description: "Cypress 下载的测试程序",
		warning:     "删除后要重新下载 Cypress（npx cypress install）才能运行测试",
		dirs:        dirs(e.pick(filepath.Join(e.caches(), "Cypress"), filepath.Join(e.localAppData(), "Cypress", "Cache"))),
	})
	add(&tool{
		id: "electron", title: "Electron 缓存", icon: "package", selected: true,
		description: "下载的 Electron 程序包，打包时会重新下载",
		dirs:        dirs(e.pick(filepath.Join(e.caches(), "electron"), filepath.Join(e.localAppData(), "electron", "Cache"))),
	})
	add(&tool{
		id: "electron-builder", title: "electron-builder 缓存", icon: "build", selected: true,
		description: "electron-builder 下载的打包工具，打包时会重新下载",
		dirs:        dirs(e.pick(filepath.Join(e.caches(), "electron-builder"), filepath.Join(e.localAppData(), "electron-builder", "Cache"))),
	})
	add(&tool{
		id: "typescript", title: "TypeScript 缓存", icon: "cache", selected: true,
		description: "编辑器自动下载的类型定义，需要时会重新下载",
		dirs:        dirs(e.pick(filepath.Join(e.caches(), "typescript"), filepath.Join(e.localAppData(), "Microsoft", "TypeScript"))),
	})
	add(&tool{
		id: "deno", title: "Deno 缓存", icon: "package", selected: true,
		description: "Deno 下载的模块和编译缓存，运行时会重新生成",
		dirs:        dirs(e.pick(filepath.Join(e.caches(), "deno"), filepath.Join(e.localAppData(), "deno"))),
	})

	goCache := firstNonEmpty(l.goCache, e.dirVar("GOCACHE", e.pick(filepath.Join(e.caches(), "go-build"), filepath.Join(e.localAppData(), "go-build"))))
	add(&tool{
		id: "go-build", title: "Go 编译缓存", icon: "build", selected: true,
		description:        "Go 的编译缓存，下次编译时会重新生成",
		commandDescription: "运行 go clean -cache，清空 Go 的编译缓存",
		dirs:               dirs(goCache),
		command:            e.commandFor(l, "go", "clean", "-cache"),
	})
	add(&tool{
		id: "go-mod", title: "Go 模块缓存", icon: "package", commandOnly: true,
		commandDescription: "运行 go clean -modcache，删除下载的全部 Go 模块",
		warning:            "删除后构建时要重新下载全部 Go 依赖",
		dirs:               dirs(l.goModCache),
		command:            e.commandFor(l, "go", "clean", "-modcache"),
	})

	add(&tool{
		id: "go-tools", title: "Go 工具缓存", icon: "build", selected: true,
		description: "gopls、goimports、golangci-lint 和 staticcheck 的分析缓存，用到时会重新生成",
		dirs:        dirs(e.userCache("gopls"), e.userCache("go"), e.userCache("goimports"), e.userCache("golangci-lint"), e.userCache("staticcheck")),
	})

	cargo := e.cargoHome()
	add(&tool{
		id: "cargo", title: "Cargo 缓存", icon: "package", selected: true,
		description: "Cargo 下载的 crate 压缩包和解压后的源码，构建时会重新下载；Git 依赖的检出默认不选",
		dirs: []cacheDir{
			{path: filepath.Join(cargo, "registry", "cache")},
			{path: filepath.Join(cargo, "registry", "src")},
			{path: filepath.Join(cargo, "git", "checkouts"), off: true},
		},
	})

	add(&tool{
		id: "rust-tools", title: "Rust 工具缓存", icon: "build", selected: true,
		description: "sccache 的编译缓存和 rust-analyzer 的缓存，用到时会重新生成",
		dirs:        dirs(e.pick(filepath.Join(e.caches(), "Mozilla.sccache"), filepath.Join(e.localAppData(), "Mozilla", "sccache")), e.userCache("rust-analyzer")),
	})

	pipName := "pip3"
	if e.windows() {
		pipName = "pip"
	}
	add(&tool{
		id: "pip", title: "pip 缓存", icon: "package", selected: true,
		description:        "pip 下载的包和编译好的 wheel，安装时会重新下载",
		commandDescription: "运行 pip cache purge，清空 pip 的下载缓存",
		dirs:               dirs(e.pick(filepath.Join(e.caches(), "pip"), filepath.Join(e.localAppData(), "pip", "Cache"))),
		command:            e.commandFor(l, pipName, "cache", "purge"),
	})
	// uv keeps its cache in ~/.cache/uv on macOS; versions before 0.2 used ~/Library/Caches/uv.
	add(&tool{
		id: "uv", title: "uv 缓存", icon: "package", selected: true,
		description: "uv 下载的 Python 包，安装时会重新下载",
		dirs:        dirs(e.pick(filepath.Join(home, ".cache", "uv"), filepath.Join(e.localAppData(), "uv", "cache")), e.pick(filepath.Join(e.caches(), "uv"), "")),
	})
	poetry := e.pick(filepath.Join(e.caches(), "pypoetry"), filepath.Join(e.localAppData(), "pypoetry", "Cache"))
	add(&tool{
		id: "poetry", title: "Poetry 缓存", icon: "package", selected: true,
		description: "Poetry 下载的包和构建的 wheel，安装时会重新下载；项目的虚拟环境默认不选",
		dirs: []cacheDir{
			{path: filepath.Join(poetry, "cache")},
			{path: filepath.Join(poetry, "artifacts")},
			{path: filepath.Join(poetry, "virtualenvs"), off: true},
		},
	})

	gradle := e.gradleHome()
	add(&tool{
		id: "gradle-caches", title: "Gradle 缓存", icon: "build", selected: true,
		description: "Gradle 下载的依赖和构建缓存，构建时会重新下载",
		dirs:        dirs(filepath.Join(gradle, "caches")),
	})
	add(&tool{
		id: "gradle-wrapper", title: "Gradle Wrapper 旧版本", icon: "build",
		description: "Gradle Wrapper 下载的发行版，最新的版本不在列表中",
		warning:     "用到这些版本的项目构建时会重新下载",
		list:        func(*tool) ([]entry, error) { return listGradleDists(filepath.Join(gradle, "wrapper", "dists")) },
	})
	add(&tool{
		id: "maven", title: "Maven 本地仓库", icon: "package",
		description: "Maven 下载的全部依赖",
		warning:     "删除后 Maven 构建要重新下载全部依赖",
		dirs:        dirs(filepath.Join(e.profile(), ".m2", "repository")),
	})

	if !e.windows() {
		add(&tool{
			id: "cocoapods", title: "CocoaPods 缓存", icon: "package", selected: true,
			description: "CocoaPods 下载的库，pod install 时会重新下载",
			dirs:        dirs(filepath.Join(e.caches(), "CocoaPods")),
		})
		add(&tool{
			id: "swiftpm", title: "Swift Package Manager 缓存", icon: "package", selected: true,
			description: "SwiftPM 下载的包，解析依赖时会重新下载",
			dirs:        dirs(filepath.Join(e.caches(), "org.swift.swiftpm")),
		})
	}

	jetBrains := e.pick(filepath.Join(e.caches(), "JetBrains"), filepath.Join(e.localAppData(), "JetBrains"))
	add(&tool{
		id: "jetbrains", title: "JetBrains IDE 缓存", icon: "cache", selected: true,
		description: "IDE 的索引和缓存；旧版本的默认选中，每个 IDE 最新版本的默认保留",
		list:        func(*tool) ([]entry, error) { return listJetBrains(jetBrains, e.windows()) },
	})

	code := e.pick(filepath.Join(home, "Library", "Application Support", "Code"), filepath.Join(e.appData(), "Code"))
	vscode := &tool{
		id: "vscode", title: "VS Code 缓存", icon: "cache", selected: !l.vscodeRunning,
		description: "VS Code 的网页缓存、编译缓存和下载的扩展安装包，会自动重新生成",
		dirs:        dirs(filepath.Join(code, "Cache"), filepath.Join(code, "CachedData"), filepath.Join(code, "CachedExtensionVSIXs")),
	}
	if l.vscodeRunning {
		vscode.warning = "VS Code 正在运行，请先退出再清理"
	}
	add(vscode)

	if !e.windows() {
		add(&tool{
			id: "bazel", title: "Bazel 缓存", icon: "build",
			description: "Bazel 的下载和构建缓存",
			warning:     "删除后 Bazel 要重新下载依赖并完整重新构建",
			dirs:        dirs(filepath.Join(e.caches(), "bazel")),
		})
		add(&tool{
			id: "xcodes", title: "Xcodes 缓存", icon: "xcode", selected: true,
			description: "Xcodes 应用的缓存，会自动重新生成",
			dirs:        dirs(filepath.Join(e.caches(), "com.github.xcodes")),
		})
		add(&tool{
			id: "mise", title: "mise 缓存", icon: "package", selected: true,
			description: "mise 下载的工具安装包和插件缓存，需要时会重新下载",
			dirs:        dirs(filepath.Join(e.caches(), "mise")),
		})
	}

	if e.windows() {
		nuget := filepath.Join(e.localAppData(), "NuGet")
		add(&tool{
			id: "nuget-cache", title: "NuGet 缓存", icon: "package", selected: true,
			description: "NuGet 的下载缓存，还原包时会重新下载",
			dirs:        dirs(filepath.Join(nuget, "v3-cache"), filepath.Join(nuget, "plugins-cache")),
		})
		add(&tool{
			id: "nuget-packages", title: "NuGet 全局包", icon: "package",
			description: "NuGet 还原到本机的全部包",
			warning:     "删除后每个 .NET 项目都要重新还原依赖",
			dirs:        dirs(filepath.Join(e.profile(), ".nuget", "packages")),
		})
	}
	return ts
}

// userCache is a tool's folder in the user cache folder (Go's os.UserCacheDir): ~/Library/Caches on macOS, %LOCALAPPDATA% on Windows.
func (e env) userCache(name string) string {
	return filepath.Join(e.pick(e.caches(), e.localAppData()), name)
}

// pick returns mac on macOS and win on Windows.
func (e env) pick(mac, win string) string {
	if e.windows() {
		return win
	}
	return mac
}

// simctlCommand is xcrun simctl delete unavailable, when Xcode is installed.
func simctlCommand(l located) *execx.Command {
	if l.xcrun == "" {
		return nil
	}
	return &execx.Command{Name: l.xcrun, Args: []string{"simctl", "delete", "unavailable"}, Timeout: cleanTimeout}
}
