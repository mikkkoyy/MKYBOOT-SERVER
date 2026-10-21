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
	"net"
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
		dryRun     = fs.Bool("dry-run", false, "validate PXE (TFTP/DHCP) configuration and exit without binding")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	// Dry-run: validate the configured PXE services without opening sockets.
	// This is the safe path for checking lab configuration before activation.
	if *dryRun {
		cfg, err := LoadConfig(*configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		return runDryRun(cfg)
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

	logger, err := NewFileLogger(cfg.LogFilePath())
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

	// Start enabled PXE services (TFTP/DHCP). A bind failure aborts startup so
	// a port conflict is surfaced loudly rather than silently ignored. This is
	// intentionally gated on config; with defaults both are disabled.
	pxeStops, pxeErr := startPXE(cfg, logger)
	if pxeErr != nil {
		logger.Error("pxe service start failed", map[string]any{"error": pxeErr.Error()})
		fmt.Fprintf(os.Stderr, "error: PXE service failed to start: %v\n", pxeErr)
		return 1
	}
	defer func() {
		for i := len(pxeStops) - 1; i >= 0; i-- {
			pxeStops[i]()
		}
	}()

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

// runDryRun validates the configured PXE services without binding any socket,
// printing a summary. It returns 0 when everything is valid (or disabled).
func runDryRun(cfg Config) int {
	fmt.Println("MKYBOOT Phase 3 PXE dry-run (no sockets will be opened)")
	ok := true
	if cfg.PXE.TFTP.Enabled {
		if err := cfg.PXE.TFTP.Validate(); err != nil {
			fmt.Printf("  TFTP:  INVALID - %v\n", err)
			ok = false
		} else {
			srv, err := NewTFTPServer(cfg.PXE.TFTP.Root, discardLogger())
			if err != nil {
				fmt.Printf("  TFTP:  INVALID - %v\n", err)
				ok = false
			} else {
				fmt.Printf("  TFTP:  OK bind=%s root=%s\n", net.JoinHostPort(cfg.PXE.TFTP.BindAddress, fmt.Sprint(cfg.PXE.TFTP.BindPort)), srv.root)
			}
		}
	} else {
		fmt.Println("  TFTP:  disabled")
	}
	if cfg.PXE.DHCP.Enabled {
		ds, err := NewDHCPServer(cfg.PXE.DHCP, discardLogger())
		if err != nil {
			fmt.Printf("  DHCP:  INVALID - %v\n", err)
			ok = false
		} else {
			fmt.Println("  DHCP:  " + ds.dryRun())
		}
	} else {
		fmt.Println("  DHCP:  disabled")
	}
	if !ok {
		fmt.Println("dry-run FAILED: fix the configuration above before enabling PXE services")
		return 1
	}
	fmt.Println("dry-run OK")
	return 0
}

// startPXE starts the enabled TFTP and DHCP services alongside the HTTP
// server. Returns stop functions; on any bind failure it stops already-started
// services and returns an error so the caller aborts.
func startPXE(cfg Config, log *Logger) (stops []func(), err error) {
	stopAll := func() {
		for i := len(stops) - 1; i >= 0; i-- {
			stops[i]()
		}
	}
	if cfg.PXE.TFTP.Enabled {
		if err := cfg.PXE.TFTP.Validate(); err != nil {
			return nil, fmt.Errorf("tftp config: %w", err)
		}
		addr := net.JoinHostPort(cfg.PXE.TFTP.BindAddress, fmt.Sprint(cfg.PXE.TFTP.BindPort))
		stop, err := RunTFTP(addr, cfg.PXE.TFTP.Root, log, nil)
		if err != nil {
			stopAll()
			return nil, err
		}
		stops = append(stops, stop)
	}
	if cfg.PXE.DHCP.Enabled {
		ds, err := NewDHCPServer(cfg.PXE.DHCP, log)
		if err != nil {
			stopAll()
			return nil, fmt.Errorf("dhcp config: %w", err)
		}
		if err := ds.Listen(); err != nil {
			stopAll()
			return nil, err
		}
		go ds.Serve()
		log.Info("dhcp listening", map[string]any{"addr": ds.Addr(), "subnet": cfg.PXE.DHCP.Subnet})
		stops = append(stops, ds.Close)
	}
	return stops, nil
}
