package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// Config is the persisted server configuration.
//
// It intentionally stores NO secrets. The administrator token is provisioned
// separately (see admin.go) and never written into config.json.
type Config struct {
	// BindAddress is the address the HTTP server listens on. The safe
	// default is 127.0.0.1 so the administrative API is never exposed to
	// the LAN unless an operator explicitly configures it.
	BindAddress string `json:"bind_address"`
	// BindPort is the TCP port. Default 8888 to match the existing MKYBOOT.
	BindPort int `json:"bind_port"`
	// LogDir is where the structured log file is written. Empty means the
	// data root.
	LogDir string `json:"log_dir,omitempty"`
	// PXE holds the TFTP and DHCP settings for network boot delivery. Both
	// default to disabled so nothing binds a network port unless an operator
	// explicitly enables it for an isolated lab.
	PXE PXEConfig `json:"pxe"`
	// Storage holds the image storage settings. The storage root defaults to
	// <data-root>/images and is created with restrictive permissions.
	Storage StorageConfig `json:"storage"`
}

// StorageConfig configures the image storage subsystem. The root directory
// contains base images and per-client overlay reserves. The VHDX backend is
// auto-detected at startup (DiskPart on Windows); capabilities are reported
// via /api/storage/health.
type StorageConfig struct {
	Enabled bool   `json:"enabled"` // whether the storage subsystem is active
	Root    string `json:"root"`    // image storage root; defaults to <data-root>/images
}

// PXEConfig groups the network-boot services. Everything is opt-in.
type PXEConfig struct {
	TFTP TFTPConfig `json:"tftp"`
	DHCP DHCPConfig `json:"dhcp"`
}

// TFTPConfig configures the built-in TFTP server (RFC 1350).
type TFTPConfig struct {
	Enabled     bool   `json:"enabled"`
	BindAddress string `json:"bind_address"` // specific IP required; never 0.0.0.0
	BindPort    int    `json:"bind_port"`    // default 69
	Root        string `json:"root"`         // TFTP root directory (files served from here)
}

// DHCPConfig configures the isolated-network DHCP offer logic. It is
// deliberately explicit: every field must be set for a safe lab, and the
// bind address must be a specific unicast IP (never 0.0.0.0), so the server
// can never answer on a production interface by accident.
type DHCPConfig struct {
	Enabled      bool   `json:"enabled"`
	BindAddress  string `json:"bind_address"` // MUST be a specific lab IP, never 0.0.0.0
	BindPort     int    `json:"bind_port"`    // default 67
	Subnet       string `json:"subnet"`       // CIDR, e.g. 198.51.100.0/24
	RangeStart   string `json:"range_start"`  // first offerable address
	RangeEnd     string `json:"range_end"`    // last offerable address
	LeaseSeconds int    `json:"lease_seconds"`
	Gateway      string `json:"gateway"`
	DNSServer    string `json:"dns_server"`
	NextServer   string `json:"next_server"` // TFTP server IP (siaddr / option 66)
	// BootFileUEFI is served to UEFI clients (must be the 64-bit ipxe.efi).
	BootFileUEFI string `json:"boot_file_uefi"`
	// BootFileBIOS is served to legacy BIOS clients (ipxe.kpxe).
	BootFileBIOS string `json:"boot_file_bios"`
}

const (
	defaultBindAddress = "127.0.0.1"
	defaultBindPort    = 8888
	minPort            = 1
	maxPort            = 65535
	maxBindAddressLen  = 253
)

// DefaultConfig returns the safe out-of-the-box configuration: localhost only.
func DefaultConfig() Config {
	return Config{BindAddress: defaultBindAddress, BindPort: defaultBindPort}
}

// Validate checks the configuration and normalizes trivial omissions.
// It refuses empty/invalid addresses and out-of-range ports so a misconfigured
// file fails fast at startup with an actionable error rather than binding
// something unintended.
func (c *Config) Validate() error {
	c.BindAddress = strings.TrimSpace(c.BindAddress)
	if c.BindAddress == "" {
		c.BindAddress = defaultBindAddress
	}
	if len(c.BindAddress) > maxBindAddressLen {
		return fmt.Errorf("bind_address too long (%d > %d)", len(c.BindAddress), maxBindAddressLen)
	}
	// Accept an IP literal or a resolvable host name. Loopback is fine; the
	// operator opts into any wider exposure by configuration, never by default.
	if net.ParseIP(c.BindAddress) == nil {
		if _, err := net.LookupHost(c.BindAddress); err != nil {
			return fmt.Errorf("bind_address %q is not a valid IP or resolvable host", c.BindAddress)
		}
	}
	if c.BindPort == 0 {
		c.BindPort = defaultBindPort
	}
	if c.BindPort < minPort || c.BindPort > maxPort {
		return fmt.Errorf("bind_port %d out of range %d-%d", c.BindPort, minPort, maxPort)
	}
	return nil
}

// Addr returns the host:port listen address.
func (c Config) Addr() string { return net.JoinHostPort(c.BindAddress, strconv.Itoa(c.BindPort)) }

// LoadConfig reads config from path. A missing file yields defaults with no
// error; a corrupt or invalid file yields defaults together with an error the
// caller surfaces as an actionable startup notice.
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DefaultConfig(), nil
		}
		return DefaultConfig(), fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return DefaultConfig(), fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return DefaultConfig(), fmt.Errorf("invalid config %s: %w", path, err)
	}
	return cfg, nil
}

// Save persists the configuration atomically with 0600 permissions after
// validating it.
func (c Config) Save(path string) error {
	if err := c.Validate(); err != nil {
		return fmt.Errorf("refusing to save invalid config: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(path, append(data, '\n'), 0o600)
}

// generateToken returns a cryptographically random hex token of n bytes.
func generateToken(n int) (string, error) {
	if n < 16 {
		return "", errors.New("token entropy too small")
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// constantTimeEqual compares two tokens without early exit, so an attacker
// cannot learn a valid prefix from timing differences.
func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
