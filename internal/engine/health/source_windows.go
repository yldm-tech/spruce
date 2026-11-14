package health

import (
	"context"
	"fmt"
	"strings"
	"unsafe"

	"github.com/shirou/gopsutil/v4/process"
	"golang.org/x/sys/windows"
)

// displayName is the executable's FileDescription as Task Manager shows it ("Google Chrome" for chrome.exe), else the image name without ".exe". Pseudo-processes without an executable (System, Registry, Memory Compression) keep their names, which Config.Ignore matches. It is empty when the process is gone.
func displayName(ctx context.Context, p *process.Process) string {
	name, err := p.NameWithContext(ctx)
	if err != nil {
		return ""
	}
	if exe, err := p.ExeWithContext(ctx); err == nil {
		if desc := fileDescription(exe); desc != "" {
			return desc
		}
	}
	if len(name) > len(".exe") && strings.EqualFold(name[len(name)-len(".exe"):], ".exe") {
		return name[:len(name)-len(".exe")]
	}
	return name
}

// fileDescription reads the FileDescription string of the first language in the executable's version resource; it is empty when there is none.
func fileDescription(path string) string {
	var zero windows.Handle
	size, err := windows.GetFileVersionInfoSize(path, &zero)
	if err != nil || size == 0 {
		return ""
	}
	buf := make([]byte, size)
	block := unsafe.Pointer(&buf[0])
	if err := windows.GetFileVersionInfo(path, 0, size, block); err != nil {
		return ""
	}
	var lang *[2]uint16 // language ID, code page
	var n uint32
	if err := windows.VerQueryValue(block, `\VarFileInfo\Translation`, unsafe.Pointer(&lang), &n); err != nil || n < 4 || lang == nil {
		return ""
	}
	var desc *uint16
	sub := fmt.Sprintf(`\StringFileInfo\%04x%04x\FileDescription`, lang[0], lang[1])
	if err := windows.VerQueryValue(block, sub, unsafe.Pointer(&desc), &n); err != nil || n == 0 || desc == nil {
		return ""
	}
	return strings.TrimSpace(windows.UTF16PtrToString(desc))
}
