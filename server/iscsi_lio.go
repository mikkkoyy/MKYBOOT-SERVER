package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// linuxLIOBackend manages iSCSI targets on a remote Linux VM that runs
// LIO/targetcli. Every operation is a single, predefined targetcli command
// sequence issued over a non-interactive SSH session.
//
// The backend is host-platform independent: it never touches LIO locally, it
// only drives the remote target over SSH. The Windows-native MKYBOOT server
// and a Linux server therefore use exactly the same code path.
//
// Security model:
//   - No request-supplied value is ever interpolated into executable command
//     text. IQNs, LUN paths and CHAP user names are validated before they
//     reach a command line; only values that pass validation are used.
//   - The SSH session is non-interactive (BatchMode), does not forward the
//     agent or X11, and verifies the host key strictly.
//   - The CHAP secret is written to the remote shell's standard input and
//     read into a variable there, so it never appears in the remote process
//     arguments. It is never logged.
type linuxLIOBackend struct {
	host       string
	sshPort    int
	sshUser    string
	sshKey     string // path to private key; secret, never logged
	timeout    time.Duration
	configured bool
}

const (
	defaultSSHPort    = 22
	defaultSSHTimeout = 30 * time.Second
	targetcliCmd      = "targetcli"
	iscsiadmCmd       = "iscsiadm"
	// chapSecretVar is the shell variable that carries the CHAP secret on the
	// remote side. The secret is read from stdin into this variable, so it
	// never appears in the remote process arguments.
	chapSecretVar = "__mkyboot_chap_secret"
	// chapSecretRef is the literal text substituted for the secret when it is
	// used as a command word. The remote shell expands it to a single word.
	chapSecretRef = `"$` + chapSecretVar + `"`
	// loopbackPortal is the portal used for the loopback iSCSI discovery that
	// proves the target is actually reachable.
	loopbackPortal = "127.0.0.1:3260"
)

// newLinuxLIOBackend creates a linuxLIOBackend configured to manage a remote
// Linux VM running targetcli/LIO over SSH. The backend stays unavailable when
// the configuration is incomplete or invalid.
func newLinuxLIOBackend(host, user, keyPath string, port int) *linuxLIOBackend {
	if port <= 0 {
		port = defaultSSHPort
	}
	host = strings.TrimSpace(host)
	user = strings.TrimSpace(user)
	keyPath = strings.TrimSpace(keyPath)
	return &linuxLIOBackend{
		host:       host,
		sshPort:    port,
		sshUser:    user,
		sshKey:     keyPath,
		timeout:    defaultSSHTimeout,
		configured: validSSHHost(host) && validSSHUser(user) && keyPath != "",
	}
}

// Name returns the backend name.
func (b *linuxLIOBackend) Name() string { return "linux-lio" }

// IsAvailable reports whether the backend is fully configured and the local
// ssh client is resolvable. It does NOT prove that the remote target is
// reachable; that requires a successful VerifyConnection call.
func (b *linuxLIOBackend) IsAvailable() bool {
	if !b.configured {
		return false
	}
	_, err := exec.LookPath("ssh")
	return err == nil
}

// sshArgs builds the base ssh argument list with security-hardening options:
// no password authentication, no agent forwarding, no X11 forwarding, strict
// host key checking, and bounded connection timeouts.
func (b *linuxLIOBackend) sshArgs() []string {
	return []string{
		"-o", "BatchMode=yes",
		"-o", "PasswordAuthentication=no",
		"-o", "ForwardAgent=no",
		"-o", "ForwardX11=no",
		"-o", "StrictHostKeyChecking=yes",
		"-o", fmt.Sprintf("ConnectTimeout=%d", int(b.timeout.Seconds())),
		"-p", fmt.Sprintf("%d", b.sshPort),
		"-i", b.sshKey,
		fmt.Sprintf("%s@%s", b.sshUser, b.host),
	}
}

// runSSH executes a single command on the remote host via ssh and returns its
// combined stdout/stderr. The command slice contains only validated,
// server-controlled values.
func (b *linuxLIOBackend) runSSH(cmd []string) (string, error) {
	return b.runSSHWithInput(cmd, "")
}

// runSSHWithInput executes a single command on the remote host via ssh and
// feeds input to the remote command's standard input.
func (b *linuxLIOBackend) runSSHWithInput(cmd []string, input string) (string, error) {
	args := append(b.sshArgs(), cmd...)
	ctx, cancel := context.WithTimeout(context.Background(), b.timeout)
	defer cancel()
	command := exec.CommandContext(ctx, "ssh", args...)
	if input != "" {
		command.Stdin = strings.NewReader(input)
	}
	out, err := command.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("ssh command failed: %w", err)
	}
	return string(out), nil
}

// targetcli runs targetcli on the remote host with the given sub-commands.
func (b *linuxLIOBackend) targetcli(subCmds ...string) (string, error) {
	cmd := append([]string{targetcliCmd}, subCmds...)
	return b.runSSH(cmd)
}

// targetcliWithSecret runs targetcli on the remote host while supplying the
// CHAP secret on standard input. The remote shell reads the secret into a
// variable, so the secret is never part of the remote process arguments.
func (b *linuxLIOBackend) targetcliWithSecret(secret string, subCmds ...string) (string, error) {
	if secret == "" {
		return b.targetcli(subCmds...)
	}
	if strings.ContainsAny(secret, "\r\n") {
		return "", errors.New("chap secret must not contain line breaks")
	}
	cmd := []string{"read", "-r", chapSecretVar, ";", targetcliCmd}
	cmd = append(cmd, subCmds...)
	cmd = append(cmd, "exit")
	return b.runSSHWithInput(cmd, secret+"\n")
}

// CreateLUN creates a new block-backed LUN file at path with sizeMB capacity
// on the remote host using targetcli.
func (b *linuxLIOBackend) CreateLUN(path string, sizeMB int64) error {
	if !validateLinuxPath(path) {
		return fmt.Errorf("invalid LUN path: %q", path)
	}
	if sizeMB <= 0 {
		return fmt.Errorf("size_mb must be positive, got %d", sizeMB)
	}
	name := safeTargetName(path)
	// The backing store directory must exist before targetcli creates the
	// file. The path is already validated and shell-quoted here.
	if out, err := b.runSSH([]string{"mkdir", "-p", "--", shellQuote(filepath.Dir(path))}); err != nil {
		return fmt.Errorf("create LUN directory: %w (%s)", err, strings.TrimSpace(out))
	}
	out, err := b.targetcli(
		"/backstores/fileio", "create", name, shellQuote(path), fmt.Sprintf("%dMB", sizeMB),
		"exit",
	)
	if err != nil {
		return fmt.Errorf("create LUN: %w", err)
	}
	if targetcliFailed(out) {
		return fmt.Errorf("create LUN failed: %s", strings.TrimSpace(out))
	}
	return nil
}

// RemoveLUN removes the LUN backing store at path. The LUN must be
// unexported first (targetcli enforces this).
func (b *linuxLIOBackend) RemoveLUN(path string) error {
	if !validateLinuxPath(path) {
		return fmt.Errorf("invalid LUN path: %q", path)
	}
	name := safeTargetName(path)
	out, err := b.targetcli(
		"/backstores/fileio", "delete", name,
		"exit",
	)
	if err != nil {
		return fmt.Errorf("remove LUN: %w", err)
	}
	if targetcliFailed(out) {
		return fmt.Errorf("remove LUN failed: %s", strings.TrimSpace(out))
	}
	return nil
}

// ExportLUN exports the LUN at path as an iSCSI target with the given IQN.
// chapUser/chapSecret may be empty to disable CHAP authentication.
func (b *linuxLIOBackend) ExportLUN(iqn, path string, sizeMB int64, chapUser, chapSecret string) error {
	if err := validateIQN(iqn); err != nil {
		return err
	}
	if !validateLinuxPath(path) {
		return fmt.Errorf("invalid LUN path: %q", path)
	}
	if sizeMB <= 0 {
		return fmt.Errorf("size_mb must be positive, got %d", sizeMB)
	}
	if chapUser != "" && !validChapUser(chapUser) {
		return fmt.Errorf("invalid chap_user %q", chapUser)
	}
	if chapSecret != "" && chapUser == "" {
		return errors.New("chap_secret requires chap_user")
	}
	if chapSecret != "" && (len(chapSecret) < 12 || len(chapSecret) > 64) {
		return fmt.Errorf("chap_secret must be 12-64 characters, got %d", len(chapSecret))
	}

	// The LIO target name IS the IQN, so the validated IQN is used verbatim
	// as the targetcli object name. This keeps the exported IQN identical to
	// the IQN the client is told to use.
	targetName := iqn
	name := safeTargetName(path)

	out, err := b.targetcli("/iscsi", "create", targetName, "exit")
	if err != nil {
		return fmt.Errorf("create iSCSI target: %w", err)
	}
	if targetcliFailed(out) {
		return fmt.Errorf("create iSCSI target failed: %s", strings.TrimSpace(out))
	}

	out, err = b.targetcli(
		fmt.Sprintf("/iscsi/%s/tpg1/luns", targetName), "create", fmt.Sprintf("/backstores/fileio/%s", name),
		"exit",
	)
	if err != nil {
		return fmt.Errorf("create LUN mapping: %w", err)
	}
	if targetcliFailed(out) {
		return fmt.Errorf("create LUN mapping failed: %s", strings.TrimSpace(out))
	}

	out, err = b.targetcli(
		fmt.Sprintf("/iscsi/%s", targetName), "set", "attribute", "enable=1",
		"exit",
	)
	if err != nil {
		return fmt.Errorf("enable iSCSI target: %w", err)
	}
	if targetcliFailed(out) {
		return fmt.Errorf("enable iSCSI target failed: %s", strings.TrimSpace(out))
	}

	if chapUser == "" {
		return nil
	}

	// CHAP: enable authentication on the TPG, create the ACL and bind the
	// credential to it.
	out, err = b.targetcli(
		fmt.Sprintf("/iscsi/%s/tpg1", targetName), "set", "attribute", "authentication=1",
		"exit",
	)
	if err != nil {
		return fmt.Errorf("enable CHAP authentication: %w", err)
	}
	if targetcliFailed(out) {
		return fmt.Errorf("enable CHAP authentication failed: %s", strings.TrimSpace(out))
	}

	out, err = b.targetcli(
		fmt.Sprintf("/iscsi/%s/tpg1/acls", targetName), "create", chapUser,
		"exit",
	)
	if err != nil {
		return fmt.Errorf("create CHAP ACL: %w", err)
	}
	if targetcliFailed(out) {
		return fmt.Errorf("create CHAP ACL failed: %s", strings.TrimSpace(out))
	}

	out, err = b.targetcli(
		fmt.Sprintf("/iscsi/%s/tpg1/acls/%s", targetName, chapUser), "set", "auth_username", chapUser,
		"exit",
	)
	if err != nil {
		return fmt.Errorf("set CHAP auth_username: %w", err)
	}
	if targetcliFailed(out) {
		return fmt.Errorf("set CHAP auth_username failed: %s", strings.TrimSpace(out))
	}

	out, err = b.targetcliWithSecret(
		chapSecret,
		fmt.Sprintf("/iscsi/%s/tpg1/acls/%s", targetName, chapUser), "set", "auth_password", chapSecretRef,
		"exit",
	)
	if err != nil {
		return fmt.Errorf("set CHAP auth_password: %w", err)
	}
	if targetcliFailed(out) {
		return fmt.Errorf("set CHAP auth_password failed: %s", strings.TrimSpace(out))
	}
	return nil
}

// UnexportLUN removes the iSCSI export for the given IQN. The LUN file is
// not deleted.
func (b *linuxLIOBackend) UnexportLUN(iqn string) error {
	if err := validateIQN(iqn); err != nil {
		return err
	}
	out, err := b.targetcli("/iscsi", "delete", iqn, "exit")
	if err != nil {
		return fmt.Errorf("unexport target: %w", err)
	}
	if targetcliFailed(out) {
		return fmt.Errorf("unexport target failed: %s", strings.TrimSpace(out))
	}
	return nil
}

// ListTargets returns the currently exported iSCSI targets from targetcli.
func (b *linuxLIOBackend) ListTargets() ([]iSCSITargetInfo, error) {
	out, err := b.targetcli("/iscsi", "ls", "exit")
	if err != nil {
		return nil, fmt.Errorf("list targets: %w", err)
	}
	return parseTargetcliTargets(out), nil
}

// SetACL restricts access to the given IQN to the specified initiator IQNs.
// An empty list allows all initiators.
//
// In LIO an ACL is a named object and the initiator identity is carried by
// its auth_username attribute, so each initiator gets its own ACL whose
// auth_username is the initiator IQN.
func (b *linuxLIOBackend) SetACL(iqn string, initiatorIQNs []string) error {
	if err := validateIQN(iqn); err != nil {
		return err
	}
	targetName := iqn
	out, err := b.targetcli(
		fmt.Sprintf("/iscsi/%s/tpg1/acls", targetName), "ls",
		"exit",
	)
	if err != nil {
		return fmt.Errorf("list ACLs: %w", err)
	}
	for _, acl := range parseTargetcliACLNames(out) {
		if _, err := b.targetcli(
			fmt.Sprintf("/iscsi/%s/tpg1/acls", targetName), "delete", acl,
			"exit",
		); err != nil {
			return fmt.Errorf("delete ACL %q: %w", acl, err)
		}
	}
	for _, initiator := range initiatorIQNs {
		if err := validateIQN(initiator); err != nil {
			return fmt.Errorf("invalid initiator IQN %q: %w", initiator, err)
		}
		aclName := safeTargetName(initiator)
		out, err := b.targetcli(
			fmt.Sprintf("/iscsi/%s/tpg1/acls", targetName), "create", aclName,
			"exit",
		)
		if err != nil {
			return fmt.Errorf("create ACL %q: %w", initiator, err)
		}
		if targetcliFailed(out) {
			return fmt.Errorf("create ACL %q failed: %s", initiator, strings.TrimSpace(out))
		}
		out, err = b.targetcli(
			fmt.Sprintf("/iscsi/%s/tpg1/acls/%s", targetName, aclName), "set", "auth_username", initiator,
			"exit",
		)
		if err != nil {
			return fmt.Errorf("set ACL %q auth_username: %w", initiator, err)
		}
		if targetcliFailed(out) {
			return fmt.Errorf("set ACL %q auth_username failed: %s", initiator, strings.TrimSpace(out))
		}
	}
	return nil
}

// VerifyConnection confirms that the target is exported, that its TPG is
// enabled, that a LUN is mapped, and that the target is discoverable through
// a loopback iSCSI discovery from the target host itself.
//
// The loopback discovery is the real proof of reachability: it exercises the
// iSCSI portal rather than only the management configuration.
func (b *linuxLIOBackend) VerifyConnection(iqn string) error {
	if err := validateIQN(iqn); err != nil {
		return err
	}
	targetName := iqn

	out, err := b.targetcli(
		fmt.Sprintf("/iscsi/%s", targetName), "ls",
		"exit",
	)
	if err != nil {
		return fmt.Errorf("verify target: %w", err)
	}
	if targetcliFailed(out) {
		return fmt.Errorf("verify target failed: %s", strings.TrimSpace(out))
	}

	out, err = b.targetcli(
		fmt.Sprintf("/iscsi/%s/tpg1", targetName), "ls",
		"exit",
	)
	if err != nil {
		return fmt.Errorf("verify TPG: %w", err)
	}
	if targetcliFailed(out) {
		return fmt.Errorf("verify TPG failed: %s", strings.TrimSpace(out))
	}
	if !strings.Contains(out, "enabled") {
		return fmt.Errorf("target %q TPG is not enabled", iqn)
	}

	out, err = b.targetcli(
		fmt.Sprintf("/iscsi/%s/tpg1/luns", targetName), "ls",
		"exit",
	)
	if err != nil {
		return fmt.Errorf("verify LUN mapping: %w", err)
	}
	if targetcliFailed(out) {
		return fmt.Errorf("verify LUN mapping failed: %s", strings.TrimSpace(out))
	}
	if !strings.Contains(out, "lun") {
		return fmt.Errorf("target %q has no LUN mapped", iqn)
	}

	out, err = b.runSSH([]string{
		iscsiadmCmd, "--mode", "discovery", "--type", "sendtargets", "--portal", loopbackPortal,
	})
	if err != nil {
		return fmt.Errorf("loopback iSCSI discovery failed (is open-iscsi installed on the target host?): %w", err)
	}
	if !strings.Contains(out, iqn) {
		return fmt.Errorf("loopback iSCSI discovery did not return target %q", iqn)
	}
	return nil
}

// detectiSCSIBackend returns the iSCSI target backend for this host.
//
// The backend is only usable when iSCSI target management is explicitly
// enabled and the remote Linux VM is fully configured. Otherwise an
// unavailable backend is returned, so no management command can ever reach an
// unconfigured or unintended target.
func detectiSCSIBackend(cfg ISCSIConfig) (iSCSITargetBackend, error) {
	if !cfg.Enabled {
		return newUnavailableiSCSIBackend(), nil
	}
	host := strings.TrimSpace(cfg.Host)
	user := strings.TrimSpace(cfg.User)
	key := strings.TrimSpace(cfg.KeyPath)
	if host == "" || user == "" || key == "" {
		return newUnavailableiSCSIBackend(), errors.New("iSCSI target management is enabled but host, user and key_path must all be set")
	}
	backend := newLinuxLIOBackend(host, user, key, cfg.Port)
	if !backend.configured {
		return newUnavailableiSCSIBackend(), fmt.Errorf("iSCSI target configuration for %q is not usable", host)
	}
	return backend, nil
}
