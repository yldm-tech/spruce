package space

import (
	"context"
	"os"
	"strings"

	"github.com/shirou/gopsutil/v4/disk"
	"golang.org/x/sys/windows"
)

// Volumes returns the fixed drives with a drive letter, the system drive first.
func Volumes(ctx context.Context) ([]Volume, error) {
	parts, err := disk.PartitionsWithContext(ctx, false)
	if err != nil && len(parts) == 0 {
		return nil, err
	}
	return pickVolumes(parts, driveType, volumeUsage, volumeLabel, startupRoot()), nil
}

func volumeUsage(root string) (int64, int64, error) {
	u, err := disk.Usage(root)
	if err != nil {
		return 0, 0, err
	}
	return clampBytes(u.Total), clampBytes(u.Free), nil
}

func driveType(root string) uint32 {
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return windows.DRIVE_UNKNOWN
	}
	return windows.GetDriveType(p)
}

// volumeLabel returns the label Explorer shows for a drive, or "".
func volumeLabel(root string) string {
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return ""
	}
	buf := make([]uint16, windows.MAX_PATH+1)
	if err := windows.GetVolumeInformation(p, &buf[0], uint32(len(buf)), nil, nil, nil, nil, 0); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf)
}

// pickVolumes chooses what Volumes returns: drive letters of fixed disks with a capacity, named as Explorer names them ("本地磁盘 (C:)" without a label), the system drive first. Removable, optical and network drives and volumes mounted in folders are left out.
func pickVolumes(parts []disk.PartitionStat, kind func(string) uint32, usage usageFunc, label func(string) string, system string) []Volume {
	var out []Volume
	for _, p := range parts {
		mp := p.Mountpoint
		if len(mp) != 2 || mp[1] != ':' {
			continue
		}
		root := strings.ToUpper(mp) + `\`
		if kind(root) != windows.DRIVE_FIXED {
			continue
		}
		total, free, err := usage(root)
		if err != nil || total <= 0 {
			continue
		}
		name := label(root)
		if name == "" {
			name = "本地磁盘"
		}
		v := Volume{Path: root, Name: name + " (" + strings.ToUpper(mp) + ")", Total: total, Free: free, Startup: strings.EqualFold(root, system)}
		if v.Startup {
			out = append([]Volume{v}, out...)
		} else {
			out = append(out, v)
		}
	}
	return out
}

// Targets returns the quick targets that exist: the home folder, Downloads and Program Files.
func Targets(home string) []Target {
	downloads, err := windows.KnownFolderPath(windows.FOLDERID_Downloads, windows.KF_FLAG_DEFAULT)
	if err != nil || downloads == "" {
		downloads = home + `\Downloads`
	}
	programFiles := os.Getenv("ProgramFiles")
	if programFiles == "" {
		programFiles = `C:\Program Files`
	}
	return existingDirs([]Target{
		{ID: "home", Name: "个人文件夹", Path: home},
		{ID: "downloads", Name: "下载", Path: downloads},
		{ID: "programFiles", Name: "Program Files", Path: programFiles},
	})
}
