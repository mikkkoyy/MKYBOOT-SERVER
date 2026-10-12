//go:build windows

package main

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// serviceDisplayName is the human-friendly name shown in services.msc.
const serviceDisplayName = "MKYBOOT Diskless Server"

// serviceDescription explains the service purpose.
const serviceDescription = "MKYBOOT Windows-native diskless boot server (Phase 2 foundation: health, admin API, client registry, session timer, lock state)."

// handler implements svc.Handler so the server can run under the SCM with
// proper start/stop lifecycle and status reporting.
type handler struct {
	run func(ctx context.Context) error
}

// Execute is the SCM entry point. It reports StartPending, runs the server,
// and reacts to Stop/Shutdown by cancelling the context.
func (h *handler) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- h.run(ctx) }()

	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				cancel()
				err := <-done
				changes <- svc.Status{State: svc.Stopped}
				return false, uint32(exitCode(err))
			default:
			}
		case err := <-done:
			// Server exited on its own (e.g. fatal listen error).
			changes <- svc.Status{State: svc.Stopped}
			return true, uint32(exitCode(err))
		}
	}
}

func exitCode(err error) int {
	if err != nil {
		return 1
	}
	return 0
}

// runAsService runs the server under the SCM.
func runAsService(run func(ctx context.Context) error) error {
	return svc.Run(serviceName, &handler{run: run})
}

// isWindowsService reports whether the current process was started by the SCM.
func isWindowsService() (bool, error) {
	return svc.IsWindowsService()
}

// installService registers the service with the local SCM. It requires
// administrator privileges; a missing-privilege failure is returned so the
// caller can print an actionable message.
func installService(exePath string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager (run as Administrator): %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err == nil {
		s.Close()
		return fmt.Errorf("service %q already installed", serviceName)
	}
	cfg := mgr.Config{
		DisplayName:      serviceDisplayName,
		Description:      serviceDescription,
		StartType:        mgr.StartManual,
		DelayedAutoStart: true,
	}
	s, err = m.CreateService(serviceName, exePath, cfg)
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer s.Close()

	// Configure recovery: restart the service after a failure, so a crash
	// does not leave clients unmanaged. Bounded by SCM reset period.
	_ = s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 60 * time.Second},
	}, 24*60*60) // reset failure count after 1 day
	return nil
}

// uninstallService removes the service. It stops it first if running.
func uninstallService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager (run as Administrator): %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("open service: %w", err)
	}
	defer s.Close()
	_, _ = s.Control(svc.Stop)
	if err := s.Delete(); err != nil {
		return fmt.Errorf("delete service: %w", err)
	}
	return nil
}
