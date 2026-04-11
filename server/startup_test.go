package main

import (
	"fmt"
	"os"
	"testing"

	"golang.org/x/sys/windows/registry"
)

func TestStartupSync(t *testing.T) {
	// Ensure clean state
	key, _, err := registry.CreateKey(registry.CURRENT_USER, startupKeyPath, registry.ALL_ACCESS)
	if err != nil {
		t.Fatalf("failed to open registry key: %v", err)
	}
	key.DeleteValue(startupAppName)
	key.Close()

	// Enable
	syncStartup(true)

	exePath, err := os.Executable()
	if err != nil {
		t.Fatalf("failed to get exe path: %v", err)
	}

	key, _, err = registry.CreateKey(registry.CURRENT_USER, startupKeyPath, registry.READ)
	if err != nil {
		t.Fatalf("failed to open registry key: %v", err)
	}
	defer key.Close()

	expected := fmt.Sprintf(`"%s" --no-window`, exePath)
	val, _, err := key.GetStringValue(startupAppName)
	if err != nil {
		t.Fatalf("startup not set after syncStartup(true): %v", err)
	}
	if val != expected {
		t.Fatalf("registry value mismatch:\n  got:  %s\n  want: %s", val, expected)
	}
	t.Logf("Startup added: %s", val)

	// Disable
	key.Close()
	syncStartup(false)

	key, err = registry.OpenKey(registry.CURRENT_USER, startupKeyPath, registry.READ)
	if err != nil {
		t.Fatalf("failed to open registry key: %v", err)
	}
	defer key.Close()

	_, _, err = key.GetStringValue(startupAppName)
	if err == nil {
		t.Fatal("expected startup value to be deleted, still exists")
	}
	t.Log("Startup removed successfully")
}
