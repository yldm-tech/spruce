package advice

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"spruce/apps/spruce/internal/engine/cleanup"
)

// Card IDs that are not "tool-<id>".
const (
	IDInstallers          = "installers"
	IDIncompleteDownloads = "incomplete-downloads"
	IDAppCaches           = "app-caches"
	IDBrowserCaches       = "browser-caches"
	IDLogs                = "logs"
	IDTempFiles           = "temp-files"
	IDDerivedData         = "tool-xcode-derived-data"
	IDDeviceSupport       = "tool-xcode-device-support"
	IDSimulators          = "tool-xcode-simulators"
	IDProjectsIdle        = "projects-idle"
	IDSmall               = "small"
)

// Sources of a merged scan: the prefix of its category IDs.
const (
	SourceSystemJunk = "system-junk"
	SourceDevJunk    = "dev-junk"
	SourceInstallers = "installers"
)

// Prefixes of the merged category IDs of tool caches and of build output.
const (
	toolPrefix     = SourceDevJunk + ":tool-"
	artifactPrefix = SourceDevJunk + ":artifact-"
)

// template is how one card is worded: its ID, the automatic rule that covers it and build, which fills in the title, reason and consequence of c.
type template struct {
	id    string
	rule  string
	build func(c *card, in Inputs)
}

// fixed are the templates of the categories with a card of their own, by merged category ID.
var fixed = map[string]*template{}

func init() {
	installers := &template{id: IDInstallers, rule: "installers", build: buildInstallers}
	for _, id := range []string{"disk-images", "installer-packages", "archives", "system-images"} {
		fixed[SourceInstallers+":"+id] = installers
	}
	fixed[SourceSystemJunk+":incomplete-downloads"] = &template{id: IDIncompleteDownloads, rule: "incomplete-downloads", build: buildIncomplete}
	fixed[SourceSystemJunk+":app-caches"] = &template{id: IDAppCaches, rule: "app-caches", build: buildAppCaches}
	fixed[SourceSystemJunk+":browser-caches"] = &template{id: IDBrowserCaches, build: buildBrowserCaches}
	logs := &template{id: IDLogs, build: buildLogs}
	for _, id := range []string{"logs", "crash-reports", "error-reports"} {
		fixed[SourceSystemJunk+":"+id] = logs
	}
	fixed[SourceSystemJunk+":temp-files"] = &template{id: IDTempFiles, build: buildTempFiles}
	fixed[toolPrefix+"xcode-derived-data"] = &template{id: IDDerivedData, build: buildDerivedData}
	fixed[toolPrefix+"xcode-device-support"] = &template{id: IDDeviceSupport, build: buildDeviceSupport}
	fixed[toolPrefix+"xcode-simulators"] = &template{id: IDSimulators, build: buildSimulators}
}

// projectsIdle covers every kind of build output, the Trash variants included.
var projectsIdle = &template{id: IDProjectsIdle, rule: "build-output", build: buildProjects}

// small is the card of the categories too small for one of their own.
var small = &template{id: IDSmall, build: buildSmall}

// templateFor returns the card ID and template of a merged category ID; nil for a category no card covers (the Trash itself, apps' caches while they run, recent installers …).
func templateFor(categoryID string) (string, *template) {
	if t := fixed[categoryID]; t != nil {
		return t.id, t
	}
	if strings.HasPrefix(categoryID, artifactPrefix) {
		return projectsIdle.id, projectsIdle
	}
	if id, ok := strings.CutPrefix(categoryID, toolPrefix); ok && id != "" {
		return "tool-" + id, &template{id: "tool-" + id, build: buildTool}
	}
	return "", nil
}

// trashSentence is the consequence of moving to the Trash: undone with 放回原处 on macOS, restored from the Recycle Bin by hand on Windows.
func trashSentence(in Inputs) string {
	if in.Windows {
		return "移到回收站，需要时可以在回收站里还原"
	}
	return "移到废纸篓，需要时可以放回原处"
}

func binName(in Inputs) string {
	if in.Windows {
		return "回收站"
	}
	return "废纸篓"
}

func buildInstallers(c *card, in Inputs) {
	downloads := in.Downloads
	if downloads == "" && in.Home != "" {
		downloads = filepath.Join(in.Home, "Downloads")
	}
	inDownloads, matched := downloads != "", in.InstalledApps != nil
	for _, p := range c.items {
		inDownloads = inDownloads && within(p.item.Path, downloads)
		matched = matched && MatchInstalled(filepath.Base(p.item.Path), in.InstalledApps)
	}
	c.rec.Title = "旧安装包"
	if inDownloads {
		c.rec.Title = "下载里的安装包"
	}
	n := len(c.items)
	if matched {
		c.rec.Reason = fmt.Sprintf("%d 个安装包，对应的应用都已装好", n)
	} else {
		c.rec.Reason = fmt.Sprintf("%d 个安装包，都下载超过 7 天了", n)
	}
	if folders := installerFolders(c, in, downloads); len(folders) > 0 {
		c.rec.Reason += "，在" + strings.Join(folders, "、") + "里"
	}
	c.rec.Consequence = trashSentence(in)
}

// installerFolders names the folders the card's items are in: the installer root holding each (the innermost), else its own folder; 下载, 桌面 and 文稿 by those names, others by their folder name; in the order of the roots.
func installerFolders(c *card, in Inputs, downloads string) []string {
	var roots []string
	for _, p := range c.items {
		root := ""
		for _, r := range in.InstallerRoots {
			if within(p.item.Path, r) && len(r) > len(root) {
				root = r
			}
		}
		if root == "" {
			root = filepath.Dir(p.item.Path)
		}
		root = filepath.Clean(root)
		if !slices.Contains(roots, root) {
			roots = append(roots, root)
		}
	}
	rank := func(r string) int {
		for i, ir := range in.InstallerRoots {
			if filepath.Clean(ir) == r {
				return i
			}
		}
		return len(in.InstallerRoots)
	}
	slices.SortStableFunc(roots, func(a, b string) int { return rank(a) - rank(b) })
	documents := "文稿"
	if in.Windows {
		documents = "文档"
	}
	var names []string
	for _, r := range roots {
		name := filepath.Base(r)
		switch {
		case downloads != "" && r == filepath.Clean(downloads):
			name = "下载"
		case in.Home != "" && r == filepath.Join(in.Home, "Desktop"):
			name = "桌面"
		case in.Home != "" && r == filepath.Join(in.Home, "Documents"):
			name = documents
		}
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}

func buildIncomplete(c *card, in Inputs) {
	c.rec.Title = "未完成的下载"
	c.rec.Reason = fmt.Sprintf("%d 个下载中断后留下的临时文件，超过 1 天没有变化", len(c.items))
	c.rec.Consequence = "这些下载已经无法继续，移到" + binName(in)
}

func buildAppCaches(c *card, _ Inputs) {
	c.rec.Title = "应用缓存"
	c.rec.Reason = fmt.Sprintf("%d 个应用的缓存，最大的是 %s", len(c.items), filepath.Base(c.largest().Path))
	c.rec.Consequence = "直接删除，应用下次打开时会重新生成，第一次可能稍慢"
}

func buildBrowserCaches(c *card, _ Inputs) {
	var names []string
	for _, p := range c.items {
		name, _, _ := strings.Cut(p.item.Note, " · ")
		if name = strings.TrimSpace(name); name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	who := "浏览器"
	if len(names) > 0 {
		who = strings.Join(names, "、")
	}
	c.rec.Title = "浏览器缓存"
	c.rec.Reason = who + "的网页、脚本和图形缓存"
	c.rec.Consequence = "直接删除，不会动 Cookie、历史记录和登录信息；网页第一次打开会慢一点"
}

func buildLogs(c *card, in Inputs) {
	c.rec.Title = "日志和崩溃报告"
	if in.Windows {
		c.rec.Title = "日志和错误报告"
	}
	var oldest cleanup.Item
	for _, p := range c.items {
		if !p.item.ModTime.IsZero() && (oldest.ModTime.IsZero() || p.item.ModTime.Before(oldest.ModTime)) {
			oldest = p.item
		}
	}
	if oldest.ModTime.IsZero() {
		c.rec.Reason = fmt.Sprintf("%d 个文件", len(c.items))
	} else {
		c.rec.Reason = fmt.Sprintf("%d 个文件，最早的来自 %s", len(c.items), timeToken)
		c.rec.ReasonTime, c.rec.ReasonTimeKind = oldest.ModTime, TimeDate
	}
	c.rec.Consequence = "直接删除，只在排查问题时用得到"
}

func buildTempFiles(c *card, _ Inputs) {
	c.rec.Title = "临时文件"
	c.rec.Reason = fmt.Sprintf("%d 项，都超过 24 小时没有改动", len(c.items))
	c.rec.Consequence = "直接删除，正在使用的文件会自动跳过"
}

func buildDerivedData(c *card, in Inputs) {
	c.rec.Title = "Xcode 编译缓存"
	if d := days(in.Now, c.rec.Newest); d < 1 {
		c.rec.Reason = "今天编译过"
	} else {
		c.rec.Reason = fmt.Sprintf("%d 天没编译过", d)
	}
	c.rec.Consequence = "直接删除，下次编译会慢一些，Xcode 会重新建立索引"
}

func buildDeviceSupport(c *card, _ Inputs) {
	c.rec.Title = "设备支持文件"
	c.rec.Reason = fmt.Sprintf("%d 个旧系统版本，每个平台最新的版本会保留", len(c.items))
	c.rec.Consequence = "直接删除，再次连接这些系统版本的真机时会重新复制"
}

func buildSimulators(c *card, _ Inputs) {
	c.rec.Title = "不可用的模拟器"
	note := strings.TrimSpace(c.items[0].item.Note)
	if note == "" {
		note = "不可用的模拟器"
	}
	c.rec.Reason = note + "，系统运行时已不存在，无法再启动"
	c.rec.Consequence = "运行 xcrun simctl delete unavailable，删除后无法恢复"
}

// buildTool words a tool cache: one cleaned by its command, or one whose entries are deleted.
func buildTool(c *card, in Inputs) {
	cat := c.cats[0]
	c.rec.Title = cat.Title
	if cat.Command != "" {
		if c.rec.ID == "tool-homebrew" {
			c.rec.Reason = "下载的安装包和已升级软件的旧版本"
		} else {
			c.rec.Reason = "最近一次使用在 " + timeToken
			c.rec.ReasonTime, c.rec.ReasonTimeKind = c.rec.Newest, TimeRelative
		}
		c.rec.Consequence = "运行 " + cat.Command + "，" + commandEffect(cat.Description)
		if c.rec.ID == "tool-go-build" {
			c.rec.Consequence += "，下次编译会慢一些"
		}
		return
	}
	c.rec.Reason = fmt.Sprintf("%d 项，最近一次使用在 %s", len(c.items), timeToken)
	c.rec.ReasonTime, c.rec.ReasonTimeKind = c.rec.Newest, TimeRelative
	if cat.Deletion == cleanup.DeleteTrash {
		c.rec.Consequence = trashSentence(in)
		return
	}
	_, rest, ok := strings.Cut(cat.Description, "，")
	if !ok {
		rest = cat.Description
	}
	c.rec.Consequence = "直接删除，" + rest
}

// commandEffect is what a command category's description says the command does: the part after 「：」, else after the 「运行 …，」 it starts with.
func commandEffect(desc string) string {
	if _, rest, ok := strings.Cut(desc, "："); ok {
		return rest
	}
	if strings.HasPrefix(desc, "运行") {
		if _, rest, ok := strings.Cut(desc, "，"); ok {
			return rest
		}
	}
	return desc
}

func buildProjects(c *card, in Inputs) {
	sizes := map[string]int64{}
	var order []string
	idle := -1
	trashed := 0
	for _, p := range c.items {
		project := projectOf(p.item)
		if _, ok := sizes[project]; !ok {
			order = append(order, project)
		}
		sizes[project] += p.item.Size
		if !p.item.ModTime.IsZero() {
			if d := days(in.Now, p.item.ModTime); idle < 0 || d < idle {
				idle = d
			}
		}
		if p.cat.Deletion == cleanup.DeleteTrash {
			trashed++
		}
	}
	largest := order[0]
	for _, p := range order[1:] {
		if sizes[p] > sizes[largest] {
			largest = p
		}
	}
	c.rec.Title = "闲置项目的构建产物"
	if idle >= 1 {
		c.rec.Reason = fmt.Sprintf("%d 个项目超过 %d 天没有改动，最大的是 %s", len(order), idle, filepath.Base(largest))
	} else {
		c.rec.Reason = fmt.Sprintf("%d 个项目的构建产物，最大的是 %s", len(order), filepath.Base(largest))
	}
	if n := c.dirtyProjects; n > 0 {
		c.rec.Reason2 = fmt.Sprintf("%d 个有未提交改动的项目没有选入", n)
	}
	c.rec.Consequence = "直接删除 node_modules、target 等文件夹，下次安装依赖或构建时会重新生成"
	if trashed > 0 {
		places := "文稿或桌面"
		if in.Windows {
			places = "文档或桌面"
		}
		c.rec.Consequence += fmt.Sprintf("，其中 %d 个在%s里的会移到%s", trashed, places, binName(in))
	}
}

// projectOf is the project of a build output item: Item.Project, or for a scan without it the folder that holds it.
func projectOf(it cleanup.Item) string {
	if it.Project != "" {
		return it.Project
	}
	return filepath.Dir(it.Path)
}

func buildSmall(c *card, in Inputs) {
	c.rec.Title = "其他小项"
	c.rec.Reason = fmt.Sprintf("%d 类缓存和日志，每类都不到 100 MB", len(c.cats))
	// Caches come back on demand; logs and crash reports do not.
	isLog := func(cat *cleanup.Category) bool { return cat.Kind == cleanup.KindLog }
	logs := slices.ContainsFunc(c.cats, isLog)
	onlyLogs := logs && !slices.ContainsFunc(c.cats, func(cat *cleanup.Category) bool { return !isLog(cat) })
	switch {
	case c.rec.Action == ActionTrash:
		c.rec.Consequence = trashSentence(in)
	case c.rec.Action == ActionDelete && onlyLogs:
		c.rec.Consequence = "直接删除，只在排查问题时用得到"
	case c.rec.Action == ActionDelete && logs:
		c.rec.Consequence = "直接删除，缓存会在需要时重新生成，日志不会"
	case c.rec.Action == ActionDelete:
		c.rec.Consequence = "直接删除，都会在需要时重新生成"
	case logs:
		c.rec.Consequence = "缓存和日志直接删除，缓存会在需要时重新生成；其余移到" + binName(in)
	default:
		c.rec.Consequence = "缓存直接删除，会在需要时重新生成；其余移到" + binName(in)
	}
}
