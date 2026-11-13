package maintenance

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// The registered Disk Cleanup handler of the thumbnail cache. Its key's default value is the CLSID of the in-process COM server that implements IEmptyVolumeCache; Disk Cleanup (cleanmgr) loads the same handler for its "Thumbnails" entry.
const (
	thumbnailCacheHandlerKey  = `SOFTWARE\Microsoft\Windows\CurrentVersion\Explorer\VolumeCaches\Thumbnail Cache`
	thumbnailCacheHandlerName = "Thumbnail Cache"
)

// Interface IDs from emptyvc.h. golang.org/x/sys/windows declares neither these interfaces nor CoCreateInstance, so the vtables and the call are declared here, as elevate_windows.go does for ShellExecuteExW.
var (
	iidIUnknown                  = windows.GUID{Data1: 0x00000000, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidIEmptyVolumeCache         = windows.GUID{Data1: 0x8FCE5227, Data2: 0x04DA, Data3: 0x11D1, Data4: [8]byte{0xA0, 0x04, 0x00, 0x80, 0x5F, 0x8A, 0xBE, 0x06}}
	iidIEmptyVolumeCache2        = windows.GUID{Data1: 0x02B7E3BA, Data2: 0x4DB3, Data3: 0x11D2, Data4: [8]byte{0xB2, 0xD9, 0x00, 0xC0, 0x4F, 0x8E, 0xEC, 0x8C}}
	iidIEmptyVolumeCacheCallBack = windows.GUID{Data1: 0x6E793361, Data2: 0x73C6, Data3: 0x11D0, Data4: [8]byte{0x84, 0x69, 0x00, 0xAA, 0x00, 0x44, 0x29, 0x01}}
)

// HRESULT values the handler and the callback use.
const (
	hrOK          = 0
	hrFalse       = 1
	hrNoInterface = 0x80004002
	hrAbort       = 0x80004004
)

var procCoCreateInstance = windows.NewLazySystemDLL("ole32.dll").NewProc("CoCreateInstance")

// winCachePurge is what a Disk Cleanup handler reported.
type winCachePurge struct {
	// used is the space the handler said its files take, before the purge.
	used uint64
	// freed is the space PurgeProgress last reported as freed.
	freed uint64
	// empty is true when the handler had nothing to clean: Initialize returned S_FALSE or GetSpaceUsed reported 0 bytes.
	empty bool
}

// emptyVolumeCacheVtbl is the vtable of IEmptyVolumeCache2, which extends IEmptyVolumeCache with InitializeEx. initializeEx must not be read through an object that only implements IEmptyVolumeCache. The blank slots are methods this file never calls; they keep the layout.
type emptyVolumeCacheVtbl struct {
	_            uintptr // QueryInterface
	_            uintptr // AddRef
	release      uintptr
	initialize   uintptr
	getSpaceUsed uintptr
	purge        uintptr
	_            uintptr // ShowProperties
	deactivate   uintptr
	initializeEx uintptr
}

type emptyVolumeCache struct {
	vtbl *emptyVolumeCacheVtbl
}

// call invokes a method of the handler and returns its HRESULT. Callers pass Go pointers converted to uintptr (out parameters); go:uintptrescapes keeps them on the heap and alive for the call, as for windows.LazyProc.Call.
//
//go:uintptrescapes
func (c *emptyVolumeCache) call(method uintptr, args ...uintptr) int32 {
	hr, _, _ := syscall.SyscallN(method, append([]uintptr{uintptr(unsafe.Pointer(c))}, args...)...)
	return int32(uint32(hr))
}

// volumeCacheCallback is a static COM object implementing IEmptyVolumeCacheCallBack. The handler calls it during GetSpaceUsed and Purge, on the calling thread, to report progress; it answers E_ABORT once the context of the purge in progress has ended.
type volumeCacheCallback struct {
	vtbl *volumeCacheCallbackVtbl
}

type volumeCacheCallbackVtbl struct {
	queryInterface uintptr
	addRef         uintptr
	release        uintptr
	scanProgress   uintptr
	purgeProgress  uintptr
}

var (
	// purgeMu serialises purges: the callback object and the state below are shared.
	purgeMu sync.Mutex
	// purgeCtx is the context of the purge in progress, read by the callback.
	purgeCtx context.Context
	// purgeFreed is the last amount PurgeProgress reported.
	purgeFreed atomic.Uint64

	callbackVtbl   volumeCacheCallbackVtbl
	callbackObject = volumeCacheCallback{vtbl: &callbackVtbl}
	// callbackSetup creates the callback trampolines once; syscall.NewCallback slots are never released.
	callbackSetup sync.Once
)

func setupCallback() {
	callbackVtbl = volumeCacheCallbackVtbl{
		queryInterface: syscall.NewCallback(func(this uintptr, riid *windows.GUID, ppv *uintptr) uintptr {
			if *riid == iidIUnknown || *riid == iidIEmptyVolumeCacheCallBack {
				*ppv = this
				return hrOK
			}
			*ppv = 0
			return hrNoInterface
		}),
		// The object is static, so reference counting has nothing to free.
		addRef:  syscall.NewCallback(func(uintptr) uintptr { return 1 }),
		release: syscall.NewCallback(func(uintptr) uintptr { return 1 }),
		scanProgress: syscall.NewCallback(func(_, _, _, _ uintptr) uintptr {
			return callbackVerdict()
		}),
		purgeProgress: syscall.NewCallback(func(_, freed, _, _, _ uintptr) uintptr {
			purgeFreed.Store(uint64(freed))
			return callbackVerdict()
		}),
	}
}

func callbackVerdict() uintptr {
	if purgeCtx.Err() != nil {
		return hrAbort
	}
	return hrOK
}

// purgeThumbnailCache empties the thumbnail cache on volume (e.g. `C:\`) through the system's registered "Thumbnail Cache" Disk Cleanup handler, the way Disk Cleanup does. The handler works while Explorer runs, so Explorer is never restarted. It needs 64-bit Windows: the handler's 64-bit (DWORDLONG) arguments are passed as single machine words.
func purgeThumbnailCache(ctx context.Context, volume string) (winCachePurge, error) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		return winCachePurge{}, fmt.Errorf("calling the thumbnail cache handler needs 64-bit Windows: %w", errors.ErrUnsupported)
	}
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, thumbnailCacheHandlerKey, registry.READ)
	if err != nil {
		return winCachePurge{}, fmt.Errorf(`open HKLM\%s: %w`, thumbnailCacheHandlerKey, err)
	}
	defer key.Close()
	clsidText, _, err := key.GetStringValue("")
	if err != nil {
		return winCachePurge{}, fmt.Errorf(`read the handler CLSID from HKLM\%s: %w`, thumbnailCacheHandlerKey, err)
	}
	clsid, err := windows.GUIDFromString(clsidText)
	if err != nil {
		return winCachePurge{}, fmt.Errorf("handler CLSID %q: %w", clsidText, err)
	}
	volumePtr, err := windows.UTF16PtrFromString(volume)
	if err != nil {
		return winCachePurge{}, err
	}
	keyName, err := windows.UTF16PtrFromString(thumbnailCacheHandlerName)
	if err != nil {
		return winCachePurge{}, err
	}

	purgeMu.Lock()
	defer purgeMu.Unlock()
	callbackSetup.Do(setupCallback)
	purgeCtx = ctx
	purgeFreed.Store(0)

	// The handler is an apartment-threaded in-process server: initialise COM as STA on a thread that stays the same for every call.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	switch err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE); err {
	case nil, syscall.Errno(windows.S_FALSE):
		defer windows.CoUninitialize()
	case syscall.Errno(windows.RPC_E_CHANGED_MODE):
		// The thread already belongs to a multithreaded apartment; COM marshals the calls to the handler's apartment.
	default:
		return winCachePurge{}, fmt.Errorf("CoInitializeEx: %w", err)
	}

	cache, v2, err := createVolumeCache(&clsid)
	if err != nil {
		return winCachePurge{}, err
	}
	defer cache.call(cache.vtbl.release)

	var display, description, button *uint16
	var flags uint32
	var hr int32
	if v2 {
		hr = cache.call(cache.vtbl.initializeEx, uintptr(key), uintptr(unsafe.Pointer(volumePtr)), uintptr(unsafe.Pointer(keyName)),
			uintptr(unsafe.Pointer(&display)), uintptr(unsafe.Pointer(&description)), uintptr(unsafe.Pointer(&button)), uintptr(unsafe.Pointer(&flags)))
	} else {
		hr = cache.call(cache.vtbl.initialize, uintptr(key), uintptr(unsafe.Pointer(volumePtr)),
			uintptr(unsafe.Pointer(&display)), uintptr(unsafe.Pointer(&description)), uintptr(unsafe.Pointer(&flags)))
	}
	for _, s := range []*uint16{display, description, button} {
		if s != nil {
			windows.CoTaskMemFree(unsafe.Pointer(s))
		}
	}
	switch {
	case hr == hrFalse:
		return winCachePurge{empty: true}, nil
	case hr < 0:
		return winCachePurge{}, fmt.Errorf("thumbnail cache handler Initialize: HRESULT %#08x", uint32(hr))
	}
	defer func() {
		var out uint32
		cache.call(cache.vtbl.deactivate, uintptr(unsafe.Pointer(&out)))
	}()

	callback := uintptr(unsafe.Pointer(&callbackObject))
	var used uint64
	if hr := cache.call(cache.vtbl.getSpaceUsed, uintptr(unsafe.Pointer(&used)), callback); hr < 0 {
		return winCachePurge{}, purgeError("GetSpaceUsed", hr, ctx)
	}
	if used == 0 {
		return winCachePurge{empty: true}, nil
	}
	// DWORDLONG(-1) asks the handler to delete all of its files.
	hr = cache.call(cache.vtbl.purge, ^uintptr(0), callback)
	result := winCachePurge{used: used, freed: purgeFreed.Load()}
	if hr < 0 {
		return result, purgeError("Purge", hr, ctx)
	}
	return result, nil
}

// createVolumeCache creates the handler, preferring IEmptyVolumeCache2 (InitializeEx, which also receives the handler's key name) and falling back to IEmptyVolumeCache.
func createVolumeCache(clsid *windows.GUID) (*emptyVolumeCache, bool, error) {
	var cache *emptyVolumeCache
	hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(clsid)), 0, windows.CLSCTX_INPROC_SERVER, uintptr(unsafe.Pointer(&iidIEmptyVolumeCache2)), uintptr(unsafe.Pointer(&cache)))
	if int32(uint32(hr)) >= 0 && cache != nil {
		return cache, true, nil
	}
	cache = nil
	hr, _, _ = procCoCreateInstance.Call(uintptr(unsafe.Pointer(clsid)), 0, windows.CLSCTX_INPROC_SERVER, uintptr(unsafe.Pointer(&iidIEmptyVolumeCache)), uintptr(unsafe.Pointer(&cache)))
	if int32(uint32(hr)) < 0 || cache == nil {
		return nil, false, fmt.Errorf("create the thumbnail cache handler %s: HRESULT %#08x", clsid, uint32(hr))
	}
	return cache, false, nil
}

// purgeError reports a failed handler call; E_ABORT after the context ended is the cancellation the callback asked for.
func purgeError(method string, hr int32, ctx context.Context) error {
	if uint32(hr) == hrAbort && ctx.Err() != nil {
		return fmt.Errorf("thumbnail cache handler %s: %w", method, ctx.Err())
	}
	return fmt.Errorf("thumbnail cache handler %s: HRESULT %#08x", method, uint32(hr))
}
