package elevate

import (
	"errors"
	"os"
	"runtime"
	"slices"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestShellExecuteInfoLayout(t *testing.T) {
	want := map[string]uintptr{"amd64": 112, "arm64": 112, "386": 60}[runtime.GOARCH]
	if want == 0 {
		t.Skipf("no reference size for %s", runtime.GOARCH)
	}
	if got := unsafe.Sizeof(shellExecuteInfo{}); got != want {
		t.Fatalf("sizeof(SHELLEXECUTEINFOW) = %d, want %d", got, want)
	}
}

// TestJoinParametersRoundTrip checks that the child's command line splits back into exactly the arguments the parent passed, including a temp path with spaces and trailing backslashes.
func TestJoinParametersRoundTrip(t *testing.T) {
	args := []string{
		Flag, "--tasks", "dns,icons", "--out",
		`C:\Users\Mei Ling\AppData\Local\Temp\spruce-elevated-123`,
		`C:\dir with space\`,
		`a"b`,
		`\\server\share`,
		"",
	}
	got, err := windows.DecomposeCommandLine(`"C:\Program Files\Spruce\Spruce.exe" ` + joinParameters(args))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got[1:], args) {
		t.Fatalf("got %q, want %q", got[1:], args)
	}
}

func TestShellExecuteErrorMapsCancel(t *testing.T) {
	if err := shellExecuteError(windows.ERROR_CANCELLED); err != ErrCancelled {
		t.Errorf("ERROR_CANCELLED gave %v", err)
	}
	if err := shellExecuteError(syscall.Errno(windows.ERROR_FILE_NOT_FOUND)); err == nil || errors.Is(err, ErrCancelled) {
		t.Errorf("ERROR_FILE_NOT_FOUND gave %v", err)
	}
}

func TestPlatformChildEnv(t *testing.T) {
	env := platformChildEnv()
	if env.channel != viaFile || env.checkDir == nil {
		t.Fatalf("env %+v", env)
	}
	// A directory this (unelevated or elevated) test process creates is owned by its user, or by Administrators when the test runs elevated.
	d, err := os.MkdirTemp("", exchangeDirPrefix)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(d)
	if err := env.checkDir(d); (err == nil) == windows.GetCurrentProcessToken().IsElevated() {
		t.Errorf("checkDir(%q) = %v with an elevated token %v; the directory's owner is %s, the token's default owner %s, its elevation type %s", d, err, windows.GetCurrentProcessToken().IsElevated(), ownerOf(d), tokenOwner(), elevationType())
	}
	if sys := os.Getenv("SystemRoot"); env.checkDir(sys) == nil {
		t.Errorf("checkDir accepted the Windows directory %q, owned by %s", sys, ownerOf(sys))
	}
	if env.elevated() != windows.GetCurrentProcessToken().IsElevated() {
		t.Error("elevated does not follow the process token")
	}
}

func TestSamePathIgnoresCase(t *testing.T) {
	if !samePath(`C:\Users\A\AppData\Local\Temp`, `c:\users\a\appdata\local\temp`) {
		t.Error("paths differing in case are the same directory on Windows")
	}
}

func TestPrivilegedOwner(t *testing.T) {
	for _, k := range []windows.WELL_KNOWN_SID_TYPE{windows.WinLocalSystemSid, windows.WinBuiltinAdministratorsSid, windows.WinLocalServiceSid, windows.WinNetworkServiceSid} {
		sid, err := windows.CreateWellKnownSid(k)
		if err != nil {
			t.Fatal(err)
		}
		if !privilegedOwner(sid) {
			t.Errorf("%s is not treated as privileged", sid)
		}
	}
	ti, err := windows.StringToSid(trustedInstallerSID)
	if err != nil {
		t.Fatal(err)
	}
	if !privilegedOwner(ti) {
		t.Error("TrustedInstaller is not treated as privileged")
	}
	user, err := windows.StringToSid("S-1-5-21-1004336348-1177238915-682003330-1001")
	if err != nil {
		t.Fatal(err)
	}
	if privilegedOwner(user) {
		t.Error("an ordinary user account is treated as privileged")
	}
}

// ownerOf names the owner of path, for failure messages.
func ownerOf(path string) string {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return err.Error()
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err.Error()
	}
	return accountName(owner)
}

// tokenOwner names the default owner of the objects this process creates (TokenOwner).
func tokenOwner() string {
	n := uint32(256)
	for {
		b := make([]byte, n)
		err := windows.GetTokenInformation(windows.GetCurrentProcessToken(), windows.TokenOwner, &b[0], n, &n)
		if err == nil {
			return accountName((*struct{ Owner *windows.SID })(unsafe.Pointer(&b[0])).Owner)
		}
		if err != windows.ERROR_INSUFFICIENT_BUFFER || n <= uint32(len(b)) {
			return err.Error()
		}
	}
}

// elevationType is the process token's TokenElevationType: default (no split token, as with UAC off), full or limited.
func elevationType() string {
	var v, n uint32
	if err := windows.GetTokenInformation(windows.GetCurrentProcessToken(), windows.TokenElevationType, (*byte)(unsafe.Pointer(&v)), 4, &n); err != nil {
		return err.Error()
	}
	return map[uint32]string{1: "default", 2: "full", 3: "limited"}[v]
}

func accountName(sid *windows.SID) string {
	account, domain, _, err := sid.LookupAccount("")
	if err != nil {
		return sid.String()
	}
	return domain + `\` + account + " (" + sid.String() + ")"
}
