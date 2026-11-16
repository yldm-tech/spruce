package uninstall

import "testing"

// TestParseMdlsTakesSpotlightsNullDateAsUnknown reads a date before 2001, such as the 1979-12-31 15:00:00 +0000 Spotlight reports for apps used today, as unknown rather than as a use decades ago.
func TestParseMdlsTakesSpotlightsNullDateAsUnknown(t *testing.T) {
	out := "com.apple.application-bundle\x001979-12-31 15:00:00 +0000\x00com.apple.application-bundle\x002000-12-31 23:59:59 +0000\x00com.apple.application-bundle\x002001-01-01 00:00:00 +0000"
	got, ok := parseMdls(out, 3)
	if !ok {
		t.Fatal("not parsed")
	}
	if got[0].Usage != UsageUnknown || !got[0].LastUsed.IsZero() {
		t.Errorf("1979: %+v", got[0])
	}
	if got[1].Usage != UsageUnknown {
		t.Errorf("before 2001: %+v", got[1])
	}
	if got[2].Usage != UsageUsed {
		t.Errorf("2001: %+v", got[2])
	}
}
