// Package fsx is the file system layer every cleaner shares: measuring directories (DirSize), deleting permanently but only inside allowed roots (SafeRemove), revealing an item in Finder or Explorer (Reveal) and matching the user's exclusions (Excluded). It does not import any UI framework.
//
// # Measuring
//
//	// DirSize walks root concurrently and returns its size. It never follows symlinks (or Windows junctions and other reparse points), stays on root's volume unless SizeOptions.CrossDevices is set (macOS; on Windows other volumes are reachable only through reparse points, which are never followed), counts allocated bytes on macOS (st_blocks*512, hard-linked files once) and file sizes on Windows, and counts unreadable directories in SizeResult.Denied instead of failing. Only a root that cannot be read at all, or a cancelled ctx, returns an error; the partial result comes with it.
//	func DirSize(ctx context.Context, root string, opts SizeOptions) (SizeResult, error)
//
// # Deleting
//
//	// SafeRemove permanently deletes path (a file, a symlink or a directory tree) if CheckRemovable accepts it, and returns what it freed and every entry below path it could not delete.
//	func SafeRemove(ctx context.Context, path string, allowedRoots []string) (Removal, error)
//
//	// CheckRemovable reports why path must not be deleted permanently, or nil: it must be absolute, strictly inside one of allowedRoots once symlinks in its parent and in the roots are resolved, and not protected.
//	func CheckRemovable(path string, allowedRoots []string) error
//
//	// CheckTrashable is CheckRemovable for moving to the Trash, which can be undone: the protected subtrees (Documents, Desktop, Pictures …) are allowed, the protected folders themselves are not.
//	func CheckTrashable(path string, allowedRoots []string) error
//
// Protection has two levels, built per platform from the user's home folder (protect_darwin.go, protect_windows.go):
//
//   - Folders that are never deleted themselves but may hold junk: volume roots, the home folder and the folder holding all homes, the standard folders of the home (Documents, Downloads, Library, Library/Caches, AppData …), /Applications, /Library, C:\Windows, Program Files and so on. Every allowed root is in this set too.
//   - Subtrees where SafeRemove deletes nothing at all: the operating system (/System, /usr/bin, C:\Windows\System32, WinSxS …), keychains and credential stores, iCloud Drive and other cloud folders, mail and messages, ~/.ssh, and the user's Documents, Desktop and Pictures. A cleaner that offers items there (an installer on the Desktop, a node_modules folder under Documents) must use the Trash.
//
// An allowed root that is itself a volume root, the home folder or inside a protected subtree is ignored, so a misconfigured root cannot widen what may be deleted. Paths compare case-insensitively against the protection lists on both platforms (APFS and NTFS are case-insensitive by default) and against allowed roots case-sensitively on macOS and case-insensitively on Windows; both choices err towards refusing.
//
// Deletion runs through an os.Root opened at the allowed root, so even a folder swapped for a symlink while the deletion runs cannot lead it outside the root, and it never follows a symlink: a link is removed, not its target. Files that are open in another program on Windows (sharing or lock violation) are skipped and reported with Failure.InUse.
//
// # Revealing and exclusions
//
//	// Reveal shows path selected in Finder (/usr/bin/open -R through runner) or Explorer (SHOpenFolderAndSelectItems; runner is not used there).
//	func Reveal(ctx context.Context, runner execx.Runner, path string) error
//
//	// Excluded reports whether path is one of exclusions or inside one. An exclusion is an absolute path (a leading "~" is the home folder), a name or pattern without a separator, matched against every component of path ("node_modules", "*.photoslibrary"), or an absolute pattern with * ? or [ ], matched against path and each of its parents ("~/Logs/*.log").
//	func Excluded(path string, exclusions []string) bool
//
// The walk uses the standard library (os.File.ReadDir with a bounded pool of goroutines) rather than github.com/karrick/godirwalk: godirwalk is single-threaded and has not been released since 2021, and on Go 1.16+ os.ReadDir already reads directory entries without a stat per entry where the platform allows, which was godirwalk's main advantage.
package fsx
