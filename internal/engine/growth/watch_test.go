package growth

import (
	"path/filepath"
	"testing"
)

func byID(ws []Watch) map[string]Watch {
	out := map[string]Watch{}
	for _, w := range ws {
		out[w.ID] = w
	}
	return out
}

func TestDefaultWatchesMac(t *testing.T) {
	home := filepath.FromSlash("/Users/me")
	in := func(p string) string { return filepath.Join(home, filepath.FromSlash(p)) }
	ws := DefaultWatches("darwin", home, nil, []string{in("code"), in("code"), "", home, filepath.Dir(home), in("Documents/work")})
	got := byID(ws)
	want := map[string]struct {
		path, label, icon string
		children          bool
	}{
		"library-caches":              {in("Library/Caches"), "", "cache", true},
		"library-application-support": {in("Library/Application Support"), "", "folder", true},
		"library-developer":           {in("Library/Developer"), "", "xcode", true},
		"xcode-derived-data":          {in("Library/Developer/Xcode/DerivedData"), "Xcode 编译缓存", "xcode", false},
		"library-containers":          {in("Library/Containers"), "", "folder", true},
		"library-group-containers":    {in("Library/Group Containers"), "", "folder", true},
		"downloads":                   {in("Downloads"), "下载", "installer", true},
		"desktop":                     {in("Desktop"), "桌面", "folder", true},
		"documents":                   {in("Documents"), "文稿", "folder", true},
		"movies":                      {in("Movies"), "影片", "video", false},
		"music":                       {in("Music"), "音乐", "folder", false},
		"pictures":                    {in("Pictures"), "图片", "folder", false},
		"trash":                       {in(".Trash"), "废纸篓", "trash", false},
		"npm":                         {in(".npm"), "npm 缓存", "node", false},
		"cargo":                       {in(".cargo"), "Cargo", "package", false},
		"go":                          {in("go"), "Go", "package", false},
		"gradle":                      {in(".gradle"), "Gradle", "package", false},
		"maven":                       {in(".m2"), "Maven", "package", false},
		"dev-root:0":                  {in("code"), "", "build", true},
		"dev-root:1":                  {in("Documents/work"), "", "build", true},
	}
	if len(ws) != len(want) {
		t.Errorf("%d watches, want %d: %+v", len(ws), len(want), ws)
	}
	for id, w := range want {
		g, ok := got[id]
		if !ok {
			t.Errorf("no %s", id)
			continue
		}
		if g.Path != w.path || g.Label != w.label || g.Icon != w.icon || g.Children != w.children {
			t.Errorf("%s = %+v", id, g)
		}
	}
	if !AppDataWatch(got["library-containers"]) || !AppDataWatch(got["library-group-containers"]) || AppDataWatch(got["library-caches"]) {
		t.Error("AppDataWatch")
	}
	if DefaultWatches("darwin", "", nil, nil) != nil {
		t.Error("watches without a home folder")
	}
}

func TestDefaultWatchesWindows(t *testing.T) {
	home := filepath.FromSlash("/C/Users/me")
	env := map[string]string{"LOCALAPPDATA": filepath.FromSlash("/C/Users/me/AppData/Local"), "TEMP": filepath.FromSlash("/D/Temp")}
	ws := DefaultWatches("windows", home, func(k string) string { return env[k] }, []string{filepath.FromSlash("/D/src")})
	got := byID(ws)
	in := func(p string) string { return filepath.Join(home, filepath.FromSlash(p)) }
	want := map[string]struct {
		path     string
		children bool
	}{
		"local-appdata":   {env["LOCALAPPDATA"], true},
		"roaming-appdata": {in("AppData/Roaming"), true},
		"temp":            {env["TEMP"], false},
		"downloads":       {in("Downloads"), true},
		"desktop":         {in("Desktop"), true},
		"documents":       {in("Documents"), true},
		"nuget":           {in(".nuget"), false},
		"gradle":          {in(".gradle"), false},
		"maven":           {in(".m2"), false},
		"cargo":           {in(".cargo"), false},
		"go":              {in("go"), false},
		"dev-root:0":      {filepath.FromSlash("/D/src"), true},
	}
	if len(ws) != len(want) {
		t.Errorf("%d watches, want %d: %+v", len(ws), len(want), ws)
	}
	for id, w := range want {
		if g := got[id]; g.Path != w.path || g.Children != w.children {
			t.Errorf("%s = %+v", id, g)
		}
	}
	if got["temp"].Label != "临时文件" || got["downloads"].Label != "下载" {
		t.Errorf("labels %+v %+v", got["temp"], got["downloads"])
	}
	// Without the variables the folders under the profile stand in.
	ws = DefaultWatches("windows", home, func(string) string { return "" }, nil)
	got = byID(ws)
	if got["local-appdata"].Path != in("AppData/Local") || got["temp"].Path != in("AppData/Local/Temp") {
		t.Errorf("fallbacks %+v %+v", got["local-appdata"], got["temp"])
	}
}
