package main

// app.go owns launcher lifecycle, background status polling and the action
// flows (open dashboard, server status, restart, stop).

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"
	"unsafe"
)

// controlAPISupported reports whether the MKYBOOT backend exposes the
// authenticated server control API (GET /?api=server&op=restart|stop).
//
// Repository inspection confirmed the endpoint exists in bin/mkyctl.lua:
// it requires a valid session cookie, enforces an Origin check and only ever
// invokes the validated mkyboot.inc.systemctl whitelist with the fixed "nginx"
// service. Restart/Stop are therefore enabled in the UI. Deployments running
// an older backend receive an explicit error response instead of a faked
// success.
const controlAPISupported = true

const (
	statusPollInterval   = 10 * time.Second
	controlVerifyTimeout = 30 * time.Second
	verifyPollInterval   = 2 * time.Second
)

// Notice carries a dialog message from a background goroutine to the UI
// thread. References stay alive via the buffered channel (a raw pointer in
// lParam could be garbage collected before the message is handled).
type Notice struct {
	Title string
	Text  string
}

// gdiFonts holds the shared font handles.
type gdiFonts struct {
	title uintptr // Segoe UI 20 bold
	big   uintptr // Segoe UI 17 bold
	label uintptr // Segoe UI 12 semibold
	body  uintptr // Segoe UI 12
	small uintptr // Segoe UI 11
}

// gdiBrushes holds the shared brush handles.
type gdiBrushes struct {
	bg      uintptr
	line    uintptr
	card    uintptr
	editBG  uintptr
	text    uintptr
	muted   uintptr
	accent  uintptr
	online  uintptr
	offline uintptr
	primary uintptr
	warn    uintptr
}

// App aggregates the complete launcher state.
type App struct {
	// configuration (set once in NewApp, then only on the UI thread)
	cfg     Config
	cfgErr  error
	exePath string
	server  *MKYBootServer

	// mu guards server and the shared status fields below.
	mu       sync.Mutex
	result   CheckResult
	lastGood time.Time // last successful online check
	busy     bool      // a control operation is in flight
	notice   string    // short footer notice (config/URL problems)

	// UI thread only after Run starts
	hwnd  uintptr
	hEdit uintptr
	hBtn  [4]uintptr // dashboard, status, restart, stop
	hChk  uintptr
	// startupOn mirrors the "Start with Windows" row. Ownerdraw buttons cannot
	// store a check state (Windows ignores BM_SETCHECK/BM_GETCHECK for
	// BS_OWNERDRAW), so the launcher keeps the state itself.
	startupOn bool
	addrEdit  bool // address edit has keyboard focus
	hotHwnd   uintptr
	btnOrig   map[uintptr]uintptr
	fonts     gdiFonts
	brushes   gdiBrushes
	iconMain  uintptr // cached 32px window icon
	trayIcon  uintptr
	trayOn    bool

	// background channels
	checkNow chan struct{}
	noticeCh chan *Notice
	pollStop chan struct{}
	// stopCtx is cancelled when the launcher shuts down. Every background
	// health check, retry loop and control verification is bound to it so no
	// goroutine keeps working - or posting UI messages to a destroyed window -
	// after the message loop has ended.
	stopCtx    context.Context
	stopCancel context.CancelFunc
}

// NewApp loads configuration and prepares the launcher.
func NewApp() (*App, error) {
	cfg, cfgErr := LoadConfig()
	stopCtx, stopCancel := context.WithCancel(context.Background())
	a := &App{
		cfg:        cfg,
		cfgErr:     cfgErr,
		checkNow:   make(chan struct{}, 1),
		noticeCh:   make(chan *Notice, 8),
		pollStop:   make(chan struct{}),
		stopCtx:    stopCtx,
		stopCancel: stopCancel,
		btnOrig:    map[uintptr]uintptr{},
		result:     CheckResult{State: StateConnecting, Detail: "Checking server..."},
	}
	if exe, err := os.Executable(); err == nil {
		a.exePath = exe
	}
	srv, err := NewMKYBootServer(cfg.ServerURL)
	if err != nil {
		// Config validation should have caught this; fall back to defaults.
		fallback := DefaultConfig()
		a.cfg = fallback
		a.cfgErr = errors.Join(cfgErr, err)
		srv, err = NewMKYBootServer(fallback.ServerURL)
		if err != nil {
			return nil, err
		}
	}
	a.mu.Lock()
	a.server = srv
	a.mu.Unlock()

	// Align the per-user start-with-Windows entry with the configuration
	// (self-heals after the executable was moved). Best effort only.
	if cfg.StartWithWindows {
		_ = setStartWithWindows(true, a.exePath)
	} else {
		_ = setStartWithWindows(false, a.exePath)
	}
	return a, nil
}

// currentServer returns the active client (safe for goroutines).
func (a *App) currentServer() *MKYBootServer {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.server
}

// currentURL returns the configured server base URL.
func (a *App) currentURL() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.server != nil {
		return a.server.BaseURL()
	}
	return a.cfg.ServerURL
}

// applyResult stores a check result if it still belongs to the active server.
func (a *App) applyResult(baseURL string, res CheckResult) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.server == nil || a.server.BaseURL() != baseURL {
		return // stale result from a replaced server
	}
	a.result = res
	if res.State == StateOnline {
		a.lastGood = res.CheckedAt
	}
}

// stateSnapshot returns a consistent copy for painting and menu building.
func (a *App) stateSnapshot() (res CheckResult, lastGood time.Time, busy bool, notice string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.result, a.lastGood, a.busy, a.notice
}

func (a *App) setBusy(v bool) {
	a.mu.Lock()
	a.busy = v
	a.mu.Unlock()
}

func (a *App) setNotice(s string) {
	a.mu.Lock()
	a.notice = s
	a.mu.Unlock()
}

// requestCheck triggers an immediate background status check.
func (a *App) requestCheck() {
	select {
	case a.checkNow <- struct{}{}:
	default:
	}
}

// postNotice queues a background-thread dialog message for the UI thread.
func (a *App) postNotice(title, text string, controlDone bool) {
	select {
	case a.noticeCh <- &Notice{Title: title, Text: text}:
	default:
	}
	msg := uintptr(wmAppNotice)
	if controlDone {
		msg = uintptr(wmAppControl)
	}
	if a.hwnd != 0 {
		postAppMessage(a.hwnd, msg, 0, 0)
	}
}

// pollLoop runs on its own goroutine: periodic plus on-demand status checks.
// Exactly one check is ever in flight, so repeated clicks or retries can never
// stack up concurrent probes.
func (a *App) pollLoop() {
	ticker := time.NewTicker(statusPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-a.pollStop:
			return
		case <-a.stopCtx.Done():
			return
		case <-ticker.C:
		case <-a.checkNow:
		}
		srv := a.currentServer()
		if srv == nil {
			continue
		}
		// Bounded retry: a server that is still coming up, or one whose worker
		// is momentarily blocked on the per-client tgtadm probes, gets another
		// chance before it is declared unreachable.
		res := srv.CheckStatusRetry(a.stopCtx)
		if a.stopCtx.Err() != nil {
			return // shutting down: do not touch UI state
		}
		a.applyResult(srv.BaseURL(), res)
		if a.hwnd != 0 {
			postAppMessage(a.hwnd, wmAppStatus, 0, 0)
		}
	}
}

// Run creates the window and tray, starts polling and enters the message loop.
func (a *App) Run() {
	theApp = a
	if err := a.createMainWindow(); err != nil {
		msgBoxError("MKYBOOT Launcher", "Failed to create the launcher window:\r\n"+err.Error())
		os.Exit(1)
	}
	a.trayAdd()
	go a.pollLoop()
	a.requestCheck()

	if a.cfg.StartMinimized {
		// Start hidden in the tray as configured.
		callSimple(procShowWindow, a.hwnd, uintptr(swHide))
	} else {
		callSimple(procShowWindow, a.hwnd, uintptr(swShow))
		callSimple(procUpdateWindow, a.hwnd)
	}

	// Message loop.
	var m winMsg
	pm := uintptr(unsafe.Pointer(&m))
	for {
		r, _, _ := procGetMessageW.Call(pm, 0, 0, 0)
		if int32(r) <= 0 { // WM_QUIT or error
			break
		}
		// The launcher window is not a dialog, but the dialog manager gives
		// TAB/arrow navigation between its controls (and Space activation).
		if callSimple(procIsDialogMessageW, a.hwnd, pm) != 0 {
			continue
		}
		callSimple(procTranslateMessage, pm)
		callSimple(procDispatchMessageW, pm)
	}

	close(a.pollStop)
	a.stopCancel() // cancel in-flight health checks, retries and verification
	a.trayRemove()
}

// restoreFromTray shows and focuses the main window.
func (a *App) restoreFromTray() {
	if a.hwnd == 0 {
		return
	}
	callSimple(procShowWindow, a.hwnd, uintptr(swRestore))
	callSimple(procSetForegroundWindow, a.hwnd)
}

// hideToTray keeps the process alive with only the tray icon visible.
func (a *App) hideToTray() {
	if a.hwnd != 0 {
		callSimple(procShowWindow, a.hwnd, uintptr(swHide))
	}
}

// exit terminates the launcher completely (tray Exit item).
func (a *App) exit() {
	if a.hwnd != 0 {
		// Destroys the window; wmDestroy removes the tray icon and quits.
		callSimple(procDestroyWindow, a.hwnd)
	}
}

// ---- action handlers (UI thread unless stated otherwise) ----

// openDashboard launches the configured dashboard URL in the user's default
// browser via ShellExecuteW. No command shell, no shell string parsing, no
// credentials are passed - authentication happens in the browser.
func (a *App) openDashboard() {
	srv := a.currentServer()
	if _, err := NormalizeServerURL(srv.BaseURL()); err != nil {
		a.showNotice("Invalid server address", "The configured server address is not valid.")
		return
	}
	ret, _, _ := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(utf16Ptr("open"))),
		uintptr(unsafe.Pointer(utf16Ptr(srv.DashboardURL()))),
		0, 0,
		1, // SW_SHOWNORMAL
	)
	if ret <= 32 {
		a.showNotice("Browser error",
			"Could not open the default browser (Windows code "+itoa(int(ret))+").")
	}
}

// canControlServer reports whether a restart/stop request can be sent to the
// backend. All three states below mean nginx is alive and therefore serving the
// control endpoint; only a refusal/timeout/garbage answer rules it out.
//
// Degraded counts: nginx answers fine, it is the diskless services (DHCP,
// TFTP, iSCSI) that are down, and restarting nginx is a legitimate response.
// Unauthorized counts too - the launcher signs in on demand.
func canControlServer(s ServerState) bool {
	switch s {
	case StateOnline, StateDegraded, StateUnauthorized:
		return true
	default:
		return false
	}
}

// setTransientState shows the requested transition while the control request
// and its verification are in flight.
func (a *App) setTransientState(op ControlOp) {
	st := StateStarting
	detail := "Restart requested - waiting for the service to answer..."
	if op == OpStop {
		st = StateStopping
		detail = "Stop requested - waiting for the service to go down..."
	}
	a.applyResult(a.currentURL(), CheckResult{
		State:     st,
		Detail:    detail,
		CheckedAt: time.Now(),
	})
	a.refreshUI()
}

// manualCheck forces an immediate status refresh from the UI. This is the
// launcher's retry action: it re-runs the bounded check sequence on demand.
func (a *App) manualCheck() {
	a.applyResult(a.currentURL(), CheckResult{
		State:     StateConnecting,
		Detail:    "Checking...",
		CheckedAt: time.Now(),
	})
	a.refreshUI()
	a.requestCheck()
}

// onAddressEdited validates the address field while typing; fully valid URLs
// are adopted and persisted immediately. Partial input is left alone.
func (a *App) onAddressEdited() {
	text := windowText(a.hEdit)
	norm, err := NormalizeServerURL(text)
	if err != nil {
		return
	}
	if norm == a.currentURL() {
		return
	}
	a.adoptServerURL(norm)
}

// onAddressFocusLost reverts invalid leftover input to the saved address.
func (a *App) onAddressFocusLost() {
	text := windowText(a.hEdit)
	current := a.currentURL()
	norm, err := NormalizeServerURL(text)
	if err != nil {
		setWindowText(a.hEdit, current)
		a.setNotice("Invalid server address - previous value kept")
		a.refreshUI()
		return
	}
	if norm != current {
		// Valid but change was not adopted (e.g. mid-edit race): adopt now.
		a.adoptServerURL(norm)
	}
}

// adoptServerURL persists a validated address and restarts checking against
// the new server. The previous in-memory session is dropped implicitly
// because a fresh client (fresh cookie jar) is created.
func (a *App) adoptServerURL(norm string) {
	newSrv, err := NewMKYBootServer(norm)
	if err != nil {
		a.setNotice("Invalid server address")
		a.refreshUI()
		return
	}
	a.mu.Lock()
	a.cfg.ServerURL = norm
	a.server = newSrv
	a.result = CheckResult{State: StateConnecting, Detail: "Connecting to new address..."}
	a.lastGood = time.Time{}
	a.mu.Unlock()

	if err := a.cfg.Save(); err != nil {
		a.setNotice("Config save failed: " + sanitizeDetail(err.Error()))
	} else {
		a.setNotice("")
	}
	a.requestCheck()
	a.refreshUI()
}

// toggleStartup persists the start-with-Windows preference and maintains the
// per-user HKCU Run entry (never requires Administrator).
func (a *App) toggleStartup(enabled bool) {
	a.mu.Lock()
	a.cfg.StartWithWindows = enabled
	a.mu.Unlock()

	var problems []error
	if err := a.cfg.Save(); err != nil {
		problems = append(problems, err)
	}
	var regErr error
	if enabled {
		regErr = setStartWithWindows(true, a.exePath)
	} else {
		regErr = setStartWithWindows(false, a.exePath)
	}
	if regErr != nil {
		problems = append(problems, regErr)
	}
	if len(problems) > 0 {
		a.setNotice("Start with Windows: " + sanitizeDetail(problems[0].Error()))
	} else {
		a.setNotice("")
	}
	a.refreshUI()
}

// showNotice displays a simple OK dialog (UI thread).
func (a *App) showNotice(title, text string) {
	a.runDialog(&dialogCtx{kind: dlgOK, title: title, text: text})
}

// promptPassword asks for the administrator password once. The value lives
// only in this stack frame and the dialog buffer - it is never persisted.
func (a *App) promptPassword() (string, bool) {
	ctx := &dialogCtx{
		kind:  dlgPassword,
		title: "Administrator sign-in required",
		text:  "Enter the MKYBOOT administrator password to authorize this operation. The password stays in memory only and is never saved by the launcher.",
	}
	if a.runDialog(ctx) != idOK || ctx.password == "" {
		return "", false
	}
	return ctx.password, true
}

// controlFlow drives confirm -> sign-in -> control request -> verification.
// UI thread until the goroutine hand-off.
func (a *App) controlFlow(op ControlOp) {
	if !controlAPISupported {
		a.showNotice("Server control unavailable",
			"Server control API not available.\r\nThis backend build does not expose a supported restart/stop endpoint.")
		return
	}
	res, _, busy, _ := a.stateSnapshot()
	if busy {
		return
	}
	if !canControlServer(res.State) {
		// Be specific about why, instead of a generic "not reachable": the
		// control API is served BY nginx, so it is genuinely unreachable while
		// nginx is down - the launcher cannot restart nginx via nginx.
		reason := res.Detail
		if reason == "" {
			reason = res.State.String()
		}
		if !res.State.Reachable() {
			a.showNotice("Server control unavailable",
				"Restart/Stop needs a reachable MKYBOOT server.\r\n"+
					"Current state: "+res.State.String()+" - "+reason+".\r\n\r\n"+
					"The control API is served by the same nginx process it controls, "+
					"so it cannot be used while nginx is down. Start nginx on the "+
					"MKYBOOT server (for example: sudo systemctl start nginx), then "+
					"press Server Status to re-check.")
			return
		}
		a.showNotice("Server control unavailable",
			"Restart/Stop needs the server to finish its current transition.\r\n"+
				"Current state: "+res.State.String()+" - "+reason+".")
		return
	}

	// Reflect the requested transition immediately so the UI never claims
	// Online while a restart/stop is in progress.
	a.setTransientState(op)

	verb := "Restart"
	confirm := "Restart the MKYBOOT web service on " + a.currentURL() + "?\r\n\r\n" +
		"Connected clients may be briefly interrupted while the service restarts."
	if op == OpStop {
		verb = "Stop"
		confirm = "Stop the MKYBOOT web service on " + a.currentURL() + "?\r\n\r\n" +
			"The dashboard and launcher status checks will be unavailable until the service is started again on the server."
	}
	if a.runDialog(&dialogCtx{kind: dlgYesNo, title: verb + " Server", text: confirm}) != idYes {
		return
	}

	srv := a.currentServer()
	password := ""
	if !srv.HasSession() {
		pwd, ok := a.promptPassword()
		if !ok {
			return
		}
		password = pwd
	}

	a.setBusy(true)
	a.refreshUI()

	go func(srv *MKYBootServer, op ControlOp, password string) {
		if password != "" {
			err := srv.Login(password)
			password = "" // drop the only reference as soon as possible
			if err != nil {
				srv.ResetSession()
				a.setBusy(false)
				a.postNotice("Sign-in failed", sanitizeDetail(err.Error()), true)
				return
			}
		}

		ack, err := srv.Control(op)
		if errors.Is(err, ErrNotAuthenticated) {
			// Session expired: force a fresh sign-in prompt on retry.
			srv.ResetSession()
			a.setBusy(false)
			a.postNotice("Sign-in required",
				"The server session expired. Run the operation again; the launcher will ask for the administrator password.", true)
			return
		}
		interrupted := err != nil && !errors.Is(err, ErrNotAuthenticated)

		verified := false
		if err == nil || interrupted {
			// The operation was accepted or the connection died mid-request
			// (typical while nginx restarts/stops); verify the real effect.
			verified = a.verifyControl(srv, op)
		}

		var title, text string
		switch {
		case err == nil && verified:
			if op == OpRestart {
				title = "Restart complete"
				text = "The restart request completed and the server responds online again."
			} else {
				title = "Stop complete"
				text = "The stop request completed and the server is now offline."
			}
		case err == nil && !verified:
			title = "Operation sent"
			text = "The server accepted the request, but the resulting state could not be confirmed within the verification window."
		case interrupted && verified:
			title = "Operation completed"
			text = "The connection closed during the request (typical while the web service restarts). Observed result: " +
				opEffect(op) + "."
		case interrupted && !verified:
			title = "Operation not confirmed"
			text = "The connection closed during the request and the server state could not be confirmed. Last observed state: " +
				lastObserved(srv) + "."
		default:
			title = stringCapital(op) + " failed"
			text = sanitizeDetail(err.Error())
			_ = ack
		}
		a.setBusy(false)
		a.requestCheck()
		a.postNotice(title, text, true)
	}(srv, op, password)
}

// verifyControl polls the server after a control operation until the expected
// effect is observed or the verification window elapses.
//
// It aborts as soon as the launcher starts shutting down. Previously it slept
// in a plain time.Sleep loop with no cancellation, so it kept polling - and
// could still post a dialog to an already destroyed window - for up to
// controlVerifyTimeout after the message loop had ended.
func (a *App) verifyControl(srv *MKYBootServer, op ControlOp) bool {
	deadline := time.Now().Add(controlVerifyTimeout)
	for {
		if a.stopped() {
			return false
		}
		if !time.Now().Before(deadline) {
			return false
		}
		select {
		case <-a.stopCtx.Done():
			return false
		case <-time.After(verifyPollInterval):
		}
		res := srv.CheckStatusContext(a.stopCtx)
		if a.stopped() {
			return false
		}
		a.applyResult(srv.BaseURL(), res)
		if a.hwnd != 0 {
			postAppMessage(a.hwnd, wmAppStatus, 0, 0)
		}
		if op == OpRestart {
			// Online *or* Degraded both mean nginx is serving again.
			if res.State == StateOnline || res.State == StateDegraded {
				return true
			}
		} else if res.State == StateOffline || res.State == StateTimeout {
			return true
		}
	}
}

// stopped reports whether shutdown has begun.
func (a *App) stopped() bool {
	select {
	case <-a.stopCtx.Done():
		return true
	default:
		return false
	}
}

func opEffect(op ControlOp) string {
	if op == OpRestart {
		return "the server is online"
	}
	return "the server is offline"
}

func stringCapital(op ControlOp) string {
	if op == OpRestart {
		return "Restart"
	}
	return "Stop"
}

func lastObserved(srv *MKYBootServer) string {
	return srv.CheckStatus().State.String()
}

// onCommand dispatches control IDs from buttons and the tray menu.
func (a *App) onCommand(id uint16) {
	switch int(id) {
	case idBtnDashboard:
		a.openDashboard()
	case idBtnStatus:
		a.manualCheck()
	case idBtnRestart:
		a.controlFlow(OpRestart)
	case idBtnStop:
		a.controlFlow(OpStop)
	case idTrayOpenLauncher:
		a.restoreFromTray()
	case idTrayExit:
		a.exit()
	}
}
