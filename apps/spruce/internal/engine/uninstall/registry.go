package uninstall

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"spruce/apps/spruce/internal/engine/cleanup"
)

// Source is where a Windows Uninstall entry lives in the registry.
type Source struct {
	// Hive is "HKLM" or "HKCU".
	Hive string
	// View is "64" or "32" for HKLM (WOW64_64KEY, WOW64_32KEY), "" for HKCU.
	View string
	// Key is the entry's subkey name under ...\CurrentVersion\Uninstall.
	Key string
}

// String renders the source for IDs and logs, e.g. `HKLM64\{GUID}`.
func (s Source) String() string { return s.Hive + s.View + `\` + s.Key }

// Entry is one program of the Windows Uninstall keys, as ParseEntry reads it.
type Entry struct {
	Source          Source
	DisplayName     string
	DisplayVersion  string
	Publisher       string
	InstallDate     time.Time
	EstimatedSize   int64
	InstallLocation string
	// IconPath and IconIndex are DisplayIcon split as "path,index".
	IconPath         string
	IconIndex        int
	UninstallString  string
	WindowsInstaller bool
	// ProductCode is the key name when it is a Windows Installer product code ({GUID}).
	ProductCode string
}

// ID returns the stable ID of the entry's app.
func (e Entry) ID() string { return cleanup.ItemID(e.Source.String()) }

var productCode = regexp.MustCompile(`^\{[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}\}$`)

// ParseEntry reads an Uninstall entry from its values, keyed by value name (any case), each a string (REG_SZ, REG_EXPAND_SZ already expanded) or a uint64 (REG_DWORD, REG_QWORD). It reports false for entries the page does not list: system components (SystemComponent=1), updates (ParentKeyName set, or ReleaseType Update, Hotfix or Security Update), and entries without a DisplayName or an UninstallString. QuietUninstallString is never read: the page always runs the uninstaller the user can see.
func ParseEntry(src Source, values map[string]any) (Entry, bool) {
	v := make(map[string]any, len(values))
	for k, val := range values {
		v[strings.ToLower(k)] = val
	}
	str := func(name string) string {
		switch x := v[name].(type) {
		case string:
			return strings.TrimSpace(strings.TrimRight(x, "\x00"))
		case uint64:
			return strconv.FormatUint(x, 10)
		}
		return ""
	}
	num := func(name string) (uint64, bool) {
		switch x := v[name].(type) {
		case uint64:
			return x, true
		case string:
			n, err := strconv.ParseUint(strings.TrimSpace(x), 0, 64)
			return n, err == nil
		}
		return 0, false
	}

	if n, ok := num("systemcomponent"); ok && n == 1 {
		return Entry{}, false
	}
	if str("parentkeyname") != "" {
		return Entry{}, false
	}
	switch strings.ToLower(str("releasetype")) {
	case "update", "hotfix", "security update":
		return Entry{}, false
	}
	e := Entry{
		Source:          src,
		DisplayName:     str("displayname"),
		DisplayVersion:  str("displayversion"),
		Publisher:       str("publisher"),
		UninstallString: str("uninstallstring"),
		InstallLocation: trimQuotes(str("installlocation")),
	}
	if e.DisplayName == "" || e.UninstallString == "" {
		return Entry{}, false
	}
	if n, ok := num("windowsinstaller"); ok && n == 1 {
		e.WindowsInstaller = true
	}
	if productCode.MatchString(src.Key) {
		e.ProductCode = src.Key
	}
	if n, ok := num("estimatedsize"); ok && n > 0 && n < 1<<40 {
		e.EstimatedSize = int64(n) * 1024
	}
	if t, err := time.ParseInLocation("20060102", str("installdate"), time.Local); err == nil && t.Year() > 1980 {
		e.InstallDate = t
	}
	e.IconPath, e.IconIndex = ParseIconLocation(str("displayicon"))
	return e, true
}

// ParseIconLocation splits a DisplayIcon value, `path`, `path,index` or either with the path quoted, into the path and the icon index (0 when absent; negative values are resource IDs).
func ParseIconLocation(s string) (string, int) {
	s = strings.TrimSpace(s)
	idx := 0
	if i := strings.LastIndexByte(s, ','); i >= 0 {
		if n, err := strconv.Atoi(strings.TrimSpace(s[i+1:])); err == nil {
			idx = n
			s = strings.TrimSpace(s[:i])
		}
	}
	return trimQuotes(s), idx
}

func trimQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	return strings.TrimSpace(s)
}

// DedupeEntries drops entries with the same name, version and publisher as an earlier one (a product registered in both registry views, or for the machine and the user), keeping the first.
func DedupeEntries(entries []Entry) []Entry {
	seen := make(map[string]bool, len(entries))
	out := entries[:0:0]
	for _, e := range entries {
		k := strings.ToLower(e.DisplayName + "\x00" + e.DisplayVersion + "\x00" + e.Publisher)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, e)
	}
	return out
}
