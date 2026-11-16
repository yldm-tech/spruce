package loginitem

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// SMAppService's statuses (SMAppServiceStatus).
const (
	smNotRegistered    = 0
	smEnabled          = 1
	smRequiresApproval = 2
)

const (
	foundationPath = "/System/Library/Frameworks/Foundation.framework/Foundation"
	smPath         = "/System/Library/Frameworks/ServiceManagement.framework/ServiceManagement"
)

// load makes Foundation's and ServiceManagement's classes available to the Objective-C runtime; ServiceManagement is optional (SMAppService needs macOS 13).
var load = sync.OnceValue(func() error {
	if _, err := purego.Dlopen(foundationPath, purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
		return fmt.Errorf("loginitem: load Foundation: %w", err)
	}
	_, _ = purego.Dlopen(smPath, purego.RTLD_GLOBAL|purego.RTLD_NOW)
	return nil
})

var (
	selNew                  = objc.RegisterName("new")
	selDrain                = objc.RegisterName("drain")
	selMainBundle           = objc.RegisterName("mainBundle")
	selBundlePath           = objc.RegisterName("bundlePath")
	selUTF8String           = objc.RegisterName("UTF8String")
	selMainAppService       = objc.RegisterName("mainAppService")
	selStatus               = objc.RegisterName("status")
	selRegister             = objc.RegisterName("registerAndReturnError:")
	selUnregister           = objc.RegisterName("unregisterAndReturnError:")
	selLocalizedDescription = objc.RegisterName("localizedDescription")
)

// withPool runs fn on one locked thread inside an autorelease pool.
func withPool(fn func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(selNew)
	defer pool.Send(selDrain)
	fn()
}

func goString(ns objc.ID) string {
	if ns == 0 {
		return ""
	}
	return objc.Send[string](ns, selUTF8String)
}

// bundle returns the path of the app bundle the process runs from, or an error outside one (a development build).
func bundle() (string, error) {
	var path string
	withPool(func() {
		path = goString(objc.ID(objc.GetClass("NSBundle")).Send(selMainBundle).Send(selBundlePath))
	})
	if !strings.HasSuffix(path, ".app") {
		return "", errors.New("loginitem: starting at login needs the app bundle that nsgo build makes")
	}
	return path, nil
}

// mainAppService returns the app's SMAppService, 0 before macOS 13. Call it inside withPool.
func mainAppService() objc.ID {
	c := objc.GetClass("SMAppService")
	if c == 0 {
		return 0
	}
	return objc.ID(c).Send(selMainAppService)
}

// SetOpenAtLogin has the system open the app at login, or no more.
func (it *Item) SetOpenAtLogin(open bool) error {
	if err := load(); err != nil {
		return err
	}
	app, err := bundle()
	if err != nil {
		return err
	}
	withPool(func() {
		svc := mainAppService()
		if svc == 0 {
			err = setLaunchAgent(launchAgentPath(it.id), it.id, app, open)
			return
		}
		if !open && objc.Send[int](svc, selStatus) == smNotRegistered {
			return
		}
		sel := selUnregister
		if open {
			sel = selRegister
		}
		nserr := new(objc.ID)
		if !objc.Send[bool](svc, sel, unsafe.Pointer(nserr)) {
			err = fmt.Errorf("loginitem: %s", goString((*nserr).Send(selLocalizedDescription)))
			return
		}
		if open && objc.Send[int](svc, selStatus) == smRequiresApproval {
			err = ErrNeedsApproval
		}
	})
	return err
}

// OpenAtLogin reports whether the system opens the app at login.
func (it *Item) OpenAtLogin() bool {
	if load() != nil {
		return false
	}
	if _, err := bundle(); err != nil {
		return false
	}
	on := false
	withPool(func() {
		if svc := mainAppService(); svc != 0 {
			on = objc.Send[int](svc, selStatus) == smEnabled
			return
		}
		_, err := os.Stat(launchAgentPath(it.id))
		on = err == nil
	})
	return on
}

// Refresh rewrites the launch agent of macOS 11 and 12 when an earlier build wrote it without LoginArg, so the next login start is told apart; it does nothing on macOS 13 and later, outside an app bundle, or when the agent is not there (starting at login is off).
func (it *Item) Refresh() error {
	if err := load(); err != nil {
		return err
	}
	app, err := bundle()
	if err != nil {
		return nil
	}
	modern := false
	withPool(func() { modern = mainAppService() != 0 })
	if modern {
		return nil
	}
	return refreshAgent(launchAgentPath(it.id), it.id, app)
}

// refreshAgent rewrites the launch agent at path for app when it exists and does not pass LoginArg.
func refreshAgent(path, id, app string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("loginitem: %w", err)
	}
	if bytes.Contains(data, []byte("<string>"+LoginArg+"</string>")) {
		return nil
	}
	return setLaunchAgent(path, id, app, true)
}

func launchAgentPath(id string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", id+".plist")
}

// setLaunchAgent writes the launch agent at path that opens app at login, or removes it.
func setLaunchAgent(path, id, app string, open bool) error {
	if !open {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("loginitem: %w", err)
		}
		return nil
	}
	esc := func(s string) string {
		var b bytes.Buffer
		_ = xml.EscapeText(&b, []byte(s))
		return b.String()
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + esc(id) + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>/usr/bin/open</string>
		<string>-a</string>
		<string>` + esc(app) + `</string>
		<string>--args</string>
		<string>` + LoginArg + `</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
</dict>
</plist>
`
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("loginitem: %w", err)
	}
	if err := os.WriteFile(path, []byte(plist), 0o644); err != nil {
		return fmt.Errorf("loginitem: %w", err)
	}
	return nil
}
