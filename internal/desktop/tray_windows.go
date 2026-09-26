package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
)

// The desktop companion runs in the signed-in session; the service never owns UI.
type Settings struct {
	URL string `json:"url"`
}

const windowClass = "BrrpBrrp.Tray.v1"

var user32 = windows.NewLazySystemDLL("user32.dll")
var shell32 = windows.NewLazySystemDLL("shell32.dll")

func proc(name string) *windows.LazyProc { return user32.NewProc(name) }
func wide(s string) *uint16              { p, _ := windows.UTF16PtrFromString(s); return p }

type point struct{ X, Y int32 }
type message struct {
	Window         uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Point          point
	Private        uint32
}
type windowInfo struct {
	Size, Style                        uint32
	Callback                           uintptr
	ClassExtra, WindowExtra            int32
	Instance, Icon, Cursor, Background uintptr
	Menu, Class                        *uint16
	SmallIcon                          uintptr
}
type notifyIcon struct {
	Size                uint32
	Window              uintptr
	ID, Flags, Callback uint32
	Icon                uintptr
	Tip                 [128]uint16
	State, StateMask    uint32
	Info                [256]uint16
	Version             uint32
	Title               [64]uint16
	InfoFlags           uint32
	GUID                windows.GUID
	BalloonIcon         uintptr
}

func ServiceState() (svc.State, error) {
	manager, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return 0, err
	}
	defer windows.CloseServiceHandle(manager)
	service, err := windows.OpenService(manager, wide("BrrpBrrp"), windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return 0, err
	}
	defer windows.CloseServiceHandle(service)
	var status windows.SERVICE_STATUS
	if err = windows.QueryServiceStatus(service, &status); err != nil {
		return 0, err
	}
	return svc.State(status.CurrentState), nil
}

func Launch(binary string, args ...string) error {
	for i := range args {
		args[i] = syscall.EscapeArg(args[i])
	}
	return windows.ShellExecute(0, wide("open"), wide(binary), wide(strings.Join(args, " ")), nil, windows.SW_HIDE)
}
func elevate(binary string, args ...string) error {
	for i := range args {
		args[i] = syscall.EscapeArg(args[i])
	}
	return windows.ShellExecute(0, wide("runas"), wide(binary), wide(strings.Join(args, " ")), nil, windows.SW_HIDE)
}
func Alert(text string) {
	proc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(wide(text))), uintptr(unsafe.Pointer(wide("BrrpBrrp"))), 0x10)
}
func Confirm(text string) bool {
	r, _, _ := proc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(wide(text))), uintptr(unsafe.Pointer(wide("BrrpBrrp"))), 0x24)
	return r == 6
}
func CloseTray() error {
	h, _, _ := proc("FindWindowW").Call(uintptr(unsafe.Pointer(wide(windowClass))), 0)
	if h == 0 {
		return nil
	}
	var pid uint32
	proc("GetWindowThreadProcessId").Call(h, uintptr(unsafe.Pointer(&pid)))
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	if result, _, err := proc("PostMessageW").Call(h, 0x10, 0, 0); result == 0 {
		return err
	}
	result, err := windows.WaitForSingleObject(process, 5000)
	if err != nil {
		return err
	}
	if result != windows.WAIT_OBJECT_0 {
		return errors.New("Close the BrrpBrrp tray icon before continuing.")
	}
	return nil
}

func Run(ctx context.Context) error {
	// Detach this companion only; never hide the user's terminal window.
	windows.NewLazySystemDLL("kernel32.dll").NewProc("FreeConsole").Call()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	mutex, err := windows.CreateMutex(nil, false, wide(`Local\BrrpBrrp.Tray.v1`))
	if mutex != 0 {
		defer windows.CloseHandle(mutex)
	}
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return nil
	}
	if err != nil {
		return err
	}
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(binary), "desktop.json"))
	if err != nil {
		return fmt.Errorf("Run setup once to add desktop controls: %w", err)
	}
	var settings Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		return err
	}
	address, err := url.Parse(settings.URL)
	if err != nil || address.Host == "" || address.User != nil || (address.Scheme != "http" && address.Scheme != "https") {
		return errors.New("invalid desktop server address")
	}
	open := func(fragment string) {
		if err := windows.ShellExecute(0, wide("open"), wide(settings.URL+fragment), nil, nil, windows.SW_SHOWNORMAL); err != nil {
			Alert(err.Error())
		}
	}
	var icon notifyIcon
	// Use a bundled application icon rather than depending on an external file.
	iconPath := filepath.Join(filepath.Dir(binary), "brrpbrrp.ico")
	iconHandle, _, _ := proc("LoadImageW").Call(0, uintptr(unsafe.Pointer(wide(iconPath))), 1, 32, 32, 0x10)
	if iconHandle == 0 {
		iconHandle, _, _ = proc("LoadIconW").Call(0, 32512)
	} else {
		defer proc("DestroyIcon").Call(iconHandle)
	}
	taskbar, _, _ := proc("RegisterWindowMessageW").Call(uintptr(unsafe.Pointer(wide("TaskbarCreated"))))
	update := func(add bool) {
		state, err := ServiceState()
		text := "BrrpBrrp — stopped"
		if err != nil {
			text = "BrrpBrrp — service unavailable"
		} else if state == svc.Running {
			text = "BrrpBrrp — running"
		} else if state != svc.Stopped {
			text = "BrrpBrrp — changing state"
		}
		icon.Tip = [128]uint16{}
		copy(icon.Tip[:], windows.StringToUTF16(text))
		op := uintptr(1)
		if add {
			op = 0
		}
		result, _, _ := shell32.NewProc("Shell_NotifyIconW").Call(op, uintptr(unsafe.Pointer(&icon)))
		if result == 0 && !add {
			result, _, _ = shell32.NewProc("Shell_NotifyIconW").Call(0, uintptr(unsafe.Pointer(&icon)))
			add = true
		}
		if add && result != 0 {
			icon.Version = 4
			shell32.NewProc("Shell_NotifyIconW").Call(3, uintptr(unsafe.Pointer(&icon)))
		}
	}
	menu := func(hwnd uintptr) {
		handle, _, _ := proc("CreatePopupMenu").Call()
		if handle == 0 {
			return
		}
		defer proc("DestroyMenu").Call(handle)
		appendItem := func(id uintptr, label string, disabled bool) {
			flags := uintptr(0)
			if disabled {
				flags = 1
			}
			proc("AppendMenuW").Call(handle, flags, id, uintptr(unsafe.Pointer(wide(label))))
		}
		state, err := ServiceState()
		running := err == nil && state == svc.Running
		stopped := err == nil && state == svc.Stopped
		label := "Server stopped"
		if running {
			label = "Server running"
		} else if err != nil {
			label = "Service unavailable"
		} else if !stopped {
			label = "Server changing state…"
		}
		appendItem(0, label, true)
		appendItem(1, "Open BrrpBrrp", !running)
		appendItem(2, "Server settings", !running)
		appendItem(3, "Setup & certificates…", false)
		proc("AppendMenuW").Call(handle, 0x800, 0, 0)
		appendItem(4, "Start server", !stopped)
		appendItem(5, "Restart server", !running)
		appendItem(6, "Stop server", !running)
		appendItem(7, "Stop server and exit", !running)
		proc("AppendMenuW").Call(handle, 0x800, 0, 0)
		appendItem(8, "Exit tray (keep server running)", false)
		var p point
		proc("GetCursorPos").Call(uintptr(unsafe.Pointer(&p)))
		proc("SetForegroundWindow").Call(hwnd)
		selected, _, _ := proc("TrackPopupMenu").Call(handle, 0x100|0x2, uintptr(p.X), uintptr(p.Y), 0, hwnd, 0)
		proc("PostMessageW").Call(hwnd, 0, 0, 0)
		var actionErr error
		switch selected {
		case 1:
			open("")
		case 2:
			open("/#server-settings")
		case 3:
			actionErr = Launch(binary, "install")
		case 4:
			actionErr = elevate(binary, "service-start")
		case 5:
			if Confirm("Restart the server? Active voice calls will disconnect briefly.") {
				actionErr = elevate(binary, "service-restart")
			}
		case 6, 7:
			if Confirm("Stop the server? Everyone will be disconnected until you start it again.") {
				command := "service-stop"
				if selected == 7 {
					command = "service-stop-and-exit"
				}
				actionErr = elevate(binary, command)
			}
		case 8:
			proc("DestroyWindow").Call(hwnd)
		}
		if actionErr != nil && !errors.Is(actionErr, windows.ERROR_CANCELLED) {
			Alert(actionErr.Error())
		}
	}
	callback := windows.NewCallback(func(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
		switch msg {
		case 0x8001:
			switch uint32(lp) & 0xffff {
			case 0x202, 0x400:
				open("")
			case 0x205, 0x7b, 0x401:
				menu(hwnd)
			}
			return 0
		case 0x113:
			update(false)
			return 0
		case 0x10:
			proc("DestroyWindow").Call(hwnd)
			return 0
		case 2:
			shell32.NewProc("Shell_NotifyIconW").Call(2, uintptr(unsafe.Pointer(&icon)))
			proc("PostQuitMessage").Call(0)
			return 0
		}
		if uintptr(msg) == taskbar {
			update(true)
			return 0
		}
		result, _, _ := proc("DefWindowProcW").Call(hwnd, uintptr(msg), wp, lp)
		return result
	})
	var instance windows.Handle
	if err := windows.GetModuleHandleEx(0, nil, &instance); err != nil {
		return err
	}
	class := windowInfo{Callback: callback, Instance: uintptr(instance), Class: wide(windowClass)}
	class.Size = uint32(unsafe.Sizeof(class))
	if result, _, err := proc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&class))); result == 0 {
		return err
	}
	defer proc("UnregisterClassW").Call(uintptr(unsafe.Pointer(class.Class)), class.Instance)
	hwnd, _, err := proc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(class.Class)), uintptr(unsafe.Pointer(wide("BrrpBrrp"))), 0, 0, 0, 0, 0, 0, 0, class.Instance, 0)
	if hwnd == 0 {
		return err
	}
	defer proc("DestroyWindow").Call(hwnd)
	icon = notifyIcon{Window: hwnd, ID: 1, Flags: 1 | 2 | 4 | 0x20 | 0x80, GUID: windows.GUID{Data1: 0xe73591c7, Data2: 0x1a52, Data3: 0x4c17, Data4: [8]byte{0x97, 0x42, 0x66, 0x6b, 0x31, 0x82, 0x41, 0xef}}, Callback: 0x8001, Icon: iconHandle}
	icon.Size = uint32(unsafe.Sizeof(icon))
	update(true)
	proc("SetTimer").Call(hwnd, 1, 3000, 0)
	defer proc("KillTimer").Call(hwnd, 1)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			proc("PostMessageW").Call(hwnd, 0x10, 0, 0)
		case <-done:
		}
	}()
	var msg message
	for {
		result, _, err := proc("GetMessageW").Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(result) == -1 {
			return err
		}
		if result == 0 {
			return nil
		}
		proc("TranslateMessage").Call(uintptr(unsafe.Pointer(&msg)))
		proc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&msg)))
	}
}
