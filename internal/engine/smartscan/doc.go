// Package smartscan is the engine of the 智能扫描 page: it runs the scans of several cleaners at once (系统垃圾, 开发者垃圾 and 安装包, in that order), merges what they found into one result, and hands each part of a clean back to the cleaner that found it, so every check of that cleaner applies. Scanner implements cleanup.Scanner; wire.go registers it under app.CleanerSmartScan. It does not import any UI framework and has no scanning logic of its own.
//
// # Own instances
//
// A cleanup.Scanner remembers its last scan, and its Clean deletes only from that scan. If 智能扫描 used the scanners the cleaner pages use, scanning on a page would replace what 智能扫描 shows (its clean would then be rejected as stale, or act on a scan the user did not look at), and the other way round. So each Source gives a constructor, and New builds instances that only 智能扫描 uses.
//
// # Merging
//
// Every category of a source keeps its title, description, icon, kind, deletion mode, command, warning and default selection (Category.Selected and Item.Selected); its ID becomes "<source>:<id>" and Category.Source names the source. An item that an earlier source already listed (the same path) is left out of the later one. ScanResult.Denied sums the sources'.
//
// A source whose scan fails does not fail the others: its SourceState says so with a sentence for the page, and the result has the other sources. Scan fails only when every source failed (with the first source's error) or ctx ended.
//
// # Progress
//
// While scanning, every event carries Progress.Sources, the state of every source, so an event the services layer throttles away loses nothing. Done and Total add up the sources' fractions in thousandths, so Done equals Total only on the last event.
//
// # Cleaning
//
// Clean checks the whole request against the merged scan first (an unknown category or item rejects it with cleanup.ErrStale before anything is deleted), then cleans source by source, in source order, naming every item explicitly. Progress and the result are the sources' added up, with category IDs in the merged form and CategoryOutcome.Source set. Stopping takes effect between sources too. CleanRequest.Mode goes to every source, and the items each source moved to the Trash are reported, with merged category IDs, in CleanResult.Moved and to the callback cleanup.WithOnMoved put on the context.
//
// Clean keeps the rest of the merged scan, so several cleans can follow one scan: it drops what was handled (every planned item that did not fail; after an interrupted clean only what reached the Trash), and a command category once its command ran. A scan that did not finish with every source (stopped, failed or still running) leaves the sources with remembered scans the merged one does not match, so until a scan completes Clean refuses with cleanup.ErrStale and Pending reports true.
package smartscan
