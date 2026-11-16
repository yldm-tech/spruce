package trash

import (
	"testing"
	"unsafe"
)

// TestShellStructLayout checks the structs against the sizes the 64-bit Windows SDK gives them, so a field change that breaks the ABI fails here rather than in the shell.
func TestShellStructLayout(t *testing.T) {
	if got := unsafe.Sizeof(shFileOpStruct{}); got != 56 {
		t.Errorf("SHFILEOPSTRUCTW is %d bytes, want 56", got)
	}
	if got := unsafe.Sizeof(shQueryRBInfo{}); got != 24 {
		t.Errorf("SHQUERYRBINFO is %d bytes, want 24", got)
	}
}

func TestDoubleNull(t *testing.T) {
	u, err := doubleNull(`C:\a b`)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(u); n != 8 || u[n-1] != 0 || u[n-2] != 0 {
		t.Errorf("encoded %v", u)
	}
}
