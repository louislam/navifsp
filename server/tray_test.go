package main

import (
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestTrayItems(t *testing.T) {
	state := trayState{
		version:    "1.2.0",
		mountpoint: "N:",
		baseURL:    "https://music.example.com",
		autostart:  true,
	}
	items := trayItems(state)

	var labels []string
	var exitIndex int
	for i, it := range items {
		if it.separator {
			continue
		}
		labels = append(labels, it.label)
		if it.id == idExit {
			exitIndex = i
		}
	}

	joined := strings.Join(labels, "\n")
	for _, want := range []string{"NaviFSP 1.2.0", "N:", "https://music.example.com", "Open folder", "Open Navidrome", "Start with Windows", "Exit"} {
		if !strings.Contains(joined, want) {
			t.Errorf("tray menu missing %q:\n%s", want, joined)
		}
	}

	// Headers must be greyed out, and the way out is the last row.
	if items[0].enabled || items[0].id != 0 {
		t.Error("first row should be a disabled header")
	}
	if exitIndex != len(items)-1 {
		t.Errorf("Exit is item %d of %d, want the last row", exitIndex, len(items))
	}

	for _, it := range items {
		if it.id == idAutostart && !it.checked {
			t.Error("Start with Windows should be checked when autostart is on")
		}
	}

	state.autostart = false
	for _, it := range trayItems(state) {
		if it.id == idAutostart && it.checked {
			t.Error("Start with Windows should not be checked when autostart is off")
		}
	}
}

func TestTrimLabel(t *testing.T) {
	short := "https://music.example.com"
	if got := trimLabel(short, 48); got != short {
		t.Errorf("short label changed: %q", got)
	}
	long := strings.Repeat("a", 60)
	got := trimLabel(long, 48)
	if n := utf8.RuneCountInString(got); n != 48 {
		t.Errorf("trimmed label length = %d, want 48", n)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("trimmed label should end with an ellipsis: %q", got)
	}
}

// TestTrayIconLifecycle really creates the notification-area icon and pumps
// its messages, so a broken Win32 binding shows up here instead of only on the
// user's desktop.
func TestTrayIconLifecycle(t *testing.T) {
	// The window belongs to this OS thread, so the test keeps it.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := createTrayWindow(trayState{version: "test", mountpoint: "T:"}); err != nil {
		t.Fatalf("create tray window: %v", err)
	}
	if trayHWnd == 0 {
		t.Fatal("tray window handle is null")
	}
	t.Cleanup(func() { removeTrayIcon() })

	// Closing from another thread is what the Ctrl+C handler does.
	go func() {
		time.Sleep(100 * time.Millisecond)
		closeTray()
	}()

	messageLoop() // returns once the window is destroyed and WM_QUIT arrives

	if trayHWnd != 0 {
		t.Error("tray window handle should be cleared after the loop ends")
	}
}
