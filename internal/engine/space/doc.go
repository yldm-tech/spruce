// Package space is the engine of the 空间透视 page: it lists the disks and quick targets worth looking at, walks a folder into a bounded size tree, and decides what the page may move to the Trash from it. It does not import mygo; internal/services/space.go adapts it to the page.
//
// # What lives where
//
//   - volumes.go, volumes_darwin.go, volumes_windows.go: Volumes (gopsutil's partitions and usage, filtered to the disks of the user: on macOS the startup disk once, as "/" with the data volume's capacity; on Windows each fixed drive letter) and Targets (home, Downloads, and Applications or Program Files).
//   - scan.go: Scan, the concurrent, cancellable walk. It never follows symlinks or reparse points, stays on the root's volume (a scan of the macOS startup disk "/" also enters the data volume through its firmlinks and leaves out /System/Volumes, see planFor), counts allocated bytes as fsx.DirSize does and reports progress at most every Options.ProgressEvery.
//   - tree.go: Tree, the result. Every folder gets a node, but each keeps only its Options.MaxChildren (300) largest children plus one KindOther node for the rest, and only files of at least Options.MinFileNode (1 MB) get nodes; smaller files become one KindFiles node per folder. A folder is folded as soon as everything under it is read, so the dropped subtrees are garbage long before the walk ends. The largest files of the whole tree (Options.LargeFileMin and up, Options.MaxLargeFiles of them) are kept apart, so a large file is listed even when its folder was folded away. Remove takes an entry out after it went to the Trash and corrects the sizes of every folder above it without walking again.
//   - guard.go: Guard, which decides whether a path may go to the Trash (strictly inside the home folder or the scanned folder, not a system location, not excluded, and accepted by fsx.CheckTrashable).
//
// # Not reused from fsx
//
// fsx.DirSize measures one total and keeps no tree; its walker, allocation size and device helpers are unexported, so this package has its own copies of usage and deviceOf (usage_darwin.go, usage_windows.go) that count exactly as fsx does.
//
// fsx.CheckTrashable needs an allowed root strictly above the path and refuses the home folder and volume roots as roots, so Guard checks each path against its own parent folder after its own containment check, and refuses items directly in the home folder or in a volume root (ErrTopLevel): fsx has no way to vouch for them.
package space
