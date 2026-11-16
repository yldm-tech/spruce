//go:build !darwin

package kit

import "github.com/nsgo-dev/nsgo/core"

// namesFaces is whether FontFace names each weight's faces (macOS only).
const namesFaces = false

// fontFace is the family list; the platform matches the weight.
func fontFace(core.FontWeight) string {
	return "Inter, PingFang SC, Microsoft YaHei UI, Noto Sans SC, system-ui"
}
