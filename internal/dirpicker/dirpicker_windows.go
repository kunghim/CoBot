package dirpicker

import (
	"context"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	user32               = windows.NewLazySystemDLL("user32.dll")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
	procGetForeground    = user32.NewProc("GetForegroundWindow")
	procGetWindowRect    = user32.NewProc("GetWindowRect")
	procGetThreadID      = user32.NewProc("GetWindowThreadProcessId")
	procAttachInput      = user32.NewProc("AttachThreadInput")
	procSetForeground    = user32.NewProc("SetForegroundWindow")
	procCreateWindowEx   = user32.NewProc("CreateWindowExW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procShowWindow       = user32.NewProc("ShowWindow")
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	procCurrentThreadID  = kernel32.NewProc("GetCurrentThreadId")
)

var (
	clsidFileOpenDialog = windows.GUID{Data1: 0xDC1C5A9C, Data2: 0xE88A, Data3: 0x4DDE, Data4: [8]byte{0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7}}
	iidFileOpenDialog   = windows.GUID{Data1: 0xD57C7288, Data2: 0xD4AD, Data3: 0x4768, Data4: [8]byte{0xBE, 0x02, 0x9D, 0x96, 0x95, 0x32, 0xD9, 0x60}}
)

const (
	clsctxInprocServer = 0x1
	fosPickFolders     = 0x20
	fosForceFileSystem = 0x40
	fosPathMustExist   = 0x800
	sigdnFileSysPath   = 0x80058000
	hrCancelled        = 0x800704C7

	wsPopup          = 0x80000000
	wsExTopmost      = 0x00000008
	wsExToolWindow   = 0x00000080
	swShowNoActivate = 4

	// IFileOpenDialog vtable slots (IUnknown, IModalWindow, IFileDialog).
	vtRelease    = 2
	vtShow       = 3
	vtSetOptions = 9
	vtGetOptions = 10
	vtSetTitle   = 17
	vtGetResult  = 20
	// IShellItem vtable slot.
	vtGetDisplayName = 5
)

type rect struct{ left, top, right, bottom int32 }

// comObject is a COM interface pointer: its first word is the vtable.
type comObject struct{ vtbl *[32]uintptr }

func comCall(obj *comObject, slot int, args ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(obj.vtbl[slot], append([]uintptr{uintptr(unsafe.Pointer(obj))}, args...)...)
	return r
}

func failed(hr uintptr) bool { return int32(hr) < 0 }

// pick shows the modern Explorer folder chooser. It is owned by an invisible
// topmost window placed over the window the user clicked from (normally the
// browser), so it opens in front of that window instead of behind it.
func pick(_ context.Context, title string) (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE); err != nil {
		return "", fmt.Errorf("CoInitializeEx: %w", err)
	}
	defer windows.CoUninitialize()

	var dialog *comObject
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidFileOpenDialog)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidFileOpenDialog)), uintptr(unsafe.Pointer(&dialog)))
	if failed(hr) || dialog == nil {
		return "", fmt.Errorf("%w: CoCreateInstance 0x%08x", ErrUnavailable, uint32(hr))
	}
	defer comCall(dialog, vtRelease)

	var options uint32
	if hr := comCall(dialog, vtGetOptions, uintptr(unsafe.Pointer(&options))); failed(hr) {
		return "", fmt.Errorf("GetOptions 0x%08x", uint32(hr))
	}
	if hr := comCall(dialog, vtSetOptions, uintptr(options|fosPickFolders|fosForceFileSystem|fosPathMustExist)); failed(hr) {
		return "", fmt.Errorf("SetOptions 0x%08x", uint32(hr))
	}
	if title != "" {
		if p, err := windows.UTF16PtrFromString(title); err == nil {
			comCall(dialog, vtSetTitle, uintptr(unsafe.Pointer(p)))
		}
	}

	owner := topmostOwner()
	if owner != 0 {
		defer procDestroyWindow.Call(owner)
	}
	hr = comCall(dialog, vtShow, owner)
	if uint32(hr) == hrCancelled {
		return "", ErrCancelled
	}
	if failed(hr) {
		return "", fmt.Errorf("Show 0x%08x", uint32(hr))
	}

	var item *comObject
	if hr := comCall(dialog, vtGetResult, uintptr(unsafe.Pointer(&item))); failed(hr) || item == nil {
		return "", fmt.Errorf("GetResult 0x%08x", uint32(hr))
	}
	defer comCall(item, vtRelease)
	var name *uint16
	if hr := comCall(item, vtGetDisplayName, sigdnFileSysPath, uintptr(unsafe.Pointer(&name))); failed(hr) || name == nil {
		return "", fmt.Errorf("GetDisplayName 0x%08x", uint32(hr))
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(name))
	return windows.UTF16PtrToString(name), nil
}

// topmostOwner creates a zero-size topmost tool window centred on the
// current foreground window and brings it forward. Windows owned by a
// topmost window are topmost too, so the chooser cannot open hidden behind
// the browser. It returns 0 when no owner could be made; the chooser then
// opens unowned.
func topmostOwner() uintptr {
	foreground, _, _ := procGetForeground.Call()
	x, y := int32(200), int32(200)
	if foreground != 0 {
		var r rect
		if ok, _, _ := procGetWindowRect.Call(foreground, uintptr(unsafe.Pointer(&r))); ok != 0 {
			x, y = (r.left+r.right)/2-300, (r.top+r.bottom)/2-220
		}
	}
	class, _ := windows.UTF16PtrFromString("STATIC")
	owner, _, _ := procCreateWindowEx.Call(wsExTopmost|wsExToolWindow, uintptr(unsafe.Pointer(class)), 0, wsPopup,
		uintptr(x), uintptr(y), 0, 0, 0, 0, 0, 0)
	if owner == 0 {
		return 0
	}
	procShowWindow.Call(owner, swShowNoActivate)
	// The user just clicked in the foreground window; sharing its input state
	// for a moment lets the chooser take keyboard focus instead of only
	// flashing in the taskbar.
	if foreground != 0 {
		fgThread, _, _ := procGetThreadID.Call(foreground, 0)
		self, _, _ := procCurrentThreadID.Call()
		if fgThread != 0 && fgThread != self {
			procAttachInput.Call(self, fgThread, 1)
			procSetForeground.Call(owner)
			procAttachInput.Call(self, fgThread, 0)
		} else {
			procSetForeground.Call(owner)
		}
	}
	return owner
}
