package main

import (
	"bufio"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// iSCSITargetBackend is the interface for managing iSCSITargetBackend targets and LUNs.
// The actual iSCSI target runs on a separate Linux VM (e.g. LIO/targetcli
// on Ubuntu). This interface abstracts the management protocol used to
// control that remote target. All paths must be pre-validated and absolute.
// Backend implementations must never interpolate untrusted values into
// executable command text.
type iSCSITargetBackend interface {
	// Name returns a human-readable backend name (e.g. "linux-lio").
	Name() string
	// IsAvailable reports whether the backend is usable on this system.
	IsAvailable() bool
	// CreateLUN creates a new block-backed LUN file at path with sizeMB
	// capacity. The file must not already exist.
	CreateLUN(path string, sizeMB int64) error
	// RemoveLUN deletes the LUN file at path. The LUN must be unexported
	// first.
	RemoveLUN(path string) error
	// ExportLUN exports the LUN at path as an iSCSI target with the given
	// IQN. chapUser/chapSecret may be empty to disable CHAP authentication.
	ExportLUN(iqn, path string, sizeMB int64, chapUser, chapSecret string) error
	// UnexportLUN removes the iSCSI export for the given IQN. The LUN file
	// is not deleted.
	UnexportLUN(iqn string) error
	// ListTargets returns the currently exported iSCSI targets.
	ListTargets() ([]iSCSITargetInfo, error)
	// SetACL restricts access to the given IQN to the specified initiator
	// IQNs. An empty list allows all initiators.
	SetACL(iqn string, initiatorIQNs []string) error
	// VerifyConnection attempts a loopback iSCSI connection to confirm the
	// target is actually reachable and functional. Returns an error if the
	// target cannot be verified.
	VerifyConnection(iqn string) error
}

// iSCSITargetInfo describes an exported iSCSI target.
type iSCSITargetInfo struct {
	IQN         string   `json:"iqn"`
	LUNPath     string   `json:"lun_path"`
	SizeBytes   int64    `json:"size_bytes"`
	Exported    bool     `json:"exported"`
	Initiators  []string `json:"initiators"`
	CHAPEnabled bool     `json:"chap_enabled"`
	Verified    bool     `json:"verified"`
}

// iSCSIHealthState reports the verification state of the iSCSI target.
type iSCSIHealthState int

const (
	// iSCSIStateUnavailable means no iSCSI target backend is available.
	iSCSIStateUnavailable iSCSIHealthState = iota
	// iSCSIStateConfigured means a backend is present but not yet verified.
	iSCSIStateConfigured
	// iSCSIStateVerified means a loopback connection to the target succeeded.
	iSCSIStateVerified
)

func (s iSCSIHealthState) String() string {
	switch s {
	case iSCSIStateUnavailable:
		return "unavailable"
	case iSCSIStateConfigured:
		return "configured"
	case iSCSIStateVerified:
		return "verified"
	default:
		return "unknown"
	}
}

// ErrBackendUnavailable is returned when no iSCSI target backend is available.
var ErriSCSIBackendUnavailable = errors.New("no iSCSI target backend available")

// validIQNRE matches a valid iSCSI IQN: iqn.yyyy-mm.naming-authority followed
// by a restricted character set. The character set is deliberately narrow:
// the IQN is used verbatim as the LIO target name, so it must not be able to
// carry shell metacharacters or path separators into a remote command line.
var validIQNRE = regexp.MustCompile(`^iqn\.\d{4}-\d{2}\.[A-Za-z0-9][A-Za-z0-9._:-]{0,240}$`)

// validChapUserRE matches a CHAP user name that is safe to use as a bare
// targetcli ACL name and inside an object path.
var validChapUserRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// validSSHHostRE matches a host name or IP literal usable as an SSH target.
// IPv6 literals may be bracketed.
var validSSHHostRE = regexp.MustCompile(`^[A-Za-z0-9\[][A-Za-z0-9._:\[\]-]{0,253}$`)

// validSSHUserRE matches an SSH user name safe to use as a bare argument.
var validSSHUserRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)

// validateIQN checks that an iSCSI IQN is well-formed and safe to use as a
// remote object name.
func validateIQN(iqn string) error {
	if iqn == "" {
		return errors.New("iSCSI IQN is required")
	}
	if len(iqn) > 255 {
		return fmt.Errorf("iSCSI IQN too long (%d > 255)", len(iqn))
	}
	if !validIQNRE.MatchString(iqn) {
		return fmt.Errorf("iSCSI IQN %q must match iqn.yyyy-mm.naming-authority", iqn)
	}
	return nil
}

// validChapUser reports whether the CHAP user name is safe to use as a
// targetcli ACL name.
func validChapUser(user string) bool {
	return validChapUserRE.MatchString(user)
}

// validSSHHost reports whether the host is safe to use as an SSH target.
func validSSHHost(host string) bool {
	return validSSHHostRE.MatchString(host)
}

// validSSHUser reports whether the user name is safe to use as an SSH user.
func validSSHUser(user string) bool {
	return validSSHUserRE.MatchString(user)
}

// shellQuote wraps s in single quotes so it is passed to the remote shell as
// exactly one word. Embedded single quotes are escaped, so the result is
// safe for any input.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// targetcliFailed reports whether targetcli output indicates an error.
func targetcliFailed(out string) bool {
	return strings.Contains(strings.ToLower(out), "error")
}

// validateLinuxPath checks that a Linux filesystem path is absolute and does
// not contain traversal sequences or shell metacharacters.
func validateLinuxPath(p string) bool {
	if p == "" || p[0] != '/' {
		return false
	}
	if strings.Contains(p, "..") {
		return false
	}
	if strings.ContainsAny(p, "|&;$(){}[]<>\"'`\\") {
		return false
	}
	return true
}

// safeTargetName derives a safe target/LUN name from an IQN or path by
// replacing non-alphanumeric characters with underscores. It never includes
// slashes or other shell metacharacters.
func safeTargetName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	name := b.String()
	if name == "" {
		name = "target"
	}
	if len(name) > 64 {
		name = name[:64]
	}
	return name
}

// parseTargetcliTargets parses targetcli "ls" output into target info.
func parseTargetcliTargets(out string) []iSCSITargetInfo {
	var targets []iSCSITargetInfo
	re := regexp.MustCompile(`(?i)iqn\.[\w.:-]+`)
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		m := re.FindString(line)
		if m != "" {
			targets = append(targets, iSCSITargetInfo{
				IQN:      m,
				Exported: true,
			})
		}
	}
	return targets
}

// parseTargetcliACLs parses targetcli ACL listing output and returns the
// initiator IQNs referenced by the ACLs.
func parseTargetcliACLs(out string) []string {
	var acls []string
	re := regexp.MustCompile(`(?i)iqn\.[\w.:-]+`)
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		m := re.FindString(line)
		if m != "" {
			acls = append(acls, m)
		}
	}
	return acls
}

// parseTargetcliACLNames extracts the ACL object names from targetcli output.
//
// targetcli indents child objects under their parent path, so an ACL name is
// an indented entry that is neither an attribute assignment ("key=value") nor
// an object link ("name -> /path").
func parseTargetcliACLNames(out string) []string {
	var names []string
	seen := map[string]bool{}
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasSuffix(trimmed, ":") {
			continue
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			continue
		}
		if strings.Contains(trimmed, "=") || strings.Contains(trimmed, "->") {
			continue
		}
		if !seen[trimmed] {
			seen[trimmed] = true
			names = append(names, trimmed)
		}
	}
	return names
}

// DetectiSCSIBackend returns the best available iSCSITargetBackend for this
// platform, configured from cfg. On Linux it returns a linuxLIOBackend;
// on other platforms it returns an unavailableiSCSIBackend. The returned
// backend is not available until a remote Linux VM is configured and
// reachable via SSH; IsAvailable reports false until then.
func DetectiSCSIBackend(cfg ISCSIConfig) (iSCSITargetBackend, error) {
	return detectiSCSIBackend(cfg)
}
