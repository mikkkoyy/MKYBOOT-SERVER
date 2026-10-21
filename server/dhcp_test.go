package main

import (
	"encoding/binary"
	"net"
	"strings"
	"testing"
)

func testDHCPConfig() DHCPConfig {
	return DHCPConfig{
		Enabled:      true,
		BindAddress:  "198.51.100.2",
		BindPort:     67,
		Subnet:       "198.51.100.0/24",
		RangeStart:   "198.51.100.50",
		RangeEnd:     "198.51.100.70",
		Gateway:      "198.51.100.1",
		DNSServer:    "198.51.100.1",
		NextServer:   "198.51.100.2",
		BootFileUEFI: "ipxe.efi",
		BootFileBIOS: "ipxe.kpxe",
		LeaseSeconds: 3600,
	}
}

// TestBootFileForArch covers the architecture->boot-file selection matrix,
// including the deliberate refusal to serve a 64-bit loader to 32-bit UEFI.
func TestBootFileForArch(t *testing.T) {
	cfg := testDHCPConfig()
	cases := []struct {
		arch int
		want string
	}{
		{archUEFIx64, "ipxe.efi"},
		{archUEFIBCx64, "ipxe.efi"},
		{archUEFIx86, ""}, // 32-bit UEFI: refuse, not a bootable 64-bit file
		{archBIOSx86, "ipxe.kpxe"},
		{-1, "ipxe.efi"}, // unknown/absent: prefer UEFI default
	}
	for _, c := range cases {
		if got := bootFileForArch(cfg, c.arch); got != c.want {
			t.Errorf("bootFileForArch(%d) = %q, want %q", c.arch, got, c.want)
		}
	}
}

// TestDHCPPacketRoundTrip builds a reply and parses it back, verifying the
// fixed-header layout, message type, and boot filename.
func TestDHCPPacketRoundTrip(t *testing.T) {
	cfg := testDHCPConfig()
	_, subnet, err := net.ParseCIDR(cfg.Subnet)
	if err != nil {
		t.Fatal(err)
	}
	chaddr := []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}
	req := reqInfo{
		xid:    0xdeadbeef,
		flags:  0x8000,
		giaddr: net.IPv4zero,
		chaddr: append(chaddr, make([]byte, 10)...),
	}
	offered := net.ParseIP("198.51.100.50")
	pkt := buildReply(dhcpOffer, req, offered, cfg, subnet, "ipxe.efi")

	if len(pkt) < dhcpHeaderLen {
		t.Fatalf("reply too short: %d", len(pkt))
	}
	if pkt[0] != 2 {
		t.Errorf("op = %d, want BOOTREPLY(2)", pkt[0])
	}
	if xid := binary.BigEndian.Uint32(pkt[4:8]); xid != req.xid {
		t.Errorf("xid = %#x, want %#x", xid, req.xid)
	}
	yiaddr := net.IP(pkt[16:20])
	if !yiaddr.Equal(offered) {
		t.Errorf("yiaddr = %v, want %v", yiaddr, offered)
	}
	// Cookie must be present.
	if pkt[dhcpCookieOffset] != 0x63 || pkt[dhcpCookieOffset+1] != 0x82 ||
		pkt[dhcpCookieOffset+2] != 0x53 || pkt[dhcpCookieOffset+3] != 0x63 {
		t.Error("missing DHCP magic cookie")
	}
	// Parse it back.
	// Simulate a request wrapper to reuse parse helper on options.
	msgType := dhcpMessageType(pkt[dhcpHeaderLen:])
	if msgType != dhcpOffer {
		t.Errorf("message type = %d, want OFFER(%d)", msgType, dhcpOffer)
	}
	bf, ok := findOption(pkt[dhcpHeaderLen:], optBootFileName)
	if !ok || string(bf) != "ipxe.efi" {
		t.Errorf("bootfile option = %q ok=%v, want ipxe.efi", bf, ok)
	}
}

// TestParseDHCPRequestRejects verifies malformed packets are rejected before
// any processing.
func TestParseDHCPRequestRejects(t *testing.T) {
	if _, err := parseDHCPRequest(nil); err == nil {
		t.Error("nil packet accepted")
	}
	short := make([]byte, dhcpHeaderLen-1)
	if _, err := parseDHCPRequest(short); err == nil {
		t.Error("short packet accepted")
	}
	// Wrong op.
	p := make([]byte, dhcpHeaderLen)
	p[0] = 2 // BOOTREPLY, not BOOTREQUEST
	if _, err := parseDHCPRequest(p); err == nil {
		t.Error("BOOTREPLY accepted as request")
	}
	// Bad cookie.
	p[0] = 1
	if _, err := parseDHCPRequest(p); err == nil {
		t.Error("bad magic cookie accepted")
	}
}

// TestDHCPWildcardBindRefused ensures the server refuses to start on a wildcard
// address (production-network safety).
func TestDHCPWildcardBindRefused(t *testing.T) {
	for _, bind := range []string{"0.0.0.0", "", "::", "[::]", "255.255.255.255"} {
		cfg := testDHCPConfig()
		cfg.BindAddress = bind
		if _, err := NewDHCPServer(cfg, discardLogger()); err == nil {
			t.Errorf("bind %q accepted; expected refusal", bind)
		}
	}
}

// TestDHCPConfigValidation ensures invalid ranges/subnets are rejected.
func TestDHCPConfigValidation(t *testing.T) {
	bad := func(fn func(*DHCPConfig)) DHCPConfig {
		c := testDHCPConfig()
		fn(&c)
		return c
	}
	cases := map[string]DHCPConfig{
		"range start after end": bad(func(c *DHCPConfig) { c.RangeStart = "198.51.100.90" }),
		"range outside subnet":  bad(func(c *DHCPConfig) { c.RangeStart = "10.0.0.5" }),
		"bad subnet":            bad(func(c *DHCPConfig) { c.Subnet = "not-a-cidr" }),
		"next server mismatch":  bad(func(c *DHCPConfig) { c.NextServer = "192.0.2.1" }),
	}
	for name, cfg := range cases {
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
	// Valid config passes.
	if err := testDHCPConfig().Validate(); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
}

// TestLeasePoolExhaustion allocates the whole range and expects an error on the
// next request.
func TestLeasePoolExhaustion(t *testing.T) {
	cfg := testDHCPConfig()
	cfg.RangeStart = "198.51.100.1"
	cfg.RangeEnd = "198.51.100.3" // 3 addresses
	pool, err := newLeasePool(cfg)
	if err != nil {
		t.Fatal(err)
	}
	macs := [][]byte{
		{0, 0, 0, 0, 0, 1},
		{0, 0, 0, 0, 0, 2},
		{0, 0, 0, 0, 0, 3},
	}
	for i, m := range macs {
		if _, err := pool.acquire(m); err != nil {
			t.Fatalf("acquire %d failed: %v", i, err)
		}
	}
	// Fourth client exceeds capacity.
	if _, err := pool.acquire([]byte{0, 0, 0, 0, 0, 4}); err == nil {
		t.Error("expected pool exhaustion, got nil")
	}
	// Re-acquiring an existing MAC reuses its lease without extra capacity.
	if _, err := pool.acquire(macs[0]); err != nil {
		t.Errorf("existing MAC should reuse lease: %v", err)
	}
}

// TestDHCPDryRun ensures dry-run never opens a socket and reports a summary.
func TestDHCPDryRun(t *testing.T) {
	ds, err := NewDHCPServer(testDHCPConfig(), discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	summary := ds.dryRun()
	if !strings.Contains(summary, "198.51.100.2") || !strings.Contains(summary, "ipxe.efi") {
		t.Errorf("dry-run summary missing details: %s", summary)
	}
	if ds.pc != nil {
		t.Error("dry-run must not bind a socket")
	}
}
