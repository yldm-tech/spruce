// Package installers is the engine of the 安装包 page: it finds installer files the user probably no longer needs in the folders of settings Cleanup.InstallerRoots (by default Downloads and Desktop) and moves the ones the user picked to the Trash or Recycle Bin. It implements cleanup.Scanner; wire.go registers it under app.CleanerInstallers. It does not import any UI framework.
//
// # What counts
//
// The search goes two levels deep (the root's files, and the files of its sub-folders), never into hidden folders or folders that look like packages (Photos.photoslibrary, Some.app), and never follows symlinks. classify decides, per platform:
//
//   - macOS: .dmg (磁盘映像), .pkg and .mpkg, flat or bundle (安装器), .iso (系统映像), .xip (压缩包), and .zip files of 10 MB or more whose name looks like an installer's (a version number, "setup", "installer", "mac", "darwin", "arm64", "x64", "universal" …) (压缩包).
//   - Windows: .msi, .msix, .msixbundle, .appx and .appxbundle (安装器), .iso (系统映像), and .exe: every one directly in Downloads, elsewhere only those whose name looks like an installer's, since a portable program unpacked into a folder, or kept on the Desktop, is not one.
//
// An item is checked by default only when it arrived more than 7 days ago (the later of its modification and creation times) and, for a disk image, is not attached: those go to the type's category; the others to 最近下载或正在使用的安装包, which starts unchecked. Attached images are read from `hdiutil info -plist` on macOS.
//
// Everything goes to the Trash (cleanup.DeleteTrash), whatever the folder: these are files the user downloaded, and Put Back must work. The user's exclusions are respected.
//
// When the attached images cannot be listed, 磁盘映像 carries the warning 「无法确认哪些磁盘映像正在使用」: the page still offers it, but automatic rules skip a category with a warning. Env.MayRead, the background read gate, is asked before each root and sub-folder is listed (after the exclusions); a folder it refuses, on macOS Downloads and Desktop until a foreground read has entered them, is skipped silently.
package installers
