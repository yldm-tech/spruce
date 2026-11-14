// Package cleanup is the model and the deletion engine every cleaner page shares (系统垃圾, 开发者垃圾, 安装包, 智能扫描, and the leftovers of 应用卸载). A page's engine package scans and builds Categories of Items; Executor deletes what the user picked. It does not import mygo; the services layer binds Scanners to the page.
//
// # Writing a cleaner
//
//	type Scanner interface {
//		Scan(ctx context.Context, progress func(Progress)) (ScanResult, error)
//		Clean(ctx context.Context, req CleanRequest, progress func(Progress)) (CleanResult, error)
//	}
//
// A page's engine package (internal/engine/<page>) implements Scanner. The usual shape embeds an Executor:
//
//	type Cleaner struct{ exec *cleanup.Executor }
//
//	func (c *Cleaner) Scan(ctx context.Context, progress func(cleanup.Progress)) (cleanup.ScanResult, error) {
//		// … walk the page's locations, building Categories with NewItem …
//		res := cleanup.NewResult(categories, started)
//		if err := c.exec.Remember(res); err != nil { return cleanup.ScanResult{}, err }
//		return res, nil
//	}
//
//	func (c *Cleaner) Clean(ctx context.Context, req cleanup.CleanRequest, progress func(cleanup.Progress)) (cleanup.CleanResult, error) {
//		return c.exec.Clean(ctx, req, progress)
//	}
//
// Rules:
//
//   - Build items with NewItem from an os.Lstat of the path: it sets the stable ID and records whether the item was a folder or a symlink, which the Executor checks again before deleting.
//   - Category.Roots lists the folders the category's items live in. Permanent deletion goes through fsx.SafeRemove and the Trash through fsx.CheckTrashable, both restricted to these roots, so an item outside them is refused, whatever the scan found.
//   - Use DeletePermanent only for data that is rebuilt on demand (caches, logs, build output); anything the user made or downloaded (installers, large files, app leftovers) goes to the Trash with DeleteTrash. fsx refuses permanent deletion inside Documents, Desktop and Pictures, so items there must use the Trash.
//   - Apply the user's exclusions (settings Cleanup.Exclusions, fsx.Excluded) while scanning; the Executor only deletes what the scan returned.
//
// # Executor guarantees
//
// Clean deletes only items of the last ScanResult passed to Remember, looked up by ID; an unknown category or item ID rejects the whole request with ErrStale, so a page that shows an old scan cannot delete paths it never saw. Before each item it takes an os.Lstat again and skips the item, as a Failure, when it became a symlink or changed between file and folder; an item already gone counts as Skipped. It runs one Clean at a time per Executor (ErrBusy), stops between items when ctx ends (CleanResult.Interrupted), and drops the items it deleted from the remembered scan.
package cleanup
