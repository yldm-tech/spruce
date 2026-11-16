package health

import "testing"

func TestAppBundleName(t *testing.T) {
	cases := map[string]string{
		"/Applications/Xcode.app/Contents/MacOS/Xcode": "Xcode",
		"/Applications/Google Chrome.app/Contents/Frameworks/Google Chrome Framework.framework/Versions/131/Helpers/Google Chrome Helper (Renderer).app/Contents/MacOS/Google Chrome Helper (Renderer)": "Google Chrome",
		"/Applications/Xcode.app/Contents/Developer/Toolchains/XcodeDefault.xctoolchain/usr/bin/sourcekit-lsp":                                                                                          "Xcode",
		"/System/Library/PrivateFrameworks/SkyLight.framework/Versions/A/Resources/WindowServer":                                                                                                        "",
		"/usr/libexec/logd": "",
		"/opt/.app/bin/x":   "",
		"":                  "",
	}
	for exe, want := range cases {
		if got := appBundleName(exe); got != want {
			t.Errorf("appBundleName(%q) = %q, want %q", exe, got, want)
		}
	}
}
