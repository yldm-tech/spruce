package space

import (
	"testing"

	"github.com/shirou/gopsutil/v4/disk"
	"golang.org/x/sys/windows"
)

func TestPickVolumesKeepsFixedDriveLetters(t *testing.T) {
	parts := []disk.PartitionStat{
		{Mountpoint: "A:"}, {Mountpoint: "C:"}, {Mountpoint: "D:"}, {Mountpoint: "E:"}, {Mountpoint: "Z:"},
		{Mountpoint: `C:\mnt\vol\`},
	}
	kinds := map[string]uint32{`A:\`: windows.DRIVE_REMOVABLE, `C:\`: windows.DRIVE_FIXED, `D:\`: windows.DRIVE_FIXED, `E:\`: windows.DRIVE_CDROM, `Z:\`: windows.DRIVE_REMOTE}
	kind := func(root string) uint32 { return kinds[root] }
	usage := func(string) (int64, int64, error) { return 1e12, 4e11, nil }
	label := func(root string) string {
		if root == `D:\` {
			return "Data"
		}
		return ""
	}
	got := pickVolumes(parts, kind, usage, label, `D:\`)
	want := []Volume{
		{Path: `D:\`, Name: "Data (D:)", Total: 1e12, Free: 4e11, Startup: true},
		{Path: `C:\`, Name: "本地磁盘 (C:)", Total: 1e12, Free: 4e11},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("volumes %+v, want %+v", got, want)
	}
}

func TestStartupVolumeReadsOnlyTheSystemDrive(t *testing.T) {
	var asked []string
	usage := func(root string) (int64, int64, error) {
		asked = append(asked, root)
		return 1e12, 4e11, nil
	}
	v, err := startupVolume(usage, func(string) string { return "" }, `C:\`)
	if want := (Volume{Path: `C:\`, Name: "本地磁盘 (C:)", Total: 1e12, Free: 4e11, Startup: true}); err != nil || v != want {
		t.Errorf("startup volume %+v %v, want %+v", v, err, want)
	}
	if len(asked) != 1 || asked[0] != `C:\` {
		t.Errorf("asked %v", asked)
	}
	if v, _ := startupVolume(usage, func(string) string { return "Data" }, `D:\`); v.Name != "Data (D:)" {
		t.Errorf("labelled drive %+v", v)
	}
}
