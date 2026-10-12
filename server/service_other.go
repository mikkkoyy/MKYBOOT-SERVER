//go:build !windows

package main

import (
	"context"
	"errors"
)

// Non-Windows builds cannot use the SCM. These stubs let the code compile and
// the core logic (config, state, HTTP API) be tested on any OS. Service
// install is a Windows-only capability and returns a clear error elsewhere.
func runAsService(run func(ctx context.Context) error) error {
	return errors.New("windows service mode is only available on Windows")
}

func isWindowsService() (bool, error) { return false, nil }

func installService(exePath string) error {
	return errors.New("service installation is only supported on Windows")
}

func uninstallService() error {
	return errors.New("service removal is only supported on Windows")
}
