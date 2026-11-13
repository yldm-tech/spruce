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
	if env.channel != viaFile || env.tempDir != os.TempDir() {
		t.Fatalf("env %+v", env)
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
