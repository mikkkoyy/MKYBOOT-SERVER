// Command mkyboot-server is the Windows-native MKYBOOT diskless server
// foundation (Phase 2).
//
// It provides:
//   - a health endpoint,
//   - an authenticated administrative API,
//   - client registration and heartbeat with online/offline status,
//   - a server-side session timer and authoritative lock/unlock state,
//   - structured logging, graceful shutdown, and Windows service lifecycle.
//
// It does NOT start DHCP, TFTP, iSCSI or any PXE/network boot service. Those
// are later milestones.
//
// Usage:
//
//	mkyboot-server                 run in console (foreground)
//	mkyboot-server -install        install the Windows service (admin)
//	mkyboot-server -uninstall      remove the Windows service (admin)
//	mkyboot-server -config PATH    use an alternate config file
//
// When started by the Service Control Manager it runs as a service
// automatically; otherwise it runs in the foreground for testing.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("mkyboot-server", flag.ContinueOnError)
	var (
		install    = fs.Bool("install", false, "install the Windows service and exit")
		uninstall  = fs.Bool("uninstall", false, "remove the Windows service and exit")
		configPath = fs.String("config", DefaultConfigPath(), "path to config.json")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	switch {
	case *install:
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: cannot locate executable: %v\n", err)
			return 1
		}
		if err := installService(exe); err != nil {
			fmt.Fprintf(os.Stderr, "error: install failed: %v\n", err)
			return 1
		}
		fmt.Printf("Service %q installed. Start it with: sc start %s\n", serviceName, serviceName)
		return 0
	case *uninstall:
		if err := uninstallService(); err != nil {
			fmt.Fprintf(os.Stderr, "error: uninstall failed: %v\n", err)
			return 1
		}
		fmt.Printf("Service %q removed.\n", serviceName)
		return 0
	}

	// Ensure the data root exists with restrictive permissions before logging.
	root := ProgramDataRoot()
	if err := os.MkdirAll(root, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot create data directory %s: %v\n", root, err)
		return 1
	}

	cfg, cfgErr := LoadConfig(*configPath)
	// Persist a default config on first run so operators have a file to edit.
	if _, statErr := os.Stat(*configPath); errors.Is(statErr, os.ErrNotExist) {
		_ = cfg.Save(*configPath)
	}

	logger, err := NewFileLogger(DefaultLogPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot open log file: %v\n", err)
		return 1
	}
	defer logger.Close()

	if cfgErr != nil {
		logger.Error("config problem, using safe defaults", map[string]any{"error": cfgErr.Error()})
		fmt.Fprintf(os.Stderr, "warning: %v (using safe defaults: %s)\n", cfgErr, cfg.Addr())
	}

	srv, adminToken, err := NewServer(cfg, logger)
	if err != nil {
		logger.Error("server init failed", map[string]any{"error": err.Error()})
		fmt.Fprintf(os.Stderr, "error: server initialization failed: %v\n", err)
		return 1
	}
	if adminToken != "" {
		// Display the generated admin token exactly once, to stderr, and never
		// log it. The operator must save it now.
		fmt.Fprintf(os.Stderr, "\n=== MKYBOOT administrator token (shown once, save it now) ===\n%s\n================================================================\n\n", adminToken)
		logger.Info("administrator token generated", map[string]any{"provisioned": "generated"})
	} else {
		logger.Info("administrator token present", nil)
	}

	logger.Info("starting mkyboot-server", map[string]any{"addr": srv.Addr(), "version": serverVersion})

	runFn := func(ctx context.Context) error {
		return srv.Serve(ctx)
	}

	// If launched by the SCM, run as a service; otherwise run in the console.
	if isSvc, _ := isWindowsService(); isSvc {
		return exitCodeFromService(runAsService(runFn))
	}

	// Console mode: honor Ctrl+C / service stop signals for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Printf("mkyboot-server listening on http://%s (health: /healthz). Press Ctrl+C to stop.\n", srv.Addr())
	if err := runFn(ctx); err != nil {
		logger.Error("server exited with error", map[string]any{"error": err.Error()})
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	logger.Info("server stopped cleanly", nil)
	return 0
}

// exitCodeFromService maps a service run result to a process exit code.
func exitCodeFromService(err error) int {
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}
