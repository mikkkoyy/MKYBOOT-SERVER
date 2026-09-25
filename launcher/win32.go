package main

// win32.go contains the minimal Win32 syscall bindings used by the launcher.
// Only the exact APIs required for the window, tray, dialogs, painting and
// per-user registry entry are bound; there are no third-party dependencies.

import (
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	dwmapi   = syscall.NewLazyDLL("dwmapi.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
)

var (
	procRegisterClassExW     = user32.NewProc("RegisterClassExW")
	procCreateWindowExW      = user32.NewProc("CreateWindowExW")
	procDefWindowProcW       = user32.NewProc("DefWindowProcW")
	procShowWindow           = user32.NewProc("ShowWindow")
	procUpdateWindow         = user32.NewProc("UpdateWindow")
	procPostMessageW         = user32.NewProc("PostMessageW")
	procSendMessageW         = user32.NewProc("SendMessageW")
	procPostQuitMessage      = user32.NewProc("PostQuitMessage")
	procGetMessageW          = user32.NewProc("GetMessageW")
	procPeekMessageW         = user32.NewProc("PeekMessageW")
	procTranslateMessage     = user32.NewProc("TranslateMessage")
	procDispatchMessageW     = user32.NewProc("DispatchMessageW")
	procSetWindowTextW       = user32.NewProc("SetWindowTextW")
	procGetWindowTextW       = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW = user32.NewProc("GetWindowTextLengthW")
	procMoveWindow           = user32.NewProc("MoveWindow")
	procSetFocus             = user32.NewProc("SetFocus")
	procGetFocus             = user32.NewProc("GetFocus")
	procInvalidateRect       = user32.NewProc("InvalidateRect")
	procBeginPaint           = user32.NewProc("BeginPaint")
	procEndPaint             = user32.NewProc("EndPaint")
	procGetClientRect        = user32.NewProc("GetClientRect")
	procFillRect             = user32.NewProc("FillRect")
	procDrawTextW            = user32.NewProc("DrawTextW")
	procSetTextColor         = gdi32.NewProc("SetTextColor")
	procSetBkMode            = gdi32.NewProc("SetBkMode")
	procCreateSolidBrush     = gdi32.NewProc("CreateSolidBrush")
	procCreateFontW          = gdi32.NewProc("CreateFontW")
	procSelectObject         = gdi32.NewProc("SelectObject")
	procDeleteObject         = gdi32.NewProc("DeleteObject")
	procCreateRoundRectRgn   = gdi32.NewProc("CreateRoundRectRgn")
	procRoundRect            = gdi32.NewProc("RoundRect")
	procCreatePen            = gdi32.NewProc("CreatePen")
	procEllipse              = gdi32.NewProc("Ellipse")
	procMoveToEx             = gdi32.NewProc("MoveToEx")
	procLineTo               = gdi32.NewProc("LineTo")
	procGetStockObject       = gdi32.NewProc("GetStockObject")
	procSetWindowRgn         = user32.NewProc("SetWindowRgn")
	procDrawIconEx           = user32.NewProc("DrawIconEx")
	procLoadCursorW          = user32.NewProc("LoadCursorW")
	procEnableWindow         = user32.NewProc("EnableWindow")
	procIsWindow             = user32.NewProc("IsWindow")
	procCreatePopupMenu      = user32.NewProc("CreatePopupMenu")
	procAppendMenuW          = user32.NewProc("AppendMenuW")
	procTrackPopupMenu       = user32.NewProc("TrackPopupMenu")
	procDestroyMenu          = user32.NewProc("DestroyMenu")
	procSetForegroundWindow  = user32.NewProc("SetForegroundWindow")
	procMessageBoxW          = user32.NewProc("MessageBoxW")
	procGetDC                = user32.NewProc("GetDC")
	procReleaseDC            = user32.NewProc("ReleaseDC")
	procGetWindowLongPtrW    = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW    = user32.NewProc("SetWindowLongPtrW")
	procCreateIconFromRes    = user32.NewProc("CreateIconFromResourceEx")
	procTrackMouseEvent      = user32.NewProc("TrackMouseEvent")
	procGetCursorPos         = user32.NewProc("GetCursorPos")
	procAdjustWindowRectEx   = user32.NewProc("AdjustWindowRectEx")
	procDestroyWindow        = user32.NewProc("DestroyWindow")
	procCallWindowProcW      = user32.NewProc("CallWindowProcW")
	procGetWindowRect        = user32.NewProc("GetWindowRect")
	procIsDialogMessageW     = user32.NewProc("IsDialogMessageW")

	procShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	procShellExecuteW    = shell32.NewProc("ShellExecuteW")

	procDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")

	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")

	procRegOpenKeyExW   = advapi32.NewProc("RegOpenKeyExW")
	procRegCreateKeyExW = advapi32.NewProc("RegCreateKeyExW")
	procRegSetValueExW  = advapi32.NewProc("RegSetValueExW")
	procRegDeleteValueW = advapi32.NewProc("RegDeleteValueW")
	procRegCloseKey     = advapi32.NewProc("RegCloseKey")
)

// Win32 message, style and constant values used by the launcher.
const (
	wmCreate         = 0x0001
	wmDestroy        = 0x0002
	wmSize           = 0x0005
	wmClose          = 0x0010
	wmPaint          = 0x000F
	wmEraseBkgnd     = 0x0014
	wmCommand        = 0x0111
	wmSetFont        = 0x0030
	wmDrawItem       = 0x002B
	wmCtlColorEdit   = 0x0133
	wmCtlColorStatic = 0x0130
	wmCtlColorBtn    = 0x0135
	wmKeydown        = 0x0100
	wmSysCommand     = 0x0112
	scClose          = 0xF060
	wmMouseMove      = 0x0200
	wmMouseLeave     = 0x02A3
	wmSetIcon        = 0x0080
	wmGetDlgCode     = 0x0087
	wmVkeyReturn     = 0x0D
	wmVkeyEscape     = 0x1B
	wmAppStatus      = 0x8001 // WM_APP+1: status refresh
	wmAppControl     = 0x8002 // WM_APP+2: control operation finished
	wmAppNotice      = 0x8003 // WM_APP+3: post a notice dialog (lParam = *Notice)

	wsCaption     = 0x00C00000
	wsSysMenu     = 0x00080000
	wsMinimizeBox = 0x00020000
	wsVisible     = 0x10000000
	wsChild       = 0x40000000
	wsTabStop     = 0x00010000
	wsBorder      = 0x00800000
	wsDisabled    = 0x08000000

	bsOwnerDraw   = 0x0000000B
	esAutoHScroll = 0x0080
	esPassword    = 0x0020

	// DRAWITEMSTRUCT control types (ODT_*) and item states (ODS_*).
	// ODT_BUTTON is 4 (2 is ODT_LISTBOX, 3 is ODT_COMBOBOX).
	odtButton   = 4
	odsSelected = 0x0001
	odsGreyed   = 0x0002
	odsDisabled = 0x0004
	odsChecked  = 0x0008
	odsFocus    = 0x0010
	odsDefault  = 0x0020

	bnClicked   = 0
	enChange    = 0x0300
	enSetFocus  = 0x0100
	enKillFocus = 0x0200

	swHide    = 0
	swShow    = 5
	swRestore = 9

	sizeMinimized = 1

	cwUseDefault = 0x80000000
	idcArrow     = 32512
	idiApp       = 32512

	idOK     = 1
	idCancel = 2
	idYes    = 6
	idNo     = 7

	mbOk            = 0x00000000
	mbYesNo         = 0x00000004
	mbIconError     = 0x00000010
	mbIconQuestion  = 0x00000020
	mbIconInfo      = 0x00000040
	mbSetForeground = 0x00010000

	tpmRightButton = 0x0002
	tpmBottomAlign = 0x0020
	tpmLeftAlign   = 0x0000

	mfString    = 0x00000000
	mfSeparator = 0x00000800
	mfDisabled  = 0x00000002
	mfGrayed    = 0x00000001
	mfChecked   = 0x00000008

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004
	nimAdd     = 0x00000000
	nimModify  = 0x00000001
	nimDelete  = 0x00000002

	tmeLeave = 0x00000002

	dtLeft        = 0x0000
	dtCenter      = 0x0001
	dtRight       = 0x0002
	dtVCenter     = 0x0008
	dtSingleLine  = 0x0200
	dtEndEllipsis = 0x8000
	dtNoPrefix    = 0x0800
	dtWordBreak   = 0x0010

	transparentBk = 1
	fwNormal      = 400
	fwBold        = 700
	ansiCharset   = 0
	outDefault    = 0
	clipDefault   = 0
	clearType     = 4
	defaultPitch  = 0

	gwlpWndProc = ^uintptr(0) - 3 // GWLP_WNDPROC == -4 as a pointer-width value

	dwmwaUseImmersiveDarkMode = 20
	dwmwaWindowCornerPref     = 33
	dwmwaSystemBackdropType   = 38
	dwmwcpRound               = 2
	dwmsbtMainWindow          = 2 // Mica material
	sOK                       = 0

	hiWordShift = 16
)

// ---- Win32 structures ----

type winPoint struct {
	X, Y int32
}

type winRect struct {
	Left, Top, Right, Bottom int32
}

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

type winMsg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      winPoint
}

type paintStruct struct {
	Hdc      uintptr
	Erase    int32
	RcPaint  winRect
	Restore  int32
	IncUpd   int32
	Reserved [32]byte
}

type drawItemStruct struct {
	CtlType    uint32
	CtlID      uint32
	ItemID     uint32
	ItemAction uint32
	ItemState  uint32
	HwndItem   uintptr
	Hdc        uintptr
	RcItem     winRect
	ItemData   uintptr
}

type trackEventStruct struct {
	CbSize uint32
	DwFlag uint32
}

// notifyIconDataW matches the Win2000+ NOTIFYICONDATAW layout.
type notifyIconDataW struct {
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
	TimeoutOrVersion uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     uintptr
}

// ---- small helpers ----

func utf16Ptr(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		p, _ = syscall.UTF16PtrFromString("")
	}
	return p
}

// windowText reads the current text of an edit control.
func windowText(hwnd uintptr) string {
	length, _, _ := procGetWindowTextLengthW.Call(hwnd)
	if length == 0 {
		return ""
	}
	buf := make([]uint16, int(length)+1)
	n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

func setWindowText(hwnd uintptr, s string) {
	procSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(utf16Ptr(s))))
}

func loWord(v uintptr) uint16 { return uint16(v & 0xFFFF) }
func hiWord(v uintptr) uint16 { return uint16(v >> hiWordShift) }

// rgb packs an RGB color into a COLORREF value.
func rgb(r, g, b uintptr) uintptr {
	return r | (g << 8) | (b << 16)
}

// postAppMessage safely delivers a message to the main window from any
// goroutine.
func postAppMessage(hwnd uintptr, msg, wparam, lparam uintptr) {
	procPostMessageW.Call(hwnd, msg, wparam, lparam)
}

// callable matches both *syscall.LazyProc and *syscall.Proc.
type callable interface {
	Call(args ...uintptr) (uintptr, uintptr, error)
}

// callSimple invokes a Win32 proc and returns the raw result.
func callSimple(p callable, args ...uintptr) uintptr {
	r, _, _ := p.Call(args...)
	return r
}

// regSetStringValue writes a per-user HKCU Run entry (no admin required).
func regSetStringValue(subKey, name, value string) error {
	var h uintptr
	key, kerr := syscall.UTF16PtrFromString(subKey)
	if kerr != nil {
		return kerr
	}
	nName, nerr := syscall.UTF16PtrFromString(name)
	if nerr != nil {
		return nerr
	}
	valUTF16, verr := syscall.UTF16FromString(value)
	if verr != nil {
		return verr
	}
	const kReadWrite = 0x0002
	const optionsNonVol = 0
	ret, _, _ := procRegCreateKeyExW.Call(
		0x80000001, // HKEY_CURRENT_USER
		uintptr(unsafe.Pointer(key)),
		0,
		0,
		optionsNonVol,
		kReadWrite,
		0,
		uintptr(unsafe.Pointer(&h)),
		0,
	)
	if ret != 0 {
		return errorFromWin32(ret)
	}
	defer procRegCloseKey.Call(h)
	ret, _, _ = procRegSetValueExW.Call(
		h,
		uintptr(unsafe.Pointer(nName)),
		0,
		1, // REG_SZ
		uintptr(unsafe.Pointer(&valUTF16[0])),
		uintptr(len(valUTF16)*2), // UTF-16 bytes including terminator
	)
	if ret != 0 {
		return errorFromWin32(ret)
	}
	return nil
}

// regDeleteStringValue removes a per-user HKCU Run value if present.
func regDeleteStringValue(subKey, name string) error {
	key, kerr := syscall.UTF16PtrFromString(subKey)
	if kerr != nil {
		return kerr
	}
	nName, nerr := syscall.UTF16PtrFromString(name)
	if nerr != nil {
		return nerr
	}
	const kReadWrite = 0x0002
	var h uintptr
	ret, _, _ := procRegOpenKeyExW.Call(
		0x80000001,
		uintptr(unsafe.Pointer(key)),
		0,
		kReadWrite,
		uintptr(unsafe.Pointer(&h)),
	)
	if ret != 0 {
		return errorFromWin32(ret) // includes "value/key not found"
	}
	defer procRegCloseKey.Call(h)
	ret, _, _ = procRegDeleteValueW.Call(h, uintptr(unsafe.Pointer(nName)))
	if ret != 0 {
		return errorFromWin32(ret)
	}
	return nil
}

type win32Error uintptr

func (e win32Error) Error() string {
	// FormatMessage is intentionally not bound; a numeric code keeps the
	// binding surface minimal and contains no sensitive data.
	return "win32 error " + itoa(int(e))
}

func errorFromWin32(code uintptr) error { return win32Error(code) }

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
