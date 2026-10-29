package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

const (
	startupKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	startupAppName = "NaviFSP"
)

// syncStartup adds or removes the HKCU Run entry that starts NaviFSP hidden at
// login. It reports a registry failure instead of exiting, because it also runs
// from the tray menu where the app has to keep working.
func syncStartup(enabled bool) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, startupKeyPath, registry.ALL_ACCESS)
	if err != nil {
		return fmt.Errorf("open registry key: %w", err)
	}
	defer key.Close()

	if !enabled {
		if _, _, err := key.GetStringValue(startupAppName); err != nil {
			return nil // nothing to remove
		}
		if err := key.DeleteValue(startupAppName); err != nil {
			return fmt.Errorf("delete registry value: %w", err)
		}
		return nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}
	cmd := fmt.Sprintf(`"%s" --no-window`, exePath)
	if err := key.SetStringValue(startupAppName, cmd); err != nil {
		return fmt.Errorf("write registry: %w", err)
	}
	return nil
}

// startupEnabled reports whether the Run entry exists, so the tray menu can
// show a check mark next to "Start with Windows".
func startupEnabled() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, startupKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	_, _, err = key.GetStringValue(startupAppName)
	return err == nil
}
