package devjunk

import (
	"regexp"
	"slices"
	"strconv"
	"time"
)

// dottedVersion finds a version such as 17.0.3 in a DeviceSupport folder name ("iPhone15,2 17.0.3 (21A360)").
var dottedVersion = regexp.MustCompile(`\d+(?:\.\d+)+`)

// jetBrainsDir splits a JetBrains folder name such as IntelliJIdea2024.1 or PyCharmCE2023.3 into product and version.
var jetBrainsDir = regexp.MustCompile(`^([A-Za-z][A-Za-z-]*?)(\d{4}\.\d+(?:\.\d+)?)$`)

// gradleDist finds the version of a Gradle wrapper distribution folder such as gradle-8.10.2-bin.
var gradleDist = regexp.MustCompile(`^gradle-(\d+(?:\.\d+)*)`)

// parseVersion returns the numbers of a version string such as "17.0.3"; nil when s is empty or not all numbers.
func parseVersion(s string) []int {
	if s == "" {
		return nil
	}
	var out []int
	start := 0
	for i := 0; i <= len(s); i++ {
		if i < len(s) && s[i] != '.' {
			continue
		}
		n, err := strconv.Atoi(s[start:i])
		if err != nil {
			return nil
		}
		out = append(out, n)
		start = i + 1
	}
	return out
}

// versioned is an entry with the version its name carries (nil when none) and its modification time, for picking the newest.
type versioned struct {
	version []int
	modTime time.Time
}

// compareVersioned orders by version, a missing version first, then by modification time.
func compareVersioned(a, b versioned) int {
	switch {
	case a.version == nil && b.version != nil:
		return -1
	case a.version != nil && b.version == nil:
		return 1
	}
	if c := slices.Compare(a.version, b.version); c != 0 {
		return c
	}
	return a.modTime.Compare(b.modTime)
}

// newestIndex returns the index of the newest of vs, or -1 for none.
func newestIndex(vs []versioned) int {
	best := -1
	for i, v := range vs {
		if best < 0 || compareVersioned(v, vs[best]) > 0 {
			best = i
		}
	}
	return best
}
