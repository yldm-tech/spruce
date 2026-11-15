package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/yldm-tech/spruce/internal/app"
)

// Sources a merged scan can be limited to, in scan order: the IDs wireEngine builds the merged scans' sources with.
var sourceIDs = []string{string(app.CleanerSystemJunk), string(app.CleanerDevJunk), string(app.CleanerInstallers)}

// list is a flag that may repeat (--source a --source b); each value may also list several, separated by commas.
type list []string

func (l *list) String() string { return strings.Join(*l, ",") }

func (l *list) Set(v string) error {
	for part := range strings.SplitSeq(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			*l = append(*l, part)
		}
	}
	return nil
}

// age is --older-than: a whole number of hours, days or weeks ("12h", "30d", "2w").
type age time.Duration

func (a *age) String() string { return time.Duration(*a).String() }

func (a *age) Set(v string) error {
	d, err := parseAge(v)
	if err != nil {
		return err
	}
	*a = age(d)
	return nil
}

// parseAge reads a whole positive number followed by h, d or w.
func parseAge(v string) (time.Duration, error) {
	if len(v) < 2 {
		return 0, fmt.Errorf("无法识别的时长 %q，写成 12h、30d 或 2w", v)
	}
	unit := map[byte]time.Duration{'h': time.Hour, 'd': 24 * time.Hour, 'w': 7 * 24 * time.Hour}[v[len(v)-1]]
	n, err := strconv.Atoi(v[:len(v)-1])
	if unit == 0 || err != nil || n <= 0 || strings.ContainsAny(v[:len(v)-1], "+-") {
		return 0, fmt.Errorf("无法识别的时长 %q，写成 12h、30d 或 2w", v)
	}
	const maxDays = 100 * 365
	if time.Duration(n) > maxDays*24*time.Hour/unit {
		return 0, fmt.Errorf("时长 %q 太长", v)
	}
	return time.Duration(n) * unit, nil
}

// newFlags returns a command's flag set, which reports nothing itself: parse turns its errors into sentences.
func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	return fs
}

// parse parses args with fs, allowing flags after positional arguments (spruce clean app-caches --yes); everything after "--" is positional. It returns the positional arguments; flag.ErrHelp for -h or --help.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, err
			}
			return nil, flagError(err)
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return pos, nil
		}
		if used := len(args) - len(rest); used > 0 && args[used-1] == "--" {
			return append(pos, rest...), nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

// flagError words an error of the flag package in Chinese.
func flagError(err error) error {
	msg := err.Error()
	switch {
	case strings.HasPrefix(msg, "flag provided but not defined: "):
		return fmt.Errorf("未知的选项 %s", strings.TrimPrefix(msg, "flag provided but not defined: "))
	case strings.HasPrefix(msg, "flag needs an argument: "):
		return fmt.Errorf("选项 %s 需要一个值", strings.TrimPrefix(msg, "flag needs an argument: "))
	case strings.HasPrefix(msg, "invalid value "):
		// invalid value "x" for flag -older-than: <the Set error>
		if _, after, ok := strings.Cut(msg, " for flag "); ok {
			if _, cause, ok := strings.Cut(after, ": "); ok {
				return errors.New(cause)
			}
		}
	case strings.HasPrefix(msg, "invalid boolean value "):
		return fmt.Errorf("选项的值无效：%s", strings.TrimPrefix(msg, "invalid boolean value "))
	}
	return err
}

// wantsJSON reports whether args ask for JSON output, looked at before parsing so a usage error is reported in JSON too.
func wantsJSON(args []string) bool {
	for _, a := range args {
		switch a {
		case "--":
			return false
		case "--json", "-json", "--json=true", "-json=true":
			return true
		}
	}
	return false
}

// checkSources refuses a source no merged scan has.
func checkSources(sources []string) error {
	for _, s := range sources {
		if !slices.Contains(sourceIDs, s) {
			return fmt.Errorf("未知的来源 %q，可以是 %s", s, strings.Join(sourceIDs, "、"))
		}
	}
	return nil
}

// sourcesOrNil returns sources without repeats in scan order, nil for none.
func sourcesOrNil(sources []string) []string {
	var out []string
	for _, s := range sourceIDs {
		if slices.Contains(sources, s) {
			out = append(out, s)
		}
	}
	return out
}
