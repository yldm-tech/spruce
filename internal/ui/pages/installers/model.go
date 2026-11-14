package installers

// ScanDescription is what the scan looks for, as the engine (internal/engine/installers) classifies files on each system.
func ScanDescription(windows bool) string {
	if windows {
		return "在下面的文件夹和它们的下一层子文件夹里查找 .exe、.msi、.msix、.appx 安装程序和 .iso 映像。"
	}
	return "在下面的文件夹和它们的下一层子文件夹里查找 .dmg、.pkg、.iso、.xip 和像安装包的 .zip 文件。"
}

// TrashLine says where cleaned installers go: the Trash on macOS, the Recycle Bin on Windows.
func TrashLine(windows bool) string {
	trash := "废纸篓"
	if windows {
		trash = "回收站"
	}
	return "清理的安装包会移到" + trash + "，可以随时放回。"
}

// Notice is the line above the results: where the installers go and which ones are left unchecked.
func Notice(windows bool) string {
	if windows {
		return TrashLine(true) + "最近 7 天内下载的默认不选。"
	}
	return TrashLine(false) + "最近 7 天内下载的和仍在挂载的默认不选。"
}

// NoFolders is the idle line when 设置 lists no folders to search.
const NoFolders = "还没有要查找的文件夹，可以在「设置」里添加。"

// folderChars is how many characters of a folder's path the idle list shows before it shortens the middle.
const folderChars = 64
