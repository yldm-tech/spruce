package space

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/shirou/gopsutil/v4/disk"
)

// Volumes returns the disks worth scanning, the startup disk first: local, writable, browsable volumes with a capacity. "/" and the data volume are one APFS container and one disk to the user, so they appear once, as "/" under the startup disk's name and with the data volume's capacity.
func Volumes(ctx context.Context) ([]Volume, error) {
	parts, err := disk.PartitionsWithContext(ctx, false)
	if err != nil && len(parts) == 0 {
		return nil, err
	}
	return pickVolumes(parts, volumeUsage, startupName("/Volumes")), nil
}

func volumeUsage(mount string) (int64, int64, error) {
	u, err := disk.Usage(mount)
	if err != nil {
		return 0, 0, err
	}
	return clampBytes(u.Total), clampBytes(u.Free), nil
}

// networkFS are file systems of remote shares, which are not this Mac's disks.
var networkFS = map[string]bool{"smbfs": true, "nfs": true, "afpfs": true, "webdav": true, "cifs": true, "ftp": true}

// pickVolumes chooses what Volumes returns from the mounted file systems.
func pickVolumes(parts []disk.PartitionStat, usage usageFunc, rootName string) []Volume {
	var out []Volume
	hasRoot, hasData := false, false
	for _, p := range parts {
		switch {
		case p.Mountpoint == "/":
			hasRoot = true
		case p.Mountpoint == dataVolume:
			hasData = true
		case skipMount(p):
		default:
			total, free, err := usage(p.Mountpoint)
			if err != nil || total <= 0 {
				continue
			}
			out = append(out, Volume{Path: p.Mountpoint, Name: filepath.Base(p.Mountpoint), Total: total, Free: free})
		}
	}
	slices.SortFunc(out, func(a, b Volume) int { return strings.Compare(a.Name, b.Name) })
	if hasRoot {
		from := "/"
		if hasData {
			from = dataVolume
		}
		if total, free, err := usage(from); err == nil && total > 0 {
			out = slices.Insert(out, 0, Volume{Path: "/", Name: rootName, Total: total, Free: free, Startup: true})
		}
	}
	return out
}

// skipMount reports the mounts that are not a disk of the user's: remote shares, devfs and automounter maps, read-only volumes (the sealed system snapshots, Time Machine's local snapshots, mounted disk images), volumes hidden from Finder (VM, Preboot, Update …) and the other volumes of the startup container.
func skipMount(p disk.PartitionStat) bool {
	mp := p.Mountpoint
	switch {
	case networkFS[p.Fstype], p.Fstype == "devfs", p.Fstype == "autofs":
		return true
	case !slices.Contains(p.Opts, "local"), slices.Contains(p.Opts, "ro"), slices.Contains(p.Opts, "nobrowse"):
		return true
	case mp == "/dev", mp == "/private/var/vm", strings.HasPrefix(mp, "/System/Volumes/"):
		return true
	case strings.HasPrefix(mp, "/Volumes/com.apple.TimeMachine."), strings.HasPrefix(mp, "/Volumes/.timemachine"):
		return true
	}
	return false
}

// startupName returns the startup disk's name, which /Volumes holds as a symlink to "/" ("Macintosh HD" unless the user renamed it).
func startupName(volumes string) string {
	entries, err := os.ReadDir(volumes)
	if err == nil {
		for _, e := range entries {
			if e.Type()&os.ModeSymlink == 0 {
				continue
			}
			if target, err := os.Readlink(filepath.Join(volumes, e.Name())); err == nil && target == "/" {
				return e.Name()
			}
		}
	}
	return "Macintosh HD"
}

// Targets returns the quick targets that exist: the home folder, Downloads and Applications.
func Targets(home string) []Target {
	return existingDirs([]Target{
		{ID: "home", Name: "个人文件夹", Path: home},
		{ID: "downloads", Name: "下载", Path: filepath.Join(home, "Downloads")},
		{ID: "applications", Name: "应用程序", Path: "/Applications"},
	})
}
