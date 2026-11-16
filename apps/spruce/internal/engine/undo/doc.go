// Package undo is the ledger behind 撤销 and 放回原处: for each clean, uninstall or 空间透视 move it keeps what went to the Trash, where it came from and how to recognise it there, so the user can put it back for Window after the move. It does not import any UI framework.
//
// The ledger lives in one folder of the app's data folder (<data>/undo, 0700, made on the first write):
//
//   - <runID>.json is one Entry per history run, written atomically (temporary file, fsync, rename; 0600). Only moves Spruce can put back are kept (Keepable): the system said where the item went, inside a Trash folder trash.Restorer accepts, with its identity, and the original lies strictly inside the recorded root. At most MaxMoves per entry, MaxEntries entries, MaxBytes in all and Window of age (Prune).
//   - <runID>.partial is the journal of a clean that is still running: a line with JournalMeta, then one line per move, each written and fsynced as the move happens (Journal.Add), so a move is on disk even if Spruce ends before the clean returns. Record removes it; Recover turns a leftover one into an entry at the next launch, for the caller to add the history run Spruce could not write.
//   - restored.json holds the item IDs (hashes of the paths, never the paths) the user put back, with when, at most 5000 for 180 days, so an automatic rule never takes again what the user put back (Restored).
//
// Restore puts back every unrestored move of a run through a trash.Restorer, rewrites its entry and records the item IDs in restored.json. It never overwrites anything: an occupied original location is reported (or, for the user's own files, the item comes back under "<name> 2"), a missing folder is not created.
package undo
