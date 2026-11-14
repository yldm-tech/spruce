package services

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/settings"
	"github.com/yldm-tech/spruce/internal/engine/space"
	"github.com/yldm-tech/spruce/internal/engine/trash"
)

// SpaceKind is what a SpaceEntry stands for.
type SpaceKind string

const (
	// SpaceDir is a folder the page can open.
	SpaceDir SpaceKind = "dir"
	// SpaceFile is a file of 1 MB or more.
	SpaceFile SpaceKind = "file"
	// SpaceOther stands for the children of a folder beyond its 300 largest (「其他 N 项」).
	SpaceOther SpaceKind = "other"
	// SpaceFiles stands for a folder's files smaller than 1 MB (「N 个小文件」).
	SpaceFiles SpaceKind = "files"
)

// SpaceVolume is a disk on the start screen of 空间透视.
type SpaceVolume struct {
	// Path is what Space.scan takes to scan the whole disk.
	Path string `json:"path"`
	Name string `json:"name"`
	// Total and Free are in bytes.
	Total int64 `json:"total"`
	Free  int64 `json:"free"`
	// Startup is set on the disk the system runs from.
	Startup bool `json:"startup"`
}

// SpaceTarget is a folder on the start screen of 空间透视: 个人文件夹, 下载, and 应用程序 or Program Files.
type SpaceTarget struct {
	// ID is "home", "downloads", "applications" or "programFiles".
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

// SpaceLocations is what the start screen offers.
type SpaceLocations struct {
	Volumes []SpaceVolume `json:"volumes"`
	Targets []SpaceTarget `json:"targets"`
}

// SpaceProgress is streamed while a scan runs, at most every 80 ms.
type SpaceProgress struct {
	Files int64 `json:"files"`
	Dirs  int64 `json:"dirs"`
	Bytes int64 `json:"bytes"`
	// Path is the folder read last.
	Path string `json:"path"`
}

// SpaceEntry is one folder, file or folded group of the last scan.
type SpaceEntry struct {
	// ID names the entry in Space.children; -1 for a large file the scan folded into a group.
	ID   int       `json:"id"`
	Kind SpaceKind `json:"kind"`
	// Name and Path are empty for the folded groups.
	Name string `json:"name"`
	Path string `json:"path"`
	// Size is the space it takes on disk, in bytes.
	Size int64 `json:"size"`
	// Files counts the files at or below it.
	Files int64 `json:"files"`
	// Count is how many entries a folded group stands for.
	Count int64 `json:"count,omitempty"`
	// ModTime is set for files.
	ModTime time.Time `json:"modTime,omitzero"`
	// Denied is set on a folder Spruce could not read.
	Denied bool `json:"denied,omitempty"`
	// Locked, when set, says why the entry cannot be moved to the Trash (a sentence to show); folded groups never can.
	Locked string `json:"locked,omitempty"`
}

// SpaceSummary is the last scan as a whole.
type SpaceSummary struct {
	// ScanID names the scan; Space.children, largeFiles and trash take it and refuse an older one.
	ScanID string `json:"scanId"`
	// Name is what the page calls the scanned location, e.g. "Macintosh HD" or "个人文件夹".
	Name string     `json:"name"`
	Root SpaceEntry `json:"root"`
	// Files and Dirs count what the scan read.
	Files int64 `json:"files"`
	Dirs  int64 `json:"dirs"`
	// Denied counts folders that could not be read for lack of permission; DeniedPaths names the first few.
	Denied      int64    `json:"denied"`
	DeniedPaths []string `json:"deniedPaths"`
	// Errors counts entries that could not be read otherwise.
	Errors    int64         `json:"errors"`
	ScannedAt time.Time     `json:"scannedAt"`
	Duration  time.Duration `json:"duration"`
	// LargeFileMin is the size from which the 大文件 tab lists a file (settings.Cleanup.LargeFileMinMB at the time of the scan), in bytes.
	LargeFileMin int64 `json:"largeFileMin"`
}

// SpaceListing is one folder of the last scan: the folder, the folders from the root down to it, and its children, largest first.
type SpaceListing struct {
	Entry     SpaceEntry   `json:"entry"`
	Ancestors []SpaceEntry `json:"ancestors"`
	Children  []SpaceEntry `json:"children"`
}

// SpaceFailure is an entry Space.trash left in place, with why (a sentence to show).
type SpaceFailure struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// SpaceTrashResult is what Space.trash did.
type SpaceTrashResult struct {
	// Trashed is the size moved to the Trash, in bytes; it is freed once the Trash is emptied.
	Trashed int64 `json:"trashed"`
	// Items counts the entries moved.
	Items  int            `json:"items"`
	Failed []SpaceFailure `json:"failed"`
	// Root is the scanned location with its sizes corrected.
	Root SpaceEntry `json:"root"`
}

// Space is the bound service of the 空间透视 page. It keeps the last scan's tree, so the page opens one folder at a time without scanning again; moving an entry to the Trash corrects the sizes above it in place.
type Space struct {
	home     string
	settings func() settings.AppSettings
	bin      trash.Bin
	history  history.Store
	log      *slog.Logger

	// Replaced in tests.
	volumes func(ctx context.Context) ([]space.Volume, error)
	targets func(home string) []space.Target
	choose  func(ctx context.Context, home string) (string, error)

	scanning atomic.Bool

	mu       sync.Mutex
	tree     *space.Tree
	scanID   string
	name     string
	largeMin int64
}

// NewSpace returns the service for the user whose home folder is home, reading the exclusions and the large-file size from current, moving entries to bin (trash.System() in the app) and recording each move in store.
func NewSpace(home string, current func() settings.AppSettings, bin trash.Bin, store history.Store, log *slog.Logger) *Space {
	return &Space{
		home:     home,
		settings: current,
		bin:      bin,
		history:  store,
		log:      log,
		volumes:  space.Volumes,
		targets:  space.Targets,
		choose:   spaceChooseFolder,
	}
}

// Locations returns the disks and folders the start screen offers. A disk list the system cannot give is logged and left empty; the folders are still offered.
func (s *Space) Locations(ctx context.Context) SpaceLocations {
	out := SpaceLocations{Volumes: []SpaceVolume{}, Targets: []SpaceTarget{}}
	vols, err := s.listVolumes(ctx)
	if err != nil {
		s.log.Warn("list volumes", "err", err)
	}
	for _, v := range vols {
		out.Volumes = append(out.Volumes, SpaceVolume{Path: v.Path, Name: v.Name, Total: v.Total, Free: v.Free, Startup: v.Startup})
	}
	for _, t := range s.targets(s.home) {
		out.Targets = append(out.Targets, SpaceTarget{ID: t.ID, Name: t.Name, Path: t.Path})
	}
	return out
}

// spaceVolumesTimeout bounds listing the disks.
const spaceVolumesTimeout = 5 * time.Second

// listVolumes lists the disks but gives up after spaceVolumesTimeout: listing the mounts waits on every file system (gopsutil asks macOS with MNT_WAIT), and a network share that stopped answering would otherwise hold the start screen for good. The abandoned call ends whenever the system answers.
func (s *Space) listVolumes(ctx context.Context) ([]space.Volume, error) {
	ctx, cancel := context.WithTimeout(ctx, spaceVolumesTimeout)
	defer cancel()
	type result struct {
		vols []space.Volume
		err  error
	}
	done := make(chan result, 1)
	go func() {
		vols, err := s.volumes(ctx)
		done <- result{vols, err}
	}()
	select {
	case r := <-done:
		return r.vols, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ChooseFolder shows the system's folder picker over the calling window and returns the chosen folder, or "" when the user cancelled.
func (s *Space) ChooseFolder(ctx context.Context) (string, error) {
	p, err := s.choose(ctx, s.home)
	if err != nil {
		s.log.Warn("choose folder", "err", err)
		return "", errors.New("无法打开选择文件夹的窗口")
	}
	return p, nil
}

func spaceChooseFolder(ctx context.Context, home string) (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:      mygo.CallerWindow(ctx),
		Title:       "选择要分析的文件夹",
		Message:     "选择一个文件夹，看看它的空间都被什么占用了",
		ButtonLabel: "分析",
		DefaultPath: home,
		Directory:   true,
	})
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

// Scan walks the folder at path (a disk's path from Locations scans the whole disk), streams progress and keeps the result for children, largeFiles and trash, replacing the previous scan. Closing the channel cancels the scan; the previous result then stays. Errors are sentences in Simplified Chinese.
func (s *Space) Scan(ctx context.Context, path string, progress *mygo.Channel[SpaceProgress]) (SpaceSummary, error) {
	if !filepath.IsAbs(path) {
		return SpaceSummary{}, errors.New("请选择一个文件夹")
	}
	if !s.scanning.CompareAndSwap(false, true) {
		return SpaceSummary{}, errors.New("已有扫描正在进行，请等它结束后再试")
	}
	defer s.scanning.Store(false)

	largeMin := int64(s.settings().Cleanup.LargeFileMinMB) * spaceMegabyte()
	opts := space.Options{LargeFileMin: largeMin, ProgressEvery: progressEvery}
	if progress != nil {
		opts.Progress = func(p space.Progress) {
			// A page that stopped listening also cancels ctx, which ends the scan; the send error adds nothing.
			_ = progress.Send(SpaceProgress{Files: p.Files, Dirs: p.Dirs, Bytes: p.Bytes, Path: p.Path})
		}
	}
	started := time.Now()
	tree, err := space.Scan(ctx, filepath.Clean(path), opts)
	if err != nil {
		return SpaceSummary{}, s.scanError(ctx, path, err)
	}
	name := s.nameFor(ctx, tree.RootPath())

	s.mu.Lock()
	defer s.mu.Unlock()
	s.tree, s.scanID, s.name, s.largeMin = tree, newRunID(started), name, largeMin
	return s.summaryLocked(), nil
}

func (s *Space) scanError(ctx context.Context, path string, err error) error {
	if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		return errors.New("已取消")
	}
	s.log.Warn("space scan failed", "path", path, "err", err)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return errors.New("这个文件夹已经不存在了")
	case errors.Is(err, fs.ErrPermission):
		return errors.New("没有读取这个文件夹的权限")
	case errors.Is(err, space.ErrNotDir):
		return errors.New("只能分析文件夹")
	}
	return errors.New("扫描没能完成，详情已记录到日志")
}

// spaceMegabyte is the MB of settings.Cleanup.LargeFileMinMB in the units the page shows sizes in: 1000-based on macOS (Finder), 1024-based on Windows (Explorer).
func spaceMegabyte() int64 {
	if runtime.GOOS == "windows" {
		return 1 << 20
	}
	return 1_000_000
}

// nameFor is what the page calls a scanned folder: the name of the quick target or disk it is, else its last component.
func (s *Space) nameFor(ctx context.Context, root string) string {
	for _, t := range s.targets(s.home) {
		if filepath.Clean(t.Path) == root {
			return t.Name
		}
	}
	if vols, err := s.listVolumes(ctx); err == nil {
		for _, v := range vols {
			if filepath.Clean(v.Path) == root {
				return v.Name
			}
		}
	}
	if filepath.Dir(root) == root {
		return root
	}
	return filepath.Base(root)
}

// Current returns the last scan, or null before the first, so the page can show it again without scanning.
func (s *Space) Current() *SpaceSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tree == nil {
		return nil
	}
	sum := s.summaryLocked()
	return &sum
}

func (s *Space) summaryLocked() SpaceSummary {
	st := s.tree.Stats()
	denied := st.DeniedPaths
	if denied == nil {
		denied = []string{}
	}
	root := spaceEntryView(s.tree.Root(), "")
	root.Name = s.name
	return SpaceSummary{
		ScanID:       s.scanID,
		Name:         s.name,
		Root:         root,
		Files:        st.Files,
		Dirs:         st.Dirs,
		Denied:       st.Denied,
		DeniedPaths:  denied,
		Errors:       st.Errors,
		ScannedAt:    st.StartedAt.Add(st.Duration),
		Duration:     st.Duration,
		LargeFileMin: s.largeMin,
	}
}

// errSpaceStaleScan is returned for a ScanID that is not the last scan's.
var errSpaceStaleScan = errors.New("扫描结果已过期，请重新扫描")

func (s *Space) checkScanLocked(scanID string) error {
	if s.tree == nil || scanID != s.scanID {
		return errSpaceStaleScan
	}
	return nil
}

// Children returns the folder id of the scan scanID with its children, largest first, and the folders above it.
func (s *Space) Children(scanID string, id int) (SpaceListing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkScanLocked(scanID); err != nil {
		return SpaceListing{}, err
	}
	e, ok := s.tree.Entry(id)
	if !ok {
		return SpaceListing{}, errors.New("这个文件夹已经不在扫描结果里了")
	}
	children, ok := s.tree.Children(id)
	if !ok {
		return SpaceListing{}, errors.New("只能打开文件夹")
	}
	g := s.guardLocked()
	out := SpaceListing{Entry: s.viewLocked(e, g), Ancestors: []SpaceEntry{}, Children: make([]SpaceEntry, 0, len(children))}
	for _, a := range s.tree.Ancestors(id) {
		v := spaceEntryView(a, "")
		if a.ID == 0 {
			v.Name = s.name
		}
		out.Ancestors = append(out.Ancestors, v)
	}
	if e.ID == 0 {
		out.Entry.Name = s.name
	}
	for _, c := range children {
		out.Children = append(out.Children, s.viewLocked(c, g))
	}
	return out, nil
}

// LargeFiles returns the largest files of the scan scanID (up to 200, from LargeFileMin up), largest first.
func (s *Space) LargeFiles(scanID string) ([]SpaceEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkScanLocked(scanID); err != nil {
		return nil, err
	}
	g := s.guardLocked()
	large := s.tree.LargeFiles()
	out := make([]SpaceEntry, 0, len(large))
	for _, lf := range large {
		e, ok := s.tree.Find(lf.Path)
		if !ok {
			continue
		}
		out = append(out, s.viewLocked(e, g))
	}
	return out, nil
}

// Trash moves the entries at paths of the scan scanID to the Trash (Recycle Bin), never deleting anything for good, corrects the sizes above them, records the move in the history and returns what it did. Only entries the scan found are moved, and only where space.Guard allows it (inside the home folder or the scanned folder, not a system location, not excluded in the settings); an entry that changed since the scan is left in place.
func (s *Space) Trash(ctx context.Context, scanID string, paths []string) (SpaceTrashResult, error) {
	if s.scanning.Load() {
		return SpaceTrashResult{}, errors.New("正在扫描，请等扫描结束后再试")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkScanLocked(scanID); err != nil {
		return SpaceTrashResult{}, err
	}
	if len(paths) == 0 {
		return SpaceTrashResult{}, errors.New("没有选择任何项目")
	}
	started := time.Now()
	g := s.guardLocked()
	res := SpaceTrashResult{Failed: []SpaceFailure{}}
	var move []string
	for _, p := range slices.Compact(slices.Sorted(slices.Values(paths))) {
		if reason := s.precheckLocked(g, p); reason != "" {
			res.Failed = append(res.Failed, SpaceFailure{Path: p, Reason: reason})
			continue
		}
		move = append(move, filepath.Clean(p))
	}
	var moved []string
	if len(move) > 0 {
		outcomes, err := s.bin.MoveToTrash(ctx, move)
		if err != nil && ctx.Err() == nil {
			s.log.Warn("move to trash", "err", err)
		}
		for _, o := range outcomes {
			if o.Err != nil {
				res.Failed = append(res.Failed, SpaceFailure{Path: o.Path, Reason: s.trashReason(o.Err)})
				continue
			}
			e, _ := s.tree.Remove(o.Path)
			res.Trashed += e.Size
			res.Items++
			moved = append(moved, o.Path)
		}
	}
	root := spaceEntryView(s.tree.Root(), "")
	root.Name = s.name
	res.Root = root
	s.record(started, res, moved)
	return res, nil
}

// precheckLocked returns why p must stay where it is, or "". A path that is gone is taken out of the tree too, so the page stops showing it.
func (s *Space) precheckLocked(g space.Guard, p string) string {
	e, ok := s.tree.Find(p)
	if !ok {
		return "不在这次扫描的结果里，请重新扫描"
	}
	if reason := spaceLockedReason(g.Check(p)); reason != "" {
		return reason
	}
	info, err := os.Lstat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		s.tree.Remove(p)
		return "已经不存在了"
	case err != nil:
		s.log.Warn("stat before trash", "path", p, "err", err)
		return "无法读取这个项目"
	case e.Kind == space.KindDir && !info.IsDir(), e.Kind == space.KindFile && !info.Mode().IsRegular():
		// The scan saw a folder or a file; anything else now (a symlink above all) was swapped in since.
		return "扫描后被替换过，请重新扫描"
	}
	return ""
}

func (s *Space) trashReason(err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "已经不存在了"
	case errors.Is(err, fs.ErrPermission):
		return "没有权限移动这个项目"
	case errors.Is(err, context.Canceled):
		return "已取消"
	}
	s.log.Warn("move to trash failed", "err", err)
	return "没能移到" + binName() + "，详情已记录到日志"
}

// record writes the move to the history: what went to the Trash and what stayed.
func (s *Space) record(started time.Time, res SpaceTrashResult, moved []string) {
	if res.Items == 0 && len(res.Failed) == 0 {
		return
	}
	var lines []string
	for i, p := range moved {
		if i == maxDetailFailures {
			lines = append(lines, fmt.Sprintf("另有 %d 项已移到%s", len(moved)-maxDetailFailures, binName()))
			break
		}
		lines = append(lines, "已移到"+binName()+"："+spaceShortPath(s.home, p))
	}
	for i, f := range res.Failed {
		if i == maxDetailFailures {
			lines = append(lines, fmt.Sprintf("另有 %d 项未能处理", len(res.Failed)-maxDetailFailures))
			break
		}
		lines = append(lines, spaceShortPath(s.home, f.Path)+"："+f.Reason)
	}
	run := history.Run{
		ID:        newRunID(started),
		Kind:      history.KindSpace,
		StartedAt: started,
		EndedAt:   time.Now(),
		Summary:   &history.Totals{Trashed: res.Trashed, Items: res.Items, Failed: len(res.Failed)},
		Details:   lines,
	}
	if err := s.history.Append(run); err != nil {
		// The items are already in the Trash; failing the call would tell the user they were not.
		s.log.Error("record space trash in history", "err", err)
	}
}

// spaceShortPath replaces the home folder at the start of p with "~", as the cleaners' history lines do.
func spaceShortPath(home, p string) string {
	if home == "" {
		return p
	}
	if rel, err := filepath.Rel(home, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return filepath.Join("~", rel)
	}
	return p
}

func (s *Space) guardLocked() space.Guard {
	return space.NewGuard(s.home, s.tree.RootPath(), s.settings().Cleanup.Exclusions)
}

// viewLocked is spaceEntryView with the reason the entry cannot go to the Trash.
func (s *Space) viewLocked(e space.Entry, g space.Guard) SpaceEntry {
	locked := ""
	if e.Kind == space.KindDir || e.Kind == space.KindFile {
		locked = spaceLockedReason(g.Check(e.Path))
	}
	return spaceEntryView(e, locked)
}

func spaceEntryView(e space.Entry, locked string) SpaceEntry {
	return SpaceEntry{
		ID:      e.ID,
		Kind:    spaceKindOf[e.Kind],
		Name:    e.Name,
		Path:    e.Path,
		Size:    e.Size,
		Files:   e.Files,
		Count:   e.Count,
		ModTime: e.ModTime,
		Denied:  e.Denied,
		Locked:  locked,
	}
}

var spaceKindOf = map[space.Kind]SpaceKind{
	space.KindDir:   SpaceDir,
	space.KindFile:  SpaceFile,
	space.KindOther: SpaceOther,
	space.KindFiles: SpaceFiles,
}

// spaceLockedReason turns a refusal of space.Guard into the sentence the page shows, or "" for none.
func spaceLockedReason(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, space.ErrExcluded):
		return "这个位置在「设置」的排除列表中"
	case errors.Is(err, space.ErrOutside):
		return "只能移走个人文件夹或这次扫描的文件夹里的项目"
	case errors.Is(err, space.ErrTopLevel):
		return "个人文件夹和磁盘最上一层的项目请在" + spaceFileManager() + "中处理"
	case errors.Is(err, space.ErrSystem):
		return "这是系统位置，Spruce 不会移动这里的项目"
	case errors.Is(err, space.ErrProtected):
		return "这是系统或个人文件夹的固定位置，不能移走"
	}
	return "无法确认这个项目可以移走"
}

// spaceFileManager is the system's file manager.
func spaceFileManager() string {
	if runtime.GOOS == "windows" {
		return "资源管理器"
	}
	return "访达"
}
