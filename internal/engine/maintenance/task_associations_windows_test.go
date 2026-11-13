package maintenance

import (
	"context"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/yldm-tech/spruce/internal/engine/execx/execxtest"
	"github.com/yldm-tech/spruce/internal/engine/taskresult"
)

// fakeRegistry is an in-memory winRegistry. Key paths and value names compare case-insensitively, as in the real registry; value order is insertion order.
type fakeRegistry struct {
	keys      map[string]*fakeRegKey // by lower-case "HKCU\path"
	writes    []string
	failWrite map[string]error // by "path\name" as written
}

type fakeRegKey struct {
	root   winRegRoot
	path   string
	names  []string
	values map[string]string // by lower-case name
}

func newFakeRegistry() *fakeRegistry {
	return &fakeRegistry{keys: map[string]*fakeRegKey{}, failWrite: map[string]error{}}
}

func regID(root winRegRoot, path string) string { return strings.ToLower(root.String() + `\` + path) }

// set creates path (and its parents) and sets name=value pairs on it.
func (f *fakeRegistry) set(root winRegRoot, path string, nameValues ...string) {
	parts := strings.Split(path, `\`)
	for i := range parts {
		p := strings.Join(parts[:i+1], `\`)
		if f.keys[regID(root, p)] == nil {
			f.keys[regID(root, p)] = &fakeRegKey{root: root, path: p, values: map[string]string{}}
		}
	}
	k := f.keys[regID(root, path)]
	for i := 0; i+1 < len(nameValues); i += 2 {
		name := nameValues[i]
		if _, ok := k.values[strings.ToLower(name)]; !ok {
			k.names = append(k.names, name)
		}
		k.values[strings.ToLower(name)] = nameValues[i+1]
	}
}

func (f *fakeRegistry) key(root winRegRoot, path string) (*fakeRegKey, error) {
	k := f.keys[regID(root, path)]
	if k == nil {
		return nil, fmt.Errorf(`open %s\%s: %w`, root, path, fs.ErrNotExist)
	}
	return k, nil
}

func (f *fakeRegistry) SubKeyNames(root winRegRoot, path string) ([]string, error) {
	if _, err := f.key(root, path); err != nil {
		return nil, err
	}
	prefix := regID(root, path) + `\`
	var out []string
	for id, k := range f.keys {
		if rest, ok := strings.CutPrefix(id, prefix); ok && !strings.Contains(rest, `\`) {
			out = append(out, k.path[len(path)+1:])
		}
	}
	slices.Sort(out)
	return out, nil
}

func (f *fakeRegistry) ValueNames(root winRegRoot, path string) ([]string, error) {
	k, err := f.key(root, path)
	if err != nil {
		return nil, err
	}
	return slices.Clone(k.names), nil
}

func (f *fakeRegistry) String(root winRegRoot, path, name string) (string, error) {
	k, err := f.key(root, path)
	if err != nil {
		return "", err
	}
	v, ok := k.values[strings.ToLower(name)]
	if !ok {
		return "", fmt.Errorf(`%s\%s [%s]: %w`, root, path, name, fs.ErrNotExist)
	}
	return v, nil
}

func (f *fakeRegistry) KeyExists(root winRegRoot, path string) (bool, error) {
	return f.keys[regID(root, path)] != nil, nil
}

func (f *fakeRegistry) DeleteValue(path, name string) error {
	if err := f.failWrite[path+`\`+name]; err != nil {
		return err
	}
	k, err := f.key(winHKCU, path)
	if err != nil {
		return err
	}
	if _, ok := k.values[strings.ToLower(name)]; !ok {
		return fs.ErrNotExist
	}
	delete(k.values, strings.ToLower(name))
	k.names = slices.DeleteFunc(k.names, func(n string) bool { return strings.EqualFold(n, name) })
	f.writes = append(f.writes, "delete "+path+`\`+name)
	return nil
}

func (f *fakeRegistry) SetString(path, name, value string) error {
	if err := f.failWrite[path+`\`+name]; err != nil {
		return err
	}
	f.set(winHKCU, path, name, value)
	f.writes = append(f.writes, "set "+path+`\`+name+"="+value)
	return nil
}

func (f *fakeRegistry) has(root winRegRoot, path, name string) bool {
	_, err := f.String(root, path, name)
	return err == nil
}

// pathStates answers winPathState from a table; paths not in it are unknown.
func pathStates(m map[string]winPathState) func(string) winPathState {
	return func(p string) winPathState { return m[strings.ToLower(p)] }
}

const (
	fe        = winFileExtsPath
	txtList   = fe + `\.txt\OpenWithList`
	txtProgs  = fe + `\.txt\OpenWithProgids`
	apps      = `Software\Classes\Applications\`
	classes   = `Software\Classes\`
	openCmd   = `\shell\open\command`
	goneExe   = `C:\Program Files\Gone App\gone.exe`
	liveExe   = `C:\Program Files\Live\live.exe`
	removable = `E:\Portable\tool.exe`
)

// associationsFixture is a registry with live and stale entries of every kind.
func associationsFixture() (*fakeRegistry, func(string) winPathState) {
	r := newFakeRegistry()
	r.set(winHKCU, txtList,
		"a", "live.exe",
		"b", "gone.exe",
		"c", "notepad.exe", // no Applications registration: kept
		"d", "portable.exe", // on a removable drive: kept
		"e", "delegate.exe", // registered without an open command: kept
		"f", goneExe, // absolute path to a missing file
		"MRUList", "fbedca",
	)
	r.set(winHKCU, apps+`live.exe`+openCmd, "", `"`+liveExe+`" "%1"`)
	r.set(winHKCU, apps+`gone.exe`+openCmd, "", `"`+goneExe+`" "%1"`)
	r.set(winHKCU, apps+`portable.exe`+openCmd, "", `"`+removable+`" "%1"`)
	r.set(winHKLM, apps+`delegate.exe\shell\open`, "DelegateExecute", "{GUID}")

	r.set(winHKCU, txtProgs,
		"txtfile", "",
		"Unregistered.Prog", "",
		"Gone.Document", "",
		"AppX123", "",
		"Default.Gone", "",
	)
	r.set(winHKLM, classes+`txtfile`+openCmd, "", `C:\Windows\system32\NOTEPAD.EXE %1`)
	r.set(winHKCU, classes+`Gone.Document`+openCmd, "", `C:\Program Files\Gone App\gone.exe %1`)
	r.set(winHKCU, classes+`AppX123\Application`, "AppUserModelID", "Some.App")
	// UserChoice points at a ProgID that is gone; it is still never removed.
	r.set(winHKCU, fe+`\.txt\UserChoice`, "ProgId", "Default.Gone", "Hash", "abc")

	states := pathStates(map[string]winPathState{
		strings.ToLower(liveExe):          winPathPresent,
		strings.ToLower(goneExe):          winPathMissing,
		`c:\windows\system32\notepad.exe`: winPathPresent,
		`c:\program`:                      winPathMissing,
		`c:\program.exe`:                  winPathMissing,
		`c:\program files\gone`:           winPathMissing,
		`c:\program files\gone.exe`:       winPathMissing,
		strings.ToLower(removable):        winPathUnknown,
	})
	return r, states
}

func TestAssociationsRemovesOnlyStaleEntries(t *testing.T) {
	reg, states := associationsFixture()
	sys := newTestWinSystem(t)
	sys.registry = reg
	sys.pathState = states

	res := winAssociationsTask(sys).Run(context.Background(), testEnv(execxtest.New()))
	if res.Outcome != taskresult.Applied {
		t.Fatalf("result %+v", res)
	}
	if want := "已移除 4 条指向已卸载程序的「打开方式」记录，默认应用未改动"; res.Message != want {
		t.Errorf("message %q, want %q", res.Message, want)
	}

	for _, name := range []string{"a", "c", "d", "e"} {
		if !reg.has(winHKCU, txtList, name) {
			t.Errorf("OpenWithList %s was removed", name)
		}
	}
	for _, name := range []string{"b", "f"} {
		if reg.has(winHKCU, txtList, name) {
			t.Errorf("stale OpenWithList %s was kept", name)
		}
	}
	if mru, _ := reg.String(winHKCU, txtList, "MRUList"); mru != "edca" {
		t.Errorf("MRUList = %q, want edca", mru)
	}
	for _, name := range []string{"txtfile", "AppX123", "Default.Gone"} {
		if !reg.has(winHKCU, txtProgs, name) {
			t.Errorf("OpenWithProgids %s was removed", name)
		}
	}
	for _, name := range []string{"Unregistered.Prog", "Gone.Document"} {
		if reg.has(winHKCU, txtProgs, name) {
			t.Errorf("stale OpenWithProgids %s was kept", name)
		}
	}
	for _, w := range reg.writes {
		if strings.Contains(w, "UserChoice") || !strings.Contains(w, `\OpenWith`) {
			t.Errorf("write outside OpenWithList/OpenWithProgids: %s", w)
		}
	}
	if !strings.Contains(res.Detail, `.txt\OpenWithList\b -> gone.exe`) {
		t.Errorf("detail %q does not list the removed entry", res.Detail)
	}

	// A second run finds nothing.
	res = winAssociationsTask(sys).Run(context.Background(), testEnv(execxtest.New()))
	if res.Outcome != taskresult.Unchanged {
		t.Errorf("second run %+v, want unchanged", res)
	}
}

func TestAssociationsNothingStale(t *testing.T) {
	reg := newFakeRegistry()
	reg.set(winHKCU, txtList, "a", "live.exe", "MRUList", "a")
	reg.set(winHKCU, apps+`live.exe`+openCmd, "", `"`+liveExe+`" "%1"`)
	sys := newTestWinSystem(t)
	sys.registry = reg
	sys.pathState = pathStates(map[string]winPathState{strings.ToLower(liveExe): winPathPresent})
	res := winAssociationsTask(sys).Run(context.Background(), testEnv(execxtest.New()))
	if res.Outcome != taskresult.Unchanged || len(reg.writes) != 0 {
		t.Fatalf("result %+v, writes %v", res, reg.writes)
	}

	sys.registry = newFakeRegistry()
	res = winAssociationsTask(sys).Run(context.Background(), testEnv(execxtest.New()))
	if res.Outcome != taskresult.Unchanged {
		t.Fatalf("no FileExts key: %+v", res)
	}
}

func TestAssociationsWriteFailures(t *testing.T) {
	reg, states := associationsFixture()
	reg.failWrite[txtList+`\b`] = windows.ERROR_ACCESS_DENIED
	sys := newTestWinSystem(t)
	sys.registry = reg
	sys.pathState = states
	res := winAssociationsTask(sys).Run(context.Background(), testEnv(execxtest.New()))
	if res.Outcome != taskresult.Applied || !strings.Contains(res.Message, "另有 1 处未能修改") {
		t.Fatalf("result %+v", res)
	}
	// Only the letter actually deleted leaves the MRUList.
	if mru, _ := reg.String(winHKCU, txtList, "MRUList"); mru != "bedca" {
		t.Errorf("MRUList = %q, want bedca", mru)
	}

	reg, states = associationsFixture()
	for _, name := range []string{`\b`, `\f`} {
		reg.failWrite[txtList+name] = windows.ERROR_ACCESS_DENIED
	}
	for _, name := range []string{`\Unregistered.Prog`, `\Gone.Document`} {
		reg.failWrite[txtProgs+name] = windows.ERROR_ACCESS_DENIED
	}
	sys.registry = reg
	sys.pathState = states
	res = winAssociationsTask(sys).Run(context.Background(), testEnv(execxtest.New()))
	if res.Outcome != taskresult.Failed {
		t.Fatalf("every write failed: %+v", res)
	}
}

func TestCommandTargetMissing(t *testing.T) {
	states := pathStates(map[string]winPathState{
		`c:\apps\present.exe`:              winPathPresent,
		`c:\apps\gone.exe`:                 winPathMissing,
		`c:\program`:                       winPathMissing,
		`c:\program.exe`:                   winPathMissing,
		`c:\program files\my`:              winPathMissing,
		`c:\program files\my.exe`:          winPathMissing,
		`c:\program files\my app\a.exe`:    winPathPresent,
		`c:\program files\my app\b.exe`:    winPathMissing,
		`c:\program files\my app\b.exe -x`: winPathMissing,
	})
	cases := []struct {
		cmd  string
		want bool
	}{
		{`"C:\apps\gone.exe" "%1"`, true},
		{`"C:\apps\present.exe" "%1"`, false},
		{`C:\apps\gone.exe %1`, true},
		{`C:\Program Files\My App\a.exe %1`, false},
		{`C:\Program Files\My App\b.exe -x "%1"`, true},
		{`"%ProgramFiles%\x.exe" "%1"`, false},
		{`notepad.exe %1`, false},
		{`"\\server\share\app.exe" "%1"`, false},
		{`"C:\apps\unknown.exe"`, false},
		{`"C:\apps\gone.exe`, false},
		{``, false},
		{`rundll32.exe shell32.dll,OpenAs_RunDLL %1`, false},
	}
	for _, c := range cases {
		if got := winCommandTargetMissing(c.cmd, states); got != c.want {
			t.Errorf("winCommandTargetMissing(%q) = %v, want %v", c.cmd, got, c.want)
		}
	}
}

func TestIsDrivePath(t *testing.T) {
	for p, want := range map[string]bool{
		`C:\x.exe`: true, `d:/x.exe`: true, `C:x.exe`: false, `\\srv\x`: false, `x.exe`: false, `1:\x`: false, ``: false,
	} {
		if got := winIsDrivePath(p); got != want {
			t.Errorf("winIsDrivePath(%q) = %v, want %v", p, got, want)
		}
	}
}
