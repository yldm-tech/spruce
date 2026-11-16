package uninstall

import (
	"context"
	"strings"
	"time"

	"spruce/apps/spruce/internal/engine/execx"
)

// mdlsBatch is how many bundles one mdls run asks about.
const mdlsBatch = 50

// mdlsTime is how mdls prints a date with -raw.
const mdlsTime = "2006-01-02 15:04:05 -0700"

// mdlsNull is what mdls prints for an attribute the item does not have.
const mdlsNull = "(null)"

// earliestUse is the first last-use date taken as real. Spotlight reports 1979-12-31 15:00:00 +0000 for some apps it has no real date for, apps used the same day among them. Spotlight came with Mac OS X 10.4 in 2005, so any date before 2001 is such a placeholder.
var earliestUse = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

// lastUsed asks Spotlight when each app was last opened, in batches. It asks for kMDItemContentType too, which every indexed bundle has, to tell an app never opened (type known, no date) from one Spotlight does not know (neither). A batch whose answer cannot be lined up with its paths (mdls skips a bundle that vanished) is asked again one bundle at a time; anything that still fails is unknown.
func (e *Engine) lastUsed(ctx context.Context, apps []App) []Details {
	out := make([]Details, 0, len(apps))
	for start := 0; start < len(apps); start += mdlsBatch {
		batch := apps[start:min(start+mdlsBatch, len(apps))]
		vals, ok := e.mdls(ctx, batch)
		if !ok {
			vals = make([]Details, len(batch))
			for i, a := range batch {
				one, ok := e.mdls(ctx, []App{a})
				if ok {
					vals[i] = one[0]
				} else {
					vals[i] = Details{Usage: UsageUnknown}
				}
			}
		}
		for i, a := range batch {
			vals[i].ID = a.ID
			out = append(out, vals[i])
		}
		if ctx.Err() != nil {
			break
		}
	}
	return out
}

// mdls runs one mdls for apps and parses its answer, in the apps' order.
func (e *Engine) mdls(ctx context.Context, apps []App) ([]Details, bool) {
	args := []string{"-raw", "-name", "kMDItemContentType", "-name", "kMDItemLastUsedDate"}
	for _, a := range apps {
		args = append(args, a.Path)
	}
	res, err := e.cfg.Runner.Run(ctx, execx.Command{Name: "/usr/bin/mdls", Args: args, Timeout: 30 * time.Second})
	if err != nil {
		return nil, false
	}
	return parseMdls(string(res.Stdout), len(apps))
}

// parseMdls parses the answer of `mdls -raw -name kMDItemContentType -name kMDItemLastUsedDate` for n bundles: NUL-separated values, the attributes of each bundle in alphabetical order. A date before earliestUse is unknown.
func parseMdls(out string, n int) ([]Details, bool) {
	vals := strings.Split(strings.TrimRight(out, "\x00\n"), "\x00")
	if n == 0 || len(vals) != 2*n {
		return nil, false
	}
	res := make([]Details, n)
	for i := range n {
		typ, used := strings.TrimSpace(vals[2*i]), strings.TrimSpace(vals[2*i+1])
		switch {
		case used != "" && used != mdlsNull:
			t, err := time.Parse(mdlsTime, used)
			if err != nil || t.Before(earliestUse) {
				res[i] = Details{Usage: UsageUnknown}
				continue
			}
			res[i] = Details{Usage: UsageUsed, LastUsed: t}
		case typ != "" && typ != mdlsNull:
			res[i] = Details{Usage: UsageNever}
		default:
			res[i] = Details{Usage: UsageUnknown}
		}
	}
	return res, true
}
