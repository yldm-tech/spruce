// Package spacefake is the 空间透视 page's Service and Files for tests: a scan the test scripts, a kept scan with its folders and large files, and moves to the Trash that answer from fields. Only tests import it.
package spacefake

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/yldm-tech/spruce/internal/app"
)

// Service is a fake *app.Space.
type Service struct {
	mu sync.Mutex

	// Kept is what Current answers first; a scan that succeeds replaces it.
	Kept *app.SpaceSummary
	// Listings are the folders Children answers, by id; any other id is an error.
	Listings map[int]app.SpaceListing
	Large    []app.SpaceEntry
	// OnScan runs a scan; without it a scan waits until its ctx ends.
	OnScan func(ctx context.Context, path string, progress func(app.SpaceProgress)) (app.SpaceSummary, error)
	// TrashResult and TrashErr are what Trash answers.
	TrashResult app.SpaceTrashResult
	TrashErr    error

	scans    []string
	children []int
	larges   int
	trashes  [][]string
}

// Scan implements the page's Service.
func (s *Service) Scan(ctx context.Context, path string, progress func(app.SpaceProgress)) (app.SpaceSummary, error) {
	s.mu.Lock()
	s.scans = append(s.scans, path)
	run := s.OnScan
	s.mu.Unlock()
	if run == nil {
		<-ctx.Done()
		return app.SpaceSummary{}, errors.New("已取消")
	}
	sum, err := run(ctx, path, progress)
	if err == nil {
		s.mu.Lock()
		s.Kept = &sum
		s.mu.Unlock()
	}
	return sum, err
}

// Current implements the page's Service.
func (s *Service) Current() *app.SpaceSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Kept
}

// Children implements the page's Service.
func (s *Service) Children(scanID string, id int) (app.SpaceListing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.children = append(s.children, id)
	l, ok := s.Listings[id]
	if !ok {
		return app.SpaceListing{}, errors.New("这个文件夹已经不在扫描结果里了")
	}
	return l, nil
}

// LargeFiles implements the page's Service.
func (s *Service) LargeFiles(scanID string) ([]app.SpaceEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.larges++
	return s.Large, nil
}

// Trash implements the page's Service.
func (s *Service) Trash(ctx context.Context, scanID string, paths []string) (app.SpaceTrashResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trashes = append(s.trashes, paths)
	return s.TrashResult, s.TrashErr
}

// Scans returns the paths scanned, oldest first.
func (s *Service) Scans() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.scans...)
}

// ChildrenCalls returns the folder ids asked for, oldest first.
func (s *Service) ChildrenCalls() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.children...)
}

// LargeCalls counts the calls of LargeFiles.
func (s *Service) LargeCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.larges
}

// Trashes returns the paths of every call of Trash.
func (s *Service) Trashes() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]string(nil), s.trashes...)
}

// SetListing replaces the folder id that Children answers.
func (s *Service) SetListing(id int, l app.SpaceListing) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Listings[id] = l
}

// Files is a fake *app.Files.
type Files struct {
	mu      sync.Mutex
	Err     error
	reveals []string
}

// Reveal implements the page's Files.
func (f *Files) Reveal(ctx context.Context, path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reveals = append(f.reveals, path)
	return f.Err
}

// Reveals returns the paths shown, oldest first.
func (f *Files) Reveals() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.reveals...)
}

// Home is the home folder of the fixtures.
const Home = "/Users/me"

// Entry returns an entry of the home folder: a folder or a file at Home/name, or a folded group without a path.
func Entry(id int, kind app.SpaceKind, name string, size int64) app.SpaceEntry {
	path := ""
	switch {
	case (kind == app.SpaceDir || kind == app.SpaceFile) && id == 0:
		path = Home
	case kind == app.SpaceDir || kind == app.SpaceFile:
		path = Home + "/" + name
	}
	return app.SpaceEntry{ID: id, Kind: kind, Name: name, Path: path, Size: size, Files: 1}
}

// The entries of the fixture, as SpacePage.test.tsx had them.
var (
	Root     = with(Entry(0, app.SpaceDir, "个人文件夹", 10e9), func(e *app.SpaceEntry) { e.Files = 5000 })
	Projects = with(Entry(1, app.SpaceDir, "Projects", 6e9), func(e *app.SpaceEntry) { e.Files = 4000 })
	ISO      = with(Entry(2, app.SpaceFile, "big.iso", 3e9), func(e *app.SpaceEntry) {
		e.ModTime = time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)
	})
	Movies = with(Entry(5, app.SpaceDir, "Movies", 0.5e9), func(e *app.SpaceEntry) {
		e.Locked = "个人文件夹和磁盘最上一层的项目请在访达中处理"
	})
	Other = with(Entry(3, app.SpaceOther, "", 0.3e9), func(e *app.SpaceEntry) { e.Count = 4 })
	Small = with(Entry(4, app.SpaceFiles, "", 0.2e9), func(e *app.SpaceEntry) { e.Count = 30 })
	App   = with(Entry(6, app.SpaceDir, "app", 4e9), func(e *app.SpaceEntry) { e.Path = Home + "/Projects/app" })
	Data  = with(Entry(7, app.SpaceFile, "data.bin", 2e9), func(e *app.SpaceEntry) { e.Path = Home + "/Projects/data.bin" })
)

func with(e app.SpaceEntry, fn func(*app.SpaceEntry)) app.SpaceEntry {
	fn(&e)
	return e
}

// Locations are the start screen's disks and folders.
func Locations() app.SpaceLocations {
	return app.SpaceLocations{
		Volumes: []app.SpaceVolume{
			{Path: "/", Name: "Macintosh HD", Total: 500e9, Free: 120e9, Startup: true},
			{Path: "/Volumes/Archive", Name: "Archive", Total: 2e12, Free: 1.9e12},
		},
		Targets: []app.SpaceTarget{
			{ID: "home", Name: "个人文件夹", Path: Home},
			{ID: "downloads", Name: "下载", Path: Home + "/Downloads"},
			{ID: "applications", Name: "应用程序", Path: "/Applications"},
		},
	}
}

// Summary is the kept scan of the home folder, scanned at.
func Summary(at time.Time) app.SpaceSummary {
	return app.SpaceSummary{ScanID: "scan-1", Name: "个人文件夹", Root: Root, Files: 5000, Dirs: 700, DeniedPaths: []string{},
		ScannedAt: at, Duration: 3 * time.Second, LargeFileMin: 500e6}
}

// Listings are the home folder and Projects.
func Listings() map[int]app.SpaceListing {
	return map[int]app.SpaceListing{
		0: {Entry: Root, Ancestors: []app.SpaceEntry{Root}, Children: []app.SpaceEntry{Projects, ISO, Movies, Other, Small}},
		1: {Entry: Projects, Ancestors: []app.SpaceEntry{Root, Projects}, Children: []app.SpaceEntry{App, Data}},
	}
}

// New returns a service that keeps the home folder's scan, made at, with its two folders and two large files.
func New(at time.Time) *Service {
	sum := Summary(at)
	return &Service{Kept: &sum, Listings: Listings(), Large: []app.SpaceEntry{ISO, Data}}
}

// Crowded returns a service keeping a scan of a home folder with 300 children of falling sizes and the two folded groups, 302 cells as a real home folder gives, for the screenshots.
func Crowded(at time.Time) *Service {
	names := []string{"Library", "Projects", "Movies", "Music", "Pictures", "Documents", "Downloads", "Desktop", ".cache", ".npm", "go", ".rustup", ".cargo", "Applications", "Public"}
	var children []app.SpaceEntry
	var total int64
	for i := range 300 {
		size := int64(60e9 / float64(i+1) / (1 + float64(i)/40))
		name := fmt.Sprintf("folder-%03d", i)
		if i < len(names) {
			name = names[i]
		}
		kind := app.SpaceDir
		if i%5 == 4 {
			kind, name = app.SpaceFile, fmt.Sprintf("archive-%03d.zip", i)
		}
		e := Entry(i+10, kind, name, size)
		e.Files = size / 4e6
		children = append(children, e)
		total += size
	}
	other := with(Entry(1000, app.SpaceOther, "", 0.9e9), func(e *app.SpaceEntry) { e.Count = 1840 })
	small := with(Entry(1001, app.SpaceFiles, "", 0.4e9), func(e *app.SpaceEntry) { e.Count = 9120 })
	children = append(children, other, small)
	total += other.Size + small.Size
	root := with(Entry(0, app.SpaceDir, "个人文件夹", total), func(e *app.SpaceEntry) { e.Files = 412_000 })
	sum := Summary(at)
	sum.Root = root
	return &Service{Kept: &sum, Large: []app.SpaceEntry{ISO, Data},
		Listings: map[int]app.SpaceListing{0: {Entry: root, Ancestors: []app.SpaceEntry{root}, Children: children}}}
}
