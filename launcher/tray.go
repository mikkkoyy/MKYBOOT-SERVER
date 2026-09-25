package main

// tray.go implements the Windows system-tray icon and its context menu.

import (
	"unicode/utf16"
	"unsafe"
)

const (
	// wmTrayIcon is the callback message sent for tray icon events.
	wmTrayIcon = 0x8004 // WM_APP+4
	trayUID    = 1

	// Tray-only menu command IDs (button IDs are shared with the window).
	idTrayOpenLauncher = 1007
	idTrayExit         = 1008

	wmLButtonUp     = 0x0202
	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205
)

// trayAdd installs the notification-area icon.
func (a *App) trayAdd() {
	if a.hwnd == 0 || a.trayOn {
		return
	}
	if a.trayIcon == 0 {
		a.trayIcon = iconHandle(iconICO, 16)
	}
	nid := a.trayNID(nifMessage|nifIcon|nifTip, "MKYBOOT Launcher")
	ret, _, _ := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))
	if ret != 0 {
		a.trayOn = true
	}
	a.trayUpdateTip()
}

// trayRemove uninstalls the notification-area icon.
func (a *App) trayRemove() {
	if !a.trayOn {
		return
	}
	nid := a.trayNID(0, "")
	_, _, _ = procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
	a.trayOn = false
}

// trayUpdateTip reflects the current server state in the tray tooltip.
func (a *App) trayUpdateTip() {
	if !a.trayOn {
		return
	}
	res, _, _, _ := a.stateSnapshot()
	nid := a.trayNID(nifTip, "MKYBOOT Launcher - "+res.State.String())
	_, _, _ = procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
}

// trayNID builds a NOTIFYICONDATAW for this launcher.
func (a *App) trayNID(flags uint32, tip string) notifyIconDataW {
	nid := notifyIconDataW{
		CbSize:           uint32(unsafe.Sizeof(notifyIconDataW{})),
		HWnd:             a.hwnd,
		UID:              trayUID,
		UFlags:           flags,
		UCallbackMessage: wmTrayIcon,
		HIcon:            a.trayIcon,
	}
	if tip != "" {
		enc := utf16.Encode([]rune(tip))
		if len(enc) > len(nid.SzTip)-1 {
			enc = enc[:len(nid.SzTip)-1]
		}
		copy(nid.SzTip[:], enc)
	}
	return nid
}

// trayCallback reacts to tray icon mouse events (any thread, but posted to
// the main window so it is handled on the UI thread).
func (a *App) trayCallback(lParam uintptr) {
	switch uint16(lParam & 0xFFFF) {
	case wmLButtonUp, wmLButtonDblClk:
		a.restoreFromTray()
	case wmRButtonUp:
		a.trayShowMenu()
	}
}

// trayShowMenu pops the tray menu. Item order follows the specification:
// Open Dashboard, Server Status, Restart Server, Stop Server, Open Launcher,
// Exit. Restart/Stop are grayed out unless the control API is available, the
// server is online and no operation is already running.
func (a *App) trayShowMenu() {
	res, _, busy, _ := a.stateSnapshot()
	online := res.State == StateOnline
	canControl := controlAPISupported && !busy && online

	hMenu, _, _ := procCreatePopupMenu.Call()
	add := func(text string, id int, enabled bool) {
		flags := uintptr(mfString)
		if !enabled {
			flags |= uintptr(mfGrayed | mfDisabled)
		}
		procAppendMenuW.Call(hMenu, flags, uintptr(id), uintptr(unsafe.Pointer(utf16Ptr(text))))
	}
	add("Open Dashboard", idBtnDashboard, true)
	add("Server Status", idBtnStatus, !busy)
	add("Restart Server", idBtnRestart, canControl)
	add("Stop Server", idBtnStop, canControl)
	procAppendMenuW.Call(hMenu, uintptr(mfSeparator), 0, 0)
	add("Open Launcher", idTrayOpenLauncher, true)
	procAppendMenuW.Call(hMenu, uintptr(mfSeparator), 0, 0)
	add("Exit", idTrayExit, true)

	var pt winPoint
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	// SetForegroundWindow is required for the menu to close on outside click.
	procSetForegroundWindow.Call(a.hwnd)
	procTrackPopupMenu.Call(
		hMenu,
		uintptr(tpmRightButton|tpmBottomAlign|tpmLeftAlign),
		uintptr(pt.X), uintptr(pt.Y),
		0, a.hwnd, 0,
	)
	// Needed to dismiss the menu properly (MSKB Q135788).
	procPostMessageW.Call(a.hwnd, 0 /* WM_NULL */, 0, 0)
	procDestroyMenu.Call(hMenu)
}
