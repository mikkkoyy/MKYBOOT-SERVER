// MKYBOOT Launcher - native Windows companion application for the
// MKYBOOT-SERVER Linux diskless boot server.
//
// Build with:
//
//	go build -trimpath -ldflags "-s -w -H windowsgui" -o "dist/MKYBOOT Launcher.exe"
//
// The -H windowsgui flag suppresses the console window.
package main

import (
	"errors"
	"os"
)

// launcherVersion is shown in the launcher footer.
const launcherVersion = "1.0.0"

func main() {
	// Enforce one interactive launcher per Windows session before anything
	// else: no window, no tray icon and no background goroutine are created by
	// a duplicate instance.
	lock, err := AcquireInstance()
	if err != nil {
		if errors.Is(err, ErrAlreadyRunning) {
			// Ask the running instance to surface itself, then exit quietly.
			// No error dialog: launching twice is a normal user action, not a
			// failure worth interrupting for.
			NotifyExistingInstance()
			os.Exit(0)
		}
		// Anything else must not be ignored, otherwise duplicate instances
		// could slip through.
		msgBoxError("MKYBOOT Launcher", "Failed to acquire single-instance lock:\r\n"+err.Error())
		os.Exit(1)
	}
	// Releases automatically on exit, including a crash: the kernel destroys
	// the mutex object when this process terminates.
	defer lock.Release()

	app, err := NewApp()
	if err != nil {
		// The launcher window does not exist yet; use a system message box.
		msgBoxError("MKYBOOT Launcher", "Failed to initialize launcher:\r\n"+err.Error())
		os.Exit(1)
	}
	app.Run()
}
