package uninstall

import (
	"errors"
	"path/filepath"
	"strings"
)

// CommandLine is an uninstall command split for ShellExecuteEx: the program and its parameters as one string, which ShellExecuteEx passes on unchanged.
type CommandLine struct {
	Program string
	Params  string
	// Args are Params split as CommandLineToArgvW would, for display and tests.
	Args []string
}

// ErrEmptyCommand is returned for an empty uninstall command.
var ErrEmptyCommand = errors.New("uninstall: empty uninstall command")

// ParseCommandLine splits an UninstallString into the program and its parameters. A quoted program runs to the closing quote, as CommandLineToArgvW reads it. An unquoted one may contain spaces (C:\Program Files\Foo\uninst.exe /S is common): like CreateProcess, it takes the shortest prefix ending at a space that names an existing file (exists, with ".exe" appended if needed); when none exists, the prefix up to the first ".exe" followed by a space or the end, and failing that the first word.
func ParseCommandLine(s string, exists func(string) bool) (CommandLine, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return CommandLine{}, ErrEmptyCommand
	}
	var prog, rest string
	if s[0] == '"' {
		end := strings.IndexByte(s[1:], '"')
		if end < 0 {
			prog = s[1:]
		} else {
			prog, rest = s[1:1+end], s[2+end:]
		}
	} else {
		prog, rest = unquotedProgram(s, exists)
	}
	prog = strings.TrimSpace(prog)
	if prog == "" {
		return CommandLine{}, ErrEmptyCommand
	}
	rest = strings.TrimSpace(rest)
	return CommandLine{Program: prog, Params: rest, Args: SplitArgs(rest)}, nil
}

func unquotedProgram(s string, exists func(string) bool) (string, string) {
	for i := 0; i <= len(s); i++ {
		if i < len(s) && s[i] != ' ' && s[i] != '\t' {
			continue
		}
		cand := s[:i]
		if cand == "" {
			continue
		}
		if exists != nil && (exists(cand) || (!strings.EqualFold(filepath.Ext(cand), ".exe") && exists(cand+".exe"))) {
			if !exists(cand) {
				cand += ".exe"
			}
			if i < len(s) {
				return cand, s[i:]
			}
			return cand, ""
		}
	}
	lower := strings.ToLower(s)
	for off := 0; ; {
		i := strings.Index(lower[off:], ".exe")
		if i < 0 {
			break
		}
		end := off + i + len(".exe")
		if end == len(s) || s[end] == ' ' || s[end] == '\t' {
			return s[:end], s[end:]
		}
		off = end
	}
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i], s[i:]
	}
	return s, ""
}

// SplitArgs splits a parameter string as CommandLineToArgvW splits the arguments after the program: whitespace separates arguments outside quotes, 2n backslashes before a quote become n and the quote toggles quoting, 2n+1 become n and a literal quote, other backslashes are literal, and "" inside quotes is a literal quote that ends the quoted part.
func SplitArgs(s string) []string {
	var args []string
	for len(s) > 0 {
		if s[0] == ' ' || s[0] == '\t' {
			s = s[1:]
			continue
		}
		var arg []byte
		arg, s = nextArg(s)
		args = append(args, string(arg))
	}
	return args
}

// nextArg reads one argument from s and returns it with the rest (the same rules as the Go runtime's Windows command line parser).
func nextArg(s string) ([]byte, string) {
	var b []byte
	inQuote := false
	slashes := 0
	for ; len(s) > 0; s = s[1:] {
		c := s[0]
		switch c {
		case ' ', '\t':
			if !inQuote {
				return appendSlashes(b, slashes), s[1:]
			}
		case '"':
			b = appendSlashes(b, slashes/2)
			if slashes%2 == 0 {
				if inQuote && len(s) > 1 && s[1] == '"' {
					b = append(b, c)
					s = s[1:]
				}
				inQuote = !inQuote
			} else {
				b = append(b, c)
			}
			slashes = 0
			continue
		case '\\':
			slashes++
			continue
		}
		b = appendSlashes(b, slashes)
		slashes = 0
		b = append(b, c)
	}
	return appendSlashes(b, slashes), ""
}

func appendSlashes(b []byte, n int) []byte {
	for range n {
		b = append(b, '\\')
	}
	return b
}

// UninstallCommand returns the command that uninstalls e with its own UI: msiexec /x {ProductCode} for a Windows Installer product, otherwise its UninstallString. systemRoot is %SystemRoot%; exists tells ParseCommandLine which regular files exist (never folders: C:\Program Files exists, but is not the program).
func UninstallCommand(e Entry, systemRoot string, exists func(string) bool) (CommandLine, error) {
	if e.WindowsInstaller && e.ProductCode != "" {
		params := "/x " + e.ProductCode
		return CommandLine{Program: strings.TrimRight(systemRoot, `\`) + `\System32\msiexec.exe`, Params: params, Args: SplitArgs(params)}, nil
	}
	return ParseCommandLine(e.UninstallString, exists)
}
