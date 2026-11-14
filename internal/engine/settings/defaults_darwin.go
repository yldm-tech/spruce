package settings

import "path/filepath"

func devRootCandidates() []string {
	return []string{"Workspace", "Projects", "Developer", "code", "src"}
}

func installerRoots(home string) []string {
	return []string{filepath.Join(home, "Downloads"), filepath.Join(home, "Desktop")}
}
