package uninstall

import (
	"errors"
	"slices"
	"testing"
)

func TestParseCommandLine(t *testing.T) {
	files := map[string]bool{
		`C:\Program Files\Foo App\uninst.exe`:     true,
		`C:\Program Files (x86)\Bar\unins000.exe`: true,
		`C:\Tools\setup.exe`:                      true,
		`C:\Tools\setup`:                          false,
		`C:\Odd Name\Uninstall`:                   false,
		`C:\Odd Name\Uninstall.exe`:               true,
	}
	exists := func(p string) bool { return files[p] }
	for _, tc := range []struct {
		name, in, prog, params string
		args                   []string
	}{
		{"quoted with args", `"C:\Program Files\Foo App\uninst.exe" /S /D="x y"`, `C:\Program Files\Foo App\uninst.exe`, `/S /D="x y"`, []string{"/S", "/D=x y"}},
		{"quoted alone", `"C:\Program Files\Foo App\uninst.exe"`, `C:\Program Files\Foo App\uninst.exe`, ``, nil},
		{"quoted, no closing quote", `"C:\Program Files\Foo App\uninst.exe`, `C:\Program Files\Foo App\uninst.exe`, ``, nil},
		{"quoted, args glued", `"C:\Tools\setup.exe"/uninstall`, `C:\Tools\setup.exe`, `/uninstall`, []string{"/uninstall"}},
		{"unquoted with spaces", `C:\Program Files (x86)\Bar\unins000.exe /SILENT`, `C:\Program Files (x86)\Bar\unins000.exe`, `/SILENT`, []string{"/SILENT"}},
		{"unquoted, no args", `C:\Program Files\Foo App\uninst.exe`, `C:\Program Files\Foo App\uninst.exe`, ``, nil},
		{"unquoted, .exe appended", `C:\Odd Name\Uninstall --remove`, `C:\Odd Name\Uninstall.exe`, `--remove`, []string{"--remove"}},
		{"unquoted, missing file with .exe", `C:\Gone Away\uninst.exe /x`, `C:\Gone Away\uninst.exe`, `/x`, []string{"/x"}},
		{"msiexec", `MsiExec.exe /I{12345678-ABCD-ABCD-ABCD-1234567890AB}`, `MsiExec.exe`, `/I{12345678-ABCD-ABCD-ABCD-1234567890AB}`, []string{"/I{12345678-ABCD-ABCD-ABCD-1234567890AB}"}},
		{"rundll32", `rundll32.exe "C:\Program Files\Foo App\x.dll",Uninstall`, `rundll32.exe`, `"C:\Program Files\Foo App\x.dll",Uninstall`, []string{`C:\Program Files\Foo App\x.dll,Uninstall`}},
		{"bare word", `uninstall-foo --all`, `uninstall-foo`, `--all`, []string{"--all"}},
		{"padded", "   \"C:\\Tools\\setup.exe\"   -u   ", `C:\Tools\setup.exe`, `-u`, []string{"-u"}},
	} {
		got, err := ParseCommandLine(tc.in, exists)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if got.Program != tc.prog || got.Params != tc.params || !slices.Equal(got.Args, tc.args) {
			t.Errorf("%s: ParseCommandLine(%q) = %q %q %q; want %q %q %q", tc.name, tc.in, got.Program, got.Params, got.Args, tc.prog, tc.params, tc.args)
		}
	}
	for _, in := range []string{"", "   ", `""`} {
		if _, err := ParseCommandLine(in, exists); !errors.Is(err, ErrEmptyCommand) {
			t.Errorf("ParseCommandLine(%q) err = %v", in, err)
		}
	}
}

func TestSplitArgs(t *testing.T) {
	// The cases of Microsoft's "Parsing C++ command-line arguments" table; "" inside quotes follows CommandLineToArgvW (the msvcrt rule before 2008, as the Go runtime does): a literal quote that also ends the quoted part.
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{`"abc" d e`, []string{"abc", "d", "e"}},
		{`a\\b d"e f"g h`, []string{`a\\b`, "de fg", "h"}},
		{`a\\\"b c d`, []string{`a\"b`, "c", "d"}},
		{`a\\\\"b c" d e`, []string{`a\\b c`, "d", "e"}},
		{`a"b"" c d`, []string{`ab"`, "c", "d"}},
		{"  spaced\targs  ", []string{"spaced", "args"}},
		{``, nil},
	} {
		if got := SplitArgs(tc.in); !slices.Equal(got, tc.want) {
			t.Errorf("SplitArgs(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestUninstallCommand(t *testing.T) {
	msi := Entry{WindowsInstaller: true, ProductCode: "{12345678-ABCD-ABCD-ABCD-1234567890AB}", UninstallString: `MsiExec.exe /I{12345678-ABCD-ABCD-ABCD-1234567890AB}`}
	got, err := UninstallCommand(msi, `C:\Windows\`, nil)
	if err != nil || got.Program != `C:\Windows\System32\msiexec.exe` || got.Params != "/x {12345678-ABCD-ABCD-ABCD-1234567890AB}" {
		t.Errorf("msi = %+v, %v", got, err)
	}
	// Without a product code the entry's own command runs, even for Windows Installer.
	noCode := Entry{WindowsInstaller: true, UninstallString: `"C:\Tools\setup.exe" /remove`}
	if got, err := UninstallCommand(noCode, `C:\Windows`, nil); err != nil || got.Program != `C:\Tools\setup.exe` || got.Params != "/remove" {
		t.Errorf("no product code = %+v, %v", got, err)
	}
}
