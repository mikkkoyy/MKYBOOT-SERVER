// MKYBOOT Launcher - native Windows companion application for the
// MKYBOOT-SERVER Linux diskless boot server.
//
// Build with:
//
//	go build -trimpath -ldflags "-s -w -H windowsgui" -o "dist/MKYBOOT Launcher.exe"
//
// The -H windowsgui flag suppresses the console window.
package main

import "os"

// launcherVersion is shown in the launcher footer.
const launcherVersion = "1.0.0"

func main() {
	app, err := NewApp()
	if err != nil {
		// The launcher window does not exist yet; use a system message box.
		msgBoxError("MKYBOOT Launcher", "Failed to initialize launcher:\r\n"+err.Error())
		os.Exit(1)
	}
	app.Run()
}
