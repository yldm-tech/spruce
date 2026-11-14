package cleanup

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"slices"
	"time"
)

// CategoryKind is what sort of data a Category holds; the page may group or word categories by it.
type CategoryKind string

const (
	// KindCache is application and system caches.
	KindCache CategoryKind = "cache"
	// KindLog is log files and crash reports.
	KindLog CategoryKind = "log"
	// KindTrash is the Trash or Recycle Bin.
	KindTrash CategoryKind = "trash"
	// KindDevCache is developer tool caches (package managers, Xcode DerivedData, simulators).
	KindDevCache CategoryKind = "devCache"
	// KindProjectArtifact is build output inside projects (node_modules, target, build).
	KindProjectArtifact CategoryKind = "projectArtifact"
	// KindInstaller is downloaded installers (.dmg, .pkg, .exe, .msi).
	KindInstaller CategoryKind = "installer"
	// KindLeftover is files an uninstalled app left behind.
	KindLeftover CategoryKind = "leftover"
	// KindLargeFile is large or old files the user may not need.
	KindLargeFile CategoryKind = "largeFile"
)

// DeletionMode is how a category's items are removed.
type DeletionMode string

const (
	// DeletePermanent deletes for good, through fsx.SafeRemove.
	DeletePermanent DeletionMode = "permanent"
	// DeleteTrash moves to the Trash, which the user can undo.
	DeleteTrash DeletionMode = "trash"
)

// Item is one file or folder a cleaner offers.
type Item struct {
	// ID is stable for a path (ItemID) and unique within a ScanResult.
	ID   string `json:"id"`
	Path string `json:"path"`
	// Size is the space it takes, in bytes (fsx.DirSize Bytes for folders).
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
	// Dir is set for a folder.
	Dir bool `json:"dir,omitempty"`
	// Note is an optional short line in Simplified Chinese, e.g. "3 个月未修改" or the project a build folder belongs to.
	Note string `json:"note,omitempty"`
	// Selected is whether the item starts checked. A scanner sets it per item when a category starts checked in part (every Xcode device support version but the newest); NewResult sets it on every item of a category that starts checked, so the page reads the default selection from the items alone.
	Selected bool `json:"selected,omitempty"`

	// symlink records that the scan saw a symlink, which is the only case in which the item may be one when it is deleted.
	symlink bool
}

// NewItem returns the Item for path, as os.Lstat described it, with size measured by the scanner.
func NewItem(path string, info fs.FileInfo, size int64) Item {
	return Item{
		ID:      ItemID(path),
		Path:    path,
		Size:    size,
		ModTime: info.ModTime(),
		Dir:     info.Mode().Type() == fs.ModeDir,
		symlink: info.Mode().Type() == fs.ModeSymlink,
	}
}

// ItemID returns the stable ID of path: 16 hex digits of its SHA-256.
func ItemID(path string) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:8])
}

// Category is one group of items the page lists, with one checkbox.
type Category struct {
	// ID is stable across scans and unique within a ScanResult, e.g. "user-caches".
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Icon is a semantic icon key the page maps to a glyph (see src/components/common/CategoryIcon.tsx), e.g. "cache", "log", "xcode", "node".
	Icon     string       `json:"icon"`
	Kind     CategoryKind `json:"kind"`
	Deletion DeletionMode `json:"deletion"`
	// Items are sorted largest first by NewResult.
	Items []Item `json:"items"`
	// Size is the sum of the items' sizes.
	Size int64 `json:"size"`
	// Selected is whether the category starts checked as a whole. NewResult sets it when every item is Selected, and marks every item Selected when it is set.
	Selected bool `json:"selected"`
	// Command, when set, is the tool's own clean command that cleans the category instead of deleting its items, as typed in a terminal, e.g. "brew cleanup -s --prune=all"; the page names it in the confirmation.
	Command string `json:"command,omitempty"`
	// Warning is an optional caution in Simplified Chinese shown in the warning colour, e.g. "删除后 Xcode 需要重新建立索引".
	Warning string `json:"warning,omitempty"`
	// Source is, in a combined scan (智能扫描), the cleaner the category comes from, e.g. "system-junk"; empty on a cleaner's own page.
	Source string `json:"source,omitempty"`
	// Roots are the folders the items may be deleted from; they stay in Go.
	Roots []string `json:"-"`
}

// ScanResult is everything a scan found.
type ScanResult struct {
	Categories []Category `json:"categories"`
	// Size and Items total every category.
	Size  int64 `json:"size"`
	Items int   `json:"items"`
	// Denied counts locations that could not be read for lack of permission (on macOS usually Full Disk Access), so the page can say the result may be incomplete.
	Denied    int64         `json:"denied"`
	ScannedAt time.Time     `json:"scannedAt"`
	Duration  time.Duration `json:"duration"`
	// Sources is, for a combined scan (智能扫描), how each of its cleaners did; empty on a cleaner's own page.
	Sources []SourceState `json:"sources,omitempty"`
}

// SourceStatus is where one cleaner of a combined scan is.
type SourceStatus string

const (
	// SourceScanning is a cleaner whose scan is still running.
	SourceScanning SourceStatus = "scanning"
	// SourceDone is a cleaner whose scan finished; its categories are in the result.
	SourceDone SourceStatus = "done"
	// SourceFailed is a cleaner whose scan failed; the result has nothing of it and Error says why.
	SourceFailed SourceStatus = "failed"
)

// SourceState is how one cleaner of a combined scan (智能扫描) is doing: streamed in Progress.Sources while it scans, and in ScanResult.Sources once the scan is over.
type SourceState struct {
	// ID is the cleaner, e.g. "system-junk".
	ID     string       `json:"id"`
	Status SourceStatus `json:"status"`
	// Done and Total count the cleaner's own scan steps while it scans; Total is 0 while unknown.
	Done  int `json:"done"`
	Total int `json:"total"`
	// Bytes is found so far while scanning, and the size of what it found once done.
	Bytes int64 `json:"bytes"`
	// Items is how many items it found, once done.
	Items int `json:"items"`
	// Denied counts locations it could not read (see ScanResult.Denied).
	Denied int64 `json:"denied"`
	// Error is why the scan failed, a sentence in Simplified Chinese.
	Error string `json:"error,omitempty"`
}

// NewResult sorts each category's items largest first, fills in the sizes and totals, settles the default selection (a Selected category checks all its items; a category whose items are all Selected is Selected), and stamps the scan as started at started and finished now.
func NewResult(categories []Category, started time.Time) ScanResult {
	res := ScanResult{Categories: categories, ScannedAt: time.Now(), Duration: time.Since(started)}
	for i := range res.Categories {
		c := &res.Categories[i]
		if c.Items == nil {
			c.Items = []Item{}
		}
		slices.SortStableFunc(c.Items, func(a, b Item) int { return cmp.Compare(b.Size, a.Size) })
		c.Size = 0
		all := len(c.Items) > 0
		for j := range c.Items {
			it := &c.Items[j]
			if c.Selected {
				it.Selected = true
			}
			all = all && it.Selected
			c.Size += it.Size
		}
		if len(c.Items) > 0 {
			c.Selected = all
		}
		res.Size += c.Size
		res.Items += len(c.Items)
	}
	if res.Categories == nil {
		res.Categories = []Category{}
	}
	return res
}

// CleanRequest names what to delete from the last scan. With ItemIDs empty, every item of every category in CategoryIDs goes; otherwise only the named items, each of which must belong to one of CategoryIDs.
type CleanRequest struct {
	CategoryIDs []string `json:"categoryIds"`
	ItemIDs     []string `json:"itemIds,omitempty"`
}

// Failure is one item that was not deleted.
type Failure struct {
	ItemID     string `json:"itemId"`
	CategoryID string `json:"categoryId"`
	Path       string `json:"path"`
	// Reason is a short sentence in Simplified Chinese for the page, e.g. "文件正在使用，已跳过".
	Reason string `json:"reason"`
	// Detail is the technical cause, for the log.
	Detail string `json:"detail,omitempty"`
	// InUse is set when another program held the item open.
	InUse bool `json:"inUse,omitempty"`
}

// CategoryOutcome is what a clean did in one category.
type CategoryOutcome struct {
	CategoryID string `json:"categoryId"`
	Title      string `json:"title"`
	// Source is the cleaner the category comes from in a combined clean (Category.Source).
	Source  string `json:"source,omitempty"`
	Removed int    `json:"removed"`
	Freed   int64  `json:"freed"`
	Trashed int64  `json:"trashed"`
	Failed  int    `json:"failed"`
}

// CleanResult is what a clean did.
type CleanResult struct {
	// Freed is the space given back by permanent deletion, in bytes.
	Freed int64 `json:"freed"`
	// Trashed is the scanned size of the items moved to the Trash; it is freed once the Trash is emptied.
	Trashed int64 `json:"trashed"`
	// Removed counts items deleted or moved to the Trash completely.
	Removed int `json:"removed"`
	// Skipped counts items that were already gone.
	Skipped int `json:"skipped"`
	// Failed lists the items left in place, wholly or in part.
	Failed []Failure `json:"failed"`
	// Categories has one entry per category of the request, in scan order.
	Categories []CategoryOutcome `json:"categories"`
	// Partial is set when anything failed or the clean was interrupted.
	Partial bool `json:"partial"`
	// Interrupted is set when ctx ended the clean before every item was handled.
	Interrupted bool          `json:"interrupted"`
	StartedAt   time.Time     `json:"startedAt"`
	Duration    time.Duration `json:"duration"`
}

// Phase is the stage a Progress event belongs to.
type Phase string

const (
	// PhaseScanning is sent while a scan runs.
	PhaseScanning Phase = "scanning"
	// PhaseCleaning is sent while a clean runs.
	PhaseCleaning Phase = "cleaning"
)

// Progress is streamed to the page while a scan or a clean runs.
type Progress struct {
	Phase Phase `json:"phase"`
	// CategoryID is the category being worked on, when there is one.
	CategoryID string `json:"categoryId,omitempty"`
	// Path is the folder or item being worked on.
	Path string `json:"path,omitempty"`
	// Done and Total count steps (locations while scanning, items while cleaning); Total is 0 while unknown.
	Done  int `json:"done"`
	Total int `json:"total"`
	// Bytes is found so far while scanning and freed or trashed so far while cleaning.
	Bytes int64 `json:"bytes"`
	// Sources is, while a combined scan (智能扫描) runs, the state of each of its cleaners.
	Sources []SourceState `json:"sources,omitempty"`
}

// Scanner is what a cleaner page's engine implements; see the package documentation.
type Scanner interface {
	// Scan finds what can be cleaned, reporting progress (never concurrently) as it goes.
	Scan(ctx context.Context, progress func(Progress)) (ScanResult, error)
	// Clean deletes what req names from the last scan.
	Clean(ctx context.Context, req CleanRequest, progress func(Progress)) (CleanResult, error)
}

// UserError is an error whose Message, a sentence in Simplified Chinese, the page may show as it is. A Scanner returns one for a failure the user can understand or fix (say, "需要「完全磁盘访问权限」才能扫描邮件附件"); the services layer shows other errors as a generic sentence and logs them.
type UserError struct {
	Message string
	Err     error
}

// NewUserError returns a *UserError with message for the page and err for the log.
func NewUserError(message string, err error) error {
	return &UserError{Message: message, Err: err}
}

func (e *UserError) Error() string {
	if e.Err == nil {
		return e.Message
	}
	return e.Message + ": " + e.Err.Error()
}

func (e *UserError) Unwrap() error { return e.Err }
