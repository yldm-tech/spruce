package uninstall

import (
	"slices"
	"testing"
	"time"
)

func TestParseEntry(t *testing.T) {
	src := Source{Hive: "HKLM", View: "64", Key: "{12345678-ABCD-ABCD-ABCD-1234567890AB}"}
	base := func() map[string]any {
		return map[string]any{
			"DisplayName":      "Foo Editor",
			"DisplayVersion":   "3.2.1",
			"Publisher":        "Foo Software",
			"InstallDate":      "20250304",
			"EstimatedSize":    uint64(2048),
			"InstallLocation":  `"C:\Program Files\Foo\"`,
			"DisplayIcon":      `"C:\Program Files\Foo\foo.exe",-101`,
			"UninstallString":  `MsiExec.exe /I{12345678-ABCD-ABCD-ABCD-1234567890AB}`,
			"WindowsInstaller": uint64(1),
		}
	}
	e, ok := ParseEntry(src, base())
	if !ok {
		t.Fatal("entry skipped")
	}
	want := Entry{
		Source: src, DisplayName: "Foo Editor", DisplayVersion: "3.2.1", Publisher: "Foo Software",
		InstallDate: time.Date(2025, 3, 4, 0, 0, 0, 0, time.Local), EstimatedSize: 2048 * 1024,
		InstallLocation: `C:\Program Files\Foo\`, IconPath: `C:\Program Files\Foo\foo.exe`, IconIndex: -101,
		UninstallString: `MsiExec.exe /I{12345678-ABCD-ABCD-ABCD-1234567890AB}`, WindowsInstaller: true,
		ProductCode: "{12345678-ABCD-ABCD-ABCD-1234567890AB}",
	}
	if !e.InstallDate.Equal(want.InstallDate) {
		t.Errorf("InstallDate = %v", e.InstallDate)
	}
	e.InstallDate, want.InstallDate = time.Time{}, time.Time{}
	if e != want {
		t.Errorf("entry =\n%+v\nwant\n%+v", e, want)
	}

	// Value names are matched in any case, and numbers stored as strings are read.
	lower := map[string]any{"displayname": "Bar", "UNINSTALLSTRING": `C:\bar\uninst.exe`, "estimatedsize": "10", "SystemComponent": "0"}
	if e, ok := ParseEntry(Source{Hive: "HKCU", Key: "Bar"}, lower); !ok || e.DisplayName != "Bar" || e.EstimatedSize != 10240 || e.ProductCode != "" || e.WindowsInstaller {
		t.Errorf("lower-case entry = %+v, %v", e, ok)
	}

	for name, edit := range map[string]func(map[string]any){
		"system component":   func(v map[string]any) { v["SystemComponent"] = uint64(1) },
		"child of a product": func(v map[string]any) { v["ParentKeyName"] = "Office16" },
		"update":             func(v map[string]any) { v["ReleaseType"] = "Update" },
		"hotfix":             func(v map[string]any) { v["ReleaseType"] = "Hotfix" },
		"security update":    func(v map[string]any) { v["ReleaseType"] = "Security Update" },
		"no name":            func(v map[string]any) { delete(v, "DisplayName") },
		"blank name":         func(v map[string]any) { v["DisplayName"] = "  " },
		"no uninstaller":     func(v map[string]any) { delete(v, "UninstallString") },
	} {
		v := base()
		edit(v)
		if _, ok := ParseEntry(src, v); ok {
			t.Errorf("%s: listed", name)
		}
	}
	// Bad dates and sizes are ignored, not fatal.
	v := base()
	v["InstallDate"] = "2025-03-04"
	v["EstimatedSize"] = "lots"
	if e, ok := ParseEntry(src, v); !ok || !e.InstallDate.IsZero() || e.EstimatedSize != 0 {
		t.Errorf("bad values: %+v", e)
	}
}

func TestParseIconLocation(t *testing.T) {
	for _, tc := range []struct {
		in   string
		path string
		idx  int
	}{
		{`C:\Foo\foo.exe`, `C:\Foo\foo.exe`, 0},
		{`C:\Foo\foo.exe,0`, `C:\Foo\foo.exe`, 0},
		{`C:\Foo\foo.exe, 2`, `C:\Foo\foo.exe`, 2},
		{`"C:\Program Files\Foo\foo.exe",-101`, `C:\Program Files\Foo\foo.exe`, -101},
		{`"C:\Program Files\Foo\foo.ico"`, `C:\Program Files\Foo\foo.ico`, 0},
		{`C:\Weird,Name\foo.exe`, `C:\Weird,Name\foo.exe`, 0},
		{``, ``, 0},
	} {
		p, i := ParseIconLocation(tc.in)
		if p != tc.path || i != tc.idx {
			t.Errorf("ParseIconLocation(%q) = %q, %d; want %q, %d", tc.in, p, i, tc.path, tc.idx)
		}
	}
}

func TestDedupeEntries(t *testing.T) {
	in := []Entry{
		{Source: Source{Hive: "HKLM", View: "64", Key: "a"}, DisplayName: "Foo", DisplayVersion: "1", Publisher: "P"},
		{Source: Source{Hive: "HKLM", View: "32", Key: "a"}, DisplayName: "foo", DisplayVersion: "1", Publisher: "p"},
		{Source: Source{Hive: "HKCU", Key: "b"}, DisplayName: "Foo", DisplayVersion: "2", Publisher: "P"},
	}
	out := DedupeEntries(in)
	var views []string
	for _, e := range out {
		views = append(views, e.Source.String())
	}
	if !slices.Equal(views, []string{`HKLM64\a`, `HKCU\b`}) {
		t.Errorf("deduped = %v", views)
	}
	if in[1].Source.View != "32" {
		t.Error("the input was modified")
	}
	if out[0].ID() == out[1].ID() || out[0].ID() == "" {
		t.Error("IDs must differ by source")
	}
}
