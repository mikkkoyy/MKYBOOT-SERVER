package main

import (
	"net"
	"strconv"
	"testing"
)

func TestTFTPConfigValidation(t *testing.T) {
	base := TFTPConfig{Enabled: true, BindAddress: "198.51.100.2", BindPort: 69, Root: t.TempDir()}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid tftp config rejected: %v", err)
	}
	cases := map[string]TFTPConfig{
		"wildcard bind": {Enabled: true, BindAddress: "0.0.0.0", BindPort: 69, Root: "/tmp"},
		"empty bind":    {Enabled: true, BindAddress: "", BindPort: 69, Root: "/tmp"},
		"bad port":      {Enabled: true, BindAddress: "198.51.100.2", BindPort: 70000, Root: "/tmp"},
		"missing root":  {Enabled: true, BindAddress: "198.51.100.2", BindPort: 69, Root: ""},
	}
	for name, cfg := range cases {
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

// TestTFTPPortConflict verifies a bind failure (port already in use) is
// surfaced as an error rather than silently ignored.
func TestTFTPPortConflict(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	_, port, _ := net.SplitHostPort(pc.LocalAddr().String())

	cfg := TFTPConfig{Enabled: true, BindAddress: "127.0.0.1", Root: t.TempDir()}
	// Resolve the port to an int.
	p, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	cfg.BindPort = p

	// Validate first (passes), then bind must fail with a conflict.
	if err := cfg.Validate(); err != nil {
		t.Fatalf("config should validate: %v", err)
	}
	_, err = RunTFTP(net.JoinHostPort(cfg.BindAddress, port), cfg.Root, discardLogger(), nil)
	if err == nil {
		t.Fatal("expected bind conflict error, got nil")
	}
}

// TestDHCPPortConflict verifies DHCP Listen surfaces a port conflict.
func TestDHCPPortConflict(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	_, port, _ := net.SplitHostPort(pc.LocalAddr().String())
	p, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	cfg := testDHCPConfig()
	cfg.BindAddress = "127.0.0.1"
	cfg.BindPort = p
	// Next server / subnet must still contain bind? Validate doesn't require
	// bind inside subnet, only next_server. Keep valid config otherwise.
	ds, err := NewDHCPServer(cfg, discardLogger())
	if err != nil {
		t.Fatalf("new dhcp: %v", err)
	}
	if err := ds.Listen(); err == nil {
		ds.Close()
		t.Fatal("expected bind conflict, got nil")
	}
}
