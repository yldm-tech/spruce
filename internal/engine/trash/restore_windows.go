package trash

import "context"

// canRestore is false: SHFileOperationW does not say where an item went in the Recycle Bin, and the $Recycle.Bin layout is not documented, so moves to the Recycle Bin keep their confirmation and are not put back by Spruce.
const canRestore = false

type windowsRestorer struct{}

func systemRestorer() Restorer { return windowsRestorer{} }

func inTrash(string) bool { return false }

func (windowsRestorer) Restore(context.Context, RestoreRequest) (string, error) {
	return "", ErrRestoreUnsupported
}
