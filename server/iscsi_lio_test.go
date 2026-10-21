package main

import (
	"strings"
	"testing"
)

// ---- Backend detection gating ----

func TestDetectISCSIBackendDisabled(t *testing.T) {
	backend, err := detectiSCSIBackend(ISCSIConfig{})
	if err != nil {
		t.Fatalf("detectiSCSIBackend(disabled) returned error: %v", err)
	}
	if backend == nil {
		t.Fatal("detectiSCSIBackend(disabled) returned nil backend")
	}
	if backend.IsAvailable() {
		t.Error("disabled iSCSI backend must not be available")
	}
	if backend.Name() != "unavailable" {
		t.Errorf("backend name = %q, want %q", backend.Name(), "unavailable")
	}
	if _, err := backend.ListTargets(); err == nil {
		t.Error("ListTargets on unavailable backend should fail")
	}
}

func TestDetectISCSIBackendIncompleteConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  ISCSIConfig
	}{
		{"missing host", ISCSIConfig{Enabled: true, User: "mkyboot", KeyPath: "C:\\key"}},
		{"missing user", ISCSIConfig{Enabled: true, Host: "192.168.56.10", KeyPath: "C:\\key"}},
		{"missing key", ISCSIConfig{Enabled: true, Host: "192.168.56.10", User: "mkyboot"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backend, err := detectiSCSIBackend(tc.cfg)
			if err == nil {
				t.Fatal("expected an error for an incomplete iSCSI configuration")
			}
			if backend == nil {
				t.Fatal("expected an unavailable backend, got nil")
			}
			if backend.IsAvailable() {
				t.Error("incompletely configured backend must not be available")
			}
		})
	}
}

func TestDetectISCSIBackendConfigured(t *testing.T) {
	backend, err := detectiSCSIBackend(ISCSIConfig{
		Enabled: true,
		Host:    "192.168.56.10",
		Port:    22,
		User:    "mkyboot",
		KeyPath: "C:\\keys\\mkyboot_ed25519",
	})
	if err != nil {
		t.Fatalf("detectiSCSIBackend(configured) returned error: %v", err)
	}
	if backend.Name() != "linux-lio" {
		t.Errorf("backend name = %q, want %q", backend.Name(), "linux-lio")
	}
	lio, ok := backend.(*linuxLIOBackend)
	if !ok {
		t.Fatalf("backend type = %T, want *linuxLIOBackend", backend)
	}
	if lio.host != "192.168.56.10" || lio.sshUser != "mkyboot" {
		t.Errorf("backend host/user = %q/%q, want 192.168.56.10/mkyboot", lio.host, lio.sshUser)
	}
	if lio.sshPort != 22 {
		t.Errorf("ssh port = %d, want 22", lio.sshPort)
	}
}

func TestDetectISCSIBackendRejectsUnsafeHost(t *testing.T) {
	backend, err := detectiSCSIBackend(ISCSIConfig{
		Enabled: true,
		Host:    "192.168.56.10; touch /tmp/pwned",
		User:    "mkyboot",
		KeyPath: "C:\\key",
	})
	if err == nil {
		t.Fatal("expected an error for an unsafe host")
	}
	if backend.IsAvailable() {
		t.Error("unsafe host must not produce an available backend")
	}
}

// ---- SSH hardening ----

func TestLinuxLIOBackendSSHArgs(t *testing.T) {
	b := newLinuxLIOBackend("192.168.56.10", "mkyboot", "C:\\keys\\id_ed25519", 2222)
	args := strings.Join(b.sshArgs(), " ")
	for _, want := range []string{
		"BatchMode=yes",
		"PasswordAuthentication=no",
		"ForwardAgent=no",
		"ForwardX11=no",
		"StrictHostKeyChecking=yes",
		"ConnectTimeout=",
		"-p 2222",
		"mkyboot@192.168.56.10",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("ssh args %q missing %q", args, want)
		}
	}
}

func TestLinuxLIOBackendDefaultPort(t *testing.T) {
	b := newLinuxLIOBackend("192.168.56.10", "mkyboot", "C:\\key", 0)
	if b.sshPort != defaultSSHPort {
		t.Errorf("ssh port = %d, want %d", b.sshPort, defaultSSHPort)
	}
}

// ---- Validation helpers ----

func TestValidChapUser(t *testing.T) {
	valid := []string{"mkyboot", "chap-user", "chap.user", "UPPER_case-1"}
	for _, u := range valid {
		if !validChapUser(u) {
			t.Errorf("validChapUser(%q) = false, want true", u)
		}
	}
	invalid := []string{"", "has space", "semi;colon", "pipe|user", "dollar$user", "back`tick", "quote'user", "slash/user", strings.Repeat("x", 65)}
	for _, u := range invalid {
		if validChapUser(u) {
			t.Errorf("validChapUser(%q) = true, want false", u)
		}
	}
}

func TestValidSSHHostAndUser(t *testing.T) {
	for _, h := range []string{"192.168.56.10", "ubuntu-target", "host.local", "[fe80::1]"} {
		if !validSSHHost(h) {
			t.Errorf("validSSHHost(%q) = false, want true", h)
		}
	}
	for _, h := range []string{"", "host;rm -rf /", "host with space", "host$(id)", "-host"} {
		if validSSHHost(h) {
			t.Errorf("validSSHHost(%q) = true, want false", h)
		}
	}
	for _, u := range []string{"mkyboot", "root", "user.name", "user-name"} {
		if !validSSHUser(u) {
			t.Errorf("validSSHUser(%q) = false, want true", u)
		}
	}
	for _, u := range []string{"", "user name", "user;id", "user$(id)", "user`id`", strings.Repeat("u", 33)} {
		if validSSHUser(u) {
			t.Errorf("validSSHUser(%q) = true, want false", u)
		}
	}
}

// TestValidateIQNRejectsShellMetacharacters is a regression guard: the IQN is
// used verbatim as the LIO target name inside a remote command line, so it
// must never be able to carry shell metacharacters or a path separator.
func TestValidateIQNRejectsShellMetacharacters(t *testing.T) {
	malicious := []string{
		"iqn.2026-10.com.example:disk1; touch /tmp/pwned",
		"iqn.2026-10.com.example:disk1 && id",
		"iqn.2026-10.com.example:disk1`id`",
		"iqn.2026-10.com.example:$(id)",
		"iqn.2026-10.com.example:disk1|id",
		"iqn.2026-10.com.example:disk1 > /tmp/out",
		"iqn.2026-10.com.example:disk1\nrm -rf /",
		"iqn.2026-10.com.example:../../etc/passwd",
	}
	for _, iqn := range malicious {
		if err := validateIQN(iqn); err == nil {
			t.Errorf("validateIQN(%q) = nil, want error", iqn)
		}
	}
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"/mnt/iscsi/lun1.img": "'/mnt/iscsi/lun1.img'",
		"with space":          "'with space'",
		"it's":                `'it'\''s'`,
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTargetcliFailed(t *testing.T) {
	if !targetcliFailed("Error: Target already exists") {
		t.Error("targetcliFailed should detect an error line")
	}
	if !targetcliFailed("ERROR: something") {
		t.Error("targetcliFailed should be case-insensitive")
	}
	if targetcliFailed("iqn.2026-10.com.example:disk1\n    tpg1") {
		t.Error("targetcliFailed should not flag a normal listing")
	}
}

// ---- targetcli output parsing ----

func TestParseTargetcliACLNames(t *testing.T) {
	out := `
/iscsi/iqn.2026-10.com.example:storage.disk1/tpg1/acls:
    iqn.2026-10.com.client:initiator1
    iqn.2026-10.com.client:initiator2
`
	names := parseTargetcliACLNames(out)
	if len(names) != 2 {
		t.Fatalf("expected 2 ACL names, got %d (%v)", len(names), names)
	}
	if names[0] != "iqn.2026-10.com.client:initiator1" {
		t.Errorf("names[0] = %q, want %q", names[0], "iqn.2026-10.com.client:initiator1")
	}
	if names[1] != "iqn.2026-10.com.client:initiator2" {
		t.Errorf("names[1] = %q, want %q", names[1], "iqn.2026-10.com.client:initiator2")
	}
}

func TestParseTargetcliACLNamesIgnoresAttributesAndLinks(t *testing.T) {
	out := `
/iscsi/iqn.2026-10.com.example:storage.disk1/tpg1/acls:
    acl_one
        auth_username=iqn.2026-10.com.client:initiator1
    acl_two
    lun0 -> /backstores/fileio/lun1
`
	names := parseTargetcliACLNames(out)
	if len(names) != 2 {
		t.Fatalf("expected 2 ACL names, got %d (%v)", len(names), names)
	}
	for _, n := range names {
		if strings.Contains(n, "=") || strings.Contains(n, "->") {
			t.Errorf("ACL name %q should not be an attribute or a link", n)
		}
	}
}

func TestParseTargetcliACLNamesEmpty(t *testing.T) {
	if names := parseTargetcliACLNames(""); len(names) != 0 {
		t.Errorf("expected 0 ACL names, got %d (%v)", len(names), names)
	}
}
