package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// goSourceFiles returns every Go source file of the launcher package.
func goSourceFiles(t *testing.T) []string {
	t.Helper()
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no Go source files found")
	}
	return matches
}

// readSources loads all launcher source code for static security checks.
func readSources(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, f := range goSourceFiles(t) {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		out[f] = string(data)
	}
	return out
}

// frag builds a banned token from concatenated pieces so that this test file
// itself never contains the literal token it scans for.
func frag(parts ...string) string { return strings.Join(parts, "") }

// TestNoShellSSHOrProcessExecution guarantees the launcher never launches
// shells, SSH sessions or arbitrary processes. Browser opening uses
// ShellExecuteW directly - never the command shell or protocol-handler
// helper binaries.
func TestNoShellSSHOrProcessExecution(t *testing.T) {
	banned := []string{
		frag("os", "/", "exec"),
		frag("exec", ".", "Command"),
		frag("cmd", ".", "exe"),
		frag("run", "dll32"),
		frag("power", "shell"),
		frag("cscr", "ipt"),
		frag("wscr", "ipt"),
		frag("Start", "-Process"),
		frag("ss", "hpass"),
		frag("plin", "k"),
		frag("ps", "exec"),
		frag("os", ".", "execute"),
		frag("io", ".", "popen"),
		frag("Create", "Process"),
		frag("cmd", `\", \"/c`),
	}
	for name, src := range readSources(t) {
		for _, b := range banned {
			if strings.Contains(src, b) {
				t.Errorf("%s contains banned token %q", name, b)
			}
		}
	}
}

// TestNoInsecureTLSCertBypass guarantees certificate verification is never
// disabled anywhere in the launcher.
func TestNoInsecureTLSCertBypass(t *testing.T) {
	banned := []string{
		frag("Insecure", "SkipVerify"),
		frag("tls", ".", "Config{"),
		frag("Verify", "PeerCertificate"),
		frag("x509", ".", "CertPool{}", ";"),
	}
	for name, src := range readSources(t) {
		for _, b := range banned {
			if strings.Contains(src, b) {
				t.Errorf("%s contains banned TLS bypass pattern %q", name, b)
			}
		}
	}
}

// TestNoHardcodedCredentials uses a strict assignment-pattern scan: no
// password/secret/token/api-key variable may be initialized from a string
// literal anywhere in the launcher source.
func TestNoHardcodedCredentials(t *testing.T) {
	pattern := regexp.MustCompile(`(?i)(password|passwd|secret|token|api[-_]?key)\s*[:=]\s*"[^"\\]+"`)
	for name, src := range readSources(t) {
		if m := pattern.FindString(src); m != "" {
			t.Errorf("%s appears to hardcode a credential: %s", name, m)
		}
	}
}

// TestNoSecretsEverLogged guarantees there is no logging/printing facility in
// the launcher at all, which trivially satisfies "never log passwords,
// cookies or authorization headers".
func TestNoSecretsEverLogged(t *testing.T) {
	banned := []string{
		frag("fmt", ".", "Print"),
		frag("log", ".", "Print"),
		frag("log", ".", "Fatal"),
		frag("log", ".", "Println"),
		frag("println", "("),
		frag("Authoriz", "ation:"),
		frag("Cookie", `: `),
	}
	for name, src := range readSources(t) {
		for _, b := range banned {
			if strings.Contains(src, b) {
				t.Errorf("%s contains logging/sensitive-header token %q", name, b)
			}
		}
	}
}

// TestNoArbitraryRemoteExecutionPrimitives guarantees no SSH client, remote
// shell or arbitrary command channel exists in the launcher.
func TestNoArbitraryRemoteExecutionPrimitives(t *testing.T) {
	banned := []string{
		frag("ssh", "-", "client"),
		frag("glider", "labs"),
		frag("mitchellh/go", "-ssh"),
		frag("terminal", ".", "Session"),
		frag("CombinedOutput", "("),
		frag("CommandContext", "("),
	}
	for name, src := range readSources(t) {
		for _, b := range banned {
			if strings.Contains(src, b) {
				t.Errorf("%s contains remote-exec primitive %q", name, b)
			}
		}
	}
}
