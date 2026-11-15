package loginitem

import (
	"os"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/ebitengine/purego/objc"
)

// The Apple event keys that say how the app was launched: keyAEPropData's enumerated value is keyAELaunchedAsLogInItem when the system opened the app as a login item.
const (
	keyAEPropData            = 0x70726474 // 'prdt'
	keyAELaunchedAsLogInItem = 0x6c676974 // 'lgit'
)

var (
	selDefaultCenter        = objc.RegisterName("defaultCenter")
	selAddObserver          = objc.RegisterName("addObserver:selector:name:object:")
	selStringWithUTF8String = objc.RegisterName("stringWithUTF8String:")
	selSharedEventManager   = objc.RegisterName("sharedAppleEventManager")
	selCurrentAppleEvent    = objc.RegisterName("currentAppleEvent")
	selParamDescriptor      = objc.RegisterName("paramDescriptorForKeyword:")
	selEnumCodeValue        = objc.RegisterName("enumCodeValue")
	selDidFinishLaunching   = objc.RegisterName("spruceDidFinishLaunching:")
)

// launch is what WatchLaunch learned: whether the run started at login, and whether that is known yet.
var launch struct {
	atLogin, known atomic.Bool
	once           sync.Once
}

// WatchLaunch finds out whether the system started this run at login. Call it once before nsgo.Run: LoginArg on the command line (the launch agent of macOS 11 and 12) answers at once; otherwise an observer of NSApplicationDidFinishLaunchingNotification reads the Apple event the app was opened with, whose keyAEPropData is keyAELaunchedAsLogInItem for a login item (SMAppService on macOS 13 and later passes no arguments). When the observer cannot be set up the launch counts as known and not at login, so the window opens as before. NSGo has no API for the launch reason yet: https://github.com/nsgo-dev/nsgo/issues/124
func WatchLaunch() {
	launch.once.Do(func() {
		if atLoginArg(os.Args[1:]) {
			launch.atLogin.Store(true)
			launch.known.Store(true)
			return
		}
		if load() != nil || !observeLaunch() {
			launch.known.Store(true)
		}
	})
}

// LaunchedAtLogin reports whether the system started this run at login, and whether that is known yet: not before the app finished launching, or ever without WatchLaunch.
func LaunchedAtLogin() (atLogin, known bool) {
	return launch.atLogin.Load(), launch.known.Load()
}

// observeLaunch registers the observer of the app's finished launch and reports whether it could.
func observeLaunch() bool {
	class, err := objc.RegisterClass("SpruceLaunchObserver", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
		{Cmd: selDidFinishLaunching, Fn: func(objc.ID, objc.SEL, objc.ID) { launchFinished() }},
	})
	if err != nil {
		return false
	}
	ok := false
	withPool(func() {
		center := objc.ID(objc.GetClass("NSNotificationCenter")).Send(selDefaultCenter)
		name := nsString("NSApplicationDidFinishLaunchingNotification")
		if center == 0 || name == 0 {
			return
		}
		// The observer lives as long as the process: the center holds it unretained, and it is never released.
		observer := objc.ID(class).Send(selNew)
		center.Send(selAddObserver, observer, selDidFinishLaunching, name, objc.ID(0))
		ok = true
	})
	return ok
}

// launchFinished runs on the main thread as the app finishes launching, while the Apple event that opened it is the current one.
func launchFinished() {
	atLogin := false
	manager := objc.ID(objc.GetClass("NSAppleEventManager")).Send(selSharedEventManager)
	if manager != 0 {
		if ev := manager.Send(selCurrentAppleEvent); ev != 0 {
			if prop := ev.Send(selParamDescriptor, uint32(keyAEPropData)); prop != 0 {
				atLogin = objc.Send[uint32](prop, selEnumCodeValue) == keyAELaunchedAsLogInItem
			}
		}
	}
	launch.atLogin.Store(atLogin)
	launch.known.Store(true)
}

// nsString returns an autoreleased NSString of s; call it inside withPool.
func nsString(s string) objc.ID {
	b := append([]byte(s), 0)
	id := objc.ID(objc.GetClass("NSString")).Send(selStringWithUTF8String, &b[0])
	runtime.KeepAlive(b)
	return id
}
