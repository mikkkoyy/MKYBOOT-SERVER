package main

import (
	"fmt"
	"net"
	"strings"
)

const (
	defaultTFTPPort = 69
	defaultDHCPPort = 67
)

// unsafeBindAddresses are addresses that must never be used to bind a network
// service: the wildcard would answer on every interface, including production.
var unsafeBindAddresses = map[string]bool{
	"":                true,
	"0.0.0.0":         true,
	"::":              true,
	"[::]":            true,
	"255.255.255.255": true,
}

// isUnsafeBind reports whether addr is a wildcard/broadcast address that must
// not be used to bind TFTP or DHCP.
func isUnsafeBind(addr string) bool {
	return unsafeBindAddresses[strings.TrimSpace(addr)]
}

// ValidateTFTP checks the TFTP configuration. It requires a specific bind
// address (never 0.0.0.0), a valid port, and an absolute root directory.
func (c TFTPConfig) Validate() error {
	c.BindAddress = strings.TrimSpace(c.BindAddress)
	if isUnsafeBind(c.BindAddress) {
		return fmt.Errorf("tftp.bind_address must be a specific IP (never 0.0.0.0 or empty)")
	}
	if net.ParseIP(c.BindAddress) == nil {
		return fmt.Errorf("tftp.bind_address %q is not a valid IP", c.BindAddress)
	}
	if c.BindPort == 0 {
		c.BindPort = defaultTFTPPort
	}
	if c.BindPort < 1 || c.BindPort > 65535 {
		return fmt.Errorf("tftp.bind_port %d out of range", c.BindPort)
	}
	if strings.TrimSpace(c.Root) == "" {
		return fmt.Errorf("tftp.root is required when TFTP is enabled")
	}
	return nil
}

// ValidateDHCP checks the DHCP configuration for the isolated lab. Every
// field is validated so a misconfiguration fails before any socket is opened.
func (c DHCPConfig) Validate() error {
	c.BindAddress = strings.TrimSpace(c.BindAddress)
	if isUnsafeBind(c.BindAddress) {
		return fmt.Errorf("dhcp.bind_address must be a specific lab IP (never 0.0.0.0 or empty); refusing to risk answering on a production interface")
	}
	if net.ParseIP(c.BindAddress) == nil {
		return fmt.Errorf("dhcp.bind_address %q is not a valid IP", c.BindAddress)
	}
	if c.BindPort == 0 {
		c.BindPort = defaultDHCPPort
	}
	if c.BindPort < 1 || c.BindPort > 65535 {
		return fmt.Errorf("dhcp.bind_port %d out of range", c.BindPort)
	}
	// Subnet must be a valid CIDR.
	_, ipnet, err := net.ParseCIDR(strings.TrimSpace(c.Subnet))
	if err != nil {
		return fmt.Errorf("dhcp.subnet %q is not valid CIDR: %w", c.Subnet, err)
	}
	start := net.ParseIP(strings.TrimSpace(c.RangeStart))
	end := net.ParseIP(strings.TrimSpace(c.RangeEnd))
	if start == nil || end == nil {
		return fmt.Errorf("dhcp.range_start/range_end must be valid IPs")
	}
	if !ipnet.Contains(start) || !ipnet.Contains(end) {
		return fmt.Errorf("dhcp range %s-%s is outside subnet %s", c.RangeStart, c.RangeEnd, c.Subnet)
	}
	if compareIP(start, end) > 0 {
		return fmt.Errorf("dhcp.range_start %s is after range_end %s", c.RangeStart, c.RangeEnd)
	}
	if c.LeaseSeconds <= 0 || c.LeaseSeconds > 7*24*3600 {
		return fmt.Errorf("dhcp.lease_seconds must be between 1 and 604800")
	}
	if gw := strings.TrimSpace(c.Gateway); gw != "" && net.ParseIP(gw) == nil {
		return fmt.Errorf("dhcp.gateway %q is not a valid IP", gw)
	}
	if dns := strings.TrimSpace(c.DNSServer); dns != "" && net.ParseIP(dns) == nil {
		return fmt.Errorf("dhcp.dns_server %q is not a valid IP", dns)
	}
	ns := net.ParseIP(strings.TrimSpace(c.NextServer))
	if ns == nil {
		return fmt.Errorf("dhcp.next_server must be a valid IP (the TFTP server)")
	}
	if !ipnet.Contains(ns) {
		return fmt.Errorf("dhcp.next_server %s is outside subnet %s", c.NextServer, c.Subnet)
	}
	// Boot filenames must be simple file names (no paths, no traversal).
	if err := validateBootFilename(c.BootFileUEFI); err != nil {
		return fmt.Errorf("dhcp.boot_file_uefi: %w", err)
	}
	if err := validateBootFilename(c.BootFileBIOS); err != nil {
		return fmt.Errorf("dhcp.boot_file_bios: %w", err)
	}
	return nil
}

// validateBootFilename ensures a boot filename is a plain file name with no
// path separators or traversal.
func validateBootFilename(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("boot filename is required")
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return fmt.Errorf("boot filename %q must be a plain file name (no paths)", name)
	}
	return nil
}

// compareIP returns -1, 0, or 1 comparing two IPv4 addresses numerically.
func compareIP(a, b net.IP) int {
	a4, b4 := a.To4(), b.To4()
	if a4 == nil || b4 == nil {
		return 0
	}
	for i := 0; i < 4; i++ {
		if a4[i] != b4[i] {
			if a4[i] < b4[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}
