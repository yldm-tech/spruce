package advice

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
)

const (
	mb = 1_000_000
	gb = 1000 * mb
)

var now = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

func it(path string, size int64, selected bool) cleanup.Item {
	return cleanup.Item{ID: cleanup.ItemID(path), Path: path, Size: size, Selected: selected, ModTime: now.Add(-60 * 24 * time.Hour)}
}

func cat(id string, del cleanup.DeletionMode, items ...cleanup.Item) cleanup.Category {
	src, _, _ := strings.Cut(id, ":")
	return cleanup.Category{ID: id, Title: id, Icon: "icon-" + id, Kind: cleanup.KindCache, Deletion: del, Source: src, Items: items}
}

func with(i cleanup.Item, f func(*cleanup.Item)) cleanup.Item {
	f(&i)
	return i
}

// fullScan is a merged scan with something of every kind of card.
func fullScan() cleanup.ScanResult {
	trashCat := cat("system-junk:trash", cleanup.DeletePermanent, it("/u/.Trash", 9*gb, true))
	trashCat.Kind = cleanup.KindTrash
	npm := cat("dev-junk:tool-npm", cleanup.DeletePermanent, with(it("/u/.npm/_cacache", 500*mb, true), func(i *cleanup.Item) { i.Newest = now.Add(-3 * time.Hour) }))
	npm.Command, npm.Title, npm.Description = "npm cache clean --force", "npm 缓存", "运行 npm cache clean --force，清空 npm 下载的包"
	pnpm := cat("dev-junk:tool-pnpm", cleanup.DeletePermanent, it("/u/pnpm/store", 20*mb, true))
	pnpm.Command, pnpm.Title, pnpm.Description = "pnpm store prune", "pnpm 存储", "运行 pnpm store prune，只删除没有任何项目再引用的包；实际释放的空间可能小于显示的大小"
	derived := cat("dev-junk:tool-xcode-derived-data", cleanup.DeletePermanent, with(it("/u/Library/Developer/Xcode/DerivedData", 17*gb, true), func(i *cleanup.Item) {
		i.Newest = now.Add(-40*24*time.Hour - time.Hour)
	}))
	yarn := cat("dev-junk:tool-yarn", cleanup.DeletePermanent, it("/u/Library/Caches/Yarn/v6", 50*mb, true))
	yarn.Description = "Yarn 下载的包，安装依赖时会重新下载"
	art := func(project, name string, size int64, idle time.Duration, git cleanup.GitState, hold string) cleanup.Item {
		i := it(filepath.Join(project, name), size, hold == "")
		i.Project, i.Git, i.Hold, i.ModTime = project, git, hold, now.Add(-idle)
		return i
	}
	return cleanup.NewResult([]cleanup.Category{
		cat("system-junk:app-caches", cleanup.DeletePermanent,
			it("/u/Library/Caches/com.spotify.client", 300*mb, true),
			it("/u/Library/Caches/com.apple.Safari", 200*mb, true),
			with(it("/u/Library/Caches/held", 5*gb, true), func(i *cleanup.Item) { i.Hold = "不能动" }),
			it("/u/Library/Caches/off", 4*gb, false)),
		cat("system-junk:app-caches-running", cleanup.DeletePermanent, it("/u/Library/Caches/com.running", 3*gb, false)),
		cat("system-junk:browser-caches", cleanup.DeletePermanent,
			with(it("/u/Library/Caches/Google/Chrome/Default/Cache", 100*mb, true), func(i *cleanup.Item) { i.Note = "Chrome · Default" }),
			with(it("/u/Library/Caches/Google/Chrome/Profile 1/Cache", 40*mb, true), func(i *cleanup.Item) { i.Note = "Chrome · Profile 1" }),
			with(it("/u/Library/Caches/com.apple.Safari/WebKitCache", 30*mb, true), func(i *cleanup.Item) { i.Note = "Safari" })),
		cat("system-junk:logs", cleanup.DeletePermanent, with(it("/u/Library/Logs/a.log", 50*mb, true), func(i *cleanup.Item) { i.ModTime = now.Add(-200 * 24 * time.Hour) })),
		cat("system-junk:crash-reports", cleanup.DeletePermanent, it("/u/Library/Logs/DiagnosticReports/x.ips", 30*mb, true)),
		cat("system-junk:incomplete-downloads", cleanup.DeleteTrash, it("/u/Downloads/big.crdownload", 120*mb, true)),
		trashCat,
		cat("installers:disk-images", cleanup.DeleteTrash, it("/u/Downloads/Firefox 120.0.dmg", 150*mb, true), it("/u/Downloads/sub/VSCode-darwin.zip.dmg", 100*mb, true)),
		cat("installers:installer-packages", cleanup.DeleteTrash, it("/u/Desktop/Docker.pkg", 600*mb, true)),
		cat("installers:recent-installers", cleanup.DeleteTrash, it("/u/Downloads/new.dmg", 900*mb, false)),
		derived, npm, pnpm, yarn,
		cat("dev-junk:artifact-node-modules", cleanup.DeletePermanent,
			art("/ws/a", "node_modules", 1*gb, 100*24*time.Hour, cleanup.GitClean, ""),
			art("/ws/b", "node_modules", 600*mb, 45*24*time.Hour, cleanup.GitNone, ""),
			art("/ws/c", "node_modules", 8*gb, 200*24*time.Hour, cleanup.GitDirty, "项目有未提交的改动")),
		cat("dev-junk:artifact-web-build", cleanup.DeletePermanent, art("/ws/a", ".next", 200*mb, 100*24*time.Hour, cleanup.GitClean, "")),
		cat("dev-junk:artifact-node-modules-trash", cleanup.DeleteTrash, art("/u/Documents/d", "node_modules", 300*mb, 50*24*time.Hour, cleanup.GitNone, "")),
	}, now)
}

func byID(recs []Recommendation) map[string]Recommendation {
	m := map[string]Recommendation{}
	for _, r := range recs {
		m[r.ID] = r
	}
	return m
}

func ids(recs []Recommendation) []string {
	var out []string
	for _, r := range recs {
		out = append(out, r.ID)
	}
	return out
}

func TestDerive(t *testing.T) {
	in := Inputs{Now: now, Home: "/u", InstallerRoots: []string{"/u/Downloads", "/u/Desktop"}}
	recs := Derive(fullScan(), in)
	got := ids(recs)
	// Score order (bytes × weight), the command card under 100 MB kept, the small ones last.
	want := []string{IDDerivedData, IDProjectsIdle, IDInstallers, IDAppCaches, "tool-npm", IDBrowserCaches, IDIncompleteDownloads, "tool-pnpm", IDSmall}
	if !slices.Equal(got, want) {
		t.Fatalf("cards %v, want %v", got, want)
	}
	m := byID(recs)

	ac := m[IDAppCaches]
	if ac.Bytes != 500*mb || ac.Count != 2 || ac.Action != ActionDelete || ac.Rule != "app-caches" || ac.Source != SourceSystemJunk || ac.Icon != "icon-system-junk:app-caches" {
		t.Errorf("app caches %+v", ac)
	}
	if ac.Reason != "2 个应用的缓存，最大的是 com.spotify.client" || ac.Consequence != "直接删除，应用下次打开时会重新生成，第一次可能稍慢" || ac.Title != "应用缓存" {
		t.Errorf("app caches wording %q %q", ac.Reason, ac.Consequence)
	}
	for _, r := range ac.Items {
		if r.ItemID == cleanup.ItemID("/u/Library/Caches/held") || r.ItemID == cleanup.ItemID("/u/Library/Caches/off") {
			t.Errorf("held or unchecked item on the card: %v", r)
		}
	}
	if ac.Score != Score(500*mb, ActionDelete) {
		t.Errorf("score %v", ac.Score)
	}

	if b := m[IDBrowserCaches]; b.Reason != "Chrome、Safari的网页、脚本和图形缓存" || b.Rule != "" || b.Count != 3 {
		t.Errorf("browser %+v", b)
	}
	if d := m[IDIncompleteDownloads]; d.Action != ActionTrash || d.Rule != "incomplete-downloads" || d.Reason != "1 个下载中断后留下的临时文件，超过 1 天没有变化" || d.Consequence != "这些下载已经无法继续，移到废纸篓" {
		t.Errorf("incomplete %+v", d)
	}

	ins := m[IDInstallers]
	if ins.Title != "旧安装包" || ins.Reason != "3 个安装包，都下载超过 7 天了，在下载、桌面里" || ins.Consequence != "移到废纸篓，需要时可以放回原处" || ins.Action != ActionTrash || ins.Rule != "installers" || ins.Source != SourceInstallers {
		t.Errorf("installers %+v", ins)
	}
	if !slices.Equal(ins.Categories, []string{"installers:disk-images", "installers:installer-packages"}) || ins.Bytes != 850*mb {
		t.Errorf("installers categories %v bytes %d", ins.Categories, ins.Bytes)
	}

	dd := m[IDDerivedData]
	if dd.Title != "Xcode 编译缓存" || dd.Reason != "40 天没编译过" || dd.Consequence != "直接删除，下次编译会慢一些，Xcode 会重新建立索引" || dd.Source != SourceDevJunk {
		t.Errorf("derived data %+v", dd)
	}

	npm := m["tool-npm"]
	if npm.Action != ActionCommand || npm.Command != "npm cache clean --force" || npm.Title != "npm 缓存" || npm.Reason != "最近一次使用在 {time}" || npm.ReasonTimeKind != TimeRelative || !npm.ReasonTime.Equal(now.Add(-3*time.Hour)) {
		t.Errorf("npm %+v", npm)
	}
	if npm.Consequence != "运行 npm cache clean --force，清空 npm 下载的包" {
		t.Errorf("npm consequence %q", npm.Consequence)
	}
	if p := m["tool-pnpm"]; p.Consequence != "运行 pnpm store prune，只删除没有任何项目再引用的包；实际释放的空间可能小于显示的大小" || p.Bytes != 20*mb {
		t.Errorf("pnpm %+v", p)
	}

	pi := m[IDProjectsIdle]
	if pi.Action != ActionMixed || pi.Rule != "build-output" || pi.Count != 4 || pi.Bytes != 2100*mb {
		t.Errorf("projects %+v", pi)
	}
	if pi.Reason != "3 个项目超过 45 天没有改动，最大的是 a" || pi.Reason2 != "1 个有未提交改动的项目没有选入" {
		t.Errorf("projects reason %q / %q", pi.Reason, pi.Reason2)
	}
	if pi.Consequence != "直接删除 node_modules、target 等文件夹，下次安装依赖或构建时会重新生成，其中 1 个在文稿或桌面里的会移到废纸篓" {
		t.Errorf("projects consequence %q", pi.Consequence)
	}
	if !slices.Equal(pi.Categories, []string{"dev-junk:artifact-node-modules", "dev-junk:artifact-web-build", "dev-junk:artifact-node-modules-trash"}) {
		t.Errorf("projects categories %v", pi.Categories)
	}

	sm := m[IDSmall]
	if !slices.Equal(sm.Categories, []string{"system-junk:logs", "system-junk:crash-reports", "dev-junk:tool-yarn"}) || sm.Bytes != 130*mb || sm.Action != ActionDelete {
		t.Errorf("small %+v", sm)
	}
	if sm.Title != "其他小项" || sm.Reason != "3 类缓存和日志，每类都不到 100 MB" || sm.Consequence != "直接删除，都会在需要时重新生成" || sm.Source != SourceSystemJunk {
		t.Errorf("small wording %+v", sm)
	}

	var total int64
	for _, r := range recs {
		total += r.Bytes
		if r.Bytes <= 0 || len(r.Items) != r.Count || r.Score != Score(r.Bytes, r.Action) {
			t.Errorf("%s: bytes %d items %d count %d score %v", r.ID, r.Bytes, len(r.Items), r.Count, r.Score)
		}
	}
	if Headline(recs) != total || Headline(nil) != 0 {
		t.Errorf("headline %d, want %d", Headline(recs), total)
	}
	for _, r := range recs {
		if slices.Contains(r.Categories, "system-junk:trash") || slices.Contains(r.Categories, "installers:recent-installers") || slices.Contains(r.Categories, "system-junk:app-caches-running") {
			t.Errorf("%s takes a category no card covers: %v", r.ID, r.Categories)
		}
	}
}

func TestDeriveInstallersInDownloadsAndInstalled(t *testing.T) {
	res := cleanup.NewResult([]cleanup.Category{
		cat("installers:disk-images", cleanup.DeleteTrash, it("/u/Downloads/Firefox 120.0.dmg", 150*mb, true), it("/u/Downloads/old/VSCode-darwin-universal.dmg", 100*mb, true)),
	}, now)
	apps := []string{"Firefox", "Visual Studio Code"}
	recs := Derive(res, Inputs{Now: now, Home: "/u", InstalledApps: apps, InstallerRoots: []string{"/u/Downloads", "/u/Desktop"}})
	if len(recs) != 1 || recs[0].Title != "下载里的安装包" || recs[0].Reason != "2 个安装包，对应的应用都已装好，在下载里" {
		t.Fatalf("recs %+v", recs)
	}
	// The installed apps could not be read: the fallback reason.
	recs = Derive(res, Inputs{Now: now, Home: "/u", InstallerRoots: []string{"/u/Downloads"}})
	if recs[0].Reason != "2 个安装包，都下载超过 7 天了，在下载里" {
		t.Errorf("reason without apps %q", recs[0].Reason)
	}
	// One installer of an app that is not installed.
	recs = Derive(res, Inputs{Now: now, Home: "/u", InstalledApps: []string{"Firefox"}, Downloads: "/u/Downloads", InstallerRoots: []string{"/u/Downloads"}})
	if recs[0].Reason != "2 个安装包，都下载超过 7 天了，在下载里" || recs[0].Title != "下载里的安装包" {
		t.Errorf("one unmatched %+v", recs[0])
	}
	// An installer outside the roots is named by its own folder; Windows words the Trash its way.
	res = cleanup.NewResult([]cleanup.Category{cat("installers:installer-packages", cleanup.DeleteTrash, it(`/mnt/share/Setup.exe`, 300*mb, true), it("/u/Documents/x.msi", 200*mb, true))}, now)
	recs = Derive(res, Inputs{Now: now, Home: "/u", Windows: true, InstallerRoots: []string{"/u/Documents"}})
	if recs[0].Title != "旧安装包" || recs[0].Reason != "2 个安装包，都下载超过 7 天了，在文档、share里" || recs[0].Consequence != "移到回收站，需要时可以在回收站里还原" {
		t.Errorf("windows %+v", recs[0])
	}
}

func TestDeriveDayBoundaries(t *testing.T) {
	derived := func(age time.Duration) Recommendation {
		res := cleanup.NewResult([]cleanup.Category{cat("dev-junk:tool-xcode-derived-data", cleanup.DeletePermanent, with(it("/u/DD", gb, true), func(i *cleanup.Item) {
			i.ModTime = now.Add(-500 * 24 * time.Hour)
			i.Newest = now.Add(-age)
		}))}, now)
		return Derive(res, Inputs{Now: now})[0]
	}
	for age, want := range map[time.Duration]string{
		0:                             "今天编译过",
		23*time.Hour + 59*time.Minute: "今天编译过",
		24 * time.Hour:                "1 天没编译过",
		47 * time.Hour:                "1 天没编译过",
		48 * time.Hour:                "2 天没编译过",
		40 * 24 * time.Hour:           "40 天没编译过",
	} {
		if got := derived(age).Reason; got != want {
			t.Errorf("newest %v ago: %q, want %q", age, got, want)
		}
	}
	// The idle days of build output count from the project's activity (ModTime), whole days only.
	res := cleanup.NewResult([]cleanup.Category{cat("dev-junk:artifact-rust-target", cleanup.DeletePermanent,
		with(it("/ws/r/target", gb, true), func(i *cleanup.Item) { i.Project, i.ModTime = "/ws/r", now.Add(-90*24*time.Hour+time.Minute) }))}, now)
	if got := Derive(res, Inputs{Now: now})[0].Reason; got != "1 个项目超过 89 天没有改动，最大的是 r" {
		t.Errorf("projects reason %q", got)
	}
}

func TestDeriveOtherCards(t *testing.T) {
	logs := cat("system-junk:error-reports", cleanup.DeletePermanent, with(it(`/w/WER/a`, 300*mb, true), func(i *cleanup.Item) { i.ModTime = now.Add(-30 * 24 * time.Hour) }))
	temps := cat("system-junk:temp-files", cleanup.DeletePermanent, it(`/w/Temp/a`, 200*mb, true), it(`/w/Temp/b`, 100*mb, true))
	support := cat("dev-junk:tool-xcode-device-support", cleanup.DeletePermanent, it("/u/DS/16.0", 2*gb, true), it("/u/DS/17.0", 2*gb, false))
	sims := cat("dev-junk:tool-xcode-simulators", cleanup.DeletePermanent, with(it("/u/Devices", 3*gb, true), func(i *cleanup.Item) { i.Note = "2 台不可用的模拟器" }))
	sims.Command = "xcrun simctl delete unavailable"
	brew := cat("dev-junk:tool-homebrew", cleanup.DeletePermanent, it("/u/Library/Caches/Homebrew", 4*gb, true))
	brew.Command, brew.Description = "brew cleanup -s --prune=all", "运行 brew cleanup -s --prune=all：删除下载缓存，以及已升级软件的旧版本"
	gob := cat("dev-junk:tool-go-build", cleanup.DeletePermanent, it("/u/go-build", 5*gb, true))
	gob.Command, gob.Description = "go clean -cache", "运行 go clean -cache，清空 Go 的编译缓存"
	archives := cat("dev-junk:tool-xcode-archives", cleanup.DeleteTrash, it("/u/Archives/a", gb, true))
	jb := cat("dev-junk:tool-jetbrains", cleanup.DeletePermanent, it("/u/JetBrains/GoLand2023.3", gb, true))
	jb.Title, jb.Description = "JetBrains IDE 缓存", "IDE 的索引和缓存；旧版本的默认选中，每个 IDE 最新版本的默认保留"
	unknown := cat("system-junk:something-new", cleanup.DeletePermanent, it("/u/x", 9*gb, true))
	res := cleanup.NewResult([]cleanup.Category{logs, temps, support, sims, brew, gob, archives, jb, unknown}, now)
	m := byID(Derive(res, Inputs{Now: now, Windows: true}))
	if len(m) != 8 {
		t.Errorf("cards %v", m)
	}
	if l := m[IDLogs]; l.Title != "日志和错误报告" || l.Reason != "1 个文件，最早的来自 {time}" || l.ReasonTimeKind != TimeDate || !l.ReasonTime.Equal(now.Add(-30*24*time.Hour)) || l.Consequence != "直接删除，只在排查问题时用得到" {
		t.Errorf("logs %+v", l)
	}
	if tf := m[IDTempFiles]; tf.Reason != "2 项，都超过 24 小时没有改动" || tf.Consequence != "直接删除，正在使用的文件会自动跳过" {
		t.Errorf("temp %+v", tf)
	}
	if ds := m[IDDeviceSupport]; ds.Reason != "1 个旧系统版本，每个平台最新的版本会保留" || ds.Count != 1 || ds.Consequence != "直接删除，再次连接这些系统版本的真机时会重新复制" {
		t.Errorf("device support %+v", ds)
	}
	if s := m[IDSimulators]; s.Action != ActionCommand || s.Reason != "2 台不可用的模拟器，系统运行时已不存在，无法再启动" || s.Consequence != "运行 xcrun simctl delete unavailable，删除后无法恢复" {
		t.Errorf("simulators %+v", s)
	}
	if b := m["tool-homebrew"]; b.Reason != "下载的安装包和已升级软件的旧版本" || b.Consequence != "运行 brew cleanup -s --prune=all，删除下载缓存，以及已升级软件的旧版本" || b.ReasonTime != (time.Time{}) {
		t.Errorf("homebrew %+v", b)
	}
	if g := m["tool-go-build"]; g.Consequence != "运行 go clean -cache，清空 Go 的编译缓存，下次编译会慢一些" {
		t.Errorf("go build %q", g.Consequence)
	}
	if a := m["tool-xcode-archives"]; a.Action != ActionTrash || a.Consequence != "移到回收站，需要时可以在回收站里还原" || a.Reason != "1 项，最近一次使用在 {time}" {
		t.Errorf("archives %+v", a)
	}
	if j := m["tool-jetbrains"]; j.Consequence != "直接删除，每个 IDE 最新版本的默认保留" || j.Title != "JetBrains IDE 缓存" {
		t.Errorf("jetbrains %+v", j)
	}
}

func TestDeriveSmallAndOrder(t *testing.T) {
	// Equal scores sort by title; a small card of trash items reads as undoable; a mixed one says both.
	res := cleanup.NewResult([]cleanup.Category{
		cat("dev-junk:tool-b", cleanup.DeletePermanent, it("/b", gb, true)),
		cat("dev-junk:tool-a", cleanup.DeletePermanent, it("/a", gb, true)),
		cat("system-junk:incomplete-downloads", cleanup.DeleteTrash, it("/u/Downloads/x.download", 10*mb, true)),
	}, now)
	recs := Derive(res, Inputs{Now: now})
	if got := ids(recs); !slices.Equal(got, []string{"tool-a", "tool-b", IDSmall}) {
		t.Fatalf("order %v", got)
	}
	if s := recs[2]; s.Action != ActionTrash || s.Consequence != "移到废纸篓，需要时可以放回原处" || s.Reason != "1 类缓存和日志，每类都不到 100 MB" || s.Rule != "" {
		t.Errorf("small %+v", s)
	}
	res.Categories = append(res.Categories, cat("system-junk:logs", cleanup.DeletePermanent, it("/u/Library/Logs/x", 5*mb, true)))
	recs = Derive(res, Inputs{Now: now})
	if s := recs[len(recs)-1]; s.Action != ActionMixed || s.Consequence != "缓存和日志直接删除，都会在需要时重新生成；其余移到废纸篓" {
		t.Errorf("mixed small %+v", s)
	}
	// Nothing recommended: no cards, not nil.
	if recs := Derive(cleanup.NewResult(nil, now), Inputs{Now: now}); recs == nil || len(recs) != 0 {
		t.Errorf("empty scan %v", recs)
	}
	// Zero bytes: no card.
	if recs := Derive(cleanup.NewResult([]cleanup.Category{cat("system-junk:app-caches", cleanup.DeletePermanent, it("/z", 0, true))}, now), Inputs{Now: now}); len(recs) != 0 {
		t.Errorf("zero bytes %v", recs)
	}
}

func TestReasonWith(t *testing.T) {
	r := Recommendation{Reason: "3 个文件，最早的来自 {time}", ReasonTime: now, ReasonTimeKind: TimeDate}
	got := r.ReasonWith(func(tm time.Time, k TimeKind) string { return fmt.Sprintf("%s/%d", k, tm.Day()) })
	if got != "3 个文件，最早的来自 date/14" {
		t.Errorf("ReasonWith %q", got)
	}
	plain := Recommendation{Reason: "今天编译过"}
	if plain.ReasonWith(func(time.Time, TimeKind) string { return "x" }) != "今天编译过" || r.ReasonWith(nil) != r.Reason {
		t.Error("ReasonWith without a token or a formatter")
	}
}

func TestDrill(t *testing.T) {
	res := fullScan()
	got := Drill(res, SourceInstallers)
	var gotIDs []string
	for _, c := range got {
		gotIDs = append(gotIDs, c.ID)
	}
	if !slices.Equal(gotIDs, []string{"installers:disk-images", "installers:installer-packages", "installers:recent-installers"}) {
		t.Errorf("drill %v", gotIDs)
	}
	// Without Source the ID prefix decides.
	res.Categories[0].Source = ""
	if d := Drill(res, SourceSystemJunk); len(d) == 0 || d[0].ID != "system-junk:app-caches" {
		t.Errorf("drill without Source %v", d)
	}
	if d := Drill(res, "nothing"); d == nil || len(d) != 0 {
		t.Errorf("drill of an unknown source %v", d)
	}
}

func TestKnownID(t *testing.T) {
	for _, id := range []string{IDInstallers, IDIncompleteDownloads, IDAppCaches, IDBrowserCaches, IDLogs, IDTempFiles, IDDerivedData, IDDeviceSupport, IDSimulators, IDProjectsIdle, IDSmall, "tool-npm", "tool-homebrew", "tool-nuget-cache"} {
		if !KnownID(id) {
			t.Errorf("KnownID(%q) = false", id)
		}
	}
	for _, id := range []string{"", "tool-", "tool-nope", "app-caches-running", "installers:disk-images", "trash", "Small"} {
		if KnownID(id) {
			t.Errorf("KnownID(%q) = true", id)
		}
	}
}

func TestMatchInstalled(t *testing.T) {
	apps := []string{"Firefox", "Visual Studio Code", "Docker", "zoom.us", "Go", "IntelliJ IDEA", "iTerm", "OBS Studio", "微信"}
	yes := []string{"Firefox 120.0.dmg", "firefox-setup.exe", "VSCode-darwin-universal.zip", "Docker.dmg", "zoomusInstallerFull.pkg", "Zoom.pkg", "go1.22.darwin-arm64.pkg", "ideaIU-2024.2.dmg", "iTerm2-3_5_0.zip", "OBS-Studio-30.1-macOS.dmg", "visual studio code.dmg", "微信 3.8.dmg", "WeChatMac.dmg"}
	no := []string{"GoogleChrome.dmg", "Firefoxy.dmg", "Slack-4.41.dmg", "Dockerfile.zip", "zoomer.dmg", "OBS.dmg", "", ".dmg"}
	for _, f := range yes {
		if !MatchInstalled(f, apps) {
			t.Errorf("%q should match", f)
		}
	}
	for _, f := range no {
		if MatchInstalled(f, apps) {
			t.Errorf("%q should not match", f)
		}
	}
	if MatchInstalled("Firefox.dmg", nil) || MatchInstalled("Firefox.dmg", []string{"", "  "}) {
		t.Error("matched without apps")
	}
	if extOf("node-v20.tar.gz") != ".tar.gz" || extOf("a.dmg") != ".dmg" || extOf("noext") != "" || extOf(".hidden") != "" {
		t.Error("extOf")
	}
}
