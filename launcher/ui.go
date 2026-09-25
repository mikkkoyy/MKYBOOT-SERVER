package main

// ui.go implements the native dark Win32 launcher window, the owner-drawn
// controls, painting and the modal dialogs (message / confirm / password).

import (
	_ "embed"
	"fmt"
	"syscall"
	"unsafe"
)

//go:embed assets/mkyboot.ico
var iconICO []byte

// Control identifiers shared between window buttons and the tray menu.
const (
	idEditAddr     = 1001
	idBtnDashboard = 1002
	idBtnStatus    = 1003
	idBtnRestart   = 1004
	idBtnStop      = 1005
	idChkStartup   = 1006
	idDlgEdit      = 2002
)

// Fixed client size of the compact launcher window.
const (
	clientW = 440
	clientH = 430
)

// Dark, modern palette (COLORREF).
var (
	colBG         = rgb(15, 17, 23)
	colLine       = rgb(40, 46, 60)
	colText       = rgb(232, 236, 244)
	colMuted      = rgb(140, 148, 166)
	colAccent     = rgb(59, 130, 246)
	colOnline     = rgb(34, 197, 94)
	colOffline    = rgb(239, 68, 68)
	colWarn       = rgb(245, 158, 11)
	colBtn        = rgb(36, 43, 58)
	colBtnHot     = rgb(47, 57, 77)
	colBtnDown    = rgb(28, 34, 47)
	colBtnOff     = rgb(26, 30, 40)
	colPrimary    = rgb(37, 99, 235)
	colPrimaryHot = rgb(59, 130, 246)
	colEditBG     = rgb(21, 25, 34)
	colEditText   = rgb(232, 236, 244)
	colWhite      = rgb(255, 255, 255)
)

// Package-level UI globals. The launcher is a single-instance windowed app,
// so one pointer is sufficient and callbacks can find their owner.
var (
	theApp          *App
	mainWndProcCB   = syscall.NewCallback(mainWndProc)
	dialogWndProcCB = syscall.NewCallback(dialogWndProc)
	btnSubclassCB   = syscall.NewCallback(btnSubclassProc)
)

func hInstance() uintptr {
	h, _, _ := procGetModuleHandleW.Call(0)
	return h
}

func strPtr(s string) uintptr {
	return uintptr(unsafe.Pointer(utf16Ptr(s)))
}

func b2u(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

// u32 converts a signed coordinate into a Win32 argument (low 32 bits).
func u32(v int32) uintptr { return uintptr(uint32(v)) }

// mkFont creates a Segoe UI font with a negative height (cell height).
func mkFont(height, weight int) uintptr {
	f, _, _ := procCreateFontW.Call(
		uintptr(uint32(int32(-height))),
		0, 0, 0,
		uintptr(weight),
		0, 0, 0,
		uintptr(ansiCharset), uintptr(outDefault), uintptr(clipDefault),
		uintptr(clearType), uintptr(defaultPitch),
		strPtr("Segoe UI"),
	)
	return f
}

func newBrush(col uintptr) uintptr {
	b, _, _ := procCreateSolidBrush.Call(col)
	return b
}

func setControlFont(h, font uintptr) {
	if h != 0 && font != 0 {
		procSendMessageW.Call(h, wmSetFont, font, 1)
	}
}

// filledRect paints a solid rectangle with an ad-hoc brush.
func filledRect(hdc uintptr, l, t, r, b int32, col uintptr) {
	br, _, _ := procCreateSolidBrush.Call(col)
	rc := winRect{l, t, r, b}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), br)
	procDeleteObject.Call(br)
}

// fillCircle paints a solid circle (status dot).
func fillCircle(hdc uintptr, cx, cy, radius int32, col uintptr) {
	pen, _, _ := procCreatePen.Call(0 /* PS_SOLID */, 1, col)
	brush, _, _ := procCreateSolidBrush.Call(col)
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	oldBrush, _, _ := procSelectObject.Call(hdc, brush)
	procEllipse.Call(hdc, u32(cx-radius), u32(cy-radius), u32(cx+radius), u32(cy+radius))
	procSelectObject.Call(hdc, oldPen)
	procSelectObject.Call(hdc, oldBrush)
	procDeleteObject.Call(pen)
	procDeleteObject.Call(brush)
}

// drawText renders a string with the given font and color.
func drawText(hdc uintptr, r *winRect, s string, font, col, flags uintptr) {
	if font != 0 {
		procSelectObject.Call(hdc, font)
	}
	procSetBkMode.Call(hdc, uintptr(transparentBk))
	procSetTextColor.Call(hdc, col)
	procDrawTextW.Call(hdc, strPtr(s), ^uintptr(0), /* -1: NUL-terminated */
		uintptr(unsafe.Pointer(r)), flags)
}

// stateColor maps a server state to its indicator color.
func stateColor(s ServerState) uintptr {
	switch s {
	case StateOnline:
		return colOnline
	case StateConnecting:
		return colWarn
	case StateUnauthorized:
		return colWarn
	default:
		return colOffline
	}
}

// iconHandle picks the best frame of the embedded ICO and creates an HICON.
func iconHandle(data []byte, want int) uintptr {
	if len(data) < 6 {
		return 0
	}
	count := int(uint16(data[4]) | uint16(data[5])<<8)
	best, bestDelta := -1, 1<<30
	for i := 0; i < count; i++ {
		base := 6 + i*16
		if base+16 > len(data) {
			break
		}
		w := int(data[base])
		if w == 0 {
			w = 256
		}
		size := int(uint32(data[base+8]) | uint32(data[base+9])<<8 |
			uint32(data[base+10])<<16 | uint32(data[base+11])<<24)
		off := int(uint32(data[base+12]) | uint32(data[base+13])<<8 |
			uint32(data[base+14])<<16 | uint32(data[base+15])<<24)
		if size <= 0 || off < 0 || off+size > len(data) {
			continue
		}
		delta := w - want
		if delta < 0 {
			delta = -delta
		}
		if delta < bestDelta {
			best, bestDelta = i, delta
		}
	}
	if best < 0 {
		return 0
	}
	base := 6 + best*16
	size := int(uint32(data[base+8]) | uint32(data[base+9])<<8 |
		uint32(data[base+10])<<16 | uint32(data[base+11])<<24)
	off := int(uint32(data[base+12]) | uint32(data[base+13])<<8 |
		uint32(data[base+14])<<16 | uint32(data[base+15])<<24)
	if off+size > len(data) || size <= 0 {
		return 0
	}
	p := data[off : off+size]
	h, _, _ := procCreateIconFromRes.Call(
		uintptr(unsafe.Pointer(&p[0])), uintptr(size),
		1 /* icon */, 0x00000300, uintptr(want), uintptr(want), 0)
	return h
}

// infoLine builds the third status line (extra context under the state).
func (a *App) infoLine(res CheckResult, notice string) string {
	if notice != "" {
		return notice
	}
	if res.Status != nil {
		return fmt.Sprintf("Clients: %d/%d online - services %s",
			res.Status.ClientsOnline, res.Status.ClientsTotal,
			map[bool]string{true: "OK", false: "check server"}[res.Status.ServicesRunning])
	}
	if res.AuthRequired {
		return "Live details available after signing in on the dashboard."
	}
	return ""
}

// ---- window and control construction ----

const errClassExists = 1410 // ERROR_CLASS_ALREADY_EXISTS

// createMainWindow registers the window class and creates the main window.
func (a *App) createMainWindow() error {
	className := utf16Ptr("MkybootLauncherWnd")
	title := utf16Ptr("MKYBOOT Launcher")
	cursor, _, _ := procLoadCursorW.Call(0, uintptr(idcArrow))
	a.iconMain = iconHandle(iconICO, 32)
	hSmall := iconHandle(iconICO, 16)

	wc := wndClassEx{
		CbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		LpfnWndProc:   mainWndProcCB,
		HInstance:     hInstance(),
		HIcon:         a.iconMain,
		HCursor:       cursor,
		HIconSm:       hSmall,
		LpszClassName: className,
	}
	ret, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if ret == 0 {
		if errno, ok := err.(syscall.Errno); !ok || errno != errClassExists {
			return err
		}
	}

	style := uintptr(wsCaption | wsSysMenu | wsMinimizeBox)
	rc := winRect{0, 0, clientW, clientH}
	procAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&rc)), style, 0, 0)

	hwnd, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		style,
		cwUseDefault, cwUseDefault,
		uintptr(rc.Right-rc.Left), uintptr(rc.Bottom-rc.Top),
		0, 0, hInstance(), 0,
	)
	if hwnd == 0 {
		if err != nil {
			return err
		}
		return win32Error(0)
	}
	a.hwnd = hwnd

	procSendMessageW.Call(hwnd, wmSetIcon, 1, a.iconMain)
	procSendMessageW.Call(hwnd, wmSetIcon, 0, hSmall)

	// Modern dark caption + rounded corners on Windows 10 1809+/11.
	// Both calls fail silently on older builds (plain fallback).
	one := uintptr(1)
	procDwmSetWindowAttribute.Call(hwnd, uintptr(dwmwaUseImmersiveDarkMode),
		uintptr(unsafe.Pointer(&one)), 4)
	round := uintptr(dwmwcpRound)
	procDwmSetWindowAttribute.Call(hwnd, uintptr(dwmwaWindowCornerPref),
		uintptr(unsafe.Pointer(&round)), 4)
	return nil
}

// createControls builds the child controls (called from wmCreate).
func (a *App) createControls(hwnd uintptr) {
	a.fonts = gdiFonts{
		title: mkFont(20, fwBold),
		big:   mkFont(17, fwBold),
		label: mkFont(13, fwBold),
		body:  mkFont(13, fwNormal),
		small: mkFont(11, fwNormal),
	}
	a.brushes = gdiBrushes{
		bg:      newBrush(colBG),
		line:    newBrush(colLine),
		editBG:  newBrush(colEditBG),
		text:    newBrush(colText),
		muted:   newBrush(colMuted),
		accent:  newBrush(colAccent),
		online:  newBrush(colOnline),
		offline: newBrush(colOffline),
		primary: newBrush(colPrimary),
		warn:    newBrush(colWarn),
	}

	baseStyle := uintptr(wsChild | wsVisible | wsTabStop)
	edit, _, _ := procCreateWindowExW.Call(
		0, strPtr("EDIT"), strPtr(a.cfg.ServerURL),
		baseStyle|uintptr(esAutoHScroll),
		20, 100, 400, 34,
		hwnd, uintptr(idEditAddr), hInstance(), 0)
	a.hEdit = edit

	mk := func(id int, label string, x, y, w, h int32) uintptr {
		b, _, _ := procCreateWindowExW.Call(
			0, strPtr("BUTTON"), strPtr(label),
			baseStyle|uintptr(bsOwnerDraw),
			u32(x), u32(y), u32(w), u32(h),
			hwnd, uintptr(id), hInstance(), 0)
		return b
	}
	a.hBtn[0] = mk(idBtnDashboard, "Open Dashboard", 20, 242, 194, 40)
	a.hBtn[1] = mk(idBtnStatus, "Server Status", 226, 242, 194, 40)
	a.hBtn[2] = mk(idBtnRestart, "Restart Server", 20, 294, 194, 40)
	a.hBtn[3] = mk(idBtnStop, "Stop Server", 226, 294, 194, 40)
	a.hChk = mk(idChkStartup, "Start with Windows", 20, 350, 240, 24)
	// Ownerdraw buttons ignore BM_SETCHECK, so the row state lives in the App.
	a.startupOn = a.cfg.StartWithWindows

	setControlFont(a.hEdit, a.fonts.body)
	for _, h := range a.hBtn {
		setControlFont(h, a.fonts.body)
	}
	setControlFont(a.hChk, a.fonts.body)

	// Subclass owner-drawn controls for hover highlighting.
	for _, h := range a.hBtn {
		a.subclass(h)
	}
	a.subclass(a.hChk)
	a.layout()
}

// layout positions the fixed-size control arrangement.
func (a *App) layout() {
	mv := func(h uintptr, x, y, w, hgt int32) {
		if h != 0 {
			procMoveWindow.Call(h, u32(x), u32(y), u32(w), u32(hgt), 1)
		}
	}
	mv(a.hEdit, 20, 100, 400, 34)
	mv(a.hBtn[0], 20, 242, 194, 40)
	mv(a.hBtn[1], 226, 242, 194, 40)
	mv(a.hBtn[2], 20, 294, 194, 40)
	mv(a.hBtn[3], 226, 294, 194, 40)
	mv(a.hChk, 20, 350, 240, 24)
}

// subclass installs the hover-tracking window procedure on an owner-draw
// child control, remembering the previous procedure.
func (a *App) subclass(h uintptr) {
	if h == 0 {
		return
	}
	if a.btnOrig == nil {
		a.btnOrig = map[uintptr]uintptr{}
	}
	prev, _, _ := procSetWindowLongPtrW.Call(h, gwlpWndProc, btnSubclassCB)
	if prev != 0 {
		a.btnOrig[h] = prev
	}
}

// btnSubclassProc tracks mouse hover for owner-drawn controls.
func btnSubclassProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	a := theApp
	if a == nil {
		return 0
	}
	prev := a.btnOrig[hwnd]
	switch msg {
	case wmMouseMove:
		if a.hotHwnd != hwnd {
			old := a.hotHwnd
			a.hotHwnd = hwnd
			te := trackEventStruct{CbSize: uint32(unsafe.Sizeof(trackEventStruct{})), DwFlag: tmeLeave}
			procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&te)))
			if old != 0 {
				procInvalidateRect.Call(old, 0, 1)
			}
			procInvalidateRect.Call(hwnd, 0, 1)
		}
	case wmMouseLeave:
		if a.hotHwnd == hwnd {
			a.hotHwnd = 0
			procInvalidateRect.Call(hwnd, 0, 1)
		}
	}
	if prev == 0 {
		return 0
	}
	return callSimple(procCallWindowProcW, prev, hwnd, msg, wparam, lparam)
}

// freeGDI releases shared fonts and brushes on window destruction.
func (a *App) freeGDI() {
	for _, h := range []uintptr{
		a.fonts.title, a.fonts.big, a.fonts.label, a.fonts.body, a.fonts.small,
		a.brushes.bg, a.brushes.line, a.brushes.editBG, a.brushes.text,
		a.brushes.muted, a.brushes.accent, a.brushes.online, a.brushes.offline,
		a.brushes.primary, a.brushes.warn,
	} {
		if h != 0 {
			procDeleteObject.Call(h)
		}
	}
}

// ---- main window procedure ----

func mainWndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	a := theApp
	switch msg {
	case wmCreate:
		if a != nil {
			a.createControls(hwnd)
		}
		return 0
	case wmSize:
		if a != nil {
			if wparam == sizeMinimized {
				// Minimizing sends the launcher to the notification area.
				a.hideToTray()
				return 0
			}
			a.layout()
		}
		return 0
	case wmEraseBkgnd:
		return 1 // fully painted in wmPaint
	case wmPaint:
		if a != nil {
			a.paintWindow(hwnd)
		}
		return 0
	case wmDrawItem:
		if a == nil {
			break
		}
		dis := (*drawItemStruct)(unsafe.Pointer(lparam))
		if dis.CtlType == odtButton {
			hot := a.hotHwnd == dis.HwndItem
			// For owner-drawn buttons ItemID is not the control id, so the
			// notification is routed by CtlID, which carries the real id.
			switch int(dis.CtlID) {
			case idChkStartup:
				a.drawCheckbox(dis)
			case idBtnDashboard, idOK, idYes:
				a.drawButton(dis, hot, true)
			default:
				a.drawButton(dis, hot, false)
			}
			return 1
		}
		return 0
	case wmCtlColorEdit:
		if a != nil {
			procSetBkMode.Call(wparam, 2 /* opaque */)
			procSetTextColor.Call(wparam, uintptr(colEditText))
			return a.brushes.editBG
		}
		return 0
	case wmCommand:
		if a == nil {
			return 0
		}
		id := loWord(wparam)
		code := hiWord(wparam)
		if code == bnClicked {
			if int(id) == idChkStartup {
				// The button proc keeps no check state for ownerdraw rows, so
				// flip the state the launcher owns and repaint the row.
				a.startupOn = !a.startupOn
				a.toggleStartup(a.startupOn)
				procInvalidateRect.Call(a.hChk, 0, 1)
				return 0
			}
			a.onCommand(id)
			return 0
		}
		if int(id) == idEditAddr {
			switch code {
			case enChange:
				a.onAddressEdited()
			case enSetFocus:
				a.addrEdit = true
				procInvalidateRect.Call(hwnd, 0, 1)
			case enKillFocus:
				a.addrEdit = false
				a.onAddressFocusLost()
				procInvalidateRect.Call(hwnd, 0, 1)
			}
			return 0
		}
		return 0
	case wmAppStatus:
		if a != nil {
			a.refreshUI()
		}
		return 0
	case wmAppNotice, wmAppControl:
		if a != nil {
			n := a.takeNotice()
			a.showNotice(n.Title, n.Text)
			a.refreshUI()
		}
		return 0
	case wmTrayIcon:
		if a != nil {
			a.trayCallback(lparam)
		}
		return 0
	case wmClose:
		if a != nil {
			a.hideToTray() // closing the window minimizes to the tray
		}
		return 0
	case wmSysCommand:
		if wparam&0xFFF0 == scClose {
			if a != nil {
				a.hideToTray()
			}
			return 0
		}
	case wmDestroy:
		if a != nil {
			a.trayRemove()
			a.freeGDI()
			a.hwnd = 0
		}
		procPostQuitMessage.Call(0)
		return 0
	}
	return callSimple(procDefWindowProcW, hwnd, msg, wparam, lparam)
}

// paintWindow renders the whole dark launcher surface.
func (a *App) paintWindow(hwnd uintptr) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	// Solid background (flat dark theme - deterministic on Win10 and Win11).
	filledRect(hdc, 0, 0, clientW, clientH, uintptr(colBG))

	res, lastGood, _, notice := a.stateSnapshot()
	stateCol := stateColor(res.State)

	// Header: icon, title, connection indicator.
	if a.iconMain != 0 {
		procDrawIconEx.Call(hdc, 20, 16, a.iconMain, 30, 30, 0, 0, 3 /* DI_NORMAL */)
	}
	drawText(hdc, &winRect{60, 10, 300, 36}, "MKYBOOT", a.fonts.title, uintptr(colText),
		dtLeft|dtSingleLine|dtNoPrefix)
	drawText(hdc, &winRect{61, 38, 300, 58}, "Windows Launcher", a.fonts.small, uintptr(colMuted),
		dtLeft|dtSingleLine|dtNoPrefix)
	drawText(hdc, &winRect{230, 16, 396, 48}, res.State.String(), a.fonts.small, stateCol,
		dtRight|dtVCenter|dtSingleLine|dtNoPrefix)
	fillCircle(hdc, 410, 31, 6, stateCol)
	filledRect(hdc, 0, 64, clientW, 65, uintptr(colLine))

	// Server address section.
	drawText(hdc, &winRect{20, 76, 300, 96}, "SERVER ADDRESS", a.fonts.small, uintptr(colMuted),
		dtLeft|dtSingleLine|dtNoPrefix)
	border := uintptr(colLine)
	if a.addrEdit {
		border = uintptr(colAccent)
	}
	filledRect(hdc, 19, 99, 421, 135, border) // ring; the edit covers the middle

	// Status section.
	drawText(hdc, &winRect{20, 144, 420, 172}, res.State.String(), a.fonts.big, stateCol,
		dtLeft|dtSingleLine|dtNoPrefix)
	drawText(hdc, &winRect{20, 174, 420, 194}, res.Detail, a.fonts.body, uintptr(colMuted),
		dtLeft|dtSingleLine|dtEndEllipsis|dtNoPrefix)
	drawText(hdc, &winRect{20, 196, 420, 216}, a.infoLine(res, notice), a.fonts.small,
		uintptr(colMuted), dtLeft|dtSingleLine|dtEndEllipsis|dtNoPrefix)

	filledRect(hdc, 0, 226, clientW, 227, uintptr(colLine))

	// Footer: version + connection state + last successful check.
	filledRect(hdc, 0, 390, clientW, 391, uintptr(colLine))
	drawText(hdc, &winRect{20, 396, 240, 420}, "MKYBOOT Launcher v"+launcherVersion,
		a.fonts.small, uintptr(colMuted), dtLeft|dtSingleLine|dtNoPrefix)
	lastOK := "never"
	if !lastGood.IsZero() {
		lastOK = lastGood.Format("15:04:05")
	}
	drawText(hdc, &winRect{240, 396, 420, 420},
		res.State.String()+" - last OK "+lastOK, a.fonts.small, uintptr(colMuted),
		dtRight|dtSingleLine|dtEndEllipsis|dtNoPrefix)
}

// ---- owner-drawn control painting ----

const nullBrushStock = 5 // GetStockObject(NULL_BRUSH)

// drawButton paints a rounded modern button (hover/pressed/focus/disabled).
func (a *App) drawButton(dis *drawItemStruct, hot, primary bool) {
	hdc := dis.Hdc
	r := &dis.RcItem
	disabled := dis.ItemState&(odsDisabled|odsGreyed) != 0
	pressed := dis.ItemState&odsSelected != 0
	// Themed buttons do not always set ODS_FOCUS, so ask the system directly.
	focused := dis.ItemState&odsFocus != 0
	if !focused && !disabled {
		focus, _, _ := procGetFocus.Call()
		focused = focus == dis.HwndItem
	}

	// Erase the theme-painted button face first: the window background shows
	// through the rounded corners and around the disabled/hover surfaces.
	filledRect(hdc, r.Left, r.Top, r.Right, r.Bottom, uintptr(colBG))

	bg := uintptr(colBtn)
	textCol := uintptr(colText)
	if primary {
		bg = uintptr(colPrimary)
		textCol = uintptr(colWhite)
	}
	switch {
	case disabled:
		bg = uintptr(colBtnOff)
		textCol = uintptr(colMuted)
	case pressed:
		bg = uintptr(colBtnDown)
	case hot && primary:
		bg = uintptr(colPrimaryHot)
	case hot:
		bg = uintptr(colBtnHot)
	}

	pen, _, _ := procCreatePen.Call(0 /* PS_SOLID */, 1, bg)
	brush, _, _ := procCreateSolidBrush.Call(bg)
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	oldBrush, _, _ := procSelectObject.Call(hdc, brush)
	procRoundRect.Call(hdc, u32(r.Left), u32(r.Top), u32(r.Right), u32(r.Bottom), 12, 12)
	procSelectObject.Call(hdc, oldPen)
	procSelectObject.Call(hdc, oldBrush)
	procDeleteObject.Call(pen)
	procDeleteObject.Call(brush)

	if focused && !disabled {
		// Accent focus ring, inset by 1px.
		fpen, _, _ := procCreatePen.Call(0, 1, uintptr(colAccent))
		hollow, _, _ := procGetStockObject.Call(nullBrushStock)
		op, _, _ := procSelectObject.Call(hdc, fpen)
		ob, _, _ := procSelectObject.Call(hdc, hollow)
		procRoundRect.Call(hdc, u32(r.Left+2), u32(r.Top+2), u32(r.Right-2), u32(r.Bottom-2), 10, 10)
		procSelectObject.Call(hdc, op)
		procSelectObject.Call(hdc, ob)
		procDeleteObject.Call(fpen)
	}

	label := windowText(dis.HwndItem)
	procSetBkMode.Call(hdc, uintptr(transparentBk))
	if a.fonts.body != 0 {
		procSelectObject.Call(hdc, a.fonts.body)
	}
	procSetTextColor.Call(hdc, textCol)
	procDrawTextW.Call(hdc, strPtr(label), ^uintptr(0),
		uintptr(unsafe.Pointer(r)),
		uintptr(dtCenter|dtVCenter|dtSingleLine|dtNoPrefix))
}

// drawCheckbox paints the "Start with Windows" toggle row.
func (a *App) drawCheckbox(dis *drawItemStruct) {
	hdc := dis.Hdc
	r := &dis.RcItem
	disabled := dis.ItemState&(odsDisabled|odsGreyed) != 0
	checked := a.startupOn
	hot := a.hotHwnd == dis.HwndItem && !disabled

	// Erase the theme-painted button face so the row matches the window.
	filledRect(hdc, r.Left, r.Top, r.Right, r.Bottom, uintptr(colBG))

	cy := (r.Top + r.Bottom) / 2
	boxL, boxT := int32(r.Left)+2, cy-8
	inner := uintptr(colBtn)
	if hot {
		inner = uintptr(colBtnHot)
	}
	filledRect(hdc, boxL, boxT, boxL+17, boxT+17, uintptr(colMuted))
	filledRect(hdc, boxL+1, boxT+1, boxL+16, boxT+16, inner)

	if checked {
		pen, _, _ := procCreatePen.Call(0, 2, uintptr(colAccent))
		old, _, _ := procSelectObject.Call(hdc, pen)
		procMoveToEx.Call(hdc, u32(boxL+4), u32(cy+1), 0)
		procLineTo.Call(hdc, u32(boxL+7), u32(cy+4))
		procLineTo.Call(hdc, u32(boxL+13), u32(cy-3))
		procSelectObject.Call(hdc, old)
		procDeleteObject.Call(pen)
	}

	label := windowText(dis.HwndItem)
	textRect := winRect{boxL + 24, r.Top, r.Right, r.Bottom}
	col := uintptr(colText)
	if disabled {
		col = uintptr(colMuted)
	}
	procSetBkMode.Call(hdc, uintptr(transparentBk))
	if a.fonts.body != 0 {
		procSelectObject.Call(hdc, a.fonts.body)
	}
	procSetTextColor.Call(hdc, col)
	procDrawTextW.Call(hdc, strPtr(label), ^uintptr(0),
		uintptr(unsafe.Pointer(&textRect)),
		uintptr(dtLeft|dtVCenter|dtSingleLine|dtNoPrefix))
}

// ---- state refresh helpers (UI thread) ----

// refreshUI repaints the window, the tray tooltip and action availability.
func (a *App) refreshUI() {
	a.updateActionStates()
	a.trayUpdateTip()
	if a.hwnd != 0 {
		procInvalidateRect.Call(a.hwnd, 0, 1)
	}
}

// updateActionStates enables/disables buttons according to the current
// server state and whether a control operation is running.
func (a *App) updateActionStates() {
	res, _, busy, _ := a.stateSnapshot()
	online := res.State == StateOnline
	set := func(h uintptr, on bool) {
		if h != 0 {
			procEnableWindow.Call(h, b2u(on))
		}
	}
	available := !busy
	set(a.hBtn[0], available) // Open Dashboard
	set(a.hBtn[1], available) // Server Status
	canControl := controlAPISupported && !busy && online
	set(a.hBtn[2], canControl) // Restart Server
	set(a.hBtn[3], canControl) // Stop Server
}

// takeNotice pops a queued background notice (never returns nil).
func (a *App) takeNotice() *Notice {
	select {
	case n := <-a.noticeCh:
		return n
	default:
		return &Notice{Title: "MKYBOOT Launcher", Text: "No details available."}
	}
}

// ---- modal dialogs ----

type dialogKind int

const (
	dlgOK       dialogKind = iota // OK
	dlgYesNo                      // Yes / No
	dlgPassword                   // masked entry + Authorize / Cancel
)

// dialogCtx describes one modal dialog invocation.
type dialogCtx struct {
	parent   uintptr
	kind     dialogKind
	title    string
	text     string
	result   int
	password string
	hDlg     uintptr
	hEdit    uintptr
}

// activeDlg is only ever accessed from the UI thread; dialogs are strictly
// modal, so a single slot is sufficient.
var activeDlg *dialogCtx

// dialogWndProc renders and drives the modal dialog window.
func dialogWndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	ctx := activeDlg
	a := theApp
	switch msg {
	case wmCreate:
		if ctx == nil || a == nil {
			return 0
		}
		baseStyle := uintptr(wsChild | wsVisible | wsTabStop)
		mk := func(id int, label string, x, y, w, h int32) uintptr {
			b, _, _ := procCreateWindowExW.Call(
				0, strPtr("BUTTON"), strPtr(label),
				baseStyle|uintptr(bsOwnerDraw),
				u32(x), u32(y), u32(w), u32(h),
				hwnd, uintptr(id), hInstance(), 0)
			setControlFont(b, a.fonts.body)
			a.subclass(b)
			return b
		}
		switch ctx.kind {
		case dlgPassword:
			edit, _, _ := procCreateWindowExW.Call(
				0, strPtr("EDIT"), strPtr(""),
				baseStyle|uintptr(esAutoHScroll)|uintptr(esPassword),
				20, 112, 400, 32,
				hwnd, uintptr(idDlgEdit), hInstance(), 0)
			ctx.hEdit = edit
			setControlFont(edit, a.fonts.body)
			mk(idOK, "Authorize", 300, 156, 120, 36)
			mk(idCancel, "Cancel", 166, 156, 120, 36)
			procSetFocus.Call(edit)
		case dlgYesNo:
			yes := mk(idYes, "Yes", 300, 116, 120, 36)
			mk(idNo, "No", 166, 116, 120, 36)
			procSetFocus.Call(yes)
		default:
			ok := mk(idOK, "OK", 300, 116, 120, 36)
			procSetFocus.Call(ok)
		}
		return 0
	case wmPaint:
		if ctx == nil || a == nil {
			return 0
		}
		var ps paintStruct
		hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		filledRect(hdc, 0, 0, 440, 430, uintptr(colBG))
		if ctx.kind == dlgPassword {
			drawText(hdc, &winRect{20, 14, 420, 86}, ctx.text, a.fonts.body, uintptr(colText),
				dtLeft|dtWordBreak|dtNoPrefix)
			drawText(hdc, &winRect{20, 88, 420, 110}, "Password", a.fonts.small, uintptr(colMuted),
				dtLeft|dtSingleLine|dtNoPrefix)
		} else {
			drawText(hdc, &winRect{20, 16, 420, 108}, ctx.text, a.fonts.body, uintptr(colText),
				dtLeft|dtWordBreak|dtNoPrefix)
		}
		filledRect(hdc, 0, 0, 440, 1, uintptr(colLine))
		procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case wmDrawItem:
		if a == nil {
			return 0
		}
		dis := (*drawItemStruct)(unsafe.Pointer(lparam))
		if dis.CtlType == odtButton {
			// Owner-drawn buttons report the control id in CtlID.
			primary := dis.CtlID == idOK || dis.CtlID == idYes
			a.drawButton(dis, a.hotHwnd == dis.HwndItem, primary)
			return 1
		}
		return 0
	case wmCtlColorEdit:
		if a != nil {
			procSetBkMode.Call(wparam, 2)
			procSetTextColor.Call(wparam, uintptr(colEditText))
			return a.brushes.editBG
		}
		return 0
	case wmCommand:
		if ctx == nil {
			return 0
		}
		if hiWord(wparam) != bnClicked {
			return 0
		}
		switch int(loWord(wparam)) {
		case idOK:
			if ctx.kind == dlgPassword {
				if ctx.hEdit != 0 {
					ctx.password = windowText(ctx.hEdit)
					// Clear the visible buffer immediately.
					setWindowText(ctx.hEdit, "")
				}
				ctx.result = idOK
			} else if ctx.kind == dlgYesNo {
				// Enter maps to the default OK command: treat as Yes.
				ctx.result = idYes
			} else {
				ctx.result = idOK
			}
			procDestroyWindow.Call(hwnd)
			return 0
		case idCancel:
			ctx.result = idCancel
			procDestroyWindow.Call(hwnd)
			return 0
		case idYes:
			ctx.result = idYes
			procDestroyWindow.Call(hwnd)
			return 0
		case idNo:
			ctx.result = idNo
			procDestroyWindow.Call(hwnd)
			return 0
		}
		return 0
	case wmClose:
		if ctx != nil && ctx.result == 0 {
			ctx.result = idCancel
		}
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		return 0
	}
	return callSimple(procDefWindowProcW, hwnd, msg, wparam, lparam)
}

// runDialog shows a modal dialog on the UI thread and returns its result
// (idOK / idCancel / idYes / idNo). The main window is disabled while it is
// open; queued status messages keep flowing through the nested loop.
func (a *App) runDialog(ctx *dialogCtx) int {
	if a.hwnd == 0 {
		return idCancel
	}
	ctx.parent = a.hwnd
	activeDlg = ctx

	// Register the dialog class (ignore "already exists").
	className := utf16Ptr("MkybootDialogWnd")
	cursor, _, _ := procLoadCursorW.Call(0, uintptr(idcArrow))
	wc := wndClassEx{
		CbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		LpfnWndProc:   dialogWndProcCB,
		HInstance:     hInstance(),
		HCursor:       cursor,
		LpszClassName: className,
	}
	if ret, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); ret == 0 {
		if errno, ok := err.(syscall.Errno); !ok || errno != errClassExists {
			activeDlg = nil
			return idCancel
		}
	}

	style := uintptr(wsCaption | wsSysMenu)
	height := int32(168)
	if ctx.kind == dlgPassword {
		height = 216
	}
	rc := winRect{0, 0, 440, height}
	procAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&rc)), style, 0, 0)
	ww, wh := rc.Right-rc.Left, rc.Bottom-rc.Top

	// Center on the owner window.
	var pr winRect
	procGetWindowRect.Call(a.hwnd, uintptr(unsafe.Pointer(&pr)))
	x := pr.Left + (pr.Right-pr.Left-ww)/2
	y := pr.Top + (pr.Bottom-pr.Top-wh)/2

	hDlg, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(utf16Ptr(ctx.title))),
		style,
		u32(x), u32(y), uintptr(ww), uintptr(wh),
		a.hwnd, 0, hInstance(), 0,
	)
	if hDlg == 0 {
		activeDlg = nil
		return idCancel
	}
	ctx.hDlg = hDlg

	procEnableWindow.Call(a.hwnd, 0)
	defer func() {
		procEnableWindow.Call(a.hwnd, 1)
		procSetForegroundWindow.Call(a.hwnd)
		activeDlg = nil
	}()

	callSimple(procShowWindow, hDlg, uintptr(swShow))
	callSimple(procUpdateWindow, hDlg)

	var m winMsg
	pm := uintptr(unsafe.Pointer(&m))
	for {
		if callSimple(procIsWindow, hDlg) == 0 {
			break
		}
		r, _, _ := procGetMessageW.Call(pm, 0, 0, 0)
		if int32(r) == 0 { // WM_QUIT inside a dialog: propagate
			procPostQuitMessage.Call(m.WParam)
			break
		}
		if int32(r) < 0 {
			break
		}
		if callSimple(procIsDialogMessageW, hDlg, pm) != 0 {
			continue // TAB/Enter/Esc handled by the dialog manager
		}
		callSimple(procTranslateMessage, pm)
		callSimple(procDispatchMessageW, pm)
	}

	if ctx.result == 0 {
		ctx.result = idCancel
	}
	return ctx.result
}

// msgBoxError shows a system error box (used before the window exists).
func msgBoxError(title, text string) {
	procMessageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(utf16Ptr(text))),
		uintptr(unsafe.Pointer(utf16Ptr(title))),
		uintptr(mbOk|mbIconError|mbSetForeground),
	)
}
