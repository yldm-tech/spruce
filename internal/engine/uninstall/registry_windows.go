package uninstall

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// uninstallKey is the path of the Uninstall keys under HKLM and HKCU.
const uninstallKey = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`

// uninstallRoot is one Uninstall key to read.
type uninstallRoot struct {
	key    registry.Key
	hive   string
	view   string
	access uint32
}

// uninstallRoots are the Uninstall keys, in the order DedupeEntries prefers: the machine's 64-bit and 32-bit views, then the user's (HKCU\Software is shared by both views).
var uninstallRoots = []uninstallRoot{
	{registry.LOCAL_MACHINE, "HKLM", "64", registry.WOW64_64KEY},
	{registry.LOCAL_MACHINE, "HKLM", "32", registry.WOW64_32KEY},
	{registry.CURRENT_USER, "HKCU", "", 0},
}

// readEntries reads every Uninstall entry the page may list. Keys that are missing or cannot be read are skipped; it fails only when none could be read.
func readEntries() ([]Entry, error) {
	var out []Entry
	read := 0
	var firstErr error
	for _, r := range uninstallRoots {
		k, err := registry.OpenKey(r.key, uninstallKey, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE|r.access)
		if err != nil {
			if !errors.Is(err, registry.ErrNotExist) && firstErr == nil {
				firstErr = err
			}
			continue
		}
		names, err := k.ReadSubKeyNames(-1)
		k.Close()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		read++
		for _, name := range names {
			src := Source{Hive: r.hive, View: r.view, Key: name}
			values, err := readValues(r, name)
			if err != nil {
				continue
			}
			if e, ok := ParseEntry(src, values); ok {
				out = append(out, e)
			}
		}
	}
	if read == 0 && firstErr != nil {
		return nil, fmt.Errorf("uninstall: read the Uninstall keys: %w", firstErr)
	}
	return DedupeEntries(out), nil
}

// readValues returns the string and integer values of one entry, with environment variables expanded.
func readValues(r uninstallRoot, name string) (map[string]any, error) {
	k, err := registry.OpenKey(r.key, uninstallKey+`\`+name, registry.QUERY_VALUE|r.access)
	if err != nil {
		return nil, err
	}
	defer k.Close()
	names, err := k.ReadValueNames(-1)
	if err != nil {
		return nil, err
	}
	values := make(map[string]any, len(names))
	for _, n := range names {
		_, typ, err := k.GetValue(n, nil)
		if err != nil {
			continue
		}
		switch typ {
		case registry.SZ, registry.EXPAND_SZ:
			s, _, err := k.GetStringValue(n)
			if err != nil {
				continue
			}
			// Some installers write %ProgramFiles% and the like into plain REG_SZ values, which ShellExecuteEx would not expand either.
			if typ == registry.EXPAND_SZ || strings.Contains(s, "%") {
				if x, err := registry.ExpandString(s); err == nil {
					s = x
				}
			}
			values[n] = s
		case registry.DWORD, registry.QWORD:
			if v, _, err := k.GetIntegerValue(n); err == nil {
				values[n] = v
			}
		}
	}
	return values, nil
}

// entryPresent reports whether the entry src still exists with the same DisplayName.
func entryPresent(src Source, displayName string) (bool, error) {
	for _, r := range uninstallRoots {
		if r.hive != src.Hive || r.view != src.View {
			continue
		}
		values, err := readValues(r, src.Key)
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		name, _ := values["DisplayName"].(string)
		if name == "" {
			for k, v := range values {
				if strings.EqualFold(k, "DisplayName") {
					name, _ = v.(string)
				}
			}
		}
		return strings.TrimSpace(name) == displayName, nil
	}
	return false, fmt.Errorf("uninstall: unknown registry source %s", src)
}
