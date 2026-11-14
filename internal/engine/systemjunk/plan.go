package systemjunk

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
)

// appBundleID is Spruce's own bundle identifier (mygo.config.ts); its caches are in use while the scan runs.
const appBundleID = "ai.yldm.spruce"

// Ages below which entries are not offered.
const (
	// downloadMinAge: a download untouched for a day is not coming back; a younger one may still be running or resumable.
	downloadMinAge = 24 * time.Hour
	// tempMinAge: a temporary file written within a day may belong to a program or installer that is still running.
	tempMinAge = 24 * time.Hour
)

// incompleteExts are the extensions browsers give a download in progress: Chrome and Edge .crdownload, Firefox .part, Safari .download (a folder), and .partial of Edge Legacy and some download managers.
var incompleteExts = []string{".crdownload", ".part", ".download", ".partial"}

// devCaches are the children of ~/Library/Caches that the 开发者垃圾 page offers, so this page leaves them out (lower case).
var devCaches = []string{
	"homebrew", "pip", "go-build", "yarn", "pnpm", "ms-playwright", "node-gyp", "com.apple.dt.xcode", "jetbrains",
	"cocoapods", "typescript", "electron", "electron-builder", "deno", "bazel", "cypress", "org.swift.swiftpm", "com.github.xcodes",
}

// appleDenylist are children of ~/Library/Caches that are not offered although they are caches, because deleting them breaks something, costs the user work, or is undone at once (lower case):
//
//   - account and identity state (com.apple.akd is AuthKit, appleaccountd, amsaccountsd, ids, identityservicesd, imfoundation): losing it can sign the user out of their Apple ID, iMessage or FaceTime;
//   - iCloud and CloudKit sync state (CloudKit, com.apple.bird is iCloud Drive, cloudd, every com.apple.*cloud*): deleting it makes the daemons re-sync or re-download and can leave sync stuck;
//   - keychain, certificate trust and security caches (trustd, every com.apple.*keychain* and *security*);
//   - Family Sharing (FamilyCircle, familycircled), Home (com.apple.HomeKit, homed), Wallet (passd, PassKit) and Find My (com.apple.findmy*, fmfd);
//   - downloads and purchases in flight (nsurlsessiond holds background transfers, appstoreagent, storekitagent, commerce);
//   - Safari Safe Browsing (com.apple.Safari.SafeBrowsing), which would leave fraud protection without data until it re-downloads;
//   - containermanagerd and cache_delete (CacheDelete), which track sandbox containers and purgeable space themselves;
//   - photoanalysisd and mediaanalysisd, whose results took hours of background analysis to compute;
//   - Messages (com.apple.messages, com.apple.mobilesms), whose caches hold attachment previews of the user's conversations.
var appleDenylist = []string{
	"cloudkit", "familycircle", "familycircled", "passkit",
	"com.apple.akd", "com.apple.appleaccountd", "com.apple.amsaccountsd", "com.apple.ids", "com.apple.identityservicesd", "com.apple.imfoundation",
	"com.apple.bird", "com.apple.cloudd",
	"com.apple.trustd",
	"com.apple.homekit", "com.apple.homed", "com.apple.passd", "com.apple.fmfd",
	"com.apple.nsurlsessiond", "com.apple.appstoreagent", "com.apple.appstore", "com.apple.storekitagent", "com.apple.commerce",
	"com.apple.safari.safebrowsing",
	"com.apple.containermanagerd", "com.apple.cache_delete",
	"com.apple.photoanalysisd", "com.apple.mediaanalysisd",
	"com.apple.messages", "com.apple.mobilesms",
}

// appleDenyWords: any com.apple.* cache whose name contains one of these is left out too (see appleDenylist).
var appleDenyWords = []string{"cloud", "keychain", "security", "findmy", "authkit", "identityservices"}

// appleDenied reports whether the cache folder name (lower case) is on the Apple denylist.
func appleDenied(lower string) bool {
	if slices.Contains(appleDenylist, lower) {
		return true
	}
	if !strings.HasPrefix(lower, "com.apple.") {
		return false
	}
	for _, w := range appleDenyWords {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return strings.HasPrefix(lower, "com.apple.ids.")
}

// ownCache reports whether the cache folder name (lower case) is Spruce's.
func ownCache(lower string) bool {
	return lower == appBundleID || strings.HasPrefix(lower, appBundleID+".")
}

// browser is where one browser keeps its caches.
type browser struct {
	name string
	// running are the bundle IDs (macOS) or executable names (Windows) of the browser.
	running []string
	// chromium are user data folders of a Chromium browser: profiles are the folder itself and its Default, Profile N, Guest Profile and System Profile children.
	chromium []string
	// firefox are Firefox Profiles folders, whose children each have a cache2 folder.
	firefox []string
	// flat are folders whose every child is cache (Safari's).
	flat []string
}

// roots are the browser's folders, which are the allowed roots of its items.
func (b browser) roots() []string {
	return slices.Concat(b.chromium, b.firefox, b.flat)
}

// Cache folders of a Chromium profile, and of the user data folder as a whole (GPU shader caches). Everything else there (Cookies, History, Login Data, Local Storage, IndexedDB, Service Worker …) is the user's data and never offered.
var (
	chromiumProfileCaches = []string{"Cache", "Code Cache", "GPUCache"}
	chromiumSharedCaches  = []string{"GrShaderCache", "ShaderCache", "GraphiteDawnCache"}
)

// isChromiumProfile reports whether a child of a user data folder is a profile.
func isChromiumProfile(name string) bool {
	return name == "Default" || strings.HasPrefix(name, "Profile ") || name == "Guest Profile" || name == "System Profile"
}

// browserCaches finds the cache folders of browsers; the items of a running browser are busy.
func browserCaches(browsers []browser) func(context.Context, *scanCtx) []candidate {
	return func(ctx context.Context, sc *scanCtx) []candidate {
		var out []candidate
		for _, b := range browsers {
			if ctx.Err() != nil {
				return out
			}
			busy := sc.running.anyOf(b.running)
			add := func(path, note string) {
				info, ok := lstatDir(path)
				if !ok {
					return
				}
				if busy {
					note += " 正在运行，请先退出"
				}
				out = append(out, candidate{path: path, info: info, note: note, busy: busy})
			}
			for _, base := range b.chromium {
				if _, ok := lstatDir(base); !ok {
					continue
				}
				for _, name := range chromiumSharedCaches {
					add(filepath.Join(base, name), b.name)
				}
				profiles := []entry{{path: base}}
				for _, e := range sc.entries(base, false) {
					if e.info.IsDir() && isChromiumProfile(e.name) {
						profiles = append(profiles, e)
					}
				}
				for _, p := range profiles {
					note := b.name
					if p.name != "" {
						note += " · " + p.name
					}
					for _, name := range chromiumProfileCaches {
						add(filepath.Join(p.path, name), note)
					}
				}
			}
			for _, dir := range b.firefox {
				for _, e := range sc.entries(dir, false) {
					if e.info.IsDir() {
						add(filepath.Join(e.path, "cache2"), b.name+" · "+e.name)
					}
				}
			}
			for _, dir := range b.flat {
				// Safari's cache folder needs Full Disk Access; without it Safari is left out quietly.
				for _, e := range sc.entries(dir, true) {
					if strings.HasPrefix(e.name, ".") {
						continue
					}
					note := b.name
					if busy {
						note += " 正在运行，请先退出"
					}
					out = append(out, candidate{path: e.path, info: e.info, note: note, busy: busy})
				}
			}
		}
		return out
	}
}

// Category templates shared by both platforms.
func appCachesCategory(desc string, roots []string) cleanup.Category {
	return cleanup.Category{
		ID: catAppCaches, Title: "应用缓存", Description: desc,
		Icon: "cache", Kind: cleanup.KindCache, Deletion: cleanup.DeletePermanent, Selected: true, Roots: roots,
	}
}

func browserCategories(roots []string) (cleanup.Category, *cleanup.Category) {
	main := cleanup.Category{
		ID: catBrowserCaches, Title: "浏览器缓存", Description: "浏览器保存的网页、脚本和图形缓存，不包括 Cookie、历史记录和登录信息",
		Icon: "browser", Kind: cleanup.KindCache, Deletion: cleanup.DeletePermanent, Selected: true, Roots: roots,
	}
	busy := main
	busy.ID, busy.Title, busy.Selected = catBrowserRunning, "正在运行的浏览器的缓存", false
	busy.Warning = "浏览器正在运行，请先退出再清理"
	return main, &busy
}

func incompleteCategory(downloads string) cleanup.Category {
	return cleanup.Category{
		ID: catIncompleteDownload, Title: "未完成的下载", Description: "下载中断后留在「下载」文件夹里的临时文件，超过 1 天没有变化",
		Icon: "file", Kind: cleanup.KindCache, Deletion: cleanup.DeleteTrash, Selected: true, Roots: []string{downloads},
	}
}

// darwinSpecs is the plan on macOS.
func darwinSpecs(env Env) []spec {
	lib := filepath.Join(env.Home, "Library")
	caches := filepath.Join(lib, "Caches")
	support := filepath.Join(lib, "Application Support")
	logs := filepath.Join(lib, "Logs")
	reports := filepath.Join(logs, "DiagnosticReports")

	chromium := func(name, id, rel string) browser {
		return browser{name: name, running: []string{id}, chromium: []string{filepath.Join(caches, rel), filepath.Join(support, rel)}}
	}
	browsers := []browser{
		chromium("Google Chrome", "com.google.Chrome", filepath.Join("Google", "Chrome")),
		chromium("Microsoft Edge", "com.microsoft.edgemac", "Microsoft Edge"),
		chromium("Brave", "com.brave.Browser", filepath.Join("BraveSoftware", "Brave-Browser")),
		chromium("Arc", "company.thebrowser.Browser", filepath.Join("Arc", "User Data")),
		chromium("Opera", "com.operasoftware.Opera", "com.operasoftware.Opera"),
		{name: "Firefox", running: []string{"org.mozilla.firefox"}, firefox: []string{filepath.Join(caches, "Firefox", "Profiles")}},
		{name: "Safari", running: []string{"com.apple.Safari"}, flat: []string{filepath.Join(caches, "com.apple.Safari")}},
	}
	var browserRoots []string
	// The top-level cache folders that hold browser caches belong to 浏览器缓存, not 应用缓存.
	browserOwned := map[string]bool{}
	for _, b := range browsers {
		for _, r := range b.roots() {
			browserRoots = append(browserRoots, r)
			if rel, err := filepath.Rel(caches, r); err == nil && !strings.HasPrefix(rel, "..") {
				browserOwned[strings.ToLower(strings.Split(rel, string(filepath.Separator))[0])] = true
			}
		}
	}

	apps := appCachesCategory("应用为了加快加载保存的临时数据，删除后会在需要时重新生成", []string{caches})
	appsBusy := apps
	appsBusy.ID, appsBusy.Title, appsBusy.Selected = catAppCachesRunning, "正在运行的应用的缓存", false
	appsBusy.Warning = "这些应用正在运行，建议先退出再清理"
	browserMain, browserBusy := browserCategories(browserRoots)

	return []spec{
		{cat: apps, busy: &appsBusy, collect: func(_ context.Context, sc *scanCtx) []candidate {
			var out []candidate
			for _, e := range sc.entries(caches, false) {
				lower := strings.ToLower(e.name)
				if strings.HasPrefix(e.name, ".") || browserOwned[lower] || slices.Contains(devCaches, lower) || appleDenied(lower) || ownCache(lower) {
					continue
				}
				c := candidate{path: e.path, info: e.info}
				if sc.running.owns(e.name) {
					c.busy, c.note = true, "应用正在运行"
				}
				out = append(out, c)
			}
			return out
		}},
		{cat: browserMain, busy: browserBusy, collect: browserCaches(browsers)},
		{cat: cleanup.Category{
			ID: catLogs, Title: "日志", Description: "应用记录的运行日志，通常只在排查问题时用得到",
			Icon: "log", Kind: cleanup.KindLog, Deletion: cleanup.DeletePermanent, Selected: true, Roots: []string{logs},
		}, collect: children([]string{logs}, "", func(sc *scanCtx, e entry) bool {
			return notHidden(sc, e) && !strings.EqualFold(e.name, "DiagnosticReports")
		})},
		{cat: cleanup.Category{
			ID: catCrashReports, Title: "崩溃报告", Description: "应用崩溃或卡住时生成的诊断报告",
			Icon: "crash", Kind: cleanup.KindLog, Deletion: cleanup.DeletePermanent, Selected: true, Roots: []string{reports},
		}, collect: children([]string{reports}, "", notHidden)},
		{cat: incompleteCategory(env.Downloads), collect: incomplete(env.Downloads, incompleteExts, downloadMinAge)},
	}
}

// appCacheWindows is one cache folder under %LOCALAPPDATA% whose children 应用缓存 offers.
type appCacheWindows struct {
	rel  string
	note string
}

// appCachesWindows are the caches 应用缓存 offers on Windows, all rebuilt on demand and the ones Disk Cleanup itself clears: Internet temporary files of WinINet (INetCache) and the GPU shader caches of DirectX, NVIDIA and AMD. Left out on purpose: Microsoft\Windows\Explorer (thumbnail and icon caches, which the 维护 page rebuilds safely), Microsoft\Windows\WebCache (an ESE database that dllhost and taskhostw keep open; deleting files under it corrupts it), INetCookies (cookies), Package Cache (installers that repair and uninstall need), and Packages (Store app data, not only caches). CrashDumps are crash reports and belong to 错误报告.
var appCachesWindows = []appCacheWindows{
	{`Microsoft\Windows\INetCache`, "Internet 临时文件"},
	{`D3DSCache`, "DirectX 着色器缓存"},
	{`NVIDIA\DXCache`, "NVIDIA 着色器缓存"},
	{`NVIDIA\GLCache`, "NVIDIA 着色器缓存"},
	{`AMD\DxCache`, "AMD 着色器缓存"},
	{`AMD\DxcCache`, "AMD 着色器缓存"},
	{`AMD\GLCache`, "AMD 着色器缓存"},
	{`AMD\VkCache`, "AMD 着色器缓存"},
}

// winPath joins a Windows-style relative path (with backslashes) onto base with the platform's separator, so the plan builds the same paths on Windows and in tests on macOS.
func winPath(base, rel string) string {
	return filepath.Join(append([]string{base}, strings.Split(rel, `\`)...)...)
}

// within reports whether p is inside or equal to dir, ignoring case (Windows paths are case-insensitive).
func within(p, dir string) bool {
	rel, err := filepath.Rel(strings.ToLower(dir), strings.ToLower(p))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// tempDirsWindows are %TEMP%, %TMP% and %LOCALAPPDATA%\Temp, deduplicated, and only those inside the user's profile: a temporary folder elsewhere (C:\Windows\Temp when it is set for a service) needs administrator rights.
func tempDirsWindows(env Env, local string) []string {
	var out []string
	for _, d := range []string{env.Getenv("TEMP"), env.Getenv("TMP"), winPath(local, "Temp")} {
		if d == "" || !filepath.IsAbs(d) || local == "" {
			continue
		}
		d = filepath.Clean(d)
		if !within(d, env.Home) || slices.ContainsFunc(out, func(o string) bool { return strings.EqualFold(o, d) }) {
			continue
		}
		out = append(out, d)
	}
	return out
}

// windowsSpecs is the plan on Windows. It reads %LOCALAPPDATA%, %TEMP% and %TMP% through env.Getenv; without LOCALAPPDATA only the downloads are scanned.
func windowsSpecs(env Env) []spec {
	local := env.Getenv("LOCALAPPDATA")
	if local != "" && !filepath.IsAbs(local) {
		local = ""
	}
	var specs []spec
	if local != "" {
		temps := tempDirsWindows(env, local)
		specs = append(specs, spec{cat: cleanup.Category{
			ID: catTempFiles, Title: "临时文件", Description: "应用留下的临时文件，只列出 24 小时内没有改动的，正在使用的文件会自动跳过",
			Icon: "temp", Kind: cleanup.KindCache, Deletion: cleanup.DeletePermanent, Selected: true, Roots: temps,
		}, collect: children(temps, "", olderThan(tempMinAge))})

		var cacheRoots []string
		var collectors []func(context.Context, *scanCtx) []candidate
		for _, ac := range appCachesWindows {
			dir := winPath(local, ac.rel)
			cacheRoots = append(cacheRoots, dir)
			collectors = append(collectors, children([]string{dir}, ac.note, nil))
		}
		specs = append(specs, spec{
			cat:     appCachesCategory("Internet 临时文件和显卡的着色器缓存，删除后会在需要时重新生成", cacheRoots),
			collect: concat(collectors...),
		})

		chromium := func(name, exe, rel string) browser {
			return browser{name: name, running: []string{exe}, chromium: []string{winPath(local, rel)}}
		}
		browsers := []browser{
			chromium("Google Chrome", "chrome.exe", `Google\Chrome\User Data`),
			chromium("Microsoft Edge", "msedge.exe", `Microsoft\Edge\User Data`),
			chromium("Brave", "brave.exe", `BraveSoftware\Brave-Browser\User Data`),
			chromium("Opera", "opera.exe", `Opera Software\Opera Stable`),
			{name: "Firefox", running: []string{"firefox.exe"}, firefox: []string{winPath(local, `Mozilla\Firefox\Profiles`)}},
		}
		var roots []string
		for _, b := range browsers {
			roots = append(roots, b.roots()...)
		}
		main, busy := browserCategories(roots)
		specs = append(specs, spec{cat: main, busy: busy, collect: browserCaches(browsers)})

		archive := winPath(local, `Microsoft\Windows\WER\ReportArchive`)
		queue := winPath(local, `Microsoft\Windows\WER\ReportQueue`)
		dumps := winPath(local, "CrashDumps")
		specs = append(specs, spec{cat: cleanup.Category{
			ID: catErrorReports, Title: "错误报告", Description: "Windows 错误报告和应用崩溃时留下的转储文件",
			Icon: "crash", Kind: cleanup.KindLog, Deletion: cleanup.DeletePermanent, Selected: true, Roots: []string{archive, queue, dumps},
		}, collect: concat(
			children([]string{archive, queue}, "Windows 错误报告", nil),
			children([]string{dumps}, "崩溃转储", nil),
		)})
	}
	specs = append(specs, spec{cat: incompleteCategory(env.Downloads), collect: incomplete(env.Downloads, incompleteExts, downloadMinAge)})
	return specs
}

// concat runs collectors one after another.
func concat(collectors ...func(context.Context, *scanCtx) []candidate) func(context.Context, *scanCtx) []candidate {
	return func(ctx context.Context, sc *scanCtx) []candidate {
		var out []candidate
		for _, c := range collectors {
			out = append(out, c(ctx, sc)...)
		}
		return out
	}
}
