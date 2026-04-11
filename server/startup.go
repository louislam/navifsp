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

func syncStartup(enabled bool) {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, startupKeyPath, registry.ALL_ACCESS)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open registry key: %v\n", err)
		return
	}
	defer key.Close()

	if enabled {
		exePath, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get executable path: %v\n", err)
			return
		}
		cmd := fmt.Sprintf(`"%s" --no-window`, exePath)
		if err := key.SetStringValue(startupAppName, cmd); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to write registry: %v\n", err)
			return
		}
		fmt.Printf("Added to startup: %s\n", cmd)
	} else {
		_, _, err := key.GetStringValue(startupAppName)
		if err != nil {
			return
		}
		if err := key.DeleteValue(startupAppName); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to delete registry value: %v\n", err)
			return
		}
		fmt.Println("Removed from startup")
	}
}
