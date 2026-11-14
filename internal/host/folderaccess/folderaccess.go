// Package folderaccess is the read gate of background work. On macOS some folders prompt the user before an app may read them (TCC: Desktop, Documents, Downloads, iCloud Drive; removable and network volumes; and each File Provider domain under ~/Library/CloudStorage, which has prompts of its own and may download content kept only in the cloud). A background read must never cause such a prompt, so it may enter one of them only when a foreground read, which the user started, has entered it before (the user has answered the prompt then), or, for every one but the File Provider domains, when Spruce holds Full Disk Access. Store remembers the folders a foreground read entered, in <data>/folder-access.json. Windows has no such folders: everything may be read there.
package folderaccess

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
)

// Version is the version of folder-access.json this build writes.
const Version = 1

// maxLinks is how many symlinks resolving one path may follow.
const maxLinks = 40

// fdaFor is how long an answer of the Full Disk Access check is reused.
const fdaFor = time.Minute

// file is the content of folder-access.json.
type file struct {
	Version int      `json:"version"`
	Folders []string `json:"folders"`
}

// Store knows which prompting folders a background read may enter. Safe for concurrent use.
type Store struct {
	path string
	home string
	fda  func() bool
	goos string
	now  func() time.Time

	mu     sync.Mutex
	loaded bool
	homes  []string
	seen   map[string]string
	fdaAt  time.Time
	fdaWas bool
}

// Open returns the store of the file at path (<data>/folder-access.json) for the user whose home folder is home. fda reports whether Spruce holds Full Disk Access (fulldisk.Check() == fulldisk.Granted, which only tries a read and never prompts); nil means it does not. Open does no I/O.
func Open(path, home string, fda func() bool) *Store {
	return &Store{path: path, home: home, fda: fda, goos: runtime.GOOS, now: time.Now}
}

// Allow reports whether a background read of path may happen. It may when path, followed through any symlinks along it (resolved without entering a prompting folder), lies in no prompting folder, or the one it lies in was entered by a foreground read before, or Spruce holds Full Disk Access and the folder is not a File Provider domain. A path whose symlinks cannot be read is refused.
func (s *Store) Allow(path string) bool {
	if s.goos != "darwin" {
		return true
	}
	s.mu.Lock()
	s.loadLocked()
	homes := s.homes
	s.mu.Unlock()
	folder, ok, err := promptingFor(s.goos, homes, path)
	if err != nil {
		return false
	}
	if !ok {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, seen := s.seen[key(folder)]; seen {
		return true
	}
	if cloudDomain(homes, folder) {
		return false
	}
	return s.fdaLocked()
}

// PromptingFor returns the prompting folder a read of path would enter: the one path lies in, or, when path lies in none, the one a symlink along it leads into. ok is false when it enters none; err is set when a symlink along it cannot be read.
func (s *Store) PromptingFor(path string) (folder string, ok bool, err error) {
	if s.goos != "darwin" {
		return "", false, nil
	}
	s.mu.Lock()
	s.loadLocked()
	homes := s.homes
	s.mu.Unlock()
	return promptingFor(s.goos, homes, path)
}

// Observe records that a foreground read entered the prompting folder a read of path enters, found as Allow finds it (through any symlinks along path), so later background reads of path may enter it too; a path that enters no prompting folder records nothing, and one whose symlinks cannot be read records nothing and returns the error. Only the read gate calls it, right after it has read the folder itself, never from the settings lists.
func (s *Store) Observe(path string) error {
	if s.goos != "darwin" {
		return nil
	}
	s.mu.Lock()
	s.loadLocked()
	homes := s.homes
	s.mu.Unlock()
	folder, ok, err := promptingFor(s.goos, homes, path)
	if err != nil {
		return fmt.Errorf("folderaccess: observe: %w", err)
	}
	if !ok {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(folder)
	if _, seen := s.seen[k]; seen {
		return nil
	}
	s.seen[k] = folder
	folders := make([]string, 0, len(s.seen))
	for _, f := range s.seen {
		folders = append(folders, f)
	}
	slices.Sort(folders)
	b, err := json.Marshal(file{Version: Version, Folders: folders})
	if err != nil {
		return fmt.Errorf("folderaccess: encode: %w", err)
	}
	if err := writeAtomic(s.path, b); err != nil {
		return fmt.Errorf("folderaccess: save: %w", err)
	}
	return nil
}

// Prompting returns the prompting folder containing path, if any: on macOS ~/Desktop, ~/Documents, ~/Downloads, ~/Library/Mobile Documents, each ~/Library/CloudStorage/<domain> and each /Volumes/<volume>, the path itself included; never on Windows. Paths compare without regard to case, as the Mac's default file system does. Pure per platform.
func Prompting(home, path string) (folder string, ok bool) {
	return prompting(runtime.GOOS, []string{home}, path)
}

// homeFolders are the prompting folders directly named under home.
var homeFolders = []string{"Desktop", "Documents", "Downloads", filepath.Join("Library", "Mobile Documents")}

// cloudStorage is the folder of the File Provider domains, under home.
var cloudStorage = filepath.Join("Library", "CloudStorage")

// prompting is Prompting for the platform goos and any of homes (the home folder as given and as its symlinks resolve).
func prompting(goos string, homes []string, path string) (string, bool) {
	if goos != "darwin" || !filepath.IsAbs(path) {
		return "", false
	}
	path = filepath.Clean(path)
	for _, home := range homes {
		if home == "" || !filepath.IsAbs(home) {
			continue
		}
		home = filepath.Clean(home)
		for _, name := range homeFolders {
			if dir := filepath.Join(home, name); within(path, dir) {
				return dir, true
			}
		}
		if dir := filepath.Join(home, cloudStorage); within(path, dir) && !same(path, dir) {
			return filepath.Join(dir, firstPart(path, dir)), true
		}
	}
	if dir := "/Volumes"; within(path, dir) && !same(path, dir) {
		return filepath.Join(dir, firstPart(path, dir)), true
	}
	return "", false
}

// cloudDomain reports whether folder is a File Provider domain under one of homes.
func cloudDomain(homes []string, folder string) bool {
	for _, home := range homes {
		if home != "" && same(filepath.Dir(folder), filepath.Join(filepath.Clean(home), cloudStorage)) {
			return true
		}
	}
	return false
}

// promptingFor is Store.PromptingFor: the prompting folder of path itself, or else of where the symlinks along it lead. Each part of path is examined with os.Lstat, and each symlink read with os.Readlink, only while the walk is outside every prompting folder: it stops at the first part that lies in one, so it never reads inside such a folder.
func promptingFor(goos string, homes []string, path string) (string, bool, error) {
	if folder, ok := prompting(goos, homes, path); ok {
		return folder, true, nil
	}
	if !filepath.IsAbs(path) {
		return "", false, nil
	}
	p := filepath.Clean(path)
	for links := 0; links <= maxLinks; {
		root := filepath.VolumeName(p) + string(filepath.Separator)
		rest := strings.Split(strings.TrimPrefix(p, root), string(filepath.Separator))
		cur := root
		restarted := false
		for i, part := range rest {
			if part == "" {
				continue
			}
			next := filepath.Join(cur, part)
			if folder, ok := prompting(goos, homes, next); ok {
				return folder, true, nil
			}
			info, err := os.Lstat(next)
			if errors.Is(err, fs.ErrNotExist) {
				// Nothing to follow past a missing part: a read of path finds nothing either.
				return "", false, nil
			}
			if err != nil {
				return "", false, err
			}
			if info.Mode().Type() != fs.ModeSymlink {
				cur = next
				continue
			}
			target, err := os.Readlink(next)
			if err != nil {
				return "", false, err
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(cur, target)
			}
			p = filepath.Join(append([]string{target}, rest[i+1:]...)...)
			links++
			restarted = true
			break
		}
		if !restarted {
			return "", false, nil
		}
		if folder, ok := prompting(goos, homes, p); ok {
			return folder, true, nil
		}
	}
	return "", false, fmt.Errorf("folderaccess: more than %d symlinks along %s", maxLinks, path)
}

// loadLocked reads the file the first time it is needed, and resolves the home folder's own symlinks (the home folder is not a prompting folder). A missing, corrupt or other-version file reads as empty. s.mu is held.
func (s *Store) loadLocked() {
	if s.loaded {
		return
	}
	s.loaded = true
	s.seen = map[string]string{}
	s.homes = []string{s.home}
	if s.goos != "darwin" {
		return
	}
	if s.home != "" {
		if r, err := filepath.EvalSymlinks(s.home); err == nil && !same(r, s.home) {
			s.homes = append(s.homes, r)
		}
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var f file
	if json.Unmarshal(b, &f) != nil || f.Version != Version {
		return
	}
	for _, folder := range f.Folders {
		if filepath.IsAbs(folder) {
			s.seen[key(folder)] = folder
		}
	}
}

// fdaLocked asks fda, reusing an answer for fdaFor. s.mu is held.
func (s *Store) fdaLocked() bool {
	if s.fda == nil {
		return false
	}
	now := s.now()
	if s.fdaAt.IsZero() || now.Sub(s.fdaAt) >= fdaFor || now.Before(s.fdaAt) {
		s.fdaWas = s.fda()
		s.fdaAt = now
	}
	return s.fdaWas
}

// key is how folders compare: cleaned and in lower case.
func key(p string) string {
	return strings.ToLower(filepath.Clean(p))
}

// same reports whether a and b name the same path, ignoring case.
func same(a, b string) bool {
	return key(a) == key(b)
}

// within reports whether path is dir or inside it, ignoring case.
func within(path, dir string) bool {
	p, d := key(path), key(dir)
	return p == d || strings.HasPrefix(p, d+string(filepath.Separator))
}

// firstPart is the first element of path below dir, spelled as in path; path lies strictly inside dir.
func firstPart(path, dir string) string {
	rel := path[len(filepath.Clean(dir))+1:]
	if i := strings.IndexRune(rel, filepath.Separator); i >= 0 {
		rel = rel[:i]
	}
	return rel
}

// writeAtomic replaces path with b through a temporary file in the same folder, synced before the rename, so a crash leaves the old file or the new one. The file is 0600 and the folder is created (0700) when missing.
func writeAtomic(path string, b []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(name)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	ok = true
	return nil
}
