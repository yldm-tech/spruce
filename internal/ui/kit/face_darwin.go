package kit

import "github.com/nsgo-dev/nsgo/core"

// namesFaces is whether FontFace names each weight's faces (macOS).
const namesFaces = true

// fontFace names the Inter static face and the PingFang SC face nearest w (PingFang has no Bold: Semibold stands in).
func fontFace(w core.FontWeight) string {
	inter, pingfang := "Inter-Regular", "PingFangSC-Regular"
	switch {
	case w >= core.FontWeightBold:
		inter, pingfang = "Inter-Bold", "PingFangSC-Semibold"
	case w >= core.FontWeightSemibold:
		inter, pingfang = "Inter-SemiBold", "PingFangSC-Semibold"
	case w >= core.FontWeightMedium:
		inter, pingfang = "Inter-Medium", "PingFangSC-Medium"
	}
	return inter + ", " + pingfang + ", system-ui"
}
