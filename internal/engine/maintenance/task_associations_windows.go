package maintenance

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/yldm-tech/spruce/internal/engine/taskresult"
)

// winFileExtsPath is the per-user key under HKCU where Explorer keeps, for each extension, the programs offered in "Open with" (OpenWithList, OpenWithProgids) and the default app (UserChoice).
const winFileExtsPath = `Software\Microsoft\Windows\CurrentVersion\Explorer\FileExts`

// winRegRoot is a registry hive the associations task reads.
type winRegRoot uint8

const (
	winHKCU winRegRoot = iota
	winHKLM
)

func (r winRegRoot) String() string {
	if r == winHKLM {
		return "HKLM"
	}
	return "HKCU"
}

// winRegistry is the registry access the associations task needs. Reads look in both the 64-bit and the 32-bit view; a missing key or value is reported as an error matching fs.ErrNotExist.
type winRegistry interface {
	// SubKeyNames lists the subkeys of path.
	SubKeyNames(root winRegRoot, path string) ([]string, error)
	// ValueNames lists the value names of path.
	ValueNames(root winRegRoot, path string) ([]string, error)
	// String reads a REG_SZ or REG_EXPAND_SZ value (expanded). name "" is the default value.
	String(root winRegRoot, path, name string) (string, error)
	// KeyExists reports whether path exists.
	KeyExists(root winRegRoot, path string) (bool, error)
	// DeleteValue deletes a value of an HKCU key.
	DeleteValue(path, name string) error
	// SetString writes a REG_SZ value of an HKCU key.
	SetString(path, name, value string) error
}

// winPathState is what is known about a program path.
type winPathState uint8

const (
	// winPathUnknown: the path cannot be judged (relative, network, removable or optical drive, access denied), so it is treated as present.
	winPathUnknown winPathState = iota
	winPathPresent
	// winPathMissing: the path is on a fixed drive that is mounted, and nothing exists there.
	winPathMissing
)

// winStaleEntry is one "Open with" value to remove.
type winStaleEntry struct {
	// Key is the HKCU key path, e.g. FileExts\.txt\OpenWithList.
	Key string
	// Name is the value name: a letter for OpenWithList, the ProgID for OpenWithProgids.
	Name string
	// Target is what the entry pointed to, for the history.
	Target string
}

func (e winStaleEntry) String() string {
	return e.Key[len(winFileExtsPath)+1:] + `\` + e.Name + " -> " + e.Target
}

// winAssociationsTask removes per-user "Open with" entries that point to programs that are gone. It edits only values under HKCU\…\FileExts\<ext>\OpenWithList and \OpenWithProgids, never UserChoice (whose hash protects the default app) and never the machine-wide classes, so it needs no administrator rights.
func winAssociationsTask(sys *winSystem) TaskImpl {
	return TaskImpl{
		Task: Task{
			ID:               "associations",
			Title:            "清理失效的「打开方式」",
			Description:      "只移除指向已卸载程序的「打开方式」记录，不改默认应用",
			Icon:             IconOpenWith,
			DefaultOn:        true,
			EstimatedSeconds: 3,
		},
		Run: func(_ context.Context, env Env) taskresult.Result {
			exts, err := sys.registry.SubKeyNames(winHKCU, winFileExtsPath)
			if errors.Is(err, fs.ErrNotExist) {
				return taskresult.Result{Outcome: taskresult.Unchanged, Message: "没有发现失效的「打开方式」记录", Detail: `HKCU\` + winFileExtsPath + " does not exist"}
			}
			if err != nil {
				return taskresult.Result{Outcome: taskresult.Failed, Message: "无法读取「打开方式」记录", Detail: err.Error()}
			}
			var stale []winStaleEntry
			var mruUpdates []winStaleEntry
			for _, ext := range exts {
				if !strings.HasPrefix(ext, ".") {
					continue
				}
				list := winStaleOpenWithList(sys, ext)
				stale = append(stale, list...)
				stale = append(stale, winStaleOpenWithProgids(sys, ext)...)
				mruUpdates = append(mruUpdates, list...)
			}
			if len(stale) == 0 {
				return taskresult.Result{Outcome: taskresult.Unchanged, Message: "没有发现失效的「打开方式」记录", Detail: fmt.Sprintf("checked %d extensions", len(exts))}
			}
			return winRemoveStale(sys, env, stale, mruUpdates)
		},
	}
}

// winStaleOpenWithList returns the OpenWithList values of ext ("a".."z", each an executable name) whose program is gone.
func winStaleOpenWithList(sys *winSystem, ext string) []winStaleEntry {
	key := winFileExtsPath + `\` + ext + `\OpenWithList`
	names, err := sys.registry.ValueNames(winHKCU, key)
	if err != nil {
		return nil
	}
	var out []winStaleEntry
	for _, name := range names {
		// MRUList and anything that is not a single lower-case letter is a format this task does not know, so it is kept.
		if len(name) != 1 || name[0] < 'a' || name[0] > 'z' {
			continue
		}
		exe, err := sys.registry.String(winHKCU, key, name)
		if err != nil {
			continue
		}
		if winApplicationGone(sys, exe) {
			out = append(out, winStaleEntry{Key: key, Name: name, Target: exe})
		}
	}
	return out
}

// winStaleOpenWithProgids returns the OpenWithProgids values of ext (each named after a ProgID) whose ProgID is gone. The ProgID that UserChoice or UserChoiceLatest names is always kept, so the default app's registration is not touched even indirectly.
func winStaleOpenWithProgids(sys *winSystem, ext string) []winStaleEntry {
	key := winFileExtsPath + `\` + ext + `\OpenWithProgids`
	names, err := sys.registry.ValueNames(winHKCU, key)
	if err != nil {
		return nil
	}
	// Recent Windows 11 builds also record the default app under UserChoiceLatest.
	var defaults []string
	for _, sub := range []string{`UserChoice`, `UserChoiceLatest`} {
		progID, err := sys.registry.String(winHKCU, winFileExtsPath+`\`+ext+`\`+sub, "ProgId")
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		defaults = append(defaults, progID)
	}
	var out []winStaleEntry
	for _, progID := range names {
		if progID == "" || slices.ContainsFunc(defaults, func(d string) bool { return strings.EqualFold(d, progID) }) {
			continue
		}
		if winProgIDGone(sys, progID) {
			out = append(out, winStaleEntry{Key: key, Name: progID, Target: "ProgID " + progID})
		}
	}
	return out
}

// winApplicationGone reports whether exe, an OpenWithList entry, names a program that no longer exists: either an absolute path to a missing file, or a bare name whose Applications\<exe> registration in HKCU or HKLM opens a missing file. A bare name with no open command anywhere is kept, because Windows may still find it through App Paths or PATH.
func winApplicationGone(sys *winSystem, exe string) bool {
	if exe == "" || strings.Contains(exe, "%") {
		return false
	}
	if strings.ContainsAny(exe, `\/`) {
		return winIsDrivePath(exe) && sys.pathState(exe) == winPathMissing
	}
	commands := 0
	for _, root := range []winRegRoot{winHKCU, winHKLM} {
		cmd, err := sys.registry.String(root, `Software\Classes\Applications\`+exe+`\shell\open\command`, "")
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil || !winCommandTargetMissing(cmd, sys.pathState) {
			return false
		}
		commands++
	}
	return commands > 0
}

// winProgIDGone reports whether progID is registered nowhere (neither HKCU nor HKLM Software\Classes, in either registry view), or is registered only with open commands whose program is missing. A registered ProgID without a plain open command (packaged apps, DelegateExecute handlers) is kept.
func winProgIDGone(sys *winSystem, progID string) bool {
	if strings.Contains(progID, "%") {
		return false
	}
	for _, root := range []winRegRoot{winHKCU, winHKLM} {
		exists, err := sys.registry.KeyExists(root, `Software\Classes\`+progID)
		if err != nil {
			return false
		}
		if !exists {
			continue
		}
		cmd, err := sys.registry.String(root, `Software\Classes\`+progID+`\shell\open\command`, "")
		if err != nil || !winCommandTargetMissing(cmd, sys.pathState) {
			return false
		}
	}
	return true
}

// winMaxPathCandidates bounds how many ways an unquoted command line is split.
const winMaxPathCandidates = 16

// winCommandTargetMissing reports whether command line cmd (a shell\open\command value) starts with a program on a fixed drive that does not exist. An unquoted path with spaces is tried the way CreateProcess tries it, every prefix ending at a space, with and without ".exe"; the program counts as missing only when every candidate is missing. Anything the function cannot judge (a relative name, an unexpanded variable, a network or removable drive) counts as present.
func winCommandTargetMissing(cmd string, state func(string) winPathState) bool {
	cmd = strings.TrimSpace(cmd)
	var candidates []string
	if strings.HasPrefix(cmd, `"`) {
		end := strings.IndexByte(cmd[1:], '"')
		if end < 0 {
			return false
		}
		candidates = []string{cmd[1 : 1+end]}
	} else {
		fields := strings.Split(cmd, " ")
		for i := 1; i <= len(fields) && i <= winMaxPathCandidates; i++ {
			c := strings.Join(fields[:i], " ")
			// Arguments such as "%1" or a quoted argument end the program path.
			if strings.ContainsAny(c, `%"`) {
				break
			}
			candidates = append(candidates, c)
		}
	}
	if len(candidates) == 0 {
		return false
	}
	for _, c := range candidates {
		if strings.Contains(c, "%") || !winIsDrivePath(c) || state(c) != winPathMissing {
			return false
		}
		if filepath.Ext(c) == "" && state(c+".exe") != winPathMissing {
			return false
		}
	}
	return true
}

// winIsDrivePath reports whether p is an absolute path on a drive letter, such as C:\Program Files\App\app.exe.
func winIsDrivePath(p string) bool {
	if len(p) < 3 || p[1] != ':' || (p[2] != '\\' && p[2] != '/') {
		return false
	}
	c := p[0] | 0x20
	return c >= 'a' && c <= 'z'
}

// winRemoveStale deletes the stale values, then drops the deleted letters from each touched OpenWithList's MRUList so Explorer's order stays consistent.
func winRemoveStale(sys *winSystem, env Env, stale, listEntries []winStaleEntry) taskresult.Result {
	var removed, failed []string
	deletedLetters := make(map[string]string)
	for _, e := range stale {
		err := sys.registry.DeleteValue(e.Key, e.Name)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			env.Log.Warn("could not remove an Open with entry", "entry", e.String(), "err", err)
			failed = append(failed, e.String()+": "+err.Error())
			continue
		}
		env.Log.Info("removed a stale Open with entry", "entry", e.String())
		removed = append(removed, e.String())
		if slices.Contains(listEntries, e) {
			deletedLetters[e.Key] += e.Name
		}
	}
	for key, letters := range deletedLetters {
		mru, err := sys.registry.String(winHKCU, key, "MRUList")
		if err != nil {
			continue
		}
		kept := strings.Map(func(r rune) rune {
			if strings.ContainsRune(letters, r) {
				return -1
			}
			return r
		}, mru)
		if kept == mru {
			continue
		}
		if err := sys.registry.SetString(key, "MRUList", kept); err != nil {
			env.Log.Warn("could not update MRUList", "key", key, "err", err)
			failed = append(failed, key[len(winFileExtsPath)+1:]+`\MRUList: `+err.Error())
		}
	}

	detail := winListDetail("removed", removed) + winListDetail("; failed", failed)
	switch {
	case len(removed) == 0:
		return taskresult.Result{Outcome: taskresult.Failed, Message: "未能移除失效的「打开方式」记录", Detail: detail}
	case len(failed) == 0:
		return taskresult.Result{Outcome: taskresult.Applied, Message: fmt.Sprintf("已移除 %d 条指向已卸载程序的「打开方式」记录，默认应用未改动", len(removed)), Detail: detail}
	}
	return taskresult.Result{Outcome: taskresult.Applied, Message: fmt.Sprintf("已移除 %d 条失效的「打开方式」记录，另有 %d 处未能修改，默认应用未改动", len(removed), len(failed)), Detail: detail}
}

// winListDetail renders up to 30 items for Result.Detail.
func winListDetail(label string, items []string) string {
	if len(items) == 0 {
		return ""
	}
	const limit = 30
	s := fmt.Sprintf("%s %d: %s", label, len(items), strings.Join(items[:min(len(items), limit)], ", "))
	if len(items) > limit {
		s += fmt.Sprintf(", … %d more", len(items)-limit)
	}
	return s
}

// osRegistry is the real winRegistry.
type osRegistry struct{}

// winRegViews are the registry views reads look in: a ProgID registered by a 32-bit installer may exist only in the 32-bit view.
var winRegViews = []uint32{registry.WOW64_64KEY, registry.WOW64_32KEY}

func (r winRegRoot) key() registry.Key {
	if r == winHKLM {
		return registry.LOCAL_MACHINE
	}
	return registry.CURRENT_USER
}

// openRead opens path for reading in the first view where it exists.
func (osRegistry) openRead(root winRegRoot, path string, access uint32) (registry.Key, error) {
	var firstErr error
	for _, view := range winRegViews {
		k, err := registry.OpenKey(root.key(), path, access|view)
		if err == nil {
			return k, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return 0, fmt.Errorf(`open %s\%s: %w`, root, path, err)
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return 0, fmt.Errorf(`open %s\%s: %w`, root, path, firstErr)
}

func (o osRegistry) SubKeyNames(root winRegRoot, path string) ([]string, error) {
	k, err := o.openRead(root, path, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil, err
	}
	defer k.Close()
	return k.ReadSubKeyNames(0)
}

func (o osRegistry) ValueNames(root winRegRoot, path string) ([]string, error) {
	k, err := o.openRead(root, path, registry.QUERY_VALUE)
	if err != nil {
		return nil, err
	}
	defer k.Close()
	return k.ReadValueNames(0)
}

func (o osRegistry) String(root winRegRoot, path, name string) (string, error) {
	var lastErr error
	for _, view := range winRegViews {
		k, err := registry.OpenKey(root.key(), path, registry.QUERY_VALUE|view)
		if err != nil {
			lastErr = err
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			break
		}
		v, typ, err := k.GetStringValue(name)
		k.Close()
		if err != nil {
			lastErr = err
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			break
		}
		if typ == registry.EXPAND_SZ {
			return registry.ExpandString(v)
		}
		return v, nil
	}
	return "", fmt.Errorf(`read %s\%s [%s]: %w`, root, path, name, lastErr)
}

func (o osRegistry) KeyExists(root winRegRoot, path string) (bool, error) {
	k, err := o.openRead(root, path, registry.QUERY_VALUE)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	k.Close()
	return true, nil
}

func (osRegistry) DeleteValue(path, name string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf(`open HKCU\%s: %w`, path, err)
	}
	defer k.Close()
	return k.DeleteValue(name)
}

func (osRegistry) SetString(path, name, value string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf(`open HKCU\%s: %w`, path, err)
	}
	defer k.Close()
	return k.SetStringValue(name, value)
}

// osPathState is the real pathState: only paths on a mounted fixed drive are judged, so an unplugged external disk, a network share or a disc never makes a program look uninstalled.
func osPathState(p string) winPathState {
	if !winIsDrivePath(p) {
		return winPathUnknown
	}
	rootPath, err := windows.UTF16PtrFromString(p[:2] + `\`)
	if err != nil || windows.GetDriveType(rootPath) != windows.DRIVE_FIXED {
		return winPathUnknown
	}
	_, err = os.Stat(p)
	switch {
	case err == nil:
		return winPathPresent
	case errors.Is(err, fs.ErrNotExist):
		return winPathMissing
	}
	return winPathUnknown
}
