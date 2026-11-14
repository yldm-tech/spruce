package devjunk

import (
	"time"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
)

// Project is one project of the 项目 view: its build output, how long it has been idle and what git says about it.
type Project struct {
	// ID is cleanup.ItemID(Path).
	ID   string `json:"id"`
	Path string `json:"path"`
	// Label is the project's path relative to its root (projectLabel).
	Label      string           `json:"label"`
	LastActive time.Time        `json:"lastActive"`
	Git        cleanup.GitState `json:"git"`
	// Hold is why the project's build output must not be cleaned, "" when it may.
	Hold string `json:"hold,omitempty"`
	Size int64  `json:"size"`
	// Kinds are the kinds of build output it has: 「node_modules」「.next」…
	Kinds []string      `json:"kinds"`
	Items []ProjectItem `json:"items"`
}

// ProjectItem is one build output folder of a project in a scan.
type ProjectItem struct {
	// CategoryID is the merged ID in a merged scan.
	CategoryID string `json:"categoryId"`
	ItemID     string `json:"itemId"`
}
