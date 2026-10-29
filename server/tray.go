package main

import (
	_ "embed"
	"encoding/binary"
	"log"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

//go:embed icon.ico
var iconICO []byte

const (
	wmApp      = 0x8000
	wmTrayIcon = wmApp + 1

	wmClose         = 0x0010
	wmDestroy       = 0x0002
	wmRButtonUp     = 0x0205
	wmContextMenu   = 0x007B
	wmLButtonUp     = 0x0202
	wmLButtonDblClk = 0x0203

	nimAdd    = 0
	nimDelete = 2

	nifMessage = 0x1
	nifIcon    = 0x2
	nifTip     = 0x4

	imageIcon      = 1
	lrLoadFromFile = 0x0010
	lrDefaultSize  = 0x0040

	mfString    = 0x0000
	mfGrayed    = 0x0001
	mfChecked   = 0x0008
	mfSeparator = 0x0800

	tpmReturnCmd   = 0x0100
	tpmRightButton = 0x0002

	swShownormal = 1

	idOpenFolder    = 1
	idOpenNavidrome = 2
	idAutostart     = 3
	idExit          = 4
)

var (
	user32  = windows.NewLazySystemDLL("user32.dll")
	shell32 = windows.NewLazySystemDLL("shell32.dll")

	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
	procRegisterWindowMsg   = user32.NewProc("RegisterWindowMessageW")
	procLoadImageW          = user32.NewProc("LoadImageW")
	procLoadIconW           = user32.NewProc("LoadIconW")
	procCreateIconFromRes   = user32.NewProc("CreateIconFromResourceEx")
	procCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	procAppendMenuW         = user32.NewProc("AppendMenuW")
	procTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	procDestroyMenu         = user32.NewProc("DestroyMenu")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procPostMessageW        = user32.NewProc("PostMessageW")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")

	procGetModuleHandleW = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW")
	procShellExecuteW    = shell32.NewProc("ShellExecuteW")
	procShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
)

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         windows.GUID
	HBalloonIcon     uintptr
}

type winPoint struct {
	X, Y int32
}

type winMsg struct {
	Hwnd     uintptr
	Message  uint32
	_        uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       winPoint
	LPrivate uint32
}

// trayItem is one row of the tray menu, described without Win32 calls so the
// contents can be tested.
type trayItem struct {
	label     string
	id        uintptr
	enabled   bool
	checked   bool
	separator bool
}

type trayState struct {
	version    string
	mountpoint string
	baseURL    string
	autostart  bool
}

var (
	trayHWnd   uintptr
	trayShown  trayState
	taskbarMsg uint32
)

// trayItems lays out the whole menu, header included, in display order.
func trayItems(s trayState) []trayItem {
	return []trayItem{
		{label: "NaviFSP " + s.version},
		{label: s.mountpoint + "  ·  " + trimLabel(s.baseURL, 48)},
		{separator: true},
		{label: "Open folder", id: idOpenFolder, enabled: true},
		{label: "Open Navidrome", id: idOpenNavidrome, enabled: true},
		{separator: true},
		{label: "Start with Windows", id: idAutostart, enabled: true, checked: s.autostart},
		{separator: true},
		{label: "Exit", id: idExit, enabled: true},
	}
}

// trimLabel shortens long text so a long base URL cannot make the menu very wide.
func trimLabel(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

// runTray shows the notification-area icon and pumps its messages until the
// user exits. Win32 windows belong to their creating thread, so the caller
// keeps this goroutine for the whole life of the tray.
func runTray(s trayState) error {
	trayShown = s
	if err := createTrayWindow(s); err != nil {
		return err
	}
	messageLoop()
	removeTrayIcon()
	return nil
}

// closeTray asks the tray window to close, which ends runTray.
func closeTray() {
	if trayHWnd != 0 {
		procPostMessageW.Call(trayHWnd, wmClose, 0, 0)
	}
}

func createTrayWindow(s trayState) error {
	className, err := windows.UTF16PtrFromString("navifspTray")
	if err != nil {
		return err
	}
	hinst, _, _ := procGetModuleHandleW.Call(0)
	wc := wndClassEx{
		CbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		LpfnWndProc:   windows.NewCallback(trayWndProc),
		HInstance:     hinst,
		LpszClassName: className,
	}
	if atom, _, callErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
		return callErr
	}
	hwnd, _, callErr := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), 0, 0, 0, 0, 0, 0, 0, 0, hinst, 0)
	if hwnd == 0 {
		return callErr
	}
	trayHWnd = hwnd

	name, _ := windows.UTF16PtrFromString("TaskbarCreated")
	registered, _, _ := procRegisterWindowMsg.Call(uintptr(unsafe.Pointer(name)))
	taskbarMsg = uint32(registered)

	return addTrayIcon(s)
}

func messageLoop() {
	var m winMsg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func trayIcon() uintptr {
	// The icon travels inside the exe, so it works wherever it is started from.
	if img, ok := iconImage(iconICO); ok {
		h, _, _ := procCreateIconFromRes.Call(
			uintptr(unsafe.Pointer(&img[0])), uintptr(len(img)), 1, 0x00030000, 32, 32, 0)
		if h != 0 {
			return h
		}
	}
	if exe, err := os.Executable(); err == nil {
		path := filepath.Join(filepath.Dir(exe), "icon.ico")
		if _, statErr := os.Stat(path); statErr == nil {
			if p, ptrErr := windows.UTF16PtrFromString(path); ptrErr == nil {
				if h, _, _ := procLoadImageW.Call(0, uintptr(unsafe.Pointer(p)), imageIcon, 0, 0, lrLoadFromFile|lrDefaultSize); h != 0 {
					return h
				}
			}
		}
	}
	h, _, _ := procLoadIconW.Call(0, 32512) // IDI_APPLICATION
	return h
}

// iconImage picks the image used by the notification area: a 32x32 DIB if the
// .ico has one. CreateIconFromResourceEx wants the raw RT_ICON payload, not the
// file, and PNG entries are skipped because only the DIB form is accepted
// everywhere.
func iconImage(ico []byte) ([]byte, bool) {
	if len(ico) < 6 || binary.LittleEndian.Uint16(ico[2:4]) != 1 {
		return nil, false
	}
	count := int(binary.LittleEndian.Uint16(ico[4:6]))
	var best []byte
	bestArea := 0
	for i := 0; i < count; i++ {
		off := 6 + i*16
		if off+16 > len(ico) {
			break
		}
		w, hh := int(ico[off]), int(ico[off+1])
		if w == 0 {
			w = 256
		}
		if hh == 0 {
			hh = 256
		}
		size := int(binary.LittleEndian.Uint32(ico[off+8:]))
		start := int(binary.LittleEndian.Uint32(ico[off+12:]))
		if start < 0 || size <= 0 || start+size > len(ico) {
			continue
		}
		data := ico[start : start+size]
		if len(data) < 4 || data[0] == 0x89 {
			continue
		}
		switch {
		case w == 32 && hh == 32:
			return data, true
		case w*hh > bestArea:
			bestArea = w * hh
			best = data
		}
	}
	return best, best != nil
}

func addTrayIcon(s trayState) error {
	nid := notifyIconData{
		HWnd:             trayHWnd,
		UID:              1,
		UFlags:           nifMessage | nifIcon | nifTip,
		UCallbackMessage: wmTrayIcon,
		HIcon:            trayIcon(),
	}
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	copy(nid.SzTip[:], windows.StringToUTF16("NaviFSP - "+s.mountpoint))
	if r, _, err := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid))); r == 0 {
		return err
	}
	return nil
}

func removeTrayIcon() {
	if trayHWnd == 0 {
		return
	}
	nid := notifyIconData{HWnd: trayHWnd, UID: 1}
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
}

func trayWndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	switch uint32(msg) {
	case wmTrayIcon:
		switch uint32(lparam) & 0xFFFF {
		case wmLButtonUp, wmLButtonDblClk:
			openWithShell(trayShown.mountpoint)
		case wmRButtonUp, wmContextMenu:
			showTrayMenu(hwnd)
		}
		return 0
	case wmDestroy:
		removeTrayIcon()
		trayHWnd = 0
		procPostQuitMessage.Call(0)
		return 0
	}
	if taskbarMsg != 0 && uint32(msg) == taskbarMsg {
		// Explorer restarted and dropped every icon, so add ours again.
		addTrayIcon(trayShown)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}

func showTrayMenu(hwnd uintptr) {
	s := trayShown
	s.autostart = startupEnabled()

	menu, _, _ := procCreatePopupMenu.Call()
	for _, it := range trayItems(s) {
		switch {
		case it.separator:
			appendMenu(menu, mfSeparator, 0, "")
		case it.enabled:
			flags := uintptr(mfString)
			if it.checked {
				flags |= mfChecked
			}
			appendMenu(menu, flags, it.id, it.label)
		default:
			appendMenu(menu, mfString|mfGrayed, 0, it.label)
		}
	}

	var pt winPoint
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWindow.Call(hwnd)
	cmd, _, _ := procTrackPopupMenu.Call(menu, tpmReturnCmd|tpmRightButton, uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
	procPostMessageW.Call(hwnd, 0, 0, 0) // dismisses the menu reliably
	procDestroyMenu.Call(menu)

	switch cmd {
	case idOpenFolder:
		openWithShell(s.mountpoint)
	case idOpenNavidrome:
		openWithShell(s.baseURL)
	case idAutostart:
		if err := syncStartup(!startupEnabled()); err != nil {
			log.Printf("Start with Windows: %v", err)
		}
	case idExit:
		procDestroyWindow.Call(hwnd)
	}
}

func appendMenu(menu uintptr, flags uintptr, id uintptr, text string) {
	p, _ := windows.UTF16PtrFromString(text)
	procAppendMenuW.Call(menu, flags, id, uintptr(unsafe.Pointer(p)))
}

// openWithShell opens a folder or URL with the default handler, without a
// console window flashing up.
func openWithShell(target string) {
	if target == "" {
		return
	}
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return
	}
	p, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return
	}
	procShellExecuteW.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(p)), 0, 0, swShownormal)
}
