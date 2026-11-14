package devjunk

import (
	"cmp"
	"path/filepath"
	"slices"
	"strings"
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

// GroupProjects groups the build output items of a merged or devjunk scan by Item.Project, largest first (then by path). Label is the first part of the items' Note, which the scan starts with the project's path relative to its root; LastActive is the items' ModTime, the project's last activity; Kinds are the build output folder names, largest first. Pure.
func GroupProjects(res cleanup.ScanResult) []Project {
	type group struct {
		Project
		kinds map[string]int64
	}
	byPath := map[string]*group{}
	var out []*group
	for _, c := range res.Categories {
		for _, it := range c.Items {
			if it.Project == "" {
				continue
			}
			g := byPath[it.Project]
			if g == nil {
				g = &group{Project: Project{ID: cleanup.ItemID(it.Project), Path: it.Project, Label: labelOf(it), Git: it.Git, Kinds: []string{}, Items: []ProjectItem{}}, kinds: map[string]int64{}}
				byPath[it.Project] = g
				out = append(out, g)
			}
			if it.ModTime.After(g.LastActive) {
				g.LastActive = it.ModTime
			}
			if g.Hold == "" {
				g.Hold = it.Hold
			}
			g.Size += it.Size
			g.Items = append(g.Items, ProjectItem{CategoryID: c.ID, ItemID: it.ID})
			g.kinds[kindName(it.Project, it.Path)] += it.Size
		}
	}
	slices.SortStableFunc(out, func(a, b *group) int { return cmp.Or(cmp.Compare(b.Size, a.Size), cmp.Compare(a.Path, b.Path)) })
	projects := make([]Project, len(out))
	for i, g := range out {
		for k := range g.kinds {
			g.Kinds = append(g.Kinds, k)
		}
		slices.SortFunc(g.Kinds, func(a, b string) int { return cmp.Or(cmp.Compare(g.kinds[b], g.kinds[a]), cmp.Compare(a, b)) })
		projects[i] = g.Project
	}
	return projects
}

// labelOf is the project label the scan put first in the item's Note, or the project folder's name.
func labelOf(it cleanup.Item) string {
	if label, _, _ := strings.Cut(it.Note, " · "); label != "" {
		return label
	}
	return filepath.Base(it.Project)
}

// kindName is how the 项目 view names a build output folder: its folder name, or the rule's folder and sub-folder (.angular/cache).
func kindName(project, path string) string {
	name := filepath.Base(path)
	parent := filepath.Base(filepath.Dir(path))
	for _, r := range artifactRules {
		if r.sub != "" && r.sub == name && r.name == parent && filepath.Dir(filepath.Dir(path)) == project {
			return r.name + "/" + r.sub
		}
	}
	return name
}
