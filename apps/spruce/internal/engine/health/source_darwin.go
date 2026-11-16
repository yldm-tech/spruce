package health

import (
	"context"
	"strings"

	"github.com/shirou/gopsutil/v4/process"
)

// displayName is the process name, or the name of the outermost .app bundle its executable lives in, so helpers show as the app they belong to. It is empty when the process is gone.
func displayName(ctx context.Context, p *process.Process) string {
	name, err := p.NameWithContext(ctx)
	if err != nil {
		return ""
	}
	if exe, err := p.ExeWithContext(ctx); err == nil {
		if app := appBundleName(exe); app != "" {
			return app
		}
	}
	return name
}

// appBundleName returns "Xcode" for "/Applications/Xcode.app/Contents/MacOS/Xcode" and "Google Chrome" for a helper nested deeper inside Google Chrome.app; it is empty for executables outside any bundle.
func appBundleName(exe string) string {
	for part := range strings.SplitSeq(exe, "/") {
		if name, ok := strings.CutSuffix(part, ".app"); ok && name != "" {
			return name
		}
	}
	return ""
}
