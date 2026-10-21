package main

import (
	"fmt"
	"strings"
)

// BootScriptParams describes the generated iPXE script's target.
type BootScriptParams struct {
	// APIBase is the Windows server base URL reachable from the lab network,
	// e.g. "http://198.51.100.2:8888".
	APIBase string
	// ClientMAC is passed to the server so it can resolve the client session.
	ClientMAC string
}

// Validate checks the parameters before generating a script.
func (p BootScriptParams) Validate() error {
	if !strings.HasPrefix(p.APIBase, "http://") && !strings.HasPrefix(p.APIBase, "https://") {
		return fmt.Errorf("api_base must start with http:// or https://")
	}
	if strings.ContainsAny(p.APIBase, " \t\r\n") {
		return fmt.Errorf("api_base must not contain whitespace")
	}
	return nil
}

// RenderIPXEScript produces an iPXE script that discovers the session state
// from the Windows server API and then either boots or drops to the iPXE
// prompt. The script is intentionally explicit (no auto-continuation) so a
// failure leaves the operator at a prompt rather than looping.
func (p BootScriptParams) RenderIPXEScript() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	base := strings.TrimRight(p.APIBase, "/")
	var b strings.Builder
	b.WriteString("#!ipxe\n")
	b.WriteString("# MKYBOOT Windows server boot script (generated)\n")
	b.WriteString("echo MKYBOOT: contacting server " + base + "\n")
	b.WriteString("chain --replace " + base + "/api/bootstate?mac=${net0/mac} || goto failed\n")
	b.WriteString("echo MKYBOOT: boot state retrieved\n")
	b.WriteString("sleep 3\n")
	b.WriteString("echo MKYBOOT: no diskless image chained yet; dropping to iPXE prompt\n")
	b.WriteString("prompt --key 0x02 --timeout 5000 Press Ctrl-B for iPXE shell...\n")
	b.WriteString("shell\n")
	b.WriteString("exit\n")
	b.WriteString(":failed\n")
	b.WriteString("echo MKYBOOT: server unreachable at " + base + "\n")
	b.WriteString("echo MKYBOOT: check the isolated lab network and server status\n")
	b.WriteString("prompt Press any key to continue...\n")
	b.WriteString("shell\n")
	return b.String(), nil
}
