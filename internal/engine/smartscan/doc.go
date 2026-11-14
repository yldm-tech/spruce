// Package smartscan is the engine of the 智能扫描 page: it runs the scans of several cleaners at once (系统垃圾, 开发者垃圾 and 安装包, in that order), merges what they found into one result, and hands each part of a clean back to the cleaner that found it, so every check of that cleaner applies. Scanner implements cleanup.Scanner; main.go registers it under services.CleanerSmartScan. It does not import mygo and has no scanning logic of its own.
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
// Clean checks the whole request against the merged scan first (an unknown category or item rejects it with cleanup.ErrStale before anything is deleted), then cleans source by source, in source order, naming every item explicitly. Progress and the result are the sources' added up, with category IDs in the merged form and CategoryOutcome.Source set. Stopping takes effect between sources too. A merged scan serves one clean: the page scans again before the next.
package smartscan
