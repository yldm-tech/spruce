package uninstall

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// iconSize is the edge, in pixels, icons are extracted at.
const iconSize = 64

var (
	shell32               = windows.NewLazySystemDLL("shell32.dll")
	procSHDefExtractIconW = shell32.NewProc("SHDefExtractIconW")
	procExtractIconExW    = shell32.NewProc("ExtractIconExW")

	user32          = windows.NewLazySystemDLL("user32.dll")
	procGetIconInfo = user32.NewProc("GetIconInfo")
	procDestroyIcon = user32.NewProc("DestroyIcon")
	procGetDC       = user32.NewProc("GetDC")
	procReleaseDC   = user32.NewProc("ReleaseDC")

	gdi32            = windows.NewLazySystemDLL("gdi32.dll")
	procGetObjectW   = gdi32.NewProc("GetObjectW")
	procGetDIBits    = gdi32.NewProc("GetDIBits")
	procDeleteObject = gdi32.NewProc("DeleteObject")
)

// iconInfo is ICONINFO (winuser.h).
type iconInfo struct {
	fIcon    int32
	xHotspot uint32
	yHotspot uint32
	hbmMask  windows.Handle
	hbmColor windows.Handle
}

// bitmapObject is BITMAP (wingdi.h).
type bitmapObject struct {
	bmType       int32
	bmWidth      int32
	bmHeight     int32
	bmWidthBytes int32
	bmPlanes     uint16
	bmBitsPixel  uint16
	bmBits       uintptr
}

// bitmapInfoHeader is BITMAPINFOHEADER (wingdi.h); GetDIBits with 32 bits per pixel and BI_RGB needs no colour table.
type bitmapInfoHeader struct {
	biSize          uint32
	biWidth         int32
	biHeight        int32
	biPlanes        uint16
	biBitCount      uint16
	biCompression   uint32
	biSizeImage     uint32
	biXPelsPerMeter int32
	biYPelsPerMeter int32
	biClrUsed       uint32
	biClrImportant  uint32
}

// extractIconPNG extracts the icon at index from file (an .exe, .dll or .ico) and encodes it as a PNG: SHDefExtractIconW at iconSize, else ExtractIconExW's large icon (the system's 32 px).
func extractIconPNG(file string, index int) ([]byte, error) {
	path, err := windows.UTF16PtrFromString(file)
	if err != nil {
		return nil, err
	}
	var icon windows.Handle
	hr, _, _ := procSHDefExtractIconW.Call(uintptr(unsafe.Pointer(path)), uintptr(int32(index)), 0, uintptr(unsafe.Pointer(&icon)), 0, uintptr(iconSize))
	if hr != 0 || icon == 0 {
		icon = 0
		n, _, _ := procExtractIconExW.Call(uintptr(unsafe.Pointer(path)), uintptr(int32(index)), uintptr(unsafe.Pointer(&icon)), 0, 1)
		if n == 0 || n == ^uintptr(0) || icon == 0 {
			return nil, fmt.Errorf("uninstall: no icon %d in %s", index, file)
		}
	}
	defer destroyIcon(icon)
	img, err := iconImage(icon)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// iconImage reads an HICON's colour bitmap (and, for icons without alpha, its mask) into an image.
func iconImage(icon windows.Handle) (*image.NRGBA, error) {
	var ii iconInfo
	if ok, _, err := procGetIconInfo.Call(uintptr(icon), uintptr(unsafe.Pointer(&ii))); ok == 0 {
		return nil, fmt.Errorf("uninstall: GetIconInfo: %w", err)
	}
	if ii.hbmMask != 0 {
		defer deleteObject(ii.hbmMask)
	}
	if ii.hbmColor == 0 {
		return nil, errors.New("uninstall: monochrome icon")
	}
	defer deleteObject(ii.hbmColor)
	var bm bitmapObject
	if n, _, _ := procGetObjectW.Call(uintptr(ii.hbmColor), unsafe.Sizeof(bm), uintptr(unsafe.Pointer(&bm))); n == 0 {
		return nil, errors.New("uninstall: GetObject failed")
	}
	w, h := int(bm.bmWidth), int(bm.bmHeight)
	if w <= 0 || h <= 0 || w > 256 || h > 256 {
		return nil, fmt.Errorf("uninstall: icon of %dx%d", w, h)
	}
	hdc, _, _ := procGetDC.Call(0)
	if hdc == 0 {
		return nil, errors.New("uninstall: GetDC failed")
	}
	defer releaseDC(hdc)

	color, err := dibits(hdc, ii.hbmColor, w, h)
	if err != nil {
		return nil, err
	}
	hasAlpha := false
	for i := 3; i < len(color); i += 4 {
		if color[i] != 0 {
			hasAlpha = true
			break
		}
	}
	var mask []byte
	if !hasAlpha && ii.hbmMask != 0 {
		// The mask of a colour icon is as tall as the icon; a set bit (white) is transparent.
		if mask, err = dibits(hdc, ii.hbmMask, w, h); err != nil {
			mask = nil
		}
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		b, g, r, a := color[4*i], color[4*i+1], color[4*i+2], color[4*i+3]
		if !hasAlpha {
			a = 255
			if mask != nil && mask[4*i] != 0 {
				a = 0
			}
		}
		img.Pix[4*i], img.Pix[4*i+1], img.Pix[4*i+2], img.Pix[4*i+3] = r, g, b, a
	}
	return img, nil
}

// dibits reads a bitmap as top-down 32-bit BGRA rows.
func dibits(hdc uintptr, bitmap windows.Handle, w, h int) ([]byte, error) {
	hdr := bitmapInfoHeader{biWidth: int32(w), biHeight: -int32(h), biPlanes: 1, biBitCount: 32}
	hdr.biSize = uint32(unsafe.Sizeof(hdr))
	// BITMAPINFO is the header followed by the colour table; reserve room for the largest one GetDIBits may write.
	info := struct {
		hdr    bitmapInfoHeader
		colors [256]uint32
	}{hdr: hdr}
	buf := make([]byte, w*h*4)
	n, _, _ := procGetDIBits.Call(hdc, uintptr(bitmap), 0, uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&info)), 0)
	if n == 0 {
		return nil, errors.New("uninstall: GetDIBits failed")
	}
	return buf, nil
}

func destroyIcon(h windows.Handle) { _, _, _ = procDestroyIcon.Call(uintptr(h)) }

func deleteObject(h windows.Handle) { _, _, _ = procDeleteObject.Call(uintptr(h)) }

func releaseDC(hdc uintptr) { _, _, _ = procReleaseDC.Call(0, hdc) }

// iconSourceTime returns the modification time of the icon's file, for the cache key.
func iconSourceTime(file string) (int64, bool) {
	st, err := os.Stat(file)
	if err != nil || !st.Mode().IsRegular() {
		return 0, false
	}
	return st.ModTime().UnixNano(), true
}
