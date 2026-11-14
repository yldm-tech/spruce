// Package trash moves items to the macOS Trash or the Windows Recycle Bin, so a clean-up can be undone, and measures and empties it. It does not import mygo.
//
//	// MoveToTrash moves each of paths to the Trash and returns one Outcome per path, in order. A path that fails does not stop the others; the error is non-nil only when ctx ends the batch, and the Outcomes of the paths not reached then say so.
//	func MoveToTrash(ctx context.Context, paths []string) ([]Outcome, error)
//
//	// TrashSize returns how much the Trash holds.
//	func TrashSize(ctx context.Context) (TrashUsage, error)
//
//	// Empty deletes everything in the Trash for good and returns what it freed. It cannot be undone: the page confirms first.
//	func Empty(ctx context.Context) (TrashUsage, error)
//
// The package-level functions use System(); tests and callers that must not touch the real Trash take a Bin and pass a fake.
//
// # Platforms
//
// macOS moves items with NSFileManager trashItemAtURL:resultingItemURL:error:, the API Finder itself uses, called through the Objective-C runtime with github.com/ebitengine/purego/objc rather than cgo, since the app builds with CGO_ENABLED=0 as MyGo does: no Apple Events, so no Automation permission prompt and no Finder round trip per item; "Put Back" works as for items deleted in Finder. github.com/Bios-Marcel/wastebasket/v2 (v2.0.3) was evaluated and rejected: on macOS it runs osascript to tell Finder to delete each item, which triggers the Automation prompt and is slow, and it is MPL-2.0 licensed. TrashSize measures ~/.Trash and the per-user .Trashes folder of every mounted volume with fsx.DirSize; Empty deletes their contents with fsx.SafeRemove. Reading ~/.Trash needs Full Disk Access on macOS 10.15 and later, so both may fail with an error matching fs.ErrPermission (ErrNoAccess).
//
// Windows moves items with SHFileOperationW (FO_DELETE with FOF_ALLOWUNDO | FOF_NOCONFIRMATION | FOF_SILENT | FOF_NOERRORUI, plus FOF_WANTNUKEWARNING) through golang.org/x/sys/windows, without cgo. FOF_WANTNUKEWARNING matters: an item too large for the Recycle Bin, or on a drive without one, would otherwise be deleted for good without a word; with it the shell asks first, which is the only dialog this package can show. TrashSize and Empty use SHQueryRecycleBinW and SHEmptyRecycleBinW (SHERB_NOCONFIRMATION | SHERB_NOPROGRESSUI | SHERB_NOSOUND) over all drives.
package trash
