package services

import "github.com/yldm-tech/spruce/internal/engine/devjunk"

// DevJunkCommand is a category of the 开发者垃圾 scan that is cleaned by running its tool's own command rather than by deleting files.
type DevJunkCommand struct {
	CategoryID string `json:"categoryId"`
	// Command is what runs, as typed in a terminal, e.g. "brew cleanup -s --prune=all".
	Command string `json:"command"`
}

// DevJunkDetails is what the 开发者垃圾 page needs about its last scan beyond the cleanup model: which items start checked (a category may start checked in part, such as every Xcode device support version but the newest) and which categories run a command, which its confirmation counts apart.
type DevJunkDetails struct {
	// DefaultItems are the IDs of the items that start checked.
	DefaultItems []string `json:"defaultItems"`
	// Commands are the command categories, in scan order.
	Commands []DevJunkCommand `json:"commands"`
}

// DevJunk is the bound service of the 开发者垃圾 page. The page scans and cleans through Cleanup with CleanerDevJunk; this service only describes the last scan.
type DevJunk struct {
	cleaner *devjunk.Cleaner
}

// NewDevJunk returns the service over the cleaner main.go registers for CleanerDevJunk.
func NewDevJunk(c *devjunk.Cleaner) *DevJunk {
	return &DevJunk{cleaner: c}
}

// Details describes the last scan of Cleanup.scan("dev-junk"); empty before any scan.
func (d *DevJunk) Details() DevJunkDetails {
	src := d.cleaner.Details()
	out := DevJunkDetails{DefaultItems: src.DefaultItems, Commands: make([]DevJunkCommand, 0, len(src.Commands))}
	for _, c := range src.Commands {
		out.Commands = append(out.Commands, DevJunkCommand{CategoryID: c.CategoryID, Command: c.Command})
	}
	return out
}
