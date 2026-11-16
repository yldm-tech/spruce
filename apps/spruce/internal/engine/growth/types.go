// Package growth keeps periodic size snapshots of the main locations (caches, downloads, developer folders, project roots) in the data folder and tells what grew between two of them, for 概览 and 空间透视's 最近变化. Snapshots only: no FSEvents or USN tracking. It does not import any UI framework.
package growth

import (
	"time"
)

const (
	// Version is the snapshot file format.
	Version = 1
	// ChildMin is the smallest child a location lists.
	ChildMin = 20 << 20
	// MaxChildren is how many children a location lists at most.
	MaxChildren = 100
)

// Watch is one location snapshots measure.
type Watch struct {
	// ID is stable across snapshots: "library-caches", "downloads", "dev-root:0" …
	ID string
	// Label is the location's friendly name, 「下载」「Xcode 编译缓存」…, "" for none.
	Label string
	// Icon is a category tile key.
	Icon string
	Path string
	// Children measures each child as well as the total.
	Children bool
}

// Entry is one child of a location in a snapshot.
type Entry struct {
	Name  string `json:"n"`
	Bytes int64  `json:"b"`
}

// Location is one watched location in a snapshot.
type Location struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	// Children are at least ChildMin each, largest first, at most MaxChildren.
	Children []Entry `json:"children,omitempty"`
	// Rest is what the children left out add up to.
	Rest int64 `json:"rest,omitempty"`
	// Denied counts what could not be read for lack of permission.
	Denied int64 `json:"denied,omitempty"`
	// Skipped is set when the read gate refused the location: it has no sizes.
	Skipped bool `json:"skipped,omitempty"`
}

// Snapshot is the sizes of the watched locations at one time.
type Snapshot struct {
	Version   int        `json:"version"`
	At        time.Time  `json:"at"`
	Free      int64      `json:"free"`
	Total     int64      `json:"total"`
	Locations []Location `json:"locations"`
}

// TakeOptions tune Take.
type TakeOptions struct {
	// MayRead is the read gate; nil reads everything.
	MayRead func(path string) bool
	// Workers is fsx.SizeOptions.Workers: 2.
	Workers int
	// Budget is how long a snapshot may take (10 min); past it nothing is saved.
	Budget time.Duration
	// Usage reads the system volume's total and free space.
	Usage func(path string) (total, free int64, err error)
	// FullDiskAccess reports whether Spruce has Full Disk Access (macOS); the watches of other apps' data (~/Library/Containers and Group Containers, AppDataWatch) are read only when it says so, since reading them without it asks the user for access. Nil never reads them.
	FullDiskAccess func() bool
}

// Retention is how many snapshots are kept.
type Retention struct {
	// Daily, Weekly and MaxFiles: 14, 12, 40.
	Daily, Weekly, MaxFiles int
	// MaxBytes is the most the snapshots may take together: 8 MB.
	MaxBytes int64
}

// Growth is one location or child that grew between two snapshots.
type Growth struct {
	Path, Label, Icon string
	Before, After     int64
	// Since is when the older snapshot was taken.
	Since time.Time
	// AtLeast is set when the child was below ChildMin in the older snapshot, so it grew by at least the difference shown.
	AtLeast bool
}
