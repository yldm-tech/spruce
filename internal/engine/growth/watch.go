package growth

import (
	"path/filepath"
	"strconv"
	"strings"
)

// known is a watched location's friendly name and tile by Watch.ID; Diff names what grew from it, since snapshots keep only IDs and paths.
type known struct {
	label, icon string
	// children names some children of the location by folder name.
	children map[string]string
	// appData marks the folders of other apps' data, read only with Full Disk Access.
	appData bool
}

// knownIDs are the watches DefaultWatches makes, by ID; a project root ("dev-root:<n>") is not among them.
var knownIDs = map[string]known{
	"library-caches":              {icon: "cache"},
	"library-application-support": {icon: "folder"},
	"library-developer": {icon: "xcode", children: map[string]string{
		"Xcode": "Xcode", "CoreSimulator": "模拟器",
	}},
	"xcode-derived-data":       {label: "Xcode 编译缓存", icon: "xcode"},
	"library-containers":       {icon: "folder", appData: true},
	"library-group-containers": {icon: "folder", appData: true},
	"downloads":                {label: "下载", icon: "installer"},
	"desktop":                  {label: "桌面", icon: "folder"},
	"documents":                {label: "文稿", icon: "folder"},
	"movies":                   {label: "影片", icon: "video"},
	"music":                    {label: "音乐", icon: "folder"},
	"pictures":                 {label: "图片", icon: "folder"},
	"trash":                    {label: "废纸篓", icon: "trash"},
	"npm":                      {label: "npm 缓存", icon: "node"},
	"cargo":                    {label: "Cargo", icon: "package"},
	"go":                       {label: "Go", icon: "package"},
	"gradle":                   {label: "Gradle", icon: "package"},
	"maven":                    {label: "Maven", icon: "package"},
	"nuget":                    {label: "NuGet", icon: "package"},
	"local-appdata":            {icon: "folder"},
	"roaming-appdata":          {icon: "folder"},
	"temp":                     {label: "临时文件", icon: "temp"},
}

// devRootPrefix starts the ID of a project root's watch: "dev-root:0", "dev-root:1" …
const devRootPrefix = "dev-root:"

// describe returns the label and tile of the location id, or of its child named child ("" for the location itself). A child the table does not name has no label and the location's tile.
func describe(id, child string) (label, icon string) {
	k, ok := knownIDs[id]
	switch {
	case ok:
		icon = k.icon
	case strings.HasPrefix(id, devRootPrefix):
		icon = "build"
	default:
		icon = "folder"
	}
	if child == "" {
		return k.label, icon
	}
	return k.children[child], icon
}

// AppDataWatch reports whether w is a folder of other apps' data (~/Library/Containers, ~/Library/Group Containers), which Take reads only with Full Disk Access.
func AppDataWatch(w Watch) bool { return knownIDs[w.ID].appData }

// watch makes the Watch id of path, named from the table.
func watch(id, path string, children bool) Watch {
	label, icon := describe(id, "")
	return Watch{ID: id, Label: label, Icon: icon, Path: path, Children: children}
}

// DefaultWatches are the locations snapshots measure on goos for home: the caches, app data and developer folders, the user's folders, the tools' caches and the project roots (devRoots, from settings). It does no I/O: Take leaves out what does not exist. A project root that is the home folder or holds it is left out (it would measure everything), and so is one listed twice. Pure.
func DefaultWatches(goos, home string, getenv func(string) string, devRoots []string) []Watch {
	if home == "" {
		return nil
	}
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	in := func(parts ...string) string { return filepath.Join(append([]string{home}, parts...)...) }
	var out []Watch
	if goos == "windows" {
		local := orDefault(getenv("LOCALAPPDATA"), in("AppData", "Local"))
		roaming := orDefault(getenv("APPDATA"), in("AppData", "Roaming"))
		temp := orDefault(getenv("TEMP"), filepath.Join(local, "Temp"))
		profile := orDefault(getenv("USERPROFILE"), home)
		out = append(out,
			watch("local-appdata", local, true),
			watch("roaming-appdata", roaming, true),
			watch("temp", temp, false),
			watch("downloads", in("Downloads"), true),
			watch("desktop", in("Desktop"), true),
			watch("documents", in("Documents"), true),
			watch("nuget", filepath.Join(profile, ".nuget"), false),
			watch("gradle", filepath.Join(profile, ".gradle"), false),
			watch("maven", filepath.Join(profile, ".m2"), false),
			watch("cargo", filepath.Join(profile, ".cargo"), false),
			watch("go", filepath.Join(profile, "go"), false),
		)
	} else {
		out = append(out,
			watch("library-caches", in("Library", "Caches"), true),
			watch("library-application-support", in("Library", "Application Support"), true),
			watch("library-developer", in("Library", "Developer"), true),
			watch("xcode-derived-data", in("Library", "Developer", "Xcode", "DerivedData"), false),
			watch("library-containers", in("Library", "Containers"), true),
			watch("library-group-containers", in("Library", "Group Containers"), true),
			watch("downloads", in("Downloads"), true),
			watch("desktop", in("Desktop"), true),
			watch("documents", in("Documents"), true),
			watch("movies", in("Movies"), false),
			watch("music", in("Music"), false),
			watch("pictures", in("Pictures"), false),
			watch("trash", in(".Trash"), false),
			watch("npm", in(".npm"), false),
			watch("cargo", in(".cargo"), false),
			watch("go", in("go"), false),
			watch("gradle", in(".gradle"), false),
			watch("maven", in(".m2"), false),
		)
	}
	cleanHome := filepath.Clean(home)
	seen := map[string]bool{}
	for _, r := range devRoots {
		if r == "" {
			continue
		}
		r = filepath.Clean(r)
		if seen[r] || within(cleanHome, r) {
			continue
		}
		seen[r] = true
		out = append(out, watch(devRootPrefix+strconv.Itoa(len(seen)-1), r, true))
	}
	return out
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return filepath.Clean(v)
}

// within reports whether path is dir or lies inside it.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
