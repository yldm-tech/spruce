package loginitem

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

const (
	runKey      = `Software\Microsoft\Windows\CurrentVersion\Run`
	approvedKey = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`
)

// command is what the Run value starts: the running executable, quoted.
func command() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return `"` + exe + `"`, nil
}

// SetOpenAtLogin writes or removes the Run value. Turning it on also clears a Task Manager "disabled", since the user asks for it again; turning it off removes that too.
func (it *Item) SetOpenAtLogin(open bool) error {
	if !open {
		return errors.Join(deleteValue(runKey, it.id), deleteValue(approvedKey, it.id))
	}
	cmd, err := command()
	if err != nil {
		return fmt.Errorf("loginitem: %w", err)
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("loginitem: open Run key: %w", err)
	}
	defer k.Close()
	if err := k.SetStringValue(it.id, cmd); err != nil {
		return fmt.Errorf("loginitem: write Run value: %w", err)
	}
	return deleteValue(approvedKey, it.id)
}

// OpenAtLogin reports whether a Run value under the app's ID exists and Task Manager has not turned it off.
func (it *Item) OpenAtLogin() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	_, _, err = k.GetStringValue(it.id)
	k.Close()
	if err != nil {
		return false
	}
	a, err := registry.OpenKey(registry.CURRENT_USER, approvedKey, registry.QUERY_VALUE)
	if err != nil {
		return true
	}
	defer a.Close()
	state, _, err := a.GetBinaryValue(it.id)
	return err != nil || !disabledInTaskManager(state)
}

// deleteValue removes name under key, which is no error when either is missing.
func deleteValue(key, name string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, key, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("loginitem: open %s: %w", key, err)
	}
	defer k.Close()
	if err := k.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("loginitem: delete %s: %w", name, err)
	}
	return nil
}
