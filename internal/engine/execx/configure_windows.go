package execx

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// configure starts cmd without a console window: the app is a GUI process, and console tools such as ipconfig would otherwise flash a window.
func configure(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}
