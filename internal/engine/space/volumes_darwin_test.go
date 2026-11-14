package space

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/shirou/gopsutil/v4/disk"
)

func TestPickVolumesShowsTheStartupDiskOnce(t *testing.T) {
	rw := []string{"rw", "local", "journaled"}
	parts := []disk.PartitionStat{
		{Mountpoint: "/", Fstype: "apfs", Opts: []string{"ro", "local", "journaled"}},
		{Mountpoint: "/dev", Fstype: "devfs", Opts: []string{"rw", "local", "nobrowse"}},
		{Mountpoint: "/System/Volumes/VM", Fstype: "apfs", Opts: []string{"rw", "local", "nobrowse"}},
		{Mountpoint: "/System/Volumes/Preboot", Fstype: "apfs", Opts: []string{"rw", "local", "nobrowse"}},
		{Mountpoint: "/System/Volumes/Update", Fstype: "apfs", Opts: []string{"rw", "local", "nobrowse"}},
		{Mountpoint: "/System/Volumes/Data", Fstype: "apfs", Opts: []string{"rw", "local", "nobrowse"}},
		{Mountpoint: "/private/var/vm", Fstype: "apfs", Opts: rw},
		{Mountpoint: "/System/Volumes/Data/home", Fstype: "autofs", Opts: []string{"rw", "nobrowse", "automounted"}},
		{Mountpoint: "/Volumes/com.apple.TimeMachine.localsnapshots/Backups.backupdb/x", Fstype: "apfs", Opts: []string{"ro", "local"}},
		{Mountpoint: "/Volumes/Share", Fstype: "smbfs", Opts: []string{"rw", "nodev", "nosuid"}},
		{Mountpoint: "/Volumes/Installer", Fstype: "hfs", Opts: []string{"ro", "local"}},
		{Mountpoint: "/Volumes/Empty", Fstype: "msdos", Opts: rw},
		{Mountpoint: "/Volumes/Broken", Fstype: "exfat", Opts: rw},
		{Mountpoint: "/Volumes/Photos", Fstype: "apfs", Opts: rw},
		{Mountpoint: "/Volumes/Archive", Fstype: "exfat", Opts: rw},
	}
	usage := func(mount string) (int64, int64, error) {
		switch mount {
		case "/System/Volumes/Data":
			return 500e9, 120e9, nil
		case "/":
			return 500e9, 1, nil
		case "/Volumes/Empty":
			return 0, 0, nil
		case "/Volumes/Broken":
			return 0, 0, errors.New("io")
		}
		return 2e12, 1e12, nil
	}
	got := pickVolumes(parts, usage, "My Mac")
	want := []Volume{
		{Path: "/", Name: "My Mac", Total: 500e9, Free: 120e9, Startup: true},
		{Path: "/Volumes/Archive", Name: "Archive", Total: 2e12, Free: 1e12},
		{Path: "/Volumes/Photos", Name: "Photos", Total: 2e12, Free: 1e12},
	}
	if len(got) != len(want) {
		t.Fatalf("volumes %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("volume %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestStartupNameFollowsTheVolumesLink(t *testing.T) {
	dir := tempRoot(t)
	if got := startupName(dir); got != "Macintosh HD" {
		t.Errorf("no link: %q", got)
	}
	if err := os.Symlink("/Volumes/Other", filepath.Join(dir, "Other")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/", filepath.Join(dir, "Work Mac")); err != nil {
		t.Fatal(err)
	}
	if got := startupName(dir); got != "Work Mac" {
		t.Errorf("startup name %q", got)
	}
}

func TestTargetsKeepExistingFolders(t *testing.T) {
	home := tempRoot(t)
	got := Targets(home)
	if len(got) == 0 || got[0].ID != "home" || got[0].Path != home {
		t.Fatalf("targets %+v", got)
	}
	for _, tg := range got {
		if tg.ID == "downloads" {
			t.Errorf("a missing Downloads folder is offered: %+v", tg)
		}
	}
	if err := os.Mkdir(filepath.Join(home, "Downloads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Targets(home); len(got) < 2 || got[1].ID != "downloads" {
		t.Errorf("targets %+v", got)
	}
}
